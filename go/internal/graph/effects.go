package graph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/yuqie6/productflow/internal/platform/canonjson"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

const (
	maxProviderEffectJSONBytes = 64 * 1024
	graphProviderEffectKind    = "workflow_graph_generation"
)

type providerUnknownError struct{}

// Error 返回 ProviderUnknownDetail，供 errors.As 识别 unknown。
func (providerUnknownError) Error() string { return ProviderUnknownDetail }

// ErrProviderUnknown 把超时或 5xx 等无法证明的供应商结果标成 unknown。
func ErrProviderUnknown() error { return providerUnknownError{} }

func isProviderUnknown(err error) bool {
	var u providerUnknownError
	return errors.As(err, &u)
}

func providerEffectHash(payload map[string]any) (string, error) {
	compact, err := canonjson.Compact(payload)
	if err != nil {
		return "", err
	}
	if len(compact) > maxProviderEffectJSONBytes {
		return "", errors.New("图运行 provider effect request 超过 65536 bytes")
	}
	sum := sha256.Sum256(compact)
	return hex.EncodeToString(sum[:]), nil
}

func marshalCompactSorted(v any) ([]byte, error) {
	return canonjson.Compact(v)
}

// markNodeUnknown 把无法证明的 provider 结果写成 unknown，不是 failed。
// 须已在事务里；锁序 run → node → effect。run 已终态、节点不在 running、attempt 不匹配都直接成功返回（幂等围栏）。
// 副作用：workflow_graph_provider_effects 标 unknown；node_run 终态 unknown；事件 kind 仍是 node.failed，
// 但 payload.status 是 unknown。不要在这里 promote queued——由 completeGraphRunIfNodesTerminal 做。
func markNodeUnknown(ctx context.Context, tx *gorm.DB, runID, nodeRunID string, attemptID *string, detail string) error {
	if len(detail) > 1000 {
		detail = detail[:1000]
	}
	now := time.Now().UTC()
	var run schema.WorkflowGraphRuns
	if err := tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Where("id = ?", runID).Take(&run).Error; err != nil {
		return err
	}
	if isTerminalRun(run.Status) {
		return nil
	}
	var node schema.WorkflowGraphNodeRuns
	err := tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).
		Where("id = ? AND graph_run_id = ?", nodeRunID, runID).
		Take(&node).Error
	if err != nil {
		return err
	}
	if node.Status != NodeRunRunning || node.ActiveAttemptID == nil || *node.ActiveAttemptID == "" {
		return nil
	}
	if attemptID != nil && *attemptID != *node.ActiveAttemptID {
		return nil
	}
	resolved := *node.ActiveAttemptID
	effectRes := tx.WithContext(ctx).Model(&schema.WorkflowGraphProviderEffects{}).
		Where("node_run_id = ? AND attempt_id = ? AND effect_result NOT IN ?", nodeRunID, resolved, []string{"applied", "failed"}).
		Updates(map[string]any{
			"effect_result":        "unknown",
			"reconciliation_state": "unknown",
			"detail":               detail,
			"updated_at":           now,
		})
	if err := effectRes.Error; err != nil {
		return err
	}
	updates := terminalNodeRunUpdates(NodeRunUnknown, now)
	updates["failure_reason"] = detail
	res := tx.WithContext(ctx).Model(&schema.WorkflowGraphNodeRuns{}).Where("id = ? AND status = ? AND active_attempt_id = ?", nodeRunID, NodeRunRunning, resolved).Updates(updates)
	if err := res.Error; err != nil {
		return err
	}
	if res.RowsAffected != 1 {
		return nil
	}
	return appendGraphRunEventLocked(ctx, tx, runID, "node.failed", &nodeRunID, map[string]any{
		"status": NodeRunUnknown, "node_id": node.NodeID, "reason": detail,
	})
}

// advanceNodePhase 推进 running 节点的 progress_phase（claimed → provider_call 等）。
// run 不 running 或 attempt 不匹配返回 false,nil，调用方应停，不要当失败。写 node.progress 事件。
func advanceNodePhase(ctx context.Context, tx *gorm.DB, runID, nodeRunID, attemptID, phase string) (bool, error) {
	now := time.Now().UTC()
	var run schema.WorkflowGraphRuns
	if err := tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Select("id", "status").Where("id = ?", runID).Take(&run).Error; err != nil {
		return false, err
	}
	if run.Status != RunStatusRunning {
		return false, nil
	}
	res := tx.WithContext(ctx).Model(&schema.WorkflowGraphNodeRuns{}).
		Where("id = ? AND active_attempt_id = ? AND status = ?", nodeRunID, attemptID, "running").
		Updates(map[string]any{
			"progress_phase":      phase,
			"progress_updated_at": now,
		})
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected != 1 {
		return false, nil
	}
	var node schema.WorkflowGraphNodeRuns
	if err := tx.WithContext(ctx).Select("graph_run_id", "node_id").Where("id = ?", nodeRunID).Take(&node).Error; err != nil {
		return false, err
	}
	if err := appendGraphRunEventLocked(ctx, tx, node.GraphRunID, "node.progress", &nodeRunID, map[string]any{
		"status": NodeRunRunning, "node_id": node.NodeID, "phase": phase, "attempt_id": attemptID,
	}); err != nil {
		return false, err
	}
	return true, nil
}

