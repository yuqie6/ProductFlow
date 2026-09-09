package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

// Executor 是 Agent Turn 启动/答案恢复 worker 入口。活动状态和摘要由 PostgreSQL journal 投影。
type Executor struct {
	Service Service // 调用 SyncTurn 的 worker 入口
}

// Execute 同步一个 Turn projection；缺失行视为已处理。
//
// 未绑定 start 或已有答案待 resume 时可返回 queue.ErrLater。Gateway 失败记 sync_error 后走 syncOutcome，不把 Goal 标完成。
func (e Executor) Execute(ctx context.Context, projectionID string) error {
	return e.Service.SyncTurn(ctx, projectionID)
}

// SyncTurn 只负责绑定 agent-service harness Turn，或恢复已持久化答案的同一 Turn。
//
// 活动状态和三列摘要由 PostgreSQL journal fold。投影不存在视为已处理；resume_required 时不催 Gateway。
// Gateway 不可用只记 sync_error。未绑定 start 或答案 resume 尚未完成时经 syncOutcome 返回 queue.ErrLater。
func (s Service) SyncTurn(ctx context.Context, projectionID string) error {
	var row turnRow
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if err := queue.AssertExecution(ctx, pgxTx, queue.ActorAgentTurnSync, projectionID); err != nil {
			return err
		}
		loaded, err := loadTurnByID(ctx, pgxTx, projectionID)
		if err != nil {
			if apperr.IsNotFound(err) {
				return nil
			}
			return err
		}
		row = loaded
		return nil
	})
	if err != nil || row.ID == "" {
		return err
	}
	if !turnNeedsSync(row) {
		return nil
	}
	productID := row.ConversationProductID
	if row.ConversationScope == "global" {
		productID = nil
	}
	if s.Gateway != nil {
		if row.Status == "requires_input" && len(row.QuestionAnswerJSON) > 0 && row.HarnessTurnID != nil {
			if err := s.resumeAnsweredParent(ctx, productID, row); err != nil {
				return s.syncOutcome(ctx, projectionID)
			}
			return s.syncOutcome(ctx, projectionID)
		}
		if row.HarnessTurnID == nil {
			if _, err := s.bindGatewayTurn(ctx, productID, row.ConversationID, projectionID, true); err != nil {
				return s.syncOutcome(ctx, projectionID)
			}
		}
	}
	return s.syncOutcome(ctx, projectionID)
}

// syncOutcome 在 SyncTurn 末尾决定 worker 是否还要再来：投影仍需同步则返回 queue.ErrLater，否则视为本轮处理完。
//
// 不写表。缺失行当已处理。resume_required 的 Turn 不催 Gateway。
func (s Service) syncOutcome(ctx context.Context, projectionID string) error {
	var row turnRow
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		loaded, err := loadTurnByID(ctx, pgxTx, projectionID)
		if err != nil {
			if apperr.IsNotFound(err) {
				return nil
			}
			return err
		}
		row = loaded
		return nil
	})
	if err != nil {
		return err
	}
	if turnNeedsSync(row) {
		return queue.ErrLater
	}
	return nil
}

