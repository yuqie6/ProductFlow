package agent

import (
	"context"
	"encoding/json"
	"errors"

	sqldb "database/sql"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/canonjson"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
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
	var existingHash, status string
	var resultJSON []byte
	err = pfdb.QueryRow(ctx, pgxTx, `
		SELECT request_hash, status, result_json FROM agent_tool_mutations
		WHERE conversation_id = $1 AND tool_name = $2 AND idempotency_key = $3
	`, conversationID, toolName, key).Scan(&existingHash, &status, &resultJSON)
	if errors.Is(err, sqldb.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if existingHash != hash {
		return nil, false, apperr.Conflict("同一工具 idempotency key 不能提交不同请求")
	}
	if status != "applied" || len(resultJSON) == 0 {
		return nil, false, apperr.Conflict("工具副作用账本未处于 applied 状态")
	}
	var replay map[string]any
	_ = json.Unmarshal(resultJSON, &replay)
	return replay, true, nil
}

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
	var existingHash, status string
	var resultJSON []byte
	err = pfdb.QueryRow(ctx, pgxTx, `
		SELECT request_hash, status, result_json FROM agent_tool_mutations
		WHERE conversation_id = $1 AND tool_name = $2 AND idempotency_key = $3
	`, conversationID, toolName, key).Scan(&existingHash, &status, &resultJSON)
	if err == nil {
		if existingHash != hash {
			return nil, apperr.Conflict("同一工具 idempotency key 不能提交不同请求")
		}
		if status != "applied" || len(resultJSON) == 0 {
			return nil, apperr.Conflict("工具副作用账本未处于 applied 状态")
		}
		var replay map[string]any
		_ = json.Unmarshal(resultJSON, &replay)
		return replay, nil
	}
	if !errors.Is(err, sqldb.ErrNoRows) {
		return nil, err
	}
	preparedJSON, _ := json.Marshal(prepared)
	resultBytes, _ := json.Marshal(result)
	id := newID()
	assetID, _ := extra["asset_id"].(string)
	expectedName, _ := extra["expected_display_name"].(string)
	targetName, _ := extra["target_display_name"].(string)
	if _, err := pfdb.Exec(ctx, pgxTx, `
		INSERT INTO agent_tool_mutations (
			id, conversation_id, tool_name, idempotency_key, request_hash, asset_id,
			expected_display_name, target_display_name, prepared_json, status, result_json, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'applied', $10, NOW(), NOW())
	`, id, conversationID, toolName, key, hash, nullable(assetID), nullable(expectedName), nullable(targetName), preparedJSON, resultBytes); err != nil {
		if uniqueViolation(err) {
			return applyToolMutation(ctx, pgxTx, conversationID, toolName, idempotencyKey, operation, before, target, result, extra)
		}
		return nil, err
	}
	return result, nil
}

func reconcileToolMutation(ctx context.Context, pgxTx *gorm.DB, conversationID, toolName, idempotencyKey string, prepared map[string]any) (ReconcileResponse, error) {
	key, err := normalizeIdempotency(idempotencyKey, "idempotency key")
	if err != nil {
		return ReconcileResponse{}, err
	}
	hash, err := canonjson.SHA256Hex(map[string]any{"tool_name": toolName, "prepared": prepared})
	if err != nil {
		return ReconcileResponse{}, err
	}
	var existingHash, status string
	var resultJSON []byte
	err = pfdb.QueryRow(ctx, pgxTx, `
		SELECT request_hash, status, result_json FROM agent_tool_mutations
		WHERE conversation_id = $1 AND tool_name = $2 AND idempotency_key = $3
	`, conversationID, toolName, key).Scan(&existingHash, &status, &resultJSON)
	if errors.Is(err, sqldb.ErrNoRows) {
		return ReconcileResponse{State: "not_applied", Detail: ptr("工具副作用尚未提交")}, nil
	}
	if err != nil {
		return ReconcileResponse{}, err
	}
	if existingHash != hash {
		return ReconcileResponse{State: "conflict", Detail: ptr("工具幂等键已绑定其他请求")}, nil
	}
	if status == "applied" && len(resultJSON) > 0 {
		detail := "工具副作用已提交"
		return ReconcileResponse{State: "applied", Result: json.RawMessage(resultJSON), Detail: &detail}, nil
	}
	return ReconcileResponse{State: "unknown", Detail: ptr("副作用结果仍不明确")}, nil
}

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
