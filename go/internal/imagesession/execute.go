package imagesession

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	sqldb "database/sql"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/canonjson"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

var sessionTaskLocks sync.Map

type Executor struct {
	DB       *gorm.DB
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
		return queue.ErrBusy
	}
	defer unlock()

	claimed, attemptID, sessionID, err := e.claim(ctx, taskID)
	if errors.Is(err, errWaitingCapacity) {
		return queue.ErrLater
	}
	if err != nil {
		return err
	}
	if !claimed {
		return queue.ErrBusy
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

// ErrUnknown 把超时或 5xx 等无法证明的供应商结果标成 unknown。
func ErrUnknown() error { return unknownErr{} }

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
	err := tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		var status string
		err := pfdb.QueryRow(ctx, pgxTx, `
			SELECT session_id, status FROM image_session_generation_tasks WHERE id = $1 FOR UPDATE
		`, taskID).Scan(&sessionID, &status)
		if errors.Is(err, sqldb.ErrNoRows) {
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
			_, _ = pfdb.Exec(ctx, pgxTx, `
				UPDATE image_session_generation_tasks SET progress_phase = 'waiting_for_capacity', progress_updated_at = NOW()
				WHERE id = $1 AND status = 'queued'
			`, taskID)
			return errWaitingCapacity
		}
		attemptID = clockid.New()
		n, err := pfdb.Exec(ctx, pgxTx, `
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
		claimed = n == 1
		return nil
	})
	return claimed, attemptID, sessionID, err
}

func (e Executor) runGeneration(ctx context.Context, taskID, attemptID, sessionID string) error {
	var prompt, size string
	var baseID *string
	var refs []string
	var count, completed int
	var toolOpts map[string]any
	groupID := ""
	err := tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
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
		completed = task.CompletedCandidates
		if completed < 0 {
			completed = 0
		}
		if completed > count {
			completed = count
		}
		if task.ResultGenerationGroupID != nil && *task.ResultGenerationGroupID != "" {
			groupID = *task.ResultGenerationGroupID
			var saved int
			if err := pfdb.QueryRow(ctx, pgxTx, `
				SELECT COALESCE(MAX(candidate_index), 0) FROM image_session_rounds
				WHERE session_id = $1 AND generation_group_id = $2
			`, sessionID, groupID).Scan(&saved); err != nil {
				return err
			}
			if saved > completed {
				completed = saved
			}
			if completed > count {
				completed = count
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if groupID == "" {
		groupID = clockid.New()
	}
	if completed >= count {
		return e.finishSucceeded(ctx, taskID, attemptID, groupID)
	}

	chatCtx, err := e.loadChatContext(ctx, sessionID, baseID, refs)
	if err != nil {
		return err
	}

	prov := e.provider()
	for candidate := completed + 1; candidate <= count; {
		if err := e.raiseIfCancelled(ctx, taskID, attemptID); err != nil {
			return err
		}
		batch := 1
		// Python openai_images 一次最多 n=10；Go 落库名是 hyphen 形式 openai-images。
		if prov.Name() == "openai-images" {
			remaining := count - candidate + 1
			if remaining > 10 {
				remaining = 10
			}
			batch = remaining
		}
		if err := e.markCandidateStarted(ctx, taskID, attemptID, completed, candidate, count); err != nil {
			return err
		}
		reqJSON := map[string]any{
			"prompt": prompt, "size": size, "candidate_start_index": candidate, "candidate_count": batch,
			"base_asset_id": baseID, "selected_reference_asset_ids": refs, "tool_options": toolOpts,
			"provider": prov.Name(), "previous_response_id": nil,
		}
		hash, err := canonjson.SHA256Hex(reqJSON)
		if err != nil {
			return unknownErr{}
		}
		opKey := fmt.Sprintf("image-session-task:%s:candidates:%d-%d", taskID, candidate, batch)
		effectResult, err := e.ensureEffect(ctx, taskID, attemptID, candidate, batch, opKey, hash, prov.Name(), reqJSON)
		if err != nil {
			return err
		}
		if effectResult == "applied" {
			if err := e.acknowledgeAppliedCandidate(ctx, taskID, attemptID, groupID, candidate); err != nil {
				return err
			}
			completed = candidate
			candidate++
			continue
		}
		result, genErr := prov.Generate(ctx, ChatRequest{
			Prompt: prompt, Size: size, Count: batch, ToolOptions: toolOpts,
			BaseBytes: chatCtx.BaseBytes, ReferenceBytes: chatCtx.ReferenceBytes,
		})
		if genErr != nil {
			var ae apperr.Error
			if errors.As(genErr, &ae) && ae.Status == 400 {
				_ = e.markEffect(ctx, taskID, candidate, "failed", ae.Detail)
				return genErr
			}
			if IsConfirmedProviderFailure(genErr) {
				_ = e.markEffect(ctx, taskID, candidate, "failed", genErr.Error())
				return genErr
			}
			_ = e.markEffect(ctx, taskID, candidate, "unknown", unknownDetail)
			return unknownErr{}
		}
		images := result.Images
		if len(images) == 0 && len(result.Bytes) > 0 {
			images = [][]byte{result.Bytes}
		}
		for i, data := range images {
			one := result
			one.Bytes = data
			if err := e.saveCandidate(ctx, sessionID, taskID, attemptID, groupID, candidate+i, count, prompt, size, baseID, refs, one); err != nil {
				return err
			}
		}
		completed = candidate + len(images) - 1
		candidate += len(images)
	}
	return e.finishSucceeded(ctx, taskID, attemptID, groupID)
}

func (e Executor) raiseIfCancelled(ctx context.Context, taskID, attemptID string) error {
	var status string
	var active *string
	err := pfdb.QueryRow(ctx, e.DB, `SELECT status, active_attempt_id FROM image_session_generation_tasks WHERE id = $1`, taskID).Scan(&status, &active)
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

func (e Executor) ensureEffect(ctx context.Context, taskID, attemptID string, start, count int, opKey, hash, provider string, req map[string]any) (string, error) {
	raw, _ := json.Marshal(req)
	var result string
	err := tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		var status string
		var active *string
		if err := pfdb.QueryRow(ctx, pgxTx, `SELECT status, active_attempt_id FROM image_session_generation_tasks WHERE id = $1 FOR UPDATE`, taskID).Scan(&status, &active); err != nil {
			return errStale
		}
		if status != "running" || active == nil || *active != attemptID {
			return errStale
		}
		if count < 1 {
			count = 1
		}
		// candidate_count 必须是本批实际 n，不能写死 1，否则 reconcile/operation_key 对不上 Images 批次。
		if _, err := pfdb.Exec(ctx, pgxTx, `
			INSERT INTO image_session_provider_effects (
				id, generation_task_id, candidate_start_index, candidate_count, operation_key, effect_kind,
				request_hash, provider_name, attempt_id, effect_result, reconciliation_state, request_json, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'pending', 'not_requested', $10, NOW(), NOW())
			ON CONFLICT (generation_task_id, candidate_start_index) DO UPDATE SET
				attempt_id = EXCLUDED.attempt_id,
				candidate_count = EXCLUDED.candidate_count,
				operation_key = EXCLUDED.operation_key,
				request_json = EXCLUDED.request_json,
				updated_at = NOW()
			WHERE image_session_provider_effects.effect_result IN ('pending', 'failed')
		`, clockid.New(), taskID, start, count, opKey, effectKind, hash, provider, attemptID, raw); err != nil {
			return err
		}
		return pfdb.QueryRow(ctx, pgxTx, `
			SELECT effect_result FROM image_session_provider_effects
			WHERE generation_task_id = $1 AND candidate_start_index = $2
		`, taskID, start).Scan(&result)
	})
	return result, err
}

func (e Executor) markEffect(ctx context.Context, taskID string, start int, result, detail string) error {
	return tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		_, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE image_session_provider_effects SET
				effect_result = $3, detail = NULLIF($4, ''), updated_at = NOW()
			WHERE generation_task_id = $1 AND candidate_start_index = $2
		`, taskID, start, result, detail)
		return err
	})
}

