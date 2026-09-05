package agent

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/metrics"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

// RecoverySummary 是 dispatcher 本轮崩溃恢复的计数：待同步 Turn、实际补入 PENDING 的条数、为 queued Task 补出的首轮 Turn、以及因过期 lease 标 unknown 的 execution。
type RecoverySummary struct {
	PendingTurns       int  `json:"pending_turns"`        // 本轮选中的待同步 Turn 数
	EnqueuedTurns      int  `json:"enqueued_turns"`       // 实际补入 PENDING 的条数
	RecoveredTaskTurns int  `json:"recovered_task_turns"` // 为 queued Task 补出的首轮 Turn
	UnknownExecutions  int  `json:"unknown_executions"`   // 因过期 lease 标 unknown 的 execution
	HasMore            bool `json:"has_more"`             // 本轮批次已填满，下一轮继续探测
}

const expiredExecutionBatchLimit = 25

// RecoverUnfinished 是 dispatcher 崩溃恢复入口：用连接池构造 RecoveryService，再跑 recoverUnfinishedTurns。
//
// 过期 lease 先标 unknown（无法证明终态时），未完成 Turn 补回 ActorAgentTurnSync 的 PENDING。Pi session files 不是恢复权威。
func RecoverUnfinished(ctx context.Context, pool *pgxpool.Pool, limit int) (RecoverySummary, error) {
	gdb, err := pfdb.OpenGorm(pool)
	if err != nil {
		return RecoverySummary{}, err
	}
	if limit <= 0 {
		limit = expiredExecutionBatchLimit
	}
	return recoverUnfinishedTurns(ctx, RecoveryService(pool, gdb), limit)
}

// RecoverUnfinishedTurns 用调用方已构造的 Service 跑同一套崩溃恢复（测试与 dispatcher 共用）。
//
// Service 必须带 Graph/Product，才能在 lease 过期时 reconcile 副作用并按原幂等键重试。不要传入只读的空依赖。数据库失败返回 error。
func RecoverUnfinishedTurns(ctx context.Context, s Service) (RecoverySummary, error) {
	return recoverUnfinishedTurns(ctx, s, expiredExecutionBatchLimit)
}

// recoverUnfinishedTurns 分阶段执行 Agent recovery：每个阶段独立提交，避免把 projection、execution、task 和 outbox 锁在同一事务里。
//
// 扫描 queued/running/cancel_requested，以及已有答案的 requires_input。waiting_reason=goal_loop 的 Goal 不在这里造新 Turn。RestageIfIdle 只在 dispatch 空闲时写入 PENDING，已在飞的不重复入队。
//
// 任一阶段提交后进程退出，下一轮会按 PostgreSQL 状态继续恢复。事务提交后才 CompactExpiredTurnJournals。禁区：不要读 Pi 文件决定「该补哪条」；不要在恢复里把 Goal 标 succeeded。
func recoverUnfinishedTurns(ctx context.Context, s Service, limit int) (RecoverySummary, error) {
	if limit <= 0 {
		limit = expiredExecutionBatchLimit
	}
	var out RecoverySummary
	var batchErr error

	n, hasMore, err := recoverExpiredExecutions(ctx, s, limit)
	if err != nil {
		batchErr = errors.Join(batchErr, err)
	}
	out.UnknownExecutions = n
	out.HasMore = hasMore
	if n > 0 {
		metrics.AgentRecoveryUnknownExecutions.Add(int64(n))
	}

	recovered, hasMore, err := recoverQueuedTaskTurns(ctx, s, limit)
	if err != nil {
		batchErr = errors.Join(batchErr, err)
	}
	out.RecoveredTaskTurns = recovered
	out.HasMore = out.HasMore || hasMore

	enqueued, pending, hasMore, err := restagePendingTurns(ctx, s, limit)
	if err != nil {
		batchErr = errors.Join(batchErr, err)
	}
	out.EnqueuedTurns = enqueued
	out.PendingTurns = pending
	out.HasMore = out.HasMore || hasMore

	_, _ = CompactExpiredTurnJournals(ctx, s, time.Now().UTC())
	return out, batchErr
}

