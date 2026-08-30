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

func markNodeUnknown(ctx context.Context, tx *gorm.DB, runID, nodeRunID string, attemptID *string, detail string) error {
	if len(detail) > 1000 {
		detail = detail[:1000]
	}
	now := time.Now().UTC()
	var run schema.WorkflowGraphRuns
	if err := tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Where("id = ?", runID).Take(&run).Error; err != nil {
		return err
	}
	if run.Status == RunStatusSucceeded || run.Status == RunStatusCancelled {
		return nil
	}
	var node schema.WorkflowGraphNodeRuns
	err := tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).
		Where("id = ? AND graph_run_id = ?", nodeRunID, runID).
		Take(&node).Error
	if err != nil {
		return err
	}
	if node.Status == NodeRunUnknown {
		return nil
	}
	if attemptID != nil && node.ActiveAttemptID != nil && *node.ActiveAttemptID != "" && *node.ActiveAttemptID != *attemptID {
		return nil
	}
	resolved := attemptID
	if resolved == nil {
		resolved = node.ActiveAttemptID
	}
	if resolved != nil && *resolved != "" {
		_ = tx.WithContext(ctx).Model(&schema.WorkflowGraphProviderEffects{}).
			Where("node_run_id = ? AND attempt_id = ? AND effect_result NOT IN ?", nodeRunID, *resolved, []string{"applied", "failed"}).
			Updates(map[string]any{
				"effect_result":        "unknown",
				"reconciliation_state": "unknown",
				"detail":               detail,
				"updated_at":           now,
			}).Error
	}
	return tx.WithContext(ctx).Model(&schema.WorkflowGraphNodeRuns{}).Where("id = ?", nodeRunID).Updates(map[string]any{
		"status":              "unknown",
		"failure_reason":      detail,
		"finished_at":         now,
		"progress_phase":      "unknown_provider_effect",
		"progress_updated_at": now,
		"active_attempt_id":   nil,
	}).Error
}

func advanceNodePhase(ctx context.Context, tx *gorm.DB, nodeRunID, attemptID, phase string) (bool, error) {
	now := time.Now().UTC()
	res := tx.WithContext(ctx).Model(&schema.WorkflowGraphNodeRuns{}).
		Where("id = ? AND active_attempt_id = ? AND status = ?", nodeRunID, attemptID, "running").
		Updates(map[string]any{
			"progress_phase":      phase,
			"progress_updated_at": now,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

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