func (e Executor) saveCandidate(ctx context.Context, sessionID, taskID, attemptID, groupID string, index, count int, prompt, size string, baseID *string, refs []string, result ChatResult) error {
	var compensation storage.Compensation
	err := tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		if err := e.raiseIfCancelled(ctx, taskID, attemptID); err != nil {
			return err
		}
		obj, err := e.Media.Stage(ctx, pgxTx, result.Bytes, result.MIME, &compensation)
		if err != nil {
			return err
		}
		filename := fmt.Sprintf("generated-%s-%d%s", time.Now().UTC().Format("20060102-150405"), index, media.ExtensionForMIME(result.MIME))
		assetID := clockid.New()
		if _, err := pfdb.Exec(ctx, pgxTx, `
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
		promptVersion := promptVersionFor(result, e.provider().Name())
		if _, err := pfdb.Exec(ctx, pgxTx, `
			INSERT INTO image_session_rounds (
				id, session_id, prompt, assistant_message, size, model_name, provider_name, prompt_version,
				provider_response_id, generation_group_id, candidate_index, candidate_count, base_asset_id,
				selected_reference_asset_ids, generated_asset_id, provider_output_json, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, NOW())
		`, roundID, sessionID, prompt, defaultAssistant, size, result.Model, e.provider().Name(), promptVersion,
			respAny, groupID, index, count, baseID, refJSON, assetID, outJSON); err != nil {
			return err
		}
		title := prompt
		if len([]rune(title)) > 40 {
			title = string([]rune(title)[:40])
		}
		_, _ = pfdb.Exec(ctx, pgxTx, `
			UPDATE image_sessions SET
				title = CASE WHEN title = $2 THEN $3 ELSE title END,
				updated_at = NOW()
			WHERE id = $1
		`, sessionID, defaultTitle, title)
		_, err = pfdb.Exec(ctx, pgxTx, `
			UPDATE image_session_generation_tasks SET
				completed_candidates = $2, active_candidate_index = NULL,
				progress_phase = 'candidate_saved', progress_updated_at = NOW(),
				result_generation_group_id = $3, provider_response_status = $4
			WHERE id = $1 AND active_attempt_id = $5
		`, taskID, index, groupID, result.ProviderStatus, attemptID)
		if err != nil {
			return err
		}
		_, err = pfdb.Exec(ctx, pgxTx, `
			UPDATE image_session_provider_effects SET
				effect_result = 'applied', reconciliation_state = 'applied', detail = NULL, updated_at = NOW()
			WHERE generation_task_id = $1 AND candidate_start_index = $2
		`, taskID, index)
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
	return tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		_, err := pfdb.Exec(ctx, pgxTx, `
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
	return tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		_, err := pfdb.Exec(ctx, pgxTx, `
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
	noRetry := isNonRetryableGenerationError(cause)
	_ = tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		var attempts int
		_ = pfdb.QueryRow(ctx, pgxTx, `SELECT attempts FROM image_session_generation_tasks WHERE id = $1`, taskID).Scan(&attempts)
		if !noRetry && attempts < maxAttempts {
			_, err := pfdb.Exec(ctx, pgxTx, `
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
		_, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE image_session_generation_tasks SET
				status = 'failed', active_attempt_id = NULL, finished_at = NOW(),
				failure_reason = $3, progress_phase = 'failed', progress_updated_at = NOW(), is_retryable = $4
			WHERE id = $1 AND active_attempt_id = $2 AND status = 'running'
		`, taskID, attemptID, reason, !noRetry)
		return err
	})
}

func isNonRetryableGenerationError(err error) bool {
	if err == nil {
		return false
	}
	if IsConfirmedProviderFailure(err) {
		return true
	}
	var ae apperr.Error
	return errors.As(err, &ae) && ae.Status == 400
}

func promptVersionFor(result ChatResult, providerName string) string {
	version := strings.TrimSpace(result.PromptVersion)
	if version == "" {
		version = strings.TrimSpace(result.Model)
	}
	if version == "" {
		version = strings.TrimSpace(providerName)
	}
	if version == "" {
		version = "image-v1"
	}
	if len(version) > 32 {
		return version[:32]
	}
	return version
}

func (e Executor) markCandidateStarted(ctx context.Context, taskID, attemptID string, completed, candidate, count int) error {
	return tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		_, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE image_session_generation_tasks SET
				completed_candidates = $2, active_candidate_index = $3,
				progress_phase = 'candidate_started', progress_updated_at = NOW(),
				progress_metadata = $4
			WHERE id = $1 AND active_attempt_id = $5 AND status = 'running'
		`, taskID, completed, candidate, string(candidateProgressJSON(candidate, count)), attemptID)
		return err
	})
}