// recoverQueuedTaskTurns 给 status=queued 且 current_turn_id 为空的 Task 补首轮 Turn。
//
// 发现候选和逐条处理时跳过锁定 conversation；成功持锁后调用原 reserveTurn。
// 发现事务立即释放锁，处理事务重新取锁，避免在发现后的竞争窗口等待 conversation。
// 幂等键固定为 initial:{conversationID}:{taskID}，崩溃重入走 reserveTurn 回放，不会重复造 projection。reserve 失败的单条跳过，不让一只坏 Task 卡死整批恢复。
//
// 写 agent_turn_projections（经 reserveTurn）。不改已处于 goal_loop / 用户终态的 Task。
func recoverQueuedTaskTurns(ctx context.Context, s Service, limit int) (int, bool, error) {
	if limit <= 0 {
		limit = expiredExecutionBatchLimit
	}
	type item struct {
		ID        string  `gorm:"column:id"`
		Goal      string  `gorm:"column:goal"`
		ConvID    string  `gorm:"column:conversation_id"`
		Scope     string  `gorm:"column:scope_type"`
		ProductID *string `gorm:"column:product_id"`
	}
	var tasks []item
	if err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		return pgxTx.WithContext(ctx).Model(&schema.AgentTasks{}).
			Clauses(pfdb.ForUpdateOfSkipLocked("agent_conversations")).
			Select("agent_tasks.id, agent_tasks.goal, agent_tasks.conversation_id, agent_conversations.scope_type, agent_conversations.product_id").
			Joins("JOIN agent_conversations ON agent_conversations.id = agent_tasks.conversation_id").
			Where("agent_tasks.status = ? AND agent_tasks.current_turn_id IS NULL AND agent_tasks.conversation_id IS NOT NULL", "queued").
			Order("agent_tasks.created_at, agent_tasks.id").
			Limit(limit + 1).
			Scan(&tasks).Error
	}); err != nil {
		return 0, false, err
	}
	hasMore := len(tasks) > limit
	if hasMore {
		tasks = tasks[:limit]
	}
	created := 0
	for _, task := range tasks {
		if err := ctx.Err(); err != nil {
			return created, hasMore, err
		}
		var productID *string
		if task.Scope == "product_workflow" {
			productID = task.ProductID
		}
		key := "initial:" + task.ConvID + ":" + task.ID
		var wasCreated bool
		err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
			var conversation schema.AgentConversations
			err := pgxTx.WithContext(ctx).Select("id").Clauses(pfdb.SkipLocked()).
				Where("id = ?", task.ConvID).Take(&conversation).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			_, made, err := reserveTurn(ctx, pgxTx, productID, task.ConvID, task.Goal, nil, key, &task.ID, "", nil)
			wasCreated = made
			return err
		})
		if err != nil {
			if ctx.Err() != nil {
				return created, hasMore, ctx.Err()
			}
			continue
		}
		if wasCreated {
			created++
		}
	}
	return created, hasMore, nil
}