// applyTurnState 把命令响应或 turn/end 的 TurnState 写入 PostgreSQL 投影。
//
// projectTerminalEvent、ControlTurn、resumeLiveQuestion 调用。流式活动状态与摘要直接由 journal fold。先 validateFence：过期 fencing_token 或终态 lease 回写活动状态一律 Conflict。无 fence 的陈旧 queued 回写被 isStaleQueued 丢掉。
//
// 写 agent_turn_projections、conversation 状态；有 Task 时走 updateTaskFromTurn（商品工作流终态停在 goal_loop）。可能挂 workflow_run_request 或图库 draft revision。
//
// 禁区：不要在其它函数直接 UPDATE 投影状态绕过 fencing；不要在这里把 Goal 标 succeeded；run_id 必须匹配 conversation/task harness。
func (s Service) applyTurnState(ctx context.Context, pgxTx *gorm.DB, productID *string, conversationID, projectionID string, state TurnState) error {
	row, err := loadTurn(ctx, pgxTx, productID, conversationID, projectionID)
	if err != nil {
		return err
	}
	expected := row.ConversationHarnessRunID
	if row.TaskHarnessRunID != nil && *row.TaskHarnessRunID != "" {
		expected = *row.TaskHarnessRunID
	}
	if state.RunID != "" && state.RunID != expected {
		return apperr.Conflict("Agent 服务返回了作用域不匹配的 Turn")
	}
	if err := validateFence(ctx, pgxTx, row.ID, state); err != nil {
		return err
	}
	if stale, err := isStaleQueued(ctx, pgxTx, row, state); err != nil {
		return err
	} else if stale {
		return nil
	}
	status := state.Status
	pendingID := pendingWorkflowRequest(ctx, pgxTx, conversationID, row.TaskID, state)
	output := nullableString(state.Output)
	errText := nullableString(state.Error)
	var questionJSON any = gorm.Expr("NULL")
	if len(state.Question) > 0 && string(state.Question) != "null" {
		questionJSON = string(state.Question)
	}
	var stepsJSON []byte
	if state.ToolSteps != nil {
		stepsJSON, _ = json.Marshal(state.ToolSteps)
	} else {
		stepsJSON = []byte("[]")
	}
	updates := map[string]any{
		"harness_turn_id": gorm.Expr("COALESCE(harness_turn_id, ?)", state.TurnID),
		"status":          status,
		"error_text":      errText,
		"question_json":   questionJSON,
		"tool_steps_json": string(stepsJSON),
		"finished_at":     state.FinishedAt,
		"updated_at":      time.Now().UTC(),
	}
	if state.Output != "" {
		updates["output_text"] = output
	}
	if strings.TrimSpace(state.Thinking) != "" {
		updates["thinking_text"] = state.Thinking
	}
	if err := pgxTx.Model(&schema.AgentTurnProjections{}).Where("id = ?", projectionID).Updates(updates).Error; err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "uq_agent_turn_projections_harness_turn_id" {
			return apperr.Conflict("Agent Turn projection 已绑定其他 harness Turn")
		}
		return err
	}
	if err := applyConversationStatus(ctx, pgxTx, conversationID, status); err != nil {
		return err
	}
	if row.TaskID != nil {
		if err := updateTaskFromTurn(ctx, pgxTx, *row.TaskID, status, state.Error, taskTurnSummary(status, state)); err != nil {
			return err
		}
	}
	if status == "awaiting_confirmation" && state.Artifact != nil && row.ConversationScope == "global" {
		if state.Artifact.Name == "propose_global_draft" || state.Artifact.Name == "propose_library_organization_draft" {
			inner := state.Artifact.Value
			if state.Artifact.Name == "propose_global_draft" {
				if wrapped, ok := asMap(state.Artifact.Value)["library_payload"]; ok {
					raw, _ := json.Marshal(wrapped)
					inner = raw
				}
			}
			payload, _ := json.Marshal(inner)
			revID, err := s.Library.AppendOrganizationDraftRevisionTx(ctx, pgxTx, conversationID, payload, projectionID, state.Artifact.StepID)
			if err != nil {
				return err
			}
			if revID != "" {
				if err := pgxTx.Model(&schema.AgentTurnProjections{}).Where("id = ?", projectionID).Updates(map[string]any{
					"artifact_name":                          state.Artifact.Name,
					"artifact_step_id":                       state.Artifact.StepID,
					"library_organization_draft_revision_id": revID,
					"updated_at":                             time.Now().UTC(),
				}).Error; err != nil {
					return err
				}
			}
		}
	}
	if pendingID != "" && status == "awaiting_confirmation" {
		if err := attachWorkflowRunRequest(ctx, pgxTx, projectionID, pendingID); err != nil {
			return err
		}
	}
	return nil
}

