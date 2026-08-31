package agent

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

func (s Service) ReconcileTurnEffect(ctx context.Context, productID *string, conversationID, projectionID, toolCallID string) (EffectReconciliationResponse, error) {
	normalized := stringsTrim(toolCallID)
	if normalized == "" || len(normalized) > 120 {
		return EffectReconciliationResponse{}, apperr.Validation("tool_call_id 无效")
	}
	var out EffectReconciliationResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		row, err := loadTurn(ctx, pgxTx, productID, conversationID, projectionID)
		if err != nil {
			return err
		}
		if row.Status != "unknown" {
			return apperr.Conflict("只有 unknown Agent Turn 才能执行副作用对账")
		}
		var existing schema.AgentTurnEffectReconciliations
		err = pgxTx.Clauses(pfdb.ForUpdate()).
			Where("turn_projection_id = ? AND tool_call_id = ?", projectionID, normalized).
			Take(&existing).Error
		if err == nil && existing.EffectResult != "unknown" {
			out.SchemaVersion = 1
			out.ID = existing.ID
			out.ProjectionID = projectionID
			out.ToolCallID = normalized
			out.ToolName = existing.ToolName
			out.IdempotencyKey = existing.IdempotencyKey
			out.EffectResult = existing.EffectResult
			out.ReconciliationState = existing.ReconciliationState
			if existing.ResultJSON != nil {
				out.Result = json.RawMessage(*existing.ResultJSON)
			}
			out.Detail = existing.Detail
			out.CreatedAt = existing.CreatedAt
			out.UpdatedAt = existing.UpdatedAt
			return nil
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var checkpoint schema.AgentTurnCheckpoints
		err = pgxTx.Where("turn_projection_id = ? AND kind = ? AND payload_json->>'tool_call_id' = ?", projectionID, "tool_effect_intent", normalized).
			Order("sequence DESC").
			Take(&checkpoint).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperr.Conflict("找不到该 tool call 的副作用 intent checkpoint")
		}
		if err != nil {
			return err
		}
		var intent map[string]any
		_ = json.Unmarshal([]byte(checkpoint.PayloadJSON), &intent)
		toolName, _ := intent["tool_name"].(string)
		key, _ := intent["idempotency_key"].(string)
		if toolRecoveryPolicies[toolName] == "" || toolRecoveryPolicies[toolName] == "none" {
			return apperr.Conflict("该工具不支持副作用对账")
		}
		reconciled := ReconcileResponse{State: "not_applied", Detail: ptr("工具副作用尚未提交")}
		var mutation schema.AgentToolMutations
		mutationErr := pgxTx.WithContext(ctx).
			Where("conversation_id = ? AND tool_name = ? AND idempotency_key = ?", conversationID, toolName, key).
			Take(&mutation).Error
		if mutationErr == nil {
			switch mutation.Status {
			case "applied":
				if mutation.ResultJSON == nil {
					reconciled = ReconcileResponse{State: "unknown", Detail: ptr("工具账本缺少已提交结果")}
				} else {
					reconciled = ReconcileResponse{State: "applied", Result: json.RawMessage(*mutation.ResultJSON), Detail: ptr("工具副作用已提交")}
				}
			default:
				reconciled = ReconcileResponse{State: "unknown", Detail: ptr("副作用结果仍不明确")}
			}
		} else if !errors.Is(mutationErr, gorm.ErrRecordNotFound) {
			return mutationErr
		}
		effect := "unknown"
		switch reconciled.State {
		case "applied":
			effect = "applied"
		case "not_applied", "conflict":
			effect = "failed"
		}
		id := existing.ID
		var resultJSON *string
		if len(reconciled.Result) > 0 {
			s := string(reconciled.Result)
			resultJSON = &s
		}
		now := time.Now().UTC()
		if id == "" {
			id = newID()
			rec := schema.AgentTurnEffectReconciliations{
				ID:                  id,
				TurnProjectionID:    projectionID,
				ToolCallID:          normalized,
				ToolName:            toolName,
				IdempotencyKey:      key,
				EffectResult:        effect,
				ReconciliationState: reconciled.State,
				ResultJSON:          resultJSON,
				Detail:              reconciled.Detail,
				CreatedAt:           now,
				UpdatedAt:           now,
			}
			if err := pgxTx.Create(&rec).Error; err != nil {
				return err
			}
		} else if err := pgxTx.Model(&schema.AgentTurnEffectReconciliations{}).Where("id = ?", id).Updates(map[string]any{
			"effect_result":        effect,
			"reconciliation_state": reconciled.State,
			"result_json":          resultJSON,
			"detail":               reconciled.Detail,
			"updated_at":           now,
		}).Error; err != nil {
			return err
		}
		var saved schema.AgentTurnEffectReconciliations
		if err := pgxTx.Where("id = ?", id).Take(&saved).Error; err != nil {
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
		out.CreatedAt = saved.CreatedAt
		out.UpdatedAt = saved.UpdatedAt
		return nil
	})
	return out, err
}
