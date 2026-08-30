package localedit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"github.com/yuqie6/productflow/internal/product"
	"gorm.io/gorm"
)

type Executor struct {
	DB       *gorm.DB
	Media    media.Store
	Provider Provider
}

func (e Executor) provider() Provider {
	if e.Provider != nil {
		return e.Provider
	}
	return MockProvider{}
}

// Execute 是 worker 入口。无法证明的 provider 结果标 unknown，且返回 nil 以便 dispatch 记 CONSUMED。
func (e Executor) Execute(ctx context.Context, taskID string) error {
	claimed, attemptID, err := e.claim(ctx, taskID)
	if err != nil {
		return err
	}
	if !claimed {
		return e.releaseIdle(ctx, taskID)
	}
	snap, err := e.loadSnapshot(ctx, taskID, attemptID)
	if err != nil {
		detail := "局部编辑媒体读取失败"
		var ae apperr.Error
		if errors.As(err, &ae) && ae.Status == 400 {
			detail = ae.Detail
		}
		e.finish(ctx, taskID, attemptID, "failed", "failed", "failed", detail, false, "", "")
		return nil
	}
	cap := e.provider().Capability()
	if snap.RequestedProvider == nil || snap.RequestedMode == nil ||
		cap.ProviderName != *snap.RequestedProvider || cap.Mode != *snap.RequestedMode {
		e.finish(ctx, taskID, attemptID, "failed", "failed", "unsupported", "提交时记录的图片 provider 能力与当前绑定不一致", false, "capability_mismatch", "")
		return nil
	}
	supportedOp := false
	for _, op := range cap.Operations {
		if op == snap.Operation {
			supportedOp = true
			break
		}
	}
	if !cap.Supported || !supportedOp || len(snap.ReferenceIDs) > cap.MaxReferenceImages {
		detail := cap.Reason
		if detail == "" {
			detail = "图片 provider 未显式支持当前局部编辑操作"
		}
		e.finish(ctx, taskID, attemptID, "failed", "failed", "unsupported", detail, false, "", "")
		return nil
	}
	if err := e.markPhase(ctx, taskID, attemptID, "provider_pending", cap.ProviderName, snap.auditJSON()); err != nil {
		return nil
	}
	if err := e.markPhase(ctx, taskID, attemptID, "provider_call", cap.ProviderName, nil); err != nil {
		return nil
	}
	editSize, err := sourceEditSize(snap.SourceBytes, snap.SourceMIME)
	if err != nil {
		detail := "局部编辑源图未通过媒体核验"
		var ae apperr.Error
		if errors.As(err, &ae) && ae.Status == 400 && ae.Detail != "" {
			detail = ae.Detail
		}
		e.finish(ctx, taskID, attemptID, "failed", "failed", "failed", detail, false, "", "")
		return nil
	}
	result, err := e.provider().Edit(ctx, EditRequest{
		SourceBytes: snap.SourceBytes, SourceMIME: snap.SourceMIME, MaskPNG: snap.MaskBytes,
		ReferenceBytes: snap.ReferenceBytes,
		Instruction: providerInstruction(Draft{
			Operation: snap.Operation, Instruction: snap.Instruction,
			SourceText: snap.SourceText, ReplacementText: snap.ReplacementText,
		}),
		Operation: snap.Operation,
		Size:      editSize,
	})
	if err != nil {
		e.finish(ctx, taskID, attemptID, "unknown", "unknown", "unknown", unknownDetail, false, truncStatus(fmt.Sprintf("%T", err)), "")
		return nil
	}
	if len(result.Bytes) == 0 {
		e.finish(ctx, taskID, attemptID, "unknown", "unknown", "unknown", "图片 provider 返回的局部编辑结果数量无法确认", false, result.ProviderStatus, result.ResponseID)
		return nil
	}
	if err := e.persistResult(ctx, snap, attemptID, result); err != nil {
		e.finish(ctx, taskID, attemptID, "unknown", "unknown_provider_effect", "unknown", "provider 结果已返回，但结果资产保存状态无法确认", false, result.ProviderStatus, result.ResponseID)
		return nil
	}
	return nil
}

