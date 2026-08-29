package graph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	sqldb "database/sql"

	"github.com/yuqie6/productflow/internal/platform/canonjson"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
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
	var runStatus string
	if err := pfdb.QueryRow(ctx, tx, `SELECT status FROM workflow_graph_runs WHERE id = $1 FOR UPDATE`, runID).Scan(&runStatus); err != nil {
		return err
	}
	if runStatus == RunStatusSucceeded || runStatus == RunStatusCancelled {
		return nil
	}
	var status string
	var activeAttempt *string
	err := pfdb.QueryRow(ctx, tx, `
		SELECT status, active_attempt_id FROM workflow_graph_node_runs
		WHERE id = $1 AND graph_run_id = $2 FOR UPDATE
	`, nodeRunID, runID).Scan(&status, &activeAttempt)
	if err != nil {
		return err
	}
	if status == NodeRunUnknown {
		return nil
	}
	if attemptID != nil && activeAttempt != nil && *activeAttempt != "" && *activeAttempt != *attemptID {
		return nil
	}
	resolved := attemptID
	if resolved == nil {
		resolved = activeAttempt
	}
	if resolved != nil && *resolved != "" {
		_, _ = pfdb.Exec(ctx, tx, `
			UPDATE workflow_graph_provider_effects SET
				effect_result = 'unknown', reconciliation_state = 'unknown', detail = $2, updated_at = $3
			WHERE node_run_id = $1 AND attempt_id = $4
			  AND effect_result NOT IN ('applied', 'failed')
		`, nodeRunID, detail, now, *resolved)
	}
	_, err = pfdb.Exec(ctx, tx, `
		UPDATE workflow_graph_node_runs SET
			status = 'unknown', failure_reason = $2, finished_at = $3,
			progress_phase = 'unknown_provider_effect', progress_updated_at = $3,
			active_attempt_id = NULL
		WHERE id = $1
	`, nodeRunID, detail, now)
	return err
}

func markNodeFailed(ctx context.Context, tx *gorm.DB, runID, nodeRunID string, attemptID *string, detail string) error {
	if len(detail) > 1000 {
		detail = detail[:1000]
	}
	now := time.Now().UTC()
	var runStatus string
	if err := pfdb.QueryRow(ctx, tx, `SELECT status FROM workflow_graph_runs WHERE id = $1 FOR UPDATE`, runID).Scan(&runStatus); err != nil {
		return err
	}
	if runStatus == RunStatusSucceeded || runStatus == RunStatusCancelled {
		return nil
	}
	var status string
	var activeAttempt *string
	err := pfdb.QueryRow(ctx, tx, `
		SELECT status, active_attempt_id FROM workflow_graph_node_runs
		WHERE id = $1 AND graph_run_id = $2 FOR UPDATE
	`, nodeRunID, runID).Scan(&status, &activeAttempt)
	if err != nil {
		return err
	}
	if status == NodeRunFailed || status == NodeRunUnknown || status == NodeRunSucceeded {
		return nil
	}
	if attemptID != nil && activeAttempt != nil && *activeAttempt != "" && *activeAttempt != *attemptID {
		return nil
	}
	resolved := attemptID
	if resolved == nil {
		resolved = activeAttempt
	}
	if resolved != nil && *resolved != "" {
		_, _ = pfdb.Exec(ctx, tx, `
			UPDATE workflow_graph_provider_effects SET
				effect_result = 'failed', reconciliation_state = 'failed', detail = $2, updated_at = $3
			WHERE node_run_id = $1 AND attempt_id = $4
			  AND effect_result NOT IN ('applied', 'unknown')
		`, nodeRunID, detail, now, *resolved)
	}
	_, err = pfdb.Exec(ctx, tx, `
		UPDATE workflow_graph_node_runs SET
			status = 'failed', failure_reason = $2, finished_at = $3,
			progress_phase = 'provider_result_received', progress_updated_at = $3,
			active_attempt_id = NULL
		WHERE id = $1
	`, nodeRunID, detail, now)
	return err
}