func restagePendingTurns(ctx context.Context, s Service, limit int) (enqueued, pending int, hasMore bool, err error) {
	var ids []string
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		return pgxTx.WithContext(ctx).Model(&schema.AgentTurnProjections{}).
			Where("resume_required = FALSE AND (status IN ? OR (status = 'requires_input' AND question_answer_json IS NOT NULL))", []string{"queued", "running", "cancel_requested"}).
			Where(`NOT EXISTS (
				SELECT 1 FROM async_dispatches d
				WHERE d.delivery_key = ? || ':' || agent_turn_projections.id
				  AND d.status IN ?
			)`, queue.ActorAgentTurnSync, []string{queue.StatusPending, queue.StatusSent, queue.StatusDead}).
			Order("created_at, id").
			Limit(limit+1).
			Pluck("id", &ids).Error
	})
	if err != nil {
		return 0, 0, false, err
	}
	if len(ids) > limit {
		hasMore = true
		ids = ids[:limit]
	}
	pending = len(ids)
	var batchErr error
	for _, id := range ids {
		var changed bool
		restageErr := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
			var projection schema.AgentTurnProjections
			if takeErr := pgxTx.WithContext(ctx).Select("id", "status", "resume_required", "question_answer_json").
				Where("id = ?", id).Take(&projection).Error; takeErr != nil {
				if errors.Is(takeErr, gorm.ErrRecordNotFound) {
					return nil
				}
				return takeErr
			}
			needsSync := !projection.ResumeRequired && (inSet(inFlightTurn, projection.Status) ||
				(projection.Status == "requires_input" && projection.QuestionAnswerJSON != nil))
			if !needsSync {
				return nil
			}
			ok, restageErr := queue.RestageIfIdle(ctx, pgxTx, queue.ActorAgentTurnSync, id, nil)
			changed = ok
			return restageErr
		})
		if restageErr != nil {
			batchErr = errors.Join(batchErr, restageErr)
			continue
		}
		if changed {
			enqueued++
		}
	}
	return enqueued, pending, hasMore, batchErr
}

// recoverExpiredExecutions 回收 lease_expires_at 已过的 execution：递增 fencing_token、清空 owner，无法证明终态则把 Turn 标 unknown。
//
// 每条 execution 单独提交。必须先 SKIP LOCKED 锁 projection，再锁 execution；AppendEvents / heartbeat 用同一顺序。先锁 execution 会与已持有投影、正在等 lease 行的 live writer 死锁。
//
// journal 已有 turn/end 则 reprojectExistingTerminal，不另写终态。requires_input 且尚未过安全边界则只收 lease。否则 appendInterruptedTurnEvents（含 effect reconcile）并写 projection=unknown、started invocation=interrupted。
//
// 禁区：不要把过期当成 failed；不要覆盖用户拥有的 Goal（本函数不写 agent_tasks 终态完成）。
func recoverExpiredExecutions(ctx context.Context, s Service, limit int) (int, bool, error) {
	if limit <= 0 {
		limit = expiredExecutionBatchLimit
	}
	unknown := 0
	processed := 0
	skipped := map[string]struct{}{}
	var batchErr error
	for processed < limit {
		n, id, found, err := recoverNextExpiredExecution(ctx, s, skipped)
		if err != nil {
			if id != "" {
				skipped[id] = struct{}{}
				batchErr = errors.Join(batchErr, err)
				continue
			}
			return unknown, false, errors.Join(batchErr, err)
		}
		if !found {
			break
		}
		processed++
		unknown += n
	}
	hasMore, err := expiredExecutionsRemain(ctx, s)
	if err != nil {
		return unknown, false, errors.Join(batchErr, err)
	}
	return unknown, hasMore, batchErr
}

func expiredExecutionsRemain(ctx context.Context, s Service) (bool, error) {
	var ids []string
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		return expiredExecutionQuery(pgxTx.WithContext(ctx)).Limit(1).Pluck("agent_turn_executions.id", &ids).Error
	})
	if err != nil {
		return false, err
	}
	return len(ids) > 0, nil
}

func expiredExecutionQuery(db *gorm.DB) *gorm.DB {
	return db.Model(&schema.AgentTurnExecutions{}).
		Joins("JOIN agent_turn_projections ON agent_turn_projections.id = agent_turn_executions.turn_projection_id").
		Where("agent_turn_executions.owner_id IS NOT NULL AND agent_turn_executions.lease_expires_at IS NOT NULL AND agent_turn_executions.lease_expires_at <= NOW()").
		Order("agent_turn_executions.id")
}

