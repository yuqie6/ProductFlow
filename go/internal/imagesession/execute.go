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

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/canonjson"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
		return e.releaseIdle(ctx, taskID)
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

// ErrUnknown 把无法证明的供应商结果标成 unknown。
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
	var waitingCapacity bool
	err := tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		var row schema.ImageSessionGenerationTasks
		err := pgxTx.Clauses(pfdb.ForUpdate()).Where("id = ?", taskID).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		sessionID = row.SessionID
		if row.Status != "queued" {
			return nil
		}
		ok, err := graph.GenerationCapacityAvailable(ctx, pgxTx)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if !ok {
			if err := pgxTx.Model(&schema.ImageSessionGenerationTasks{}).
				Where("id = ? AND status = ?", taskID, "queued").
				Updates(map[string]any{"progress_phase": "waiting_for_capacity", "progress_updated_at": now}).Error; err != nil {
				return err
			}
			if err := notifyTaskSession(ctx, pgxTx, taskID); err != nil {
				return err
			}
			waitingCapacity = true
			return nil
		}
		attemptID = clockid.New()
		res := pgxTx.Model(&schema.ImageSessionGenerationTasks{}).
			Where("id = ? AND status = ? AND active_attempt_id IS NULL", taskID, "queued").
			Updates(map[string]any{
				"status":                   "running",
				"active_attempt_id":        attemptID,
				"started_at":               now,
				"finished_at":              nil,
				"failure_reason":           nil,
				"progress_phase":           "running",
				"progress_updated_at":      now,
				"active_candidate_index":   nil,
				"provider_response_id":     nil,
				"provider_response_status": nil,
				"progress_metadata":        nil,
				"attempts":                 gorm.Expr("attempts + 1"),
			})
		if res.Error != nil {
			return res.Error
		}
		claimed = res.RowsAffected == 1
		if claimed {
			return notifyTaskSession(ctx, pgxTx, taskID)
		}
		return nil
	})
	if waitingCapacity {
		return false, "", sessionID, errWaitingCapacity
	}
	return claimed, attemptID, sessionID, err
}

// releaseIdle 在 claim 不到 queued 行时决定信封命运：queued/running 表示别人持有，返回 ErrBusy；终态或缺行返回 nil，Consume 标 CONSUMED。
func (e Executor) releaseIdle(ctx context.Context, taskID string) error {
	var row schema.ImageSessionGenerationTasks
	err := e.DB.WithContext(ctx).Where("id = ?", taskID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if row.Status == "queued" || row.Status == "running" {
		return queue.ErrBusy
	}
	return nil
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
			if err := pgxTx.Model(&schema.ImageSessionRounds{}).
				Where("session_id = ? AND generation_group_id = ?", sessionID, groupID).
				Select("COALESCE(MAX(candidate_index), 0)").
				Scan(&saved).Error; err != nil {
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
		effectResult, appliedCount, err := e.ensureEffect(ctx, taskID, attemptID, candidate, batch, opKey, hash, prov.Name(), reqJSON)
		if err != nil {
			return err
		}
		if effectResult == "applied" {
			skip := appliedCount
			if skip < 1 {
				skip = 1
			}
			last := candidate + skip - 1
			if err := e.acknowledgeAppliedCandidate(ctx, taskID, attemptID, groupID, last); err != nil {
				return err
			}
			completed = last
			candidate += skip
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
			if IsUncertainProviderFailure(genErr) {
				_ = e.markEffect(ctx, taskID, candidate, "unknown", unknownDetail)
				return unknownErr{}
			}
			if IsRetryableProviderFailure(genErr) || IsConfirmedProviderFailure(genErr) {
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
		if len(images) != batch {
			_ = e.markEffect(ctx, taskID, candidate, "unknown", unknownDetail)
			return unknownErr{}
		}
		for i, data := range images {
			one := result
			one.Bytes = data
			if err := e.saveCandidate(ctx, sessionID, taskID, attemptID, groupID, candidate+i, count, prompt, size, baseID, refs, one); err != nil {
				if errors.Is(err, errCancelled) || errors.Is(err, errStale) {
					return err
				}
				_ = e.markEffect(ctx, taskID, candidate, "unknown", unknownDetail)
				return unknownErr{}
			}
		}
		if err := e.markEffect(ctx, taskID, candidate, "applied", ""); err != nil {
			return err
		}
		completed = candidate + batch - 1
		candidate += batch
	}
	return e.finishSucceeded(ctx, taskID, attemptID, groupID)
}

func (e Executor) raiseIfCancelled(ctx context.Context, taskID, attemptID string) error {
	var row schema.ImageSessionGenerationTasks
	err := e.DB.WithContext(ctx).Where("id = ?", taskID).Take(&row).Error
	if err != nil {
		return err
	}
	if row.Status == "cancelled" {
		return errCancelled
	}
	if row.ActiveAttemptID == nil || *row.ActiveAttemptID != attemptID {
		return errStale
	}
	return nil
}

func (e Executor) ensureEffect(ctx context.Context, taskID, attemptID string, start, count int, opKey, hash, provider string, req map[string]any) (string, int, error) {
	raw, _ := json.Marshal(req)
	var result string
	var storedCount int
	err := tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		var task schema.ImageSessionGenerationTasks
		if err := pgxTx.Clauses(pfdb.ForUpdate()).Where("id = ?", taskID).Take(&task).Error; err != nil {
			return errStale
		}
		if task.Status != "running" || task.ActiveAttemptID == nil || *task.ActiveAttemptID != attemptID {
			return errStale
		}
		if count < 1 {
			count = 1
		}
		now := time.Now().UTC()
		reqStr := string(raw)
		row := schema.ImageSessionProviderEffects{
			ID: clockid.New(), GenerationTaskID: taskID, CandidateStartIndex: start, CandidateCount: count,
			OperationKey: opKey, EffectKind: effectKind, RequestHash: hash, ProviderName: provider,
			AttemptID: attemptID, EffectResult: "pending", ReconciliationState: "not_requested",
			RequestJSON: &reqStr, CreatedAt: now, UpdatedAt: now,
		}
		if err := pgxTx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "generation_task_id"}, {Name: "candidate_start_index"}},
			DoUpdates: clause.Assignments(map[string]any{
				"attempt_id":      attemptID,
				"candidate_count": count,
				"operation_key":   opKey,
				"request_json":    reqStr,
				"updated_at":      now,
			}),
			Where: clause.Where{Exprs: []clause.Expression{
				clause.Expr{SQL: "image_session_provider_effects.effect_result IN (?, ?)", Vars: []any{"pending", "failed"}},
			}},
		}).Create(&row).Error; err != nil {
			return err
		}
		var stored schema.ImageSessionProviderEffects
		if err := pgxTx.Where("generation_task_id = ? AND candidate_start_index = ?", taskID, start).Take(&stored).Error; err != nil {
			return err
		}
		result = stored.EffectResult
		storedCount = stored.CandidateCount
		return nil
	})
	return result, storedCount, err
}