func candidateProgressJSON(candidate, count int) []byte {
	raw, _ := json.Marshal(map[string]any{"candidate_index": candidate, "candidate_count": count})
	return raw
}

func (e Executor) acknowledgeAppliedCandidate(ctx context.Context, taskID, attemptID, groupID string, candidate int) error {
	return tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		_, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE image_session_generation_tasks SET
				completed_candidates = GREATEST(completed_candidates, $2),
				active_candidate_index = NULL,
				progress_phase = 'candidate_saved', progress_updated_at = NOW(),
				result_generation_group_id = COALESCE(result_generation_group_id, $3)
			WHERE id = $1 AND active_attempt_id = $4 AND status = 'running'
		`, taskID, candidate, groupID, attemptID)
		return err
	})
}

type chatContext struct {
	BaseBytes      []byte
	ReferenceBytes [][]byte
}

func (e Executor) loadChatContext(ctx context.Context, sessionID string, baseID *string, refIDs []string) (chatContext, error) {
	out := chatContext{}
	if baseID != nil && strings.TrimSpace(*baseID) != "" {
		asset, err := loadAsset(ctx, e.DB, sessionID, *baseID)
		if err != nil {
			return chatContext{}, err
		}
		bytesData, err := readStoredFile(e.Media.Files, asset.StoragePath)
		if err != nil {
			return chatContext{}, err
		}
		out.BaseBytes = bytesData
	}
	for _, id := range refIDs {
		if strings.TrimSpace(id) == "" {
			continue
		}
		asset, err := loadAsset(ctx, e.DB, sessionID, id)
		if err != nil {
			return chatContext{}, err
		}
		bytesData, err := readStoredFile(e.Media.Files, asset.StoragePath)
		if err != nil {
			return chatContext{}, err
		}
		out.ReferenceBytes = append(out.ReferenceBytes, bytesData)
	}
	return out, nil
}

func readStoredFile(files storage.Local, rel string) ([]byte, error) {
	abs, err := files.Resolve(rel)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(abs)
}