func recoverNextExpiredExecution(ctx context.Context, s Service, skipped map[string]struct{}) (unknown int, executionID string, found bool, err error) {
	now := time.Now().UTC()
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		type candidate struct {
			ID           string `gorm:"column:id"`
			ProjectionID string `gorm:"column:turn_projection_id"`
		}
		query := pgxTx.WithContext(ctx).
			Clauses(pfdb.ForUpdateOfSkipLocked("agent_turn_projections")).
			Select("agent_turn_executions.id, agent_turn_executions.turn_projection_id")
		query = expiredExecutionQuery(query)
		if len(skipped) > 0 {
			ids := make([]string, 0, len(skipped))
			for id := range skipped {
				ids = append(ids, id)
			}
			query = query.Where("agent_turn_executions.id NOT IN ?", ids)
		}
		var candidates []candidate
		candidateScanStarted := time.Now()
		scanErr := query.Limit(1).Scan(&candidates).Error
		metrics.ObserveRecoveryLock("agent", time.Since(candidateScanStarted))
		if scanErr != nil {
			return scanErr
		}
		if len(candidates) == 0 {
			return nil
		}
		item := candidates[0]
		found = true
		executionID = item.ID

		var projection schema.AgentTurnProjections
		if takeErr := pgxTx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Where("id = ?", item.ProjectionID).Take(&projection).Error; takeErr != nil {
			if errors.Is(takeErr, gorm.ErrRecordNotFound) {
				return nil
			}
			return takeErr
		}
		var execution schema.AgentTurnExecutions
		takeErr := pgxTx.WithContext(ctx).Clauses(pfdb.ForUpdate()).
			Where("id = ? AND owner_id IS NOT NULL AND lease_expires_at IS NOT NULL AND lease_expires_at <= NOW()", item.ID).
			Take(&execution).Error
		if errors.Is(takeErr, gorm.ErrRecordNotFound) {
			return nil
		}
		if takeErr != nil {
			return takeErr
		}
		if updErr := pgxTx.Model(&schema.AgentTurnExecutions{}).Where("id = ?", item.ID).Updates(map[string]any{
			"fencing_token":    gorm.Expr("fencing_token + 1"),
			"owner_id":         nil,
			"lease_token":      nil,
			"lease_expires_at": nil,
			"released_at":      now,
		}).Error; updErr != nil {
			return updErr
		}
		publishLeaseChanged(pgxTx, item.ID, "expired")
		var existingTerminal schema.AgentTurnEvents
		scanErr = pgxTx.Where("turn_projection_id = ? AND kind = ?", item.ProjectionID, "turn/end").
			Order("sequence DESC").Take(&existingTerminal).Error
		if scanErr == nil {
			if projErr := reprojectExistingTerminal(ctx, s, pgxTx, item.ProjectionID, item.ID, execution, existingTerminal); projErr != nil {
				return projErr
			}
			var payload terminalEventPayload
			if json.Unmarshal([]byte(existingTerminal.PayloadJSON), &payload) == nil && payload.Status == "unknown" {
				unknown = 1
			}
			return nil
		}
		if !errors.Is(scanErr, gorm.ErrRecordNotFound) {
			return scanErr
		}
		if projection.Status == "requires_input" || projection.Status == "awaiting_confirmation" {
			return nil
		}
		if inSet(activeTurn, projection.Status) && !(projection.Status == "queued" && execution.Phase == "claimed") {
			output, appendErr := appendInterruptedTurnEvents(ctx, s, pgxTx, item.ProjectionID, item.ID, now)
			if appendErr != nil {
				return appendErr
			}
			if updErr := pgxTx.Model(&schema.AgentTurnProjections{}).Where("id = ?", item.ProjectionID).Updates(map[string]any{
				"status":               "unknown",
				"terminal_reason_code": "execution_interrupted",
				"error_text":           "Agent execution lease expired before this Turn reached a provable terminal state",
				"finished_at":          now,
				"updated_at":           now,
				"resume_required":      false,
				"output_text":          nullableString(output),
			}).Error; updErr != nil {
				return updErr
			}
			if updErr := pgxTx.Model(&schema.AgentModelInvocations{}).
				Where("turn_projection_id = ? AND status = ?", item.ProjectionID, "started").
				Updates(map[string]any{"status": "interrupted", "finished_at": now, "updated_at": now}).Error; updErr != nil {
				return updErr
			}
			if updErr := pgxTx.Model(&schema.AgentTurnExecutions{}).Where("id = ?", item.ID).Updates(map[string]any{
				"phase": "terminal",
			}).Error; updErr != nil {
				return updErr
			}
			unknown = 1
		}
		return nil
	})
	return unknown, executionID, found, err
}