type snapshot struct {
	taskRow
	SourceBytes    []byte
	SourceMIME     string
	SourceWidth    int
	SourceHeight   int
	MaskBytes      []byte
	MaskMIME       string
	MaskWidth      int
	MaskHeight     int
	MaskSHA        string
	ReferenceBytes [][]byte
	SourcePath     string
	SourceName     string
	ImageType      *string
	Display        string
}

func (s snapshot) auditJSON() map[string]any {
	return map[string]any{
		"operation": s.Operation,
		"provider_intent": map[string]any{
			"provider_name": s.RequestedProvider, "local_edit_mode": s.RequestedMode,
		},
		"size": fmt.Sprintf("%dx%d", s.SourceWidth, s.SourceHeight),
		"source": map[string]any{
			"mime_type": s.SourceMIME, "width": s.SourceWidth, "height": s.SourceHeight, "sha256": s.SourceSHA,
		},
		"mask": map[string]any{
			"mime_type": s.MaskMIME, "width": s.MaskWidth, "height": s.MaskHeight, "sha256": s.MaskSHA,
		},
		"reference_asset_ids": s.ReferenceIDs,
	}
}

func (e Executor) claim(ctx context.Context, taskID string) (bool, string, error) {
	var claimed bool
	var attemptID string
	err := tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		task, err := loadTaskByID(ctx, pgxTx, taskID)
		if err != nil {
			if apperr.IsNotFound(err) {
				return nil
			}
			return err
		}
		if task.RequestHash == nil {
			return apperr.Conflict("局部编辑任务缺少 submit request hash")
		}
		now := time.Now().UTC()
		if task.Status == "running" {
			stale := task.StartedAt == nil || now.Sub(task.StartedAt.UTC()) >= staleAfter
			if !stale {
				return queue.ErrBusy
			}
			phase := ""
			if task.ProgressPhase != nil {
				phase = *task.ProgressPhase
			}
			switch phase {
			case "claimed":
				if err := markStaleClaimed(ctx, pgxTx, task); err != nil {
					return err
				}
				task.Status = "queued"
				task.ActiveAttemptID = nil
			case "provider_pending", "provider_call", "provider_result_received":
				if err := markUnknownLocked(ctx, pgxTx, task, "provider boundary 已开始，滞留运行不能自动重投"); err != nil {
					return err
				}
				return apperr.Conflict("局部编辑 provider effect 未知，任务已停止自动重试")
			default:
				return apperr.Conflict("局部编辑任务的运行阶段未知，不能自动重投")
			}
		}
		if task.Status != "queued" && task.Status != "running" {
			return nil
		}
		attemptID = clockid.New()
		attemptNumber := task.Attempts + 1
		res := pgxTx.Model(&schema.LocalImageEditTasks{}).
			Where("id = ? AND status = ?", taskID, "queued").
			Updates(map[string]any{
				"status":            "running",
				"active_attempt_id": attemptID,
				"attempts":          attemptNumber,
				"progress_phase":    "claimed",
				"started_at":        now,
				"finished_at":       nil,
				"failure_reason":    nil,
				"is_retryable":      true,
				"updated_at":        now,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return nil
		}
		attempt := schema.LocalImageEditProviderAttempts{
			ID: clockid.New(), TaskID: taskID, AttemptID: attemptID, AttemptNumber: attemptNumber,
			OperationKey: "local-image-edit:" + taskID, RequestHash: *task.RequestHash,
			Phase: "claimed", EffectResult: "pending", ProviderName: "pending",
			CreatedAt: now, UpdatedAt: now,
		}
		if err := pgxTx.Create(&attempt).Error; err != nil {
			return err
		}
		claimed = true
		return nil
	})
	return claimed, attemptID, err
}

