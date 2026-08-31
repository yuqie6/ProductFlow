package agent

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

// ReconcileTurnEffect 只对 status=unknown 的 Turn 按 tool_call_id 对账副作用。
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
		intent, err := parseToolEffectIntent(json.RawMessage(checkpoint.PayloadJSON))
		if err != nil {
			return apperr.Conflict("副作用 intent checkpoint 必须符合 schema v1")
		}
		outcome, err := s.reconcileEffectIntent(ctx, pgxTx, conversationID, projectionID, intent, true)
		if err != nil {
			return err
		}
		out = EffectReconciliationResponse{
			SchemaVersion: 1, ID: outcome.ID, ProjectionID: projectionID,
			ToolCallID: outcome.ToolCallID, ToolName: outcome.ToolName, IdempotencyKey: outcome.IdempotencyKey,
			EffectResult: outcome.EffectResult, ReconciliationState: outcome.ReconciliationState,
			Result: outcome.Result, Detail: outcome.Detail, CreatedAt: outcome.CreatedAt, UpdatedAt: outcome.UpdatedAt,
		}
		return nil
	})
	return out, err
}