func (e Executor) markEffect(ctx context.Context, taskID string, start int, result, detail string) error {
	return tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		now := time.Now().UTC()
		var detailPtr *string
		if detail != "" {
			detailPtr = &detail
		}
		updates := map[string]any{
			"effect_result": result,
			"detail":        detailPtr,
			"updated_at":    now,
		}
		if result == "applied" {
			updates["reconciliation_state"] = "applied"
		}
		return pgxTx.Model(&schema.ImageSessionProviderEffects{}).
			Where("generation_task_id = ? AND candidate_start_index = ?", taskID, start).
			Updates(updates).Error
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
		now := time.Now().UTC()
		asset := schema.ImageSessionAssets{
			ID: assetID, SessionID: sessionID, Kind: kindGenerated,
			OriginalFilename: filename, MIMEType: obj.MIMEType, StoragePath: obj.StoragePath,
			MediaObjectID: obj.ID, CreatedAt: now,
		}
		if err := pgxTx.Create(&asset).Error; err != nil {
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
		var respPtr *string
		if result.ResponseID != "" {
			respID := result.ResponseID
			respPtr = &respID
		}
		promptVersion := promptVersionFor(result, e.provider().Name())
		refsStr := string(refJSON)
		outStr := string(outJSON)
		round := schema.ImageSessionRounds{
			ID: roundID, SessionID: sessionID, Prompt: prompt, AssistantMessage: defaultAssistant,
			Size: size, ModelName: result.Model, ProviderName: e.provider().Name(), PromptVersion: promptVersion,
			GeneratedAssetID: assetID, CreatedAt: now, ProviderResponseID: respPtr, GenerationGroupID: &groupID,
			CandidateIndex: index, CandidateCount: count, BaseAssetID: baseID,
			SelectedReferenceAssetIds: &refsStr, ProviderOutputJSON: &outStr,
		}
		if err := pgxTx.Create(&round).Error; err != nil {
			return err
		}
		title := prompt
		if len([]rune(title)) > 40 {
			title = string([]rune(title)[:40])
		}
		_ = pgxTx.Model(&schema.ImageSessions{}).Where("id = ?", sessionID).Updates(map[string]any{
			"title":      gorm.Expr("CASE WHEN title = ? THEN ? ELSE title END", defaultTitle, title),
			"updated_at": now,
		}).Error
		var statusPtr *string
		if result.ProviderStatus != "" {
			s := result.ProviderStatus
			statusPtr = &s
		}
		if err := pgxTx.Model(&schema.ImageSessionGenerationTasks{}).
			Where("id = ? AND active_attempt_id = ?", taskID, attemptID).
			Updates(map[string]any{
				"completed_candidates":       index,
				"active_candidate_index":     nil,
				"progress_phase":             "candidate_saved",
				"progress_updated_at":        now,
				"result_generation_group_id": groupID,
				"provider_response_status":   statusPtr,
			}).Error; err != nil {
			return err
		}
		return publishSession(ctx, pgxTx, sessionID)
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
		now := time.Now().UTC()
		if err := pgxTx.Model(&schema.ImageSessionGenerationTasks{}).
			Where("id = ? AND active_attempt_id = ? AND status = ?", taskID, attemptID, "running").
			Updates(map[string]any{
				"status":                     "succeeded",
				"active_attempt_id":          nil,
				"finished_at":                now,
				"is_retryable":               false,
				"progress_phase":             "succeeded",
				"progress_updated_at":        now,
				"result_generation_group_id": groupID,
			}).Error; err != nil {
			return err
		}
		return notifyTaskSession(ctx, pgxTx, taskID)
	})
}