// releaseIdle 在 claim 不到 queued 行时决定信封命运：别人正在跑则 ErrBusy；业务已终态或行不存在则 nil。
func (e Executor) releaseIdle(ctx context.Context, taskID string) error {
	var row schema.LocalImageEditTasks
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

func (e Executor) loadSnapshot(ctx context.Context, taskID, attemptID string) (snapshot, error) {
	var out snapshot
	err := tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		task, err := loadTaskByID(ctx, pgxTx, taskID)
		if err != nil {
			return err
		}
		if task.Status != "running" || task.ActiveAttemptID == nil || *task.ActiveAttemptID != attemptID {
			return apperr.Conflict("局部编辑 attempt 已失效")
		}
		source, err := product.LoadAssetRow(ctx, pgxTx, task.SourceAssetID)
		if err != nil {
			return err
		}
		var sourceObj schema.MediaObjects
		if err := pgxTx.Where("id = ?", source.MediaObjectID).Take(&sourceObj).Error; err != nil {
			return apperr.Validation("局部编辑源图或 mask 媒体不存在")
		}
		sourcePath, sourceMIME := sourceObj.StoragePath, sourceObj.MIMEType
		var maskObj schema.MediaObjects
		if err := pgxTx.Where("id = ?", task.MaskMediaID).Take(&maskObj).Error; err != nil {
			return apperr.Validation("局部编辑源图或 mask 媒体不存在")
		}
		maskSHA, maskPath, maskMIME := "", maskObj.StoragePath, maskObj.MIMEType
		if maskObj.SHA256 != nil {
			maskSHA = *maskObj.SHA256
		}
		sourceBytes, err := readFile(e.Media.Files, sourcePath)
		if err != nil {
			return apperr.Validation("局部编辑媒体读取失败")
		}
		maskBytes, err := readFile(e.Media.Files, maskPath)
		if err != nil {
			return apperr.Validation("局部编辑媒体读取失败")
		}
		if hexSHA(sourceBytes) != task.SourceSHA {
			return apperr.Validation("局部编辑源图版本已变化")
		}
		if maskSHA == "" || hexSHA(maskBytes) != maskSHA {
			return apperr.Validation("局部编辑 mask 版本已变化")
		}
		sourceMeta, err := media.Inspect(sourceBytes, sourceMIME)
		if err != nil {
			return apperr.Validation("局部编辑源图未通过媒体核验")
		}
		if maskMIME == "" {
			maskMIME = "image/png"
		}
		maskMeta, err := media.Inspect(maskBytes, maskMIME)
		if err != nil {
			return apperr.Validation("局部编辑 mask 版本已变化")
		}
		var refBytes [][]byte
		for _, refID := range task.ReferenceIDs {
			refAsset, err := product.LoadAssetRow(ctx, pgxTx, refID)
			if err != nil {
				return err
			}
			var refObj schema.MediaObjects
			if err := pgxTx.Where("id = ?", refAsset.MediaObjectID).Take(&refObj).Error; err != nil {
				return apperr.Validation("局部编辑参考图媒体不存在")
			}
			data, err := readFile(e.Media.Files, refObj.StoragePath)
			if err != nil {
				return apperr.Validation("局部编辑媒体读取失败")
			}
			refBytes = append(refBytes, data)
		}
		out = snapshot{
			taskRow: task, SourceBytes: sourceBytes, SourceMIME: sourceMIME,
			SourceWidth: sourceMeta.Width, SourceHeight: sourceMeta.Height,
			MaskBytes: maskBytes, MaskMIME: maskMeta.MIMEType,
			MaskWidth: maskMeta.Width, MaskHeight: maskMeta.Height, MaskSHA: maskSHA,
			ReferenceBytes: refBytes,
			SourcePath:     sourcePath, SourceName: source.OriginalFilename, ImageType: source.ImageTypeKey, Display: source.DisplayName,
		}
		return nil
	})
	return out, err
}