func asMap(v json.RawMessage) map[string]any {
	var out map[string]any
	_ = json.Unmarshal(v, &out)
	if out == nil {
		return map[string]any{}
	}
	return out
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// validateFence 拒绝过期 writer：state 的 attempt/fencing_token 必须等于当前 agent_turn_executions 行。
//
// 终态 lease 不能再回写活动状态。queued 且未带 fence 允许（尚未 claim）。缺 execution 却带着 fence 返回 Conflict。
func validateFence(ctx context.Context, pgxTx *gorm.DB, projectionID string, state TurnState) error {
	var exec schema.AgentTurnExecutions
	err := pgxTx.WithContext(ctx).Where("turn_projection_id = ?", projectionID).Take(&exec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if state.ExecutionAttempt != nil || state.ExecutionFence != nil {
			return apperr.Conflict("Agent Turn 返回了不存在的 execution lease")
		}
		return nil
	}
	if err != nil {
		return err
	}
	if exec.Phase == "terminal" && !inSet(terminalTurn, state.Status) {
		return apperr.Conflict("Agent execution 已进入终态，不能回写活动状态")
	}
	if state.ExecutionAttempt == nil || state.ExecutionFence == nil {
		if state.Status == "queued" {
			return nil
		}
		return apperr.Conflict("Agent Turn 缺少 execution fencing 信息")
	}
	if *state.ExecutionAttempt != exec.Attempt || *state.ExecutionFence != exec.FencingToken {
		return apperr.Conflict("Agent Turn execution fencing token 已过期")
	}
	return nil
}

// isStaleQueued 判断 Gateway 回的 queued 是否已过时：投影已离开 queued，或 execution 已越过 claimed，则忽略这次回写，避免把进行中 Turn 打回队列。
func isStaleQueued(ctx context.Context, pgxTx *gorm.DB, row turnRow, state TurnState) (bool, error) {
	if state.Status != "queued" {
		return false, nil
	}
	if state.ExecutionAttempt != nil || state.ExecutionFence != nil {
		return false, nil
	}
	var exec schema.AgentTurnExecutions
	err := pgxTx.WithContext(ctx).Where("turn_projection_id = ?", row.ID).Take(&exec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if row.Status == "requires_input" && len(row.QuestionAnswerJSON) > 0 {
		return false, nil
	}
	return row.Status != "queued" || exec.Phase != "claimed", nil
}

// pendingWorkflowRequest 从 tool_steps 里找最近一次成功的 request_workflow_run，返回已落库的确认单 id，供投影挂 awaiting_confirmation。
func pendingWorkflowRequest(ctx context.Context, pgxTx *gorm.DB, conversationID string, taskID *string, state TurnState) string {
	for i := len(state.ToolSteps) - 1; i >= 0; i-- {
		step := state.ToolSteps[i]
		kind, _ := step["kind"].(string)
		status, _ := step["status"].(string)
		if kind != "request_workflow_run" || status != "succeeded" {
			continue
		}
		stepID, _ := step["step_id"].(string)
		q := pgxTx.WithContext(ctx).Model(&schema.AgentWorkflowRunRequests{}).
			Where("conversation_id = ? AND source_step_id = ?", conversationID, stepID)
		if taskID != nil && *taskID != "" {
			q = q.Where("(task_id IS NULL OR task_id = ?)", *taskID)
		}
		var req schema.AgentWorkflowRunRequests
		if err := q.Order("created_at DESC").Take(&req).Error; err != nil || strings.TrimSpace(req.ID) == "" {
			continue
		}
		return req.ID
	}
	return ""
}

func attachWorkflowRunRequest(ctx context.Context, pgxTx *gorm.DB, projectionID, requestID string) error {
	var proj schema.AgentTurnProjections
	if err := pgxTx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Where("id = ?", projectionID).Take(&proj).Error; err != nil {
		return err
	}
	if proj.WorkflowRunRequestID != nil && *proj.WorkflowRunRequestID != "" && *proj.WorkflowRunRequestID != requestID {
		return apperr.Conflict("Agent Turn 已关联其他工作流执行请求")
	}
	return pgxTx.Model(&schema.AgentTurnProjections{}).Where("id = ?", projectionID).Updates(map[string]any{
		"workflow_run_request_id": requestID,
		"status":                  "awaiting_confirmation",
		"updated_at":              time.Now().UTC(),
	}).Error
}

// updateTaskFromTurn 把 Turn 状态投影到 AgentTask，但商品工作流的 Goal 在 Turn 终态后停在 waiting_user / goal_loop。
//
// applyTurnState 与未绑定 harness 的 cancel 调用。用户已 succeeded/canceled 的 Task 立刻返回。paused 只更新 summary。workflow_run_confirmation 等待中遇到飞行 Turn 也不改 waiting_reason。
//
// 写 agent_tasks 与 session summary。禁区：读路径与 GraphRun 同步不得覆盖 goal_loop；不要在这里把 Goal 标完成。
func updateTaskFromTurn(ctx context.Context, pgxTx *gorm.DB, taskID, turnStatus, errorText, summary string) error {
	task, err := lockTask(ctx, pgxTx, taskID)
	if err != nil {
		return err
	}
	if task.Status == "succeeded" || task.Status == "canceled" {
		return nil
	}
	keepGoal := false
	if task.ConversationID != nil {
		var conv schema.AgentConversations
		err := pgxTx.Where("id = ?", *task.ConversationID).Take(&conv).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		keepGoal = conv.ScopeType == "product_workflow"
	}
	if task.Status == "paused" && (turnStatus == "requires_input" || turnStatus == "awaiting_confirmation") {
		if summary != "" {
			if err := pgxTx.Model(&schema.AgentTasks{}).Where("id = ?", task.ID).Updates(map[string]any{
				"summary":    summary,
				"updated_at": time.Now().UTC(),
			}).Error; err != nil {
				return err
			}
			publishTaskChanged(pgxTx, task.ID, task.SessionID)
		}
		return refreshSessionSummary(ctx, pgxTx, task.SessionID)
	}
	parkedWorkflowConfirm := task.WaitingReason != nil && *task.WaitingReason == "workflow_run_confirmation"
	if parkedWorkflowConfirm && inSet(inFlightTurn, turnStatus) {
		if summary != "" {
			if err := pgxTx.Model(&schema.AgentTasks{}).Where("id = ?", task.ID).Updates(map[string]any{
				"summary":    summary,
				"updated_at": time.Now().UTC(),
			}).Error; err != nil {
				return err
			}
			publishTaskChanged(pgxTx, task.ID, task.SessionID)
		}
		return refreshSessionSummary(ctx, pgxTx, task.SessionID)
	}
	status := ""
	var waiting any
	var failure any
	setFinished := false
	clearFinished := false
	setCanceledAt := false
	switch turnStatus {
	case "queued":
		status = "queued"
	case "running", "cancel_requested":
		status = "running"
	case "requires_input":
		status = "waiting_user"
		waiting = "requires_input"
	case "awaiting_confirmation":
		status = "awaiting_confirmation"
		waiting = "awaiting_confirmation"
		if parkedWorkflowConfirm {
			waiting = "workflow_run_confirmation"
		}
	case "succeeded", "failed", "canceled", "unknown":
		if keepGoal {
			status = "waiting_user"
			waiting = "goal_loop"
			clearFinished = true
		} else {
			switch turnStatus {
			case "succeeded":
				status = "succeeded"
				setFinished = true
			case "failed":
				status = "failed"
				setFinished = true
				if errorText != "" {
					failure = errorText
				}
			case "canceled":
				status = "canceled"
				setFinished = true
				setCanceledAt = true
			default:
				status = "unknown"
				setFinished = true
				if errorText != "" {
					failure = errorText
				}
			}
		}
	default:
		return nil
	}
	now := time.Now().UTC()
	updates := map[string]any{
		"status":         status,
		"waiting_reason": waiting,
		"failure_reason": failure,
		"started_at":     gorm.Expr("COALESCE(started_at, ?)", now),
		"updated_at":     now,
	}
	if summary != "" {
		updates["summary"] = summary
	}
	if setFinished {
		updates["finished_at"] = now
	} else if clearFinished {
		updates["finished_at"] = nil
	}
	if setCanceledAt {
		updates["canceled_at"] = now
	}
	if err := pgxTx.Model(&schema.AgentTasks{}).Where("id = ?", task.ID).Updates(updates).Error; err != nil {
		return err
	}
	if err := refreshSessionSummary(ctx, pgxTx, task.SessionID); err != nil {
		return err
	}
	if taskListChangedFromTurn(task, status, waiting, failure, summary) {
		publishTaskChanged(pgxTx, task.ID, task.SessionID)
	}
	return nil
}

// taskListChangedFromTurn 判断本次投影是否改变列表可见字段，避免无意义的 task-changed 广播。
func taskListChangedFromTurn(task taskRow, status string, waiting, failure any, summary string) bool {
	if task.Status != status {
		return true
	}
	if summary != "" && (task.Summary == nil || *task.Summary != summary) {
		return true
	}
	prevWaiting := ""
	if task.WaitingReason != nil {
		prevWaiting = *task.WaitingReason
	}
	nextWaiting := ""
	if value, ok := waiting.(string); ok {
		nextWaiting = value
	}
	if prevWaiting != nextWaiting {
		return true
	}
	prevFailure := ""
	if task.FailureReason != nil {
		prevFailure = *task.FailureReason
	}
	nextFailure := ""
	if value, ok := failure.(string); ok {
		nextFailure = value
	}
	return prevFailure != nextFailure
}

// taskTurnSummary 生成有界 operational summary（等待回答、失败原因、output 摘要）。这不是第二份模型 transcript，长度截断到 2000 字。
func taskTurnSummary(status string, state TurnState) string {
	const maxLen = 2000
	bound := func(value string) string {
		normalized := strings.Join(strings.Fields(value), " ")
		if len([]rune(normalized)) > maxLen {
			return string([]rune(normalized)[:maxLen])
		}
		return normalized
	}
	switch status {
	case "requires_input":
		var question struct {
			Question string `json:"question"`
		}
		if len(state.Question) > 0 {
			_ = json.Unmarshal(state.Question, &question)
		}
		if strings.TrimSpace(question.Question) != "" {
			return bound("等待回答：" + question.Question)
		}
		return "等待回答"
	case "awaiting_confirmation":
		return "等待确认"
	}
	if strings.TrimSpace(state.Error) != "" {
		return bound("执行失败：" + state.Error)
	}
	if strings.TrimSpace(state.Output) != "" {
		return bound(state.Output)
	}
	for i := len(state.ToolSteps) - 1; i >= 0; i-- {
		if summary, ok := state.ToolSteps[i]["summary"].(string); ok && strings.TrimSpace(summary) != "" {
			return bound(summary)
		}
	}
	return bound("Agent Turn 状态：" + status)
}
