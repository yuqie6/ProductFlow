package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	sqldb "database/sql"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

type Executor struct {
	Service Service
}

func (e Executor) Execute(ctx context.Context, projectionID string) error {
	return e.Service.SyncTurn(ctx, projectionID)
}

func (s Service) SyncTurn(ctx context.Context, projectionID string) error {
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
		if row.ResumeRequired {
			return nil
		}
		return nil
	})
	if err != nil || row.ID == "" || row.ResumeRequired {
		return err
	}
	if s.Gateway == nil {
		return nil
	}
	productID := row.ConversationProductID
	if row.ConversationScope == "global" {
		productID = nil
	}
	if row.HarnessTurnID == nil {
		_, err := s.bindGatewayTurn(ctx, productID, row.ConversationID, projectionID, true)
		if err != nil {
			return nil
		}
	} else {
		state, ge := s.Gateway.GetTurn(row.ConversationID, *row.HarnessTurnID, row.TaskID)
		if ge != nil {
			_ = s.recordStartError(ctx, productID, row.ConversationID, projectionID, "Agent 服务暂时不可用")
			var status int
			var gerr GatewayError
			if errors.As(ge, &gerr) {
				status = gerr.Status
			}
			if status == 0 || status >= 500 {
				_ = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
					_, err := queue.StageForActor(ctx, pgxTx, queue.ActorAgentTurnSync, projectionID, s.pollDelay())
					return err
				})
			}
			return nil
		}
		_ = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
			return s.applyTurnState(ctx, pgxTx, productID, row.ConversationID, projectionID, state)
		})
	}
	_ = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		loaded, err := loadTurnByID(ctx, pgxTx, projectionID)
		if err != nil {
			return err
		}
		if inSet(inFlightTurn, loaded.Status) && !loaded.ResumeRequired {
			_, err := queue.StageForActor(ctx, pgxTx, queue.ActorAgentTurnSync, projectionID, s.pollDelay())
			return err
		}
		return nil
	})
	return nil
}

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
	if row.ConversationScope == "global" && state.Status == "succeeded" && state.Artifact != nil {
		status = "awaiting_confirmation"
	}
	if pendingID := pendingWorkflowRequest(ctx, pgxTx, conversationID, state); pendingID != "" && state.Status == "succeeded" {
		status = "awaiting_confirmation"
	}
	output := nullableString(state.Output)
	errText := nullableString(state.Error)
	var questionJSON []byte
	if len(state.Question) > 0 && string(state.Question) != "null" {
		questionJSON = state.Question
	}
	var stepsJSON []byte
	if state.ToolSteps != nil {
		stepsJSON, _ = json.Marshal(state.ToolSteps)
	} else {
		stepsJSON = []byte("[]")
	}
	if _, err := pfdb.Exec(ctx, pgxTx, `
		UPDATE agent_turn_projections SET
			harness_turn_id = COALESCE(harness_turn_id, $2),
			status = $3, output_text = $4, error_text = $5, question_json = $6,
			tool_steps_json = $7, finished_at = $8, updated_at = NOW()
		WHERE id = $1
	`, projectionID, state.TurnID, status, output, errText, questionJSON, stepsJSON, state.FinishedAt); err != nil {
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
		payload, _ := json.Marshal(state.Artifact.Value)
		if state.Artifact.Name == "propose_global_draft" || state.Artifact.Name == "propose_library_organization_draft" {
			inner := state.Artifact.Value
			if state.Artifact.Name == "propose_global_draft" {
				if wrapped, ok := asMap(state.Artifact.Value)["library_payload"]; ok {
					raw, _ := json.Marshal(wrapped)
					inner = raw
				}
			}
			payload, _ = json.Marshal(inner)
			revID, err := s.Library.AppendOrganizationDraftRevisionTx(ctx, pgxTx, conversationID, payload, projectionID, state.Artifact.StepID)
			if err != nil {
				return err
			}
			if revID != "" {
				_, _ = pfdb.Exec(ctx, pgxTx, `
					UPDATE agent_turn_projections
					SET artifact_name = $2, artifact_step_id = $3, library_organization_draft_revision_id = $4, updated_at = NOW()
					WHERE id = $1
				`, projectionID, state.Artifact.Name, state.Artifact.StepID, revID)
			}
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

func validateFence(ctx context.Context, pgxTx *gorm.DB, projectionID string, state TurnState) error {
	var attempt, fencing int
	var phase string
	err := pfdb.QueryRow(ctx, pgxTx, `
		SELECT attempt, fencing_token, phase FROM agent_turn_executions WHERE turn_projection_id = $1
	`, projectionID).Scan(&attempt, &fencing, &phase)
	if errors.Is(err, sqldb.ErrNoRows) {
		if state.ExecutionAttempt != nil || state.ExecutionFence != nil {
			return apperr.Conflict("Agent Turn 返回了不存在的 execution lease")
		}
		return nil
	}
	if err != nil {
		return err
	}
	if phase == "terminal" && !inSet(terminalTurn, state.Status) {
		return apperr.Conflict("Agent execution 已进入终态，不能回写活动状态")
	}
	if state.ExecutionAttempt == nil || state.ExecutionFence == nil {
		if state.Status == "queued" {
			return nil
		}
		return apperr.Conflict("Agent Turn 缺少 execution fencing 信息")
	}
	if *state.ExecutionAttempt != attempt || *state.ExecutionFence != fencing {
		return apperr.Conflict("Agent Turn execution fencing token 已过期")
	}
	return nil
}

func isStaleQueued(ctx context.Context, pgxTx *gorm.DB, row turnRow, state TurnState) (bool, error) {
	if state.Status != "queued" {
		return false, nil
	}
	if state.ExecutionAttempt != nil || state.ExecutionFence != nil {
		return false, nil
	}
	var phase string
	err := pfdb.QueryRow(ctx, pgxTx, `SELECT phase FROM agent_turn_executions WHERE turn_projection_id = $1`, row.ID).Scan(&phase)
	if errors.Is(err, sqldb.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return row.Status != "queued" || phase != "claimed", nil
}

func pendingWorkflowRequest(ctx context.Context, pgxTx *gorm.DB, conversationID string, state TurnState) string {
	for i := len(state.ToolSteps) - 1; i >= 0; i-- {
		step := state.ToolSteps[i]
		kind, _ := step["kind"].(string)
		status, _ := step["status"].(string)
		if kind == "request_workflow_run" && status == "succeeded" {
			stepID, _ := step["step_id"].(string)
			var id string
			_ = pfdb.QueryRow(ctx, pgxTx, `
				SELECT id FROM agent_workflow_run_requests
				WHERE conversation_id = $1 AND source_step_id = $2
				ORDER BY created_at DESC LIMIT 1
			`, conversationID, stepID).Scan(&id)
			return id
		}
	}
	return ""
}

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
		var scope string
		err := pfdb.QueryRow(ctx, pgxTx, `SELECT scope_type FROM agent_conversations WHERE id = $1`, *task.ConversationID).Scan(&scope)
		if err != nil && !errors.Is(err, sqldb.ErrNoRows) {
			return err
		}
		keepGoal = scope == "product_workflow"
	}
	if task.Status == "paused" && (turnStatus == "requires_input" || turnStatus == "awaiting_confirmation") {
		if summary != "" {
			if _, err := pfdb.Exec(ctx, pgxTx, `UPDATE agent_tasks SET summary = $2, updated_at = NOW() WHERE id = $1`, task.ID, summary); err != nil {
				return err
			}
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
	if _, err := pfdb.Exec(ctx, pgxTx, `
		UPDATE agent_tasks SET
			status = $2,
			waiting_reason = $3,
			failure_reason = $4,
			summary = CASE WHEN $8 <> '' THEN $8 ELSE summary END,
			started_at = COALESCE(started_at, NOW()),
			finished_at = CASE
				WHEN $5 THEN NOW()
				WHEN $6 THEN NULL
				ELSE finished_at
			END,
			canceled_at = CASE WHEN $7 THEN NOW() ELSE canceled_at END,
			updated_at = NOW()
		WHERE id = $1
	`, task.ID, status, waiting, failure, setFinished, clearFinished, setCanceledAt, summary); err != nil {
		return err
	}
	return refreshSessionSummary(ctx, pgxTx, task.SessionID)
}

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