func (e Executor) finishUnknown(ctx context.Context, taskID, attemptID string) error {
	return tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		now := time.Now().UTC()
		if err := pgxTx.Model(&schema.ImageSessionGenerationTasks{}).
			Where("id = ? AND active_attempt_id = ? AND status = ?", taskID, attemptID, "running").
			Updates(map[string]any{
				"status":              "unknown",
				"active_attempt_id":   nil,
				"finished_at":         now,
				"is_retryable":        false,
				"failure_reason":      unknownDetail,
				"progress_phase":      unknownPhase,
				"progress_updated_at": now,
			}).Error; err != nil {
			return err
		}
		return notifyTaskSession(ctx, pgxTx, taskID)
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
		var task schema.ImageSessionGenerationTasks
		_ = pgxTx.Where("id = ?", taskID).Take(&task).Error
		now := time.Now().UTC()
		if !noRetry && task.Attempts < maxAttempts {
			if err := pgxTx.Model(&schema.ImageSessionGenerationTasks{}).
				Where("id = ? AND active_attempt_id = ? AND status = ?", taskID, attemptID, "running").
				Updates(map[string]any{
					"status":              "queued",
					"active_attempt_id":   nil,
					"failure_reason":      nil,
					"started_at":          nil,
					"finished_at":         nil,
					"progress_phase":      "auto_retry_queued",
					"progress_updated_at": now,
					"is_retryable":        true,
				}).Error; err != nil {
				return err
			}
			_, err := queue.Requeue(ctx, pgxTx, queue.DeliveryKey(queue.ActorImageSession, taskID), queue.ActorImageSession, taskID, nil, nil, false)
			if err != nil {
				return err
			}
			return notifyTaskSession(ctx, pgxTx, taskID)
		}
		if err := pgxTx.Model(&schema.ImageSessionGenerationTasks{}).
			Where("id = ? AND active_attempt_id = ? AND status = ?", taskID, attemptID, "running").
			Updates(map[string]any{
				"status":              "failed",
				"active_attempt_id":   nil,
				"finished_at":         now,
				"failure_reason":      reason,
				"progress_phase":      "failed",
				"progress_updated_at": now,
				"is_retryable":        !noRetry,
			}).Error; err != nil {
			return err
		}
		return notifyTaskSession(ctx, pgxTx, taskID)
	})
}

func isNonRetryableGenerationError(err error) bool {
	if err == nil {
		return false
	}
	if IsRetryableProviderFailure(err) {
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
		now := time.Now().UTC()
		meta := string(candidateProgressJSON(candidate, count))
		if err := pgxTx.Model(&schema.ImageSessionGenerationTasks{}).
			Where("id = ? AND active_attempt_id = ? AND status = ?", taskID, attemptID, "running").
			Updates(map[string]any{
				"completed_candidates":   completed,
				"active_candidate_index": candidate,
				"progress_phase":         "candidate_started",
				"progress_updated_at":    now,
				"progress_metadata":      meta,
			}).Error; err != nil {
			return err
		}
		return notifyTaskSession(ctx, pgxTx, taskID)
	})
}

func candidateProgressJSON(candidate, count int) []byte {
	raw, _ := json.Marshal(map[string]any{"candidate_index": candidate, "candidate_count": count})
	return raw
}

func (e Executor) acknowledgeAppliedCandidate(ctx context.Context, taskID, attemptID, groupID string, candidate int) error {
	return tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		now := time.Now().UTC()
		if err := pgxTx.Model(&schema.ImageSessionGenerationTasks{}).
			Where("id = ? AND active_attempt_id = ? AND status = ?", taskID, attemptID, "running").
			Updates(map[string]any{
				"completed_candidates":       gorm.Expr("GREATEST(completed_candidates, ?)", candidate),
				"active_candidate_index":     nil,
				"progress_phase":             "candidate_saved",
				"progress_updated_at":        now,
				"result_generation_group_id": gorm.Expr("COALESCE(result_generation_group_id, ?)", groupID),
			}).Error; err != nil {
			return err
		}
		return notifyTaskSession(ctx, pgxTx, taskID)
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