// reprojectExistingTerminal 在 lease 已过期、但 journal 已有 turn/end 时，用回收后的 fencing_token（原值+1）重跑 projectTerminalEvent。
//
// 由 recoverExpiredExecutions 调用。不插入新的 turn/end；PostgreSQL journal 仍是权威。尚无 harness_turn_id 则只把 execution.phase 标 terminal。
//
// 商品 Goal 是否保持 goal_loop 仍走 applyTurnState，本函数不得直接改 agent_tasks。
func reprojectExistingTerminal(
	ctx context.Context,
	s Service,
	gdb *gorm.DB,
	projectionID, executionID string,
	execution schema.AgentTurnExecutions,
	existingTerminal schema.AgentTurnEvents,
) error {
	row, err := loadTurnByID(ctx, gdb, projectionID)
	if err != nil {
		return err
	}
	if row.HarnessTurnID == nil {
		return gdb.Model(&schema.AgentTurnExecutions{}).Where("id = ?", executionID).Updates(map[string]any{
			"phase": "terminal",
		}).Error
	}
	lease := ExecutionLeaseResponse{
		ExecutionID:   executionID,
		ProjectionID:  projectionID,
		HarnessTurnID: *row.HarnessTurnID,
		Attempt:       execution.Attempt,
		FencingToken:  execution.FencingToken + 1,
		Phase:         "terminal",
	}
	return s.projectTerminalEvent(ctx, gdb, row, lease, existingTerminal.Sequence, json.RawMessage(existingTerminal.PayloadJSON), existingTerminal.CreatedAt)
}

