package imagesession

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/canonjson"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/tx"
)

var sessionTaskLocks sync.Map

type Executor struct {
	Pool     *pgxpool.Pool
	Media    media.Store
	Provider ChatProvider
}

func (e Executor) provider() ChatProvider {
	if e.Provider != nil {
		return e.Provider
	}
	return MockChatProvider{}
}

// Execute 是 worker 入口：queued -> running -> succeeded/failed/unknown。无法证明的 provider 结果标 unknown。
func (e Executor) Execute(ctx context.Context, taskID string) error {
	unlock, ok := tryLock(taskID)
	if !ok {
		return nil
	}
	defer unlock()

	claimed, attemptID, sessionID, err := e.claim(ctx, taskID)
	if errors.Is(err, errWaitingCapacity) {
		return nil
	}
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	if err := e.runGeneration(ctx, taskID, attemptID, sessionID); err != nil {
		if errors.Is(err, errCancelled) || errors.Is(err, errStale) {
			return nil
		}
		if isUnknown(err) {
			if markErr := e.finishUnknown(ctx, taskID, attemptID); markErr != nil {
				return markErr
			}
			return nil
		}
		e.finishFailed(ctx, taskID, attemptID, err)
		return nil
	}
	return nil
}

var (
	errWaitingCapacity = errors.New("waiting_for_capacity")
	errCancelled       = errors.New("cancelled")
	errStale           = errors.New("stale_attempt")
)

type unknownErr struct{}

func (unknownErr) Error() string { return unknownDetail }

func isUnknown(err error) bool {
	var u unknownErr
	return errors.As(err, &u)
}

func tryLock(id string) (func(), bool) {
	_, loaded := sessionTaskLocks.LoadOrStore(id, struct{}{})
	if loaded {
		return nil, false
	}
	return func() { sessionTaskLocks.Delete(id) }, true
}

