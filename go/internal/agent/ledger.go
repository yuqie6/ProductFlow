package agent

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/canonjson"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

func toolPrepared(conversationID, operation string, before, target map[string]any) map[string]any {
	if before == nil {
		before = map[string]any{}
	}
	if target == nil {
		target = map[string]any{}
	}
	return map[string]any{
		"schema_version": 1,
		"operation":      operation,
		"scope":          map[string]any{"conversation_id": conversationID},
		"before":         before,
		"target":         target,
	}
}

func toolRequestHash(toolName string, prepared map[string]any) (string, error) {
	return canonjson.SHA256Hex(map[string]any{"tool_name": toolName, "prepared": prepared})
}

func mutationResultBytes(row schema.AgentToolMutations) []byte {
	if row.ResultJSON == nil {
		return nil
	}
	return []byte(*row.ResultJSON)
}

func emptyToNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// lookupToolMutation 只读账本，不插入。未命中时 found=false。
func lookupToolMutation(ctx context.Context, pgxTx *gorm.DB, conversationID, toolName, idempotencyKey, operation string, before, target map[string]any) (map[string]any, bool, error) {
	key, err := normalizeIdempotency(idempotencyKey, "idempotency key")
	if err != nil {
		return nil, false, err
	}
	hash, err := toolRequestHash(toolName, toolPrepared(conversationID, operation, before, target))
	if err != nil {
		return nil, false, err
	}
	var row schema.AgentToolMutations
	err = pgxTx.WithContext(ctx).Where("conversation_id = ? AND tool_name = ? AND idempotency_key = ?", conversationID, toolName, key).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if row.RequestHash != hash {
		return nil, false, apperr.Conflict("同一工具 idempotency key 不能提交不同请求")
	}
	resultJSON := mutationResultBytes(row)
	if row.Status != "applied" || len(resultJSON) == 0 {
		return nil, false, apperr.Conflict("工具副作用账本未处于 applied 状态")
	}
	var replay map[string]any
	_ = json.Unmarshal(resultJSON, &replay)
	return replay, true, nil
}

// applyToolMutation 按 conversation + tool + 幂等键写入 agent_tool_mutations。同键同 hash 且已 applied 则回放；同键不同 hash 返回 Conflict。
//
// 图工具、intake、工作区创建在副作用成功后调用。唯一约束冲突会重入自身做回放。证据不足（未 applied 或空 result）返回 Conflict，不能当成功。
func applyToolMutation(ctx context.Context, pgxTx *gorm.DB, conversationID, toolName, idempotencyKey, operation string, before, target, result map[string]any, extra map[string]any) (map[string]any, error) {
	key, err := normalizeIdempotency(idempotencyKey, "idempotency key")
	if err != nil {
		return nil, err
	}
	prepared := toolPrepared(conversationID, operation, before, target)
	hash, err := toolRequestHash(toolName, prepared)
	if err != nil {
		return nil, err
	}
	var existing schema.AgentToolMutations
	err = pgxTx.WithContext(ctx).Where("conversation_id = ? AND tool_name = ? AND idempotency_key = ?", conversationID, toolName, key).Take(&existing).Error
	if err == nil {
		if existing.RequestHash != hash {
			return nil, apperr.Conflict("同一工具 idempotency key 不能提交不同请求")
		}
		resultJSON := mutationResultBytes(existing)
		if existing.Status != "applied" || len(resultJSON) == 0 {
			return nil, apperr.Conflict("工具副作用账本未处于 applied 状态")
		}
		var replay map[string]any
		_ = json.Unmarshal(resultJSON, &replay)
		return replay, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	preparedJSON, _ := json.Marshal(prepared)
	resultBytes, _ := json.Marshal(result)
	resultStr := string(resultBytes)
	assetID, _ := extra["asset_id"].(string)
	expectedName, _ := extra["expected_display_name"].(string)
	targetName, _ := extra["target_display_name"].(string)
	now := time.Now().UTC()
	row := schema.AgentToolMutations{
		ID:                  newID(),
		ConversationID:      conversationID,
		ToolName:            toolName,
		IdempotencyKey:      key,
		RequestHash:         hash,
		AssetID:             emptyToNil(assetID),
		ExpectedDisplayName: emptyToNil(expectedName),
		TargetDisplayName:   emptyToNil(targetName),
		PreparedJSON:        string(preparedJSON),
		Status:              "applied",
		ResultJSON:          &resultStr,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := pgxTx.Create(&row).Error; err != nil {
		if uniqueViolation(err) {
			return applyToolMutation(ctx, pgxTx, conversationID, toolName, idempotencyKey, operation, before, target, result, extra)
		}
		return nil, err
	}
	return result, nil
}

// reconcileToolMutation 只读工具账本：未命中 not_applied，hash 冲突 conflict，已 applied 回放，其它 unknown。
//
// 不插入。不可证明的结果保持 unknown，供 effect reconcile 决定是否原键重试。
func reconcileToolMutation(ctx context.Context, pgxTx *gorm.DB, conversationID, toolName, idempotencyKey string, prepared map[string]any) (ReconcileResponse, error) {
	key, err := normalizeIdempotency(idempotencyKey, "idempotency key")
	if err != nil {
		return ReconcileResponse{}, err
	}
	hash, err := canonjson.SHA256Hex(map[string]any{"tool_name": toolName, "prepared": prepared})
	if err != nil {
		return ReconcileResponse{}, err
	}
	var row schema.AgentToolMutations
	err = pgxTx.WithContext(ctx).Where("conversation_id = ? AND tool_name = ? AND idempotency_key = ?", conversationID, toolName, key).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ReconcileResponse{State: "not_applied", Detail: ptr("工具副作用尚未提交")}, nil
	}
	if err != nil {
		return ReconcileResponse{}, err
	}
	if row.RequestHash != hash {
		return ReconcileResponse{State: "conflict", Detail: ptr("工具幂等键已绑定其他请求")}, nil
	}
	resultJSON := mutationResultBytes(row)
	if row.Status == "applied" && len(resultJSON) > 0 {
		detail := "工具副作用已提交"
		return ReconcileResponse{State: "applied", Result: json.RawMessage(resultJSON), Detail: &detail}, nil
	}
	return ReconcileResponse{State: "unknown", Detail: ptr("副作用结果仍不明确")}, nil
}

// ReconcileTool 按幂等键查询工具账本：已 applied 回放，未提交返回 not_applied，证据不足返回 unknown。
func (s Service) ReconcileTool(ctx context.Context, conversationID, toolName, idempotencyKey string, prepared map[string]any) (ReconcileResponse, error) {
	var out ReconcileResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		item, err := reconcileToolMutation(ctx, pgxTx, conversationID, toolName, idempotencyKey, prepared)
		if err != nil {
			return err
		}
		out = item
		return nil
	})
	return out, err
}

func (s Service) lookupMutation(ctx context.Context, conversationID, toolName, idempotencyKey, operation string, before, target map[string]any) (map[string]any, bool, error) {
	var replay map[string]any
	var found bool
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if _, err := loadConversationByID(ctx, pgxTx, conversationID); err != nil {
			return err
		}
		item, ok, err := lookupToolMutation(ctx, pgxTx, conversationID, toolName, idempotencyKey, operation, before, target)
		replay, found = item, ok
		return err
	})
	return replay, found, err
}

func (s Service) recordMutation(ctx context.Context, conversationID, toolName, idempotencyKey, operation string, before, target, result map[string]any, extra map[string]any) error {
	return tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		_, err := applyToolMutation(ctx, pgxTx, conversationID, toolName, idempotencyKey, operation, before, target, result, extra)
		return err
	})
}