func (e Executor) markPhase(ctx context.Context, taskID, attemptID, phase, providerName string, requestJSON map[string]any) error {
	return tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		task, attemptOK, err := lockFenced(ctx, pgxTx, taskID, attemptID)
		if err != nil || !attemptOK {
			return apperr.Conflict("局部编辑 attempt 已失效")
		}
		_ = task
		now := time.Now().UTC()
		taskUpdates := map[string]any{"progress_phase": phase, "updated_at": now}
		if providerName != "" {
			taskUpdates["provider_name"] = providerName
		}
		if err := pgxTx.Model(&schema.LocalImageEditTasks{}).
			Where("id = ? AND active_attempt_id = ?", taskID, attemptID).
			Updates(taskUpdates).Error; err != nil {
			return err
		}
		attUpdates := map[string]any{"phase": phase, "updated_at": now}
		if providerName != "" {
			attUpdates["provider_name"] = providerName
		}
		if requestJSON != nil {
			b, err := json.Marshal(requestJSON)
			if err != nil {
				return err
			}
			attUpdates["request_json"] = string(b)
		}
		return pgxTx.Model(&schema.LocalImageEditProviderAttempts{}).
			Where("task_id = ? AND attempt_id = ?", taskID, attemptID).
			Updates(attUpdates).Error
	})
}

func (e Executor) persistResult(ctx context.Context, snap snapshot, attemptID string, result EditResult) error {
	var compensation storage.Compensation
	err := tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		_, ok, err := lockFenced(ctx, pgxTx, snap.ID, attemptID)
		if err != nil || !ok {
			return apperr.Conflict("局部编辑 attempt 已失效")
		}
		if err := lockProduct(ctx, pgxTx, snap.ProductID); err != nil {
			return err
		}
		source, sha, err := lockSource(ctx, pgxTx, snap.ProductID, snap.SourceAssetID)
		if err != nil {
			return err
		}
		if sha != snap.SourceSHA {
			return apperr.Validation("provider 返回前源图版本已变化")
		}
		obj, err := e.Media.Stage(ctx, pgxTx, result.Bytes, result.MIME, &compensation)
		if err != nil {
			return err
		}
		ext := media.ExtensionForMIME(result.MIME)
		display := "局部编辑：" + source.DisplayName
		asset, err := product.InsertAssetIdentity(ctx, pgxTx, product.AssetIdentityInput{
			ProductID:     snap.ProductID,
			MediaID:       obj.ID,
			Filename:      "local-edit-" + snap.ID + ext,
			Origin:        "local_edit",
			ImageTypeKey:  source.ImageTypeKey,
			ParentAssetID: &source.ID,
			DisplayName:   display,
		})
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		var respID, statusPtr *string
		if result.ResponseID != "" {
			s := result.ResponseID
			respID = &s
		}
		if result.ProviderStatus != "" {
			s := result.ProviderStatus
			statusPtr = &s
		}
		if err := pgxTx.Model(&schema.LocalImageEditTasks{}).
			Where("id = ? AND status = ? AND active_attempt_id = ?", snap.ID, "running", attemptID).
			Updates(map[string]any{
				"status":               "succeeded",
				"active_attempt_id":    nil,
				"progress_phase":       "provider_result_received",
				"failure_reason":       nil,
				"is_retryable":         false,
				"finished_at":          now,
				"result_asset_id":      asset.ID,
				"provider_model":       result.Model,
				"provider_response_id": respID,
				"provider_status":      statusPtr,
				"updated_at":           now,
			}).Error; err != nil {
			return err
		}
		return pgxTx.Model(&schema.LocalImageEditProviderAttempts{}).
			Where("task_id = ? AND attempt_id = ?", snap.ID, attemptID).
			Updates(map[string]any{
				"phase":                "succeeded",
				"effect_result":        "applied",
				"provider_model":       result.Model,
				"provider_response_id": respID,
				"provider_status":      statusPtr,
				"updated_at":           now,
			}).Error
	})
	if err != nil {
		compensation.Rollback()
		return err
	}
	compensation.Release()
	return nil
}