func (e Executor) claim(ctx context.Context, taskID string) (bool, string, string, error) {
	var claimed bool
	var attemptID, sessionID string
	err := tx.With(ctx, e.Pool, func(pgxTx pgx.Tx) error {
		var status string
		err := pgxTx.QueryRow(ctx, `
			SELECT session_id, status FROM image_session_generation_tasks WHERE id = $1 FOR UPDATE
		`, taskID).Scan(&sessionID, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if status != "queued" {
			return nil
		}
		ok, err := graph.GenerationCapacityAvailable(ctx, pgxTx)
		if err != nil {
			return err
		}
		if !ok {
			_, _ = pgxTx.Exec(ctx, `
				UPDATE image_session_generation_tasks SET progress_phase = 'waiting_for_capacity', progress_updated_at = NOW()
				WHERE id = $1 AND status = 'queued'
			`, taskID)
			return errWaitingCapacity
		}
		attemptID = clockid.New()
		tag, err := pgxTx.Exec(ctx, `
			UPDATE image_session_generation_tasks SET
				status = 'running', active_attempt_id = $2, started_at = NOW(), finished_at = NULL,
				failure_reason = NULL, progress_phase = 'running', progress_updated_at = NOW(),
				active_candidate_index = NULL, provider_response_id = NULL, provider_response_status = NULL,
				progress_metadata = NULL, attempts = attempts + 1
			WHERE id = $1 AND status = 'queued' AND active_attempt_id IS NULL
		`, taskID, attemptID)
		if err != nil {
			return err
		}
		claimed = tag.RowsAffected() == 1
		return nil
	})
	return claimed, attemptID, sessionID, err
}

func (e Executor) runGeneration(ctx context.Context, taskID, attemptID, sessionID string) error {
	var prompt, size string
	var baseID *string
	var refs []string
	var count int
	var toolOpts map[string]any
	err := tx.With(ctx, e.Pool, func(pgxTx pgx.Tx) error {
		task, err := loadTask(ctx, pgxTx, sessionID, taskID)
		if err != nil {
			return err
		}
		if task.Status != "running" || task.ActiveAttemptID == nil || *task.ActiveAttemptID != attemptID {
			return errStale
		}
		prompt, size, baseID, count = task.Prompt, task.Size, task.BaseAssetID, task.GenerationCount
		refs = decodeStringSlice(task.SelectedRefs)
		toolOpts = decodeMap(task.ToolOptions)
		return nil
	})
	if err != nil {
		return err
	}

	completed := 0
	groupID := clockid.New()
	prov := e.provider()
	for candidate := completed + 1; candidate <= count; candidate++ {
		if err := e.raiseIfCancelled(ctx, taskID, attemptID); err != nil {
			return err
		}
		reqJSON := map[string]any{
			"prompt": prompt, "size": size, "candidate_start_index": candidate, "candidate_count": 1,
			"base_asset_id": baseID, "selected_reference_asset_ids": refs, "tool_options": toolOpts,
			"provider": prov.Name(),
		}
		hash, err := canonjson.SHA256Hex(reqJSON)
		if err != nil {
			return unknownErr{}
		}
		opKey := fmt.Sprintf("image-session-task:%s:candidates:%d-1", taskID, candidate)
		if err := e.ensureEffect(ctx, taskID, attemptID, candidate, opKey, hash, prov.Name(), reqJSON); err != nil {
			return err
		}
		result, genErr := prov.Generate(ctx, ChatRequest{Prompt: prompt, Size: size, ToolOptions: toolOpts})
		if genErr != nil {
			var ae apperr.Error
			if errors.As(genErr, &ae) && ae.Status == 400 {
				_ = e.markEffect(ctx, taskID, candidate, "failed", ae.Detail)
				return genErr
			}
			_ = e.markEffect(ctx, taskID, candidate, "unknown", unknownDetail)
			return unknownErr{}
		}
		if err := e.markEffect(ctx, taskID, candidate, "applied", ""); err != nil {
			return unknownErr{}
		}
		if err := e.saveCandidate(ctx, sessionID, taskID, attemptID, groupID, candidate, count, prompt, size, baseID, refs, result); err != nil {
			return err
		}
		completed = candidate
	}
	return e.finishSucceeded(ctx, taskID, attemptID, groupID)
}

func (e Executor) raiseIfCancelled(ctx context.Context, taskID, attemptID string) error {
	var status string
	var active *string
	err := e.Pool.QueryRow(ctx, `SELECT status, active_attempt_id FROM image_session_generation_tasks WHERE id = $1`, taskID).Scan(&status, &active)
	if err != nil {
		return err
	}
	if status == "cancelled" {
		return errCancelled
	}
	if active == nil || *active != attemptID {
		return errStale
	}
	return nil
}

func (e Executor) ensureEffect(ctx context.Context, taskID, attemptID string, start int, opKey, hash, provider string, req map[string]any) error {
	raw, _ := json.Marshal(req)
	return tx.With(ctx, e.Pool, func(pgxTx pgx.Tx) error {
		var status string
		var active *string
		if err := pgxTx.QueryRow(ctx, `SELECT status, active_attempt_id FROM image_session_generation_tasks WHERE id = $1 FOR UPDATE`, taskID).Scan(&status, &active); err != nil {
			return errStale
		}
		if status != "running" || active == nil || *active != attemptID {
			return errStale
		}
		_, err := pgxTx.Exec(ctx, `
			INSERT INTO image_session_provider_effects (
				id, generation_task_id, candidate_start_index, candidate_count, operation_key, effect_kind,
				request_hash, provider_name, attempt_id, effect_result, reconciliation_state, request_json, created_at, updated_at
			) VALUES ($1, $2, $3, 1, $4, $5, $6, $7, $8, 'pending', 'not_requested', $9, NOW(), NOW())
			ON CONFLICT (generation_task_id, candidate_start_index) DO UPDATE SET
				attempt_id = EXCLUDED.attempt_id, request_json = EXCLUDED.request_json, updated_at = NOW()
			WHERE image_session_provider_effects.effect_result IN ('pending', 'failed')
		`, clockid.New(), taskID, start, opKey, effectKind, hash, provider, attemptID, raw)
		return err
	})
}

func (e Executor) markEffect(ctx context.Context, taskID string, start int, result, detail string) error {
	return tx.With(ctx, e.Pool, func(pgxTx pgx.Tx) error {
		_, err := pgxTx.Exec(ctx, `
			UPDATE image_session_provider_effects SET
				effect_result = $3, detail = NULLIF($4, ''), updated_at = NOW()
			WHERE generation_task_id = $1 AND candidate_start_index = $2
		`, taskID, start, result, detail)
		return err
	})
}

func (e Executor) saveCandidate(ctx context.Context, sessionID, taskID, attemptID, groupID string, index, count int, prompt, size string, baseID *string, refs []string, result ChatResult) error {
	var compensation storage.Compensation
	err := tx.With(ctx, e.Pool, func(pgxTx pgx.Tx) error {
		if err := e.raiseIfCancelled(ctx, taskID, attemptID); err != nil {
			return err
		}
		obj, err := e.Media.Stage(ctx, pgxTx, result.Bytes, result.MIME, &compensation)
		if err != nil {
			return err
		}
		filename := fmt.Sprintf("generated-%s-%d%s", time.Now().UTC().Format("20060102-150405"), index, media.ExtensionForMIME(result.MIME))
		assetID := clockid.New()
		if _, err := pgxTx.Exec(ctx, `
			INSERT INTO image_session_assets (
				id, session_id, kind, original_filename, mime_type, storage_path, media_object_id, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		`, assetID, sessionID, kindGenerated, filename, obj.MIMEType, obj.StoragePath, obj.ID); err != nil {
			return err
		}
		w, h := parseSize(size)
		if meta, err := media.Inspect(result.Bytes, result.MIME); err == nil {
			w, h = meta.Width, meta.Height
		}
		output := result.OutputJSON
		if output == nil {
			output = map[string]any{}
		}
		pf := map[string]any{}
		if raw, ok := output["_productflow"].(map[string]any); ok {
			pf = raw
		}
		pf["actual_image_size"] = fmt.Sprintf("%dx%d", w, h)
		output["_productflow"] = pf
		outJSON, _ := json.Marshal(output)
		refJSON, _ := json.Marshal(refs)
		roundID := clockid.New()
		respID := result.ResponseID
		var respAny any
		if respID != "" {
			respAny = respID
		}
		if _, err := pgxTx.Exec(ctx, `
			INSERT INTO image_session_rounds (
				id, session_id, prompt, assistant_message, size, model_name, provider_name, prompt_version,
				provider_response_id, generation_group_id, candidate_index, candidate_count, base_asset_id,
				selected_reference_asset_ids, generated_asset_id, provider_output_json, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, 'mock-image-v1', $8, $9, $10, $11, $12, $13, $14, $15, NOW())
		`, roundID, sessionID, prompt, defaultAssistant, size, result.Model, e.provider().Name(),
			respAny, groupID, index, count, baseID, refJSON, assetID, outJSON); err != nil {
			return err
		}
		title := prompt
		if len([]rune(title)) > 40 {
			title = string([]rune(title)[:40])
		}
		_, _ = pgxTx.Exec(ctx, `
			UPDATE image_sessions SET
				title = CASE WHEN title = $2 THEN $3 ELSE title END,
				updated_at = NOW()
			WHERE id = $1
		`, sessionID, defaultTitle, title)
		_, err = pgxTx.Exec(ctx, `
			UPDATE image_session_generation_tasks SET
				completed_candidates = $2, active_candidate_index = $3,
				progress_phase = 'candidate_saved', progress_updated_at = NOW(),
				result_generation_group_id = $4, provider_response_status = $5
			WHERE id = $1 AND active_attempt_id = $6
		`, taskID, index, index, groupID, result.ProviderStatus, attemptID)
		return err
	})
	if err != nil {
		compensation.Rollback()
		return err
	}
	compensation.Release()
	return nil
}

func (e Executor) finishSucceeded(ctx context.Context, taskID, attemptID, groupID string) error {
	return tx.With(ctx, e.Pool, func(pgxTx pgx.Tx) error {
		_, err := pgxTx.Exec(ctx, `
			UPDATE image_session_generation_tasks SET
				status = 'succeeded', active_attempt_id = NULL, finished_at = NOW(),
				is_retryable = FALSE, progress_phase = 'succeeded', progress_updated_at = NOW(),
				result_generation_group_id = $3
			WHERE id = $1 AND active_attempt_id = $2 AND status = 'running'
		`, taskID, attemptID, groupID)
		return err
	})
}

func (e Executor) finishUnknown(ctx context.Context, taskID, attemptID string) error {
	return tx.With(ctx, e.Pool, func(pgxTx pgx.Tx) error {
		_, err := pgxTx.Exec(ctx, `
			UPDATE image_session_generation_tasks SET
				status = 'unknown', active_attempt_id = NULL, finished_at = NOW(),
				is_retryable = FALSE, failure_reason = $3,
				progress_phase = $4, progress_updated_at = NOW()
			WHERE id = $1 AND active_attempt_id = $2 AND status = 'running'
		`, taskID, attemptID, unknownDetail, unknownPhase)
		return err
	})
}

func (e Executor) finishFailed(ctx context.Context, taskID, attemptID string, cause error) {
	reason := genericFailure
	if cause != nil && cause.Error() != "" {
		reason = cause.Error()
		if len(reason) > 1000 {
			reason = reason[:1000]
		}
	}
	_ = tx.With(ctx, e.Pool, func(pgxTx pgx.Tx) error {
		var attempts int
		_ = pgxTx.QueryRow(ctx, `SELECT attempts FROM image_session_generation_tasks WHERE id = $1`, taskID).Scan(&attempts)
		if attempts < maxAttempts {
			_, err := pgxTx.Exec(ctx, `
				UPDATE image_session_generation_tasks SET
					status = 'queued', active_attempt_id = NULL, failure_reason = NULL,
					started_at = NULL, finished_at = NULL, progress_phase = 'auto_retry_queued',
					progress_updated_at = NOW(), is_retryable = TRUE
				WHERE id = $1 AND active_attempt_id = $2 AND status = 'running'
			`, taskID, attemptID)
			if err != nil {
				return err
			}
			_, err = queue.Requeue(ctx, pgxTx, queue.DeliveryKey(queue.ActorImageSession, taskID), queue.ActorImageSession, taskID, nil, nil, false)
			return err
		}
		_, err := pgxTx.Exec(ctx, `
			UPDATE image_session_generation_tasks SET
				status = 'failed', active_attempt_id = NULL, finished_at = NOW(),
				failure_reason = $3, progress_phase = 'failed', progress_updated_at = NOW(), is_retryable = TRUE
			WHERE id = $1 AND active_attempt_id = $2 AND status = 'running'
		`, taskID, attemptID, reason)
		return err
	})
}