// appendInterruptedTurnEvents 在 lease 过期且 journal 还没有 turn/end 时，补齐可证明的中断记录。
//
// 先按 checkpoint 对未收口的 tool_effect_intent 做 reconcile（能 applied 的补 tool/result）。再给只有 chunk、没有 assistant/message 的 attempt 写 interrupted 消息，最后写 turn/end status=unknown / reason_code=execution_interrupted。
//
// 已有 turn/end 则只 rebuildJournalOutput，不重复终态。写 agent_turn_events；fencing 用回收后的 execution 行。禁区：不要写成 succeeded 或 failed；证据不足必须 unknown。
func appendInterruptedTurnEvents(ctx context.Context, s Service, gdb *gorm.DB, projectionID, executionID string, now time.Time) (string, error) {
	row, err := loadTurnByID(ctx, gdb, projectionID)
	if err != nil {
		return "", err
	}
	if row.HarnessTurnID == nil {
		return "", nil
	}
	var existingTerminal schema.AgentTurnEvents
	err = gdb.Where("turn_projection_id = ? AND kind = ?", projectionID, "turn/end").Order("sequence DESC").Take(&existingTerminal).Error
	if err == nil {
		return rebuildJournalOutput(gdb.WithContext(ctx), projectionID, existingTerminal.Sequence)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}
	var events []schema.AgentTurnEvents
	if err := gdb.Where("turn_projection_id = ?", projectionID).Order("sequence").Find(&events).Error; err != nil {
		return "", err
	}
	last := 0
	type attemptStream struct {
		seqs       []int
		hasMessage bool
	}
	streams := map[string]*attemptStream{}
	attemptOrder := make([]string, 0)
	ensureAttempt := func(id string) *attemptStream {
		if id == "" {
			id = "_"
		}
		if streams[id] == nil {
			streams[id] = &attemptStream{}
			attemptOrder = append(attemptOrder, id)
		}
		return streams[id]
	}
	existingResults := map[string]bool{}
	for _, event := range events {
		if event.Sequence > last {
			last = event.Sequence
		}
		var payload struct {
			AttemptID string `json:"attempt_id"`
			Delta     string `json:"delta"`
			StepID    string `json:"step_id"`
		}
		_ = json.Unmarshal([]byte(event.PayloadJSON), &payload)
		switch event.Kind {
		case "assistant/message":
			ensureAttempt(payload.AttemptID).hasMessage = true
		case "text.chunk":
			if payload.Delta != "" {
				stream := ensureAttempt(payload.AttemptID)
				stream.seqs = append(stream.seqs, event.Sequence)
			}
		case "tool/result":
			if payload.StepID != "" {
				existingResults[payload.StepID] = true
			}
		}
	}
	var exec schema.AgentTurnExecutions
	if err := gdb.Where("id = ?", executionID).Take(&exec).Error; err != nil {
		return "", err
	}
	appendEvent := func(kind string, payload map[string]any) error {
		last++
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		attempt, fence := exec.Attempt, exec.FencingToken
		return gdb.Create(&schema.AgentTurnEvents{
			ID: newID(), TurnProjectionID: projectionID, ExecutionID: &executionID,
			RunID: leaseHarnessRunID(row), TurnID: *row.HarnessTurnID, SchemaVersion: 1, Sequence: last,
			Attempt: &attempt, FencingToken: &fence, Kind: kind, PayloadJSON: string(raw), CreatedAt: now,
		}).Error
	}
	var checkpoints []schema.AgentTurnCheckpoints
	if err := gdb.Where("turn_projection_id = ? AND kind IN ?", projectionID, []string{"tool_effect_intent", "tool_effect_result"}).Order("sequence").Find(&checkpoints).Error; err != nil {
		return "", err
	}
	resolvedCalls := map[string]bool{}
	for _, checkpoint := range checkpoints {
		if checkpoint.Kind != "tool_effect_result" {
			continue
		}
		var payload struct {
			ToolCallID string `json:"tool_call_id"`
		}
		if json.Unmarshal([]byte(checkpoint.PayloadJSON), &payload) == nil && payload.ToolCallID != "" {
			resolvedCalls[payload.ToolCallID] = true
		}
	}
	for _, checkpoint := range checkpoints {
		if checkpoint.Kind != "tool_effect_intent" {
			continue
		}
		intent, parseErr := parseToolEffectIntent(json.RawMessage(checkpoint.PayloadJSON))
		if parseErr != nil || resolvedCalls[intent.ToolCallID] {
			continue
		}
		policy := toolRecoveryPolicies[intent.ToolName]
		if intent.RecoveryPolicy != "" && intent.RecoveryPolicy != policy {
			continue
		}
		if policy == "" || policy == "none" {
			continue
		}
		outcome, reconErr := s.reconcileEffectIntent(ctx, gdb, row.ConversationID, projectionID, intent, true)
		if reconErr != nil {
			return "", reconErr
		}
		if outcome.EffectResult != effectResultApplied || len(outcome.Result) == 0 || existingResults[intent.ToolCallID] {
			continue
		}
		var result any
		_ = json.Unmarshal(outcome.Result, &result)
		if err := appendEvent("tool/result", map[string]any{
			"step_id": intent.ToolCallID, "tool_name": intent.ToolName, "status": "succeeded",
			"result": result, "reconciled": true,
		}); err != nil {
			return "", err
		}
		existingResults[intent.ToolCallID] = true
	}
	for _, attemptID := range attemptOrder {
		stream := streams[attemptID]
		if stream.hasMessage || len(stream.seqs) == 0 {
			continue
		}
		payload := map[string]any{
			"interrupted": true, "reason": "aborted", "sourceEventSeqs": stream.seqs,
		}
		if attemptID != "_" {
			payload["attempt_id"] = attemptID
		}
		if err := appendEvent("assistant/message", payload); err != nil {
			return "", err
		}
	}
	if err := appendEvent("turn/end", map[string]any{
		"reason": "unknown", "reason_code": "execution_interrupted", "status": "unknown",
		"error": "Agent execution lease expired before this Turn reached a provable terminal state",
	}); err != nil {
		return "", err
	}
	return rebuildJournalOutput(gdb.WithContext(ctx), projectionID, last)
}
