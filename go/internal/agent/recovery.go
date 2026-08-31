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

type RecoverySummary struct {
	PendingTurns       int
	EnqueuedTurns      int
	RecoveredTaskTurns int
	UnknownExecutions  int
}

const expiredExecutionBatchLimit = 25

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

func RecoverUnfinishedTurns(ctx context.Context, s Service) (RecoverySummary, error) {
	return recoverUnfinishedTurns(ctx, s, expiredExecutionBatchLimit)
}

func recoverUnfinishedTurns(ctx context.Context, s Service, limit int) (RecoverySummary, error) {
	var out RecoverySummary
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		n, err := recoverExpiredExecutions(ctx, s, pgxTx, limit)
		if err != nil {
			return err
		}
		out.UnknownExecutions = n
		if n > 0 {
			metrics.AgentRecoveryUnknownExecutions.Add(int64(n))
		}
		var ids []string
		if err := pgxTx.Model(&schema.AgentTurnProjections{}).
			Where("resume_required = FALSE AND (status IN ? OR (status = 'requires_input' AND question_answer_json IS NOT NULL))", []string{"queued", "running", "cancel_requested"}).
			Order("created_at, id").
			Pluck("id", &ids).Error; err != nil {
			return err
		}
		taskIDs, recovered, err := recoverQueuedTaskTurns(ctx, pgxTx)
		if err != nil {
			return err
		}
		ids = append(ids, taskIDs...)
		out.PendingTurns = len(ids)
		out.RecoveredTaskTurns = recovered
		for _, id := range ids {
			changed, err := queue.RestageIfIdle(ctx, pgxTx, queue.ActorAgentTurnSync, id, nil)
			if err != nil {
				return err
			}
			if changed {
				out.EnqueuedTurns++
			}
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	_, _ = CompactExpiredTurnJournals(ctx, s, time.Now().UTC())
	return out, nil
}

func recoverQueuedTaskTurns(ctx context.Context, pgxTx *gorm.DB) ([]string, int, error) {
	type item struct {
		ID        string  `gorm:"column:id"`
		Goal      string  `gorm:"column:goal"`
		ConvID    string  `gorm:"column:conversation_id"`
		Scope     string  `gorm:"column:scope_type"`
		ProductID *string `gorm:"column:product_id"`
	}
	var tasks []item
	if err := pgxTx.Model(&schema.AgentTasks{}).
		Select("agent_tasks.id, agent_tasks.goal, agent_tasks.conversation_id, agent_conversations.scope_type, agent_conversations.product_id").
		Joins("JOIN agent_conversations ON agent_conversations.id = agent_tasks.conversation_id").
		Where("agent_tasks.status = ? AND agent_tasks.current_turn_id IS NULL AND agent_tasks.conversation_id IS NOT NULL", "queued").
		Order("agent_tasks.created_at, agent_tasks.id").
		Scan(&tasks).Error; err != nil {
		return nil, 0, err
	}
	var ids []string
	created := 0
	for _, task := range tasks {
		var productID *string
		if task.Scope == "product_workflow" {
			productID = task.ProductID
		}
		key := "initial:" + task.ConvID + ":" + task.ID
		row, wasCreated, err := reserveTurn(ctx, pgxTx, productID, task.ConvID, task.Goal, nil, key, &task.ID, "", nil)
		if err != nil {
			continue
		}
		ids = append(ids, row.ID)
		if wasCreated {
			created++
		}
	}
	return ids, created, nil
}

func recoverExpiredExecutions(ctx context.Context, s Service, pgxTx *gorm.DB, limit int) (int, error) {
	if limit <= 0 {
		limit = expiredExecutionBatchLimit
	}
	type candidate struct {
		ID           string `gorm:"column:id"`
		ProjectionID string `gorm:"column:turn_projection_id"`
	}
	var candidates []candidate
	// Claim by locking the projection first (SKIP LOCKED). AppendEvents / heartbeat
	// use the same projection → execution order; locking executions here first deadlocks
	// a live writer that already holds the projection and is waiting on the lease row.
	if err := pgxTx.WithContext(ctx).
		Clauses(pfdb.ForUpdateOfSkipLocked("agent_turn_projections")).
		Model(&schema.AgentTurnExecutions{}).
		Select("agent_turn_executions.id, agent_turn_executions.turn_projection_id").
		Joins("JOIN agent_turn_projections ON agent_turn_projections.id = agent_turn_executions.turn_projection_id").
		Where("agent_turn_executions.owner_id IS NOT NULL AND agent_turn_executions.lease_expires_at IS NOT NULL AND agent_turn_executions.lease_expires_at <= NOW()").
		Order("agent_turn_executions.id").
		Limit(limit).
		Scan(&candidates).Error; err != nil {
		return 0, err
	}
	unknown := 0
	now := time.Now().UTC()
	for _, item := range candidates {
		var projection schema.AgentTurnProjections
		err := pgxTx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Where("id = ?", item.ProjectionID).Take(&projection).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return 0, err
		}
		var execution schema.AgentTurnExecutions
		err = pgxTx.WithContext(ctx).Clauses(pfdb.ForUpdate()).
			Where("id = ? AND owner_id IS NOT NULL AND lease_expires_at IS NOT NULL AND lease_expires_at <= NOW()", item.ID).
			Take(&execution).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return 0, err
		}
		if err := pgxTx.Model(&schema.AgentTurnExecutions{}).Where("id = ?", item.ID).Updates(map[string]any{
			"fencing_token":    gorm.Expr("fencing_token + 1"),
			"owner_id":         nil,
			"lease_token":      nil,
			"lease_expires_at": nil,
			"released_at":      now,
		}).Error; err != nil {
			return 0, err
		}
		publishLeaseChanged(pgxTx, item.ID, "expired")
		var existingTerminal schema.AgentTurnEvents
		scanErr := pgxTx.Where("turn_projection_id = ? AND kind = ?", item.ProjectionID, "turn/end").
			Order("sequence DESC").Take(&existingTerminal).Error
		if scanErr == nil {
			if err := reprojectExistingTerminal(ctx, s, pgxTx, item.ProjectionID, item.ID, execution, existingTerminal); err != nil {
				return 0, err
			}
			var payload terminalEventPayload
			if json.Unmarshal([]byte(existingTerminal.PayloadJSON), &payload) == nil && payload.Status == "unknown" {
				unknown++
			}
			continue
		}
		if !errors.Is(scanErr, gorm.ErrRecordNotFound) {
			return 0, scanErr
		}
		if projection.Status == "requires_input" {
			continue
		}
		if inSet(activeTurn, projection.Status) && !(projection.Status == "queued" && execution.Phase == "claimed") {
			output, err := appendInterruptedTurnEvents(ctx, s, pgxTx, item.ProjectionID, item.ID, now)
			if err != nil {
				return 0, err
			}
			if err := pgxTx.Model(&schema.AgentTurnProjections{}).Where("id = ?", item.ProjectionID).Updates(map[string]any{
				"status":               "unknown",
				"terminal_reason_code": "execution_interrupted",
				"error_text":           "Agent execution lease expired before this Turn reached a provable terminal state",
				"finished_at":          now,
				"updated_at":           now,
				"resume_required":      false,
				"output_text":          nullableString(output),
			}).Error; err != nil {
				return 0, err
			}
			if err := pgxTx.Model(&schema.AgentModelInvocations{}).
				Where("turn_projection_id = ? AND status = ?", item.ProjectionID, "started").
				Updates(map[string]any{"status": "interrupted", "finished_at": now, "updated_at": now}).Error; err != nil {
				return 0, err
			}
			if err := pgxTx.Model(&schema.AgentTurnExecutions{}).Where("id = ?", item.ID).Updates(map[string]any{
				"phase": "terminal",
			}).Error; err != nil {
				return 0, err
			}
			unknown++
		}
	}
	return unknown, nil
}

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