func advanceNodePhase(ctx context.Context, tx *gorm.DB, nodeRunID, attemptID, phase string) (bool, error) {
	now := time.Now().UTC()
	n, err := pfdb.Exec(ctx, tx, `
		UPDATE workflow_graph_node_runs SET progress_phase = $3, progress_updated_at = $4
		WHERE id = $1 AND active_attempt_id = $2 AND status = 'running'
	`, nodeRunID, attemptID, phase, now)
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func ensureProviderEffectIntent(ctx context.Context, tx *gorm.DB, nodeRunID, attemptID, requestHash, providerName string, requestJSON []byte) (bool, error) {
	var status string
	var active *string
	err := pfdb.QueryRow(ctx, tx, `
		SELECT status, active_attempt_id FROM workflow_graph_node_runs WHERE id = $1 FOR UPDATE
	`, nodeRunID).Scan(&status, &active)
	if err != nil {
		return false, err
	}
	if status != NodeRunRunning || active == nil || *active != attemptID {
		return false, nil
	}
	var existingID *string
	var existingHash, existingProvider, existingKey, existingResult string
	err = pfdb.QueryRow(ctx, tx, `
		SELECT id, request_hash, provider_name, operation_key, effect_result
		FROM workflow_graph_provider_effects WHERE node_run_id = $1 FOR UPDATE
	`, nodeRunID).Scan(&existingID, &existingHash, &existingProvider, &existingKey, &existingResult)
	opKey := "graph-node-run:" + nodeRunID
	if errors.Is(err, sqldb.ErrNoRows) {
		id := clockid.New()
		now := time.Now().UTC()
		_, err = pfdb.Exec(ctx, tx, `
			INSERT INTO workflow_graph_provider_effects (
				id, node_run_id, operation_key, effect_kind, request_hash, provider_name,
				attempt_id, effect_result, reconciliation_state, request_json, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, 'pending', 'not_requested', $8, $9, $9)
		`, id, nodeRunID, opKey, graphProviderEffectKind, requestHash, providerName, attemptID, requestJSON, now)
		return err == nil, err
	}
	if err != nil {
		return false, err
	}
	if existingKey != opKey || existingHash != requestHash || existingProvider != providerName {
		return false, errors.New("图运行 provider effect operation identity 与请求不一致")
	}
	if existingResult != "failed" {
		return false, nil
	}
	now := time.Now().UTC()
	_, err = pfdb.Exec(ctx, tx, `
		UPDATE workflow_graph_provider_effects SET
			attempt_id = $2, effect_result = 'pending', reconciliation_state = 'not_requested',
			provider_response_id = NULL, provider_status = NULL, request_json = $3,
			result_json = NULL, detail = NULL, updated_at = $4
		WHERE id = $1
	`, *existingID, attemptID, requestJSON, now)
	return err == nil, err
}

func recordProviderEffectResult(ctx context.Context, tx *gorm.DB, nodeRunID, attemptID string, resultJSON map[string]any) (bool, error) {
	var effectResult, storedAttempt string
	err := pfdb.QueryRow(ctx, tx, `
		SELECT effect_result, attempt_id FROM workflow_graph_provider_effects
		WHERE node_run_id = $1 FOR UPDATE
	`, nodeRunID).Scan(&effectResult, &storedAttempt)
	if err != nil || storedAttempt != attemptID {
		return false, err
	}
	if effectResult == "failed" {
		return true, nil
	}
	if effectResult == "unknown" {
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
	_, err = pfdb.Exec(ctx, tx, `
		UPDATE workflow_graph_provider_effects SET
			effect_result = 'applied', reconciliation_state = 'applied',
			provider_response_id = $2, provider_status = $3, result_json = $4,
			detail = NULL, updated_at = $5
		WHERE node_run_id = $1
	`, nodeRunID, responseID, providerStatus, raw, now)
	return err == nil, err
}
