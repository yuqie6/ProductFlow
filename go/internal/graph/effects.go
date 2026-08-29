package graph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/yuqie6/productflow/internal/platform/clockid"
)

const (
	maxProviderEffectJSONBytes = 64 * 1024
	graphProviderEffectKind    = "workflow_graph_generation"
)

type providerUnknownError struct{}

func (providerUnknownError) Error() string { return ProviderUnknownDetail }

func isProviderUnknown(err error) bool {
	var u providerUnknownError
	return errors.As(err, &u)
}

func providerEffectHash(payload map[string]any) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", err
	}
	compact, err := marshalCompactSorted(decoded)
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
	switch typed := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		buf := []byte{'{'}
		for i, key := range keys {
			if i > 0 {
				buf = append(buf, ',')
			}
			keyJSON, err := json.Marshal(key)
			if err != nil {
				return nil, err
			}
			valJSON, err := marshalCompactSorted(typed[key])
			if err != nil {
				return nil, err
			}
			buf = append(buf, keyJSON...)
			buf = append(buf, ':')
			buf = append(buf, valJSON...)
		}
		buf = append(buf, '}')
		return buf, nil
	case []any:
		buf := []byte{'['}
		for i, item := range typed {
			if i > 0 {
				buf = append(buf, ',')
			}
			itemJSON, err := marshalCompactSorted(item)
			if err != nil {
				return nil, err
			}
			buf = append(buf, itemJSON...)
		}
		buf = append(buf, ']')
		return buf, nil
	default:
		return json.Marshal(typed)
	}
}

func markNodeUnknown(ctx context.Context, tx pgx.Tx, runID, nodeRunID string, attemptID *string, detail string) error {
	if len(detail) > 1000 {
		detail = detail[:1000]
	}
	now := time.Now().UTC()
	var runStatus string
	if err := tx.QueryRow(ctx, `SELECT status FROM workflow_graph_runs WHERE id = $1 FOR UPDATE`, runID).Scan(&runStatus); err != nil {
		return err
	}
	if runStatus == RunStatusSucceeded || runStatus == RunStatusCancelled {
		return nil
	}
	var status string
	var activeAttempt *string
	err := tx.QueryRow(ctx, `
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
		_, _ = tx.Exec(ctx, `
			UPDATE workflow_graph_provider_effects SET
				effect_result = 'unknown', reconciliation_state = 'unknown', detail = $2, updated_at = $3
			WHERE node_run_id = $1 AND attempt_id = $4
			  AND effect_result NOT IN ('applied', 'failed')
		`, nodeRunID, detail, now, *resolved)
	}
	_, err = tx.Exec(ctx, `
		UPDATE workflow_graph_node_runs SET
			status = 'unknown', failure_reason = $2, finished_at = $3,
			progress_phase = 'unknown_provider_effect', progress_updated_at = $3,
			active_attempt_id = NULL
		WHERE id = $1
	`, nodeRunID, detail, now)
	return err
}

func advanceNodePhase(ctx context.Context, tx pgx.Tx, nodeRunID, attemptID, phase string) (bool, error) {
	now := time.Now().UTC()
	tag, err := tx.Exec(ctx, `
		UPDATE workflow_graph_node_runs SET progress_phase = $3, progress_updated_at = $4
		WHERE id = $1 AND active_attempt_id = $2 AND status = 'running'
	`, nodeRunID, attemptID, phase, now)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func ensureProviderEffectIntent(ctx context.Context, tx pgx.Tx, nodeRunID, attemptID, requestHash, providerName string, requestJSON []byte) (bool, error) {
	var status string
	var active *string
	err := tx.QueryRow(ctx, `
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
	err = tx.QueryRow(ctx, `
		SELECT id, request_hash, provider_name, operation_key, effect_result
		FROM workflow_graph_provider_effects WHERE node_run_id = $1 FOR UPDATE
	`, nodeRunID).Scan(&existingID, &existingHash, &existingProvider, &existingKey, &existingResult)
	opKey := "graph-node-run:" + nodeRunID
	if errors.Is(err, pgx.ErrNoRows) {
		id := clockid.New()
		now := time.Now().UTC()
		_, err = tx.Exec(ctx, `
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
	_, err = tx.Exec(ctx, `
		UPDATE workflow_graph_provider_effects SET
			attempt_id = $2, effect_result = 'pending', reconciliation_state = 'not_requested',
			provider_response_id = NULL, provider_status = NULL, request_json = $3,
			result_json = NULL, detail = NULL, updated_at = $4
		WHERE id = $1
	`, *existingID, attemptID, requestJSON, now)
	return err == nil, err
}

func recordProviderEffectResult(ctx context.Context, tx pgx.Tx, nodeRunID, attemptID string, resultJSON map[string]any) (bool, error) {
	var effectResult, storedAttempt string
	err := tx.QueryRow(ctx, `
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
	_, err = tx.Exec(ctx, `
		UPDATE workflow_graph_provider_effects SET
			effect_result = 'applied', reconciliation_state = 'applied',
			provider_response_id = $2, provider_status = $3, result_json = $4,
			detail = NULL, updated_at = $5
		WHERE node_run_id = $1
	`, nodeRunID, responseID, providerStatus, raw, now)
	return err == nil, err
}
