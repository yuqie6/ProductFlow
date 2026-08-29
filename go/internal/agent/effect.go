package agent

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/tx"
)

func (s Service) ReconcileTurnEffect(ctx context.Context, productID *string, conversationID, projectionID, toolCallID string) (EffectReconciliationResponse, error) {
	normalized := stringsTrim(toolCallID)
	if normalized == "" || len(normalized) > 120 {
		return EffectReconciliationResponse{}, apperr.Validation("tool_call_id 无效")
	}
	var out EffectReconciliationResponse
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		row, err := loadTurn(ctx, pgxTx, productID, conversationID, projectionID)
		if err != nil {
			return err
		}
		if row.Status != "unknown" {
			return apperr.Conflict("只有 unknown Agent Turn 才能执行副作用对账")
		}
		var existingID, existingTool, existingKey, effectResult, reconState string
		var resultJSON []byte
		var detail *string
		err = pgxTx.QueryRow(ctx, `
			SELECT id, tool_name, idempotency_key, effect_result, reconciliation_state, result_json, detail, created_at, updated_at
			FROM agent_turn_effect_reconciliations
			WHERE turn_projection_id = $1 AND tool_call_id = $2
			FOR UPDATE
		`, projectionID, normalized).Scan(
			&existingID, &existingTool, &existingKey, &effectResult, &reconState, &resultJSON, &detail, &out.CreatedAt, &out.UpdatedAt,
		)
		if err == nil && effectResult != "unknown" {
			out.SchemaVersion = 1
			out.ID = existingID
			out.ProjectionID = projectionID
			out.ToolCallID = normalized
			out.ToolName = existingTool
			out.IdempotencyKey = existingKey
			out.EffectResult = effectResult
			out.ReconciliationState = reconState
			out.Result = resultJSON
			out.Detail = detail
			return nil
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var payload []byte
		err = pgxTx.QueryRow(ctx, `
			SELECT payload_json FROM agent_turn_checkpoints
			WHERE turn_projection_id = $1 AND kind = 'tool_effect_intent'
			  AND payload_json->>'tool_call_id' = $2
			ORDER BY sequence DESC LIMIT 1
		`, projectionID, normalized).Scan(&payload)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.Conflict("找不到该 tool call 的副作用 intent checkpoint")
		}
		if err != nil {
			return err
		}
		var intent map[string]any
		_ = json.Unmarshal(payload, &intent)
		toolName, _ := intent["tool_name"].(string)
		key, _ := intent["idempotency_key"].(string)
		if _, ok := map[string]struct{}{
			"request_workflow_run_v1": {}, "create_product_workspace_v1": {}, "finalize_product_intake_v1": {},
		}[toolName]; !ok {
			return apperr.Conflict("该工具不支持副作用对账")
		}
		prepared := toolPrepared(conversationID, toolName, map[string]any{}, intent)
		reconciled, err := reconcileToolMutation(ctx, pgxTx, conversationID, toolName, key, prepared)
		if err != nil {
			return err
		}
		effect := "unknown"
		switch reconciled.State {
		case "applied":
			effect = "applied"
		case "not_applied", "conflict":
			effect = "failed"
		}
		id := existingID
		if id == "" {
			id = newID()
			if _, err := pgxTx.Exec(ctx, `
				INSERT INTO agent_turn_effect_reconciliations (
					id, turn_projection_id, tool_call_id, tool_name, idempotency_key,
					effect_result, reconciliation_state, result_json, detail, created_at, updated_at
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW())
			`, id, projectionID, normalized, toolName, key, effect, reconciled.State, reconciled.Result, reconciled.Detail); err != nil {
				return err
			}
		} else if _, err := pgxTx.Exec(ctx, `
			UPDATE agent_turn_effect_reconciliations
			SET effect_result = $2, reconciliation_state = $3, result_json = $4, detail = $5, updated_at = NOW()
			WHERE id = $1
		`, id, effect, reconciled.State, reconciled.Result, reconciled.Detail); err != nil {
			return err
		}
		if err := pgxTx.QueryRow(ctx, `
			SELECT created_at, updated_at FROM agent_turn_effect_reconciliations WHERE id = $1
		`, id).Scan(&out.CreatedAt, &out.UpdatedAt); err != nil {
			return err
		}
		out.SchemaVersion = 1
		out.ID = id
		out.ProjectionID = projectionID
		out.ToolCallID = normalized
		out.ToolName = toolName
		out.IdempotencyKey = key
		out.EffectResult = effect
		out.ReconciliationState = reconciled.State
		out.Result = reconciled.Result
		out.Detail = reconciled.Detail
		return nil
	})
	return out, err
}