func (e Executor) finish(ctx context.Context, taskID, attemptID, status, phase, effect, detail string, retryable bool, providerStatus, responseID string) {
	_ = tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		_, ok, err := lockFenced(ctx, pgxTx, taskID, attemptID)
		if err != nil || !ok {
			return nil
		}
		now := time.Now().UTC()
		var statusPtr, respPtr *string
		if providerStatus != "" {
			s := providerStatus
			statusPtr = &s
		}
		if responseID != "" {
			s := responseID
			respPtr = &s
		}
		if err := pgxTx.Model(&schema.LocalImageEditTasks{}).
			Where("id = ? AND active_attempt_id = ? AND status = ?", taskID, attemptID, "running").
			Updates(map[string]any{
				"status":               status,
				"active_attempt_id":    nil,
				"progress_phase":       phase,
				"failure_reason":       detail,
				"is_retryable":         retryable,
				"finished_at":          now,
				"provider_status":      statusPtr,
				"provider_response_id": respPtr,
				"updated_at":           now,
			}).Error; err != nil {
			return err
		}
		return pgxTx.Model(&schema.LocalImageEditProviderAttempts{}).
			Where("task_id = ? AND attempt_id = ?", taskID, attemptID).
			Updates(map[string]any{
				"phase":                phase,
				"effect_result":        effect,
				"detail":               detail,
				"provider_status":      statusPtr,
				"provider_response_id": respPtr,
				"updated_at":           now,
			}).Error
	})
}

func lockFenced(ctx context.Context, tx *gorm.DB, taskID, attemptID string) (taskRow, bool, error) {
	var row schema.LocalImageEditTasks
	err := tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).
		Where("id = ? AND status = ? AND active_attempt_id = ?", taskID, "running", attemptID).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return taskRow{}, false, nil
	}
	if err != nil {
		return taskRow{}, false, err
	}
	var attempt schema.LocalImageEditProviderAttempts
	err = tx.Clauses(pfdb.ForUpdate()).Where("task_id = ? AND attempt_id = ?", taskID, attemptID).Take(&attempt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return taskRow{}, false, apperr.Conflict("局部编辑 attempt 审计记录不存在")
	}
	return taskFromModel(row), err == nil, err
}

func markStaleClaimed(ctx context.Context, tx *gorm.DB, task taskRow) error {
	now := time.Now().UTC()
	if task.ActiveAttemptID != nil {
		res := tx.WithContext(ctx).Model(&schema.LocalImageEditProviderAttempts{}).
			Where("task_id = ? AND attempt_id = ? AND effect_result = ?", task.ID, *task.ActiveAttemptID, "pending").
			Updates(map[string]any{
				"phase":         "failed",
				"effect_result": "failed",
				"detail":        "worker claim 已过期，provider boundary 尚未开始",
				"updated_at":    now,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return apperr.Conflict("局部编辑 provider effect 已知晓，不能安全重入队")
		}
	}
	return tx.Model(&schema.LocalImageEditTasks{}).Where("id = ?", task.ID).Updates(map[string]any{
		"status":            "queued",
		"active_attempt_id": nil,
		"progress_phase":    nil,
		"started_at":        nil,
		"updated_at":        now,
	}).Error
}

func markUnknownLocked(ctx context.Context, tx *gorm.DB, task taskRow, detail string) error {
	now := time.Now().UTC()
	if task.ActiveAttemptID != nil {
		_ = tx.WithContext(ctx).Model(&schema.LocalImageEditProviderAttempts{}).
			Where("task_id = ? AND attempt_id = ?", task.ID, *task.ActiveAttemptID).
			Updates(map[string]any{
				"phase": "unknown", "effect_result": "unknown", "detail": detail, "updated_at": now,
			}).Error
	}
	return tx.Model(&schema.LocalImageEditTasks{}).Where("id = ?", task.ID).Updates(map[string]any{
		"status":            "unknown",
		"active_attempt_id": nil,
		"progress_phase":    "unknown_provider_effect",
		"failure_reason":    detail,
		"is_retryable":      false,
		"finished_at":       now,
		"updated_at":        now,
	}).Error
}

func readFile(files storage.Local, rel string) ([]byte, error) {
	abs, err := files.Resolve(rel)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(abs)
}

func hexSHA(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func truncStatus(s string) string {
	if len(s) > 80 {
		return s[:80]
	}
	return s
}

func sourceEditSize(data []byte, mime string) (string, error) {
	verified, err := media.Inspect(data, mime)
	if err != nil {
		return "", err
	}
	if verified.Width <= 0 || verified.Height <= 0 {
		return "", apperr.Validation("局部编辑源图尺寸无效")
	}
	return fmt.Sprintf("%dx%d", verified.Width, verified.Height), nil
}