// ensureProviderEffectIntent 在打 provider 前写入或复用 workflow_graph_provider_effects。
// 每节点一行：hash/provider 不一致报错；已有非 failed 结果返回 false（禁止再打）；failed 可重开 pending。
// 返回 true 才允许本次调用 provider。operation_key 固定为 graph-node-run:{nodeRunID}，不要改格式。
func ensureProviderEffectIntent(ctx context.Context, tx *gorm.DB, nodeRunID, attemptID, requestHash, providerName string, requestJSON []byte) (bool, error) {
	var node schema.WorkflowGraphNodeRuns
	err := tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Where("id = ?", nodeRunID).Take(&node).Error
	if err != nil {
		return false, err
	}
	if node.Status != NodeRunRunning || node.ActiveAttemptID == nil || *node.ActiveAttemptID != attemptID {
		return false, nil
	}
	var existing schema.WorkflowGraphProviderEffects
	err = tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Where("node_run_id = ?", nodeRunID).Take(&existing).Error
	opKey := "graph-node-run:" + nodeRunID
	if errors.Is(err, gorm.ErrRecordNotFound) {
		id := clockid.New()
		now := time.Now().UTC()
		req := string(requestJSON)
		err = tx.WithContext(ctx).Create(&schema.WorkflowGraphProviderEffects{
			ID:                  id,
			NodeRunID:           nodeRunID,
			OperationKey:        opKey,
			EffectKind:          graphProviderEffectKind,
			RequestHash:         requestHash,
			ProviderName:        providerName,
			AttemptID:           attemptID,
			EffectResult:        "pending",
			ReconciliationState: "not_requested",
			RequestJSON:         &req,
			CreatedAt:           now,
			UpdatedAt:           now,
		}).Error
		return err == nil, err
	}
	if err != nil {
		return false, err
	}
	if existing.OperationKey != opKey || existing.RequestHash != requestHash || existing.ProviderName != providerName {
		return false, errors.New("图运行 provider effect operation identity 与请求不一致")
	}
	if existing.EffectResult != "failed" {
		return false, nil
	}
	now := time.Now().UTC()
	req := string(requestJSON)
	err = tx.WithContext(ctx).Model(&schema.WorkflowGraphProviderEffects{}).Where("id = ?", existing.ID).Updates(map[string]any{
		"attempt_id":           attemptID,
		"effect_result":        "pending",
		"reconciliation_state": "not_requested",
		"provider_response_id": nil,
		"provider_status":      nil,
		"request_json":         req,
		"result_json":          nil,
		"detail":               nil,
		"updated_at":           now,
	}).Error
	return err == nil, err
}

// recordProviderEffectResult 把 provider 成功结果写成 applied。unknown 行不覆盖（返回 false）；
// 已 failed 返回 true 让调用方当已收口。attempt 不匹配返回 false。不要用它标 unknown。
func recordProviderEffectResult(ctx context.Context, tx *gorm.DB, nodeRunID, attemptID string, resultJSON map[string]any) (bool, error) {
	var existing schema.WorkflowGraphProviderEffects
	err := tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Where("node_run_id = ?", nodeRunID).Take(&existing).Error
	if err != nil || existing.AttemptID != attemptID {
		return false, err
	}
	if existing.EffectResult == "failed" {
		return true, nil
	}
	if existing.EffectResult == "unknown" {
		return false, nil
	}
	raw, _ := json.Marshal(resultJSON)
	now := time.Now().UTC()
	var responseID, providerStatus *string
	if v, ok := resultJSON["response_id"].(string); ok {
		responseID = &v
	}
	if v, ok := resultJSON["provider_status"].(string); ok {
		providerStatus = &v
	}
	rawStr := string(raw)
	err = tx.WithContext(ctx).Model(&schema.WorkflowGraphProviderEffects{}).Where("node_run_id = ?", nodeRunID).Updates(map[string]any{
		"effect_result":        "applied",
		"reconciliation_state": "applied",
		"provider_response_id": responseID,
		"provider_status":      providerStatus,
		"result_json":          rawStr,
		"detail":               nil,
		"updated_at":           now,
	}).Error
	return err == nil, err
}
