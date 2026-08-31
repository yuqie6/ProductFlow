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

// ReconcileExecutionEffect 在有效 execution lease 下，从当前 execution 的 PG intent checkpoint 对账 live 5xx 副作用。
//
// tool_call_id 无效返回 Validation；lease、execution 或 intent 不匹配返回 Conflict。查询、按 recovery_policy 重试和最终四态都由 reconcileEffectIntent 完成。
func (s Service) ReconcileExecutionEffect(ctx context.Context, conversationID, executionID, ownerID, leaseToken, toolCallID string) (EffectReconciliationResponse, error) {
	normalized := stringsTrim(toolCallID)
	if normalized == "" || len(normalized) > 120 {
		return EffectReconciliationResponse{}, apperr.Validation("tool_call_id 无效")
	}
	var out EffectReconciliationResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if err := lockProjectionForExecution(ctx, pgxTx, conversationID, executionID); err != nil {
			return err
		}
		lease, err := requireLease(ctx, pgxTx, conversationID, executionID, ownerID, leaseToken)
		if err != nil {
			return err
		}
		var checkpoint schema.AgentTurnCheckpoints
		err = pgxTx.Where("execution_id = ? AND turn_projection_id = ? AND kind = ? AND payload_json->>'tool_call_id' = ?", executionID, lease.ProjectionID, "tool_effect_intent", normalized).
			Order("sequence DESC").Take(&checkpoint).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperr.Conflict("找不到当前 execution 的副作用 intent checkpoint")
		}
		if err != nil {
			return err
		}
		intent, err := parseToolEffectIntent(json.RawMessage(checkpoint.PayloadJSON))
		if err != nil {
			return apperr.Conflict("副作用 intent checkpoint 必须符合 schema v1")
		}
		outcome, err := s.reconcileEffectIntent(ctx, pgxTx, conversationID, lease.ProjectionID, intent, true)
		if err != nil {
			return err
		}
		out = effectReconciliationResponse(lease.ProjectionID, outcome)
		return nil
	})
	return out, err
}

// ReconcileTurnEffect 只对 status=unknown 的 Turn 按 tool_call_id 对账副作用。tool_call_id 无效返回 Validation。Turn 非 unknown、找不到 intent 或不支持该工具返回 Conflict。
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
		out = effectReconciliationResponse(projectionID, outcome)
		return nil
	})
	return out, err
}

func effectReconciliationResponse(projectionID string, outcome effectReconcileOutcome) EffectReconciliationResponse {
	return EffectReconciliationResponse{
		SchemaVersion: 1, ID: outcome.ID, ProjectionID: projectionID,
		ToolCallID: outcome.ToolCallID, ToolName: outcome.ToolName, IdempotencyKey: outcome.IdempotencyKey,
		EffectResult: outcome.EffectResult, ReconciliationState: outcome.ReconciliationState,
		Result: outcome.Result, Detail: outcome.Detail, CreatedAt: outcome.CreatedAt, UpdatedAt: outcome.UpdatedAt,
	}
}
