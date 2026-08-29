package agent

import (
	"context"
	"encoding/json"
	"errors"

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
		if err := updateTaskFromTurn(ctx, pgxTx, *row.TaskID, status); err != nil {
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

func updateTaskFromTurn(ctx context.Context, pgxTx *gorm.DB, taskID, turnStatus string) error {
	var status, waiting *string
	switch turnStatus {
	case "queued", "running", "cancel_requested":
		s := "running"
		status = &s
	case "requires_input":
		s, w := "waiting_user", "requires_input"
		status, waiting = &s, &w
	case "awaiting_confirmation":
		s, w := "awaiting_confirmation", "awaiting_confirmation"
		status, waiting = &s, &w
	case "succeeded", "failed", "canceled", "unknown":
		s, w := "waiting_user", "goal_loop"
		status, waiting = &s, &w
	}
	if status == nil {
		return nil
	}
	var current string
	if err := pfdb.QueryRow(ctx, pgxTx, `SELECT status FROM agent_tasks WHERE id = $1`, taskID).Scan(&current); err != nil {
		return err
	}
	if current == "succeeded" || current == "canceled" || current == "paused" {
		return nil
	}
	_, err := pfdb.Exec(ctx, pgxTx, `
		UPDATE agent_tasks SET status = $2, waiting_reason = $3, started_at = COALESCE(started_at, NOW()), updated_at = NOW()
		WHERE id = $1
	`, taskID, *status, waiting)
	return err
}
