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

	sqldb "database/sql"

	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
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
		return queue.ErrBusy
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
	result, err := e.provider().Edit(ctx, EditRequest{
		SourceBytes: snap.SourceBytes, SourceMIME: snap.SourceMIME, MaskPNG: snap.MaskBytes,
		Instruction: providerInstruction(Draft{
			Operation: snap.Operation, Instruction: snap.Instruction,
			SourceText: snap.SourceText, ReplacementText: snap.ReplacementText,
		}),
		Operation: snap.Operation,
	})
	if err != nil {
		var ae apperr.Error
		if errors.As(err, &ae) && ae.Status == 400 {
			e.finish(ctx, taskID, attemptID, "failed", "failed", "unsupported", ae.Detail, false, "", "")
			return nil
		}
		e.finish(ctx, taskID, attemptID, "unknown", "unknown", "unknown", "图片 provider 请求结果未知", false, truncStatus(fmt.Sprintf("%T", err)), "")
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
	SourceBytes []byte
	SourceMIME  string
	MaskBytes   []byte
	SourcePath  string
	SourceName  string
	ImageType   *string
	Display     string
}

func (s snapshot) auditJSON() map[string]any {
	w, h := 0, 0
	return map[string]any{
		"operation": s.Operation,
		"provider_intent": map[string]any{
			"provider_name": s.RequestedProvider, "local_edit_mode": s.RequestedMode,
		},
		"size":                fmt.Sprintf("%dx%d", w, h),
		"source":              map[string]any{"mime_type": s.SourceMIME, "sha256": s.SourceSHA},
		"reference_asset_ids": s.ReferenceIDs,
	}
}

func (e Executor) claim(ctx context.Context, taskID string) (bool, string, error) {
	var claimed bool
	var attemptID string
	err := tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		task, err := loadTaskByID(ctx, pgxTx, taskID)
		if err != nil {
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
			if phase == "claimed" {
				if err := markStaleClaimed(ctx, pgxTx, task); err != nil {
					return err
				}
				task.Status = "queued"
				task.ActiveAttemptID = nil
			} else if phase == "provider_pending" || phase == "provider_call" || phase == "provider_result_received" {
				if err := markUnknownLocked(ctx, pgxTx, task, "provider boundary 已开始，滞留运行不能自动重投"); err != nil {
					return err
				}
				return apperr.Conflict("局部编辑 provider effect 未知，任务已停止自动重试")
			} else {
				return apperr.Conflict("局部编辑任务的运行阶段未知，不能自动重投")
			}
		}
		if task.Status != "queued" && task.Status != "running" {
			return apperr.Conflict("局部编辑任务当前状态不能 claim")
		}
		attemptID = clockid.New()
		attemptNumber := task.Attempts + 1
		n, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE local_image_edit_tasks SET
				status = 'running', active_attempt_id = $2, attempts = $3, progress_phase = 'claimed',
				started_at = NOW(), finished_at = NULL, failure_reason = NULL, is_retryable = TRUE, updated_at = NOW()
			WHERE id = $1 AND status = 'queued'
		`, taskID, attemptID, attemptNumber)
		if err != nil {
			return err
		}
		if n != 1 {
			return nil
		}
		if _, err := pfdb.Exec(ctx, pgxTx, `
			INSERT INTO local_image_edit_provider_attempts (
				id, task_id, attempt_id, attempt_number, operation_key, request_hash,
				phase, effect_result, provider_name, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, 'claimed', 'pending', 'pending', NOW(), NOW())
		`, clockid.New(), taskID, attemptID, attemptNumber, "local-image-edit:"+taskID, *task.RequestHash); err != nil {
			return err
		}
		claimed = true
		return nil
	})
	return claimed, attemptID, err
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
		var sourceSHA, sourcePath, sourceMIME string
		if err := pfdb.QueryRow(ctx, pgxTx, `
			SELECT sha256, storage_path, mime_type FROM media_objects WHERE id = $1
		`, source.MediaObjectID).Scan(&sourceSHA, &sourcePath, &sourceMIME); err != nil {
			return apperr.Validation("局部编辑源图或 mask 媒体不存在")
		}
		var maskSHA, maskPath string
		if err := pfdb.QueryRow(ctx, pgxTx, `
			SELECT sha256, storage_path FROM media_objects WHERE id = $1
		`, task.MaskMediaID).Scan(&maskSHA, &maskPath); err != nil {
			return apperr.Validation("局部编辑源图或 mask 媒体不存在")
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
		if _, err := media.Inspect(sourceBytes, sourceMIME); err != nil {
			return apperr.Validation("局部编辑源图未通过媒体核验")
		}
		if _, err := media.Inspect(maskBytes, "image/png"); err != nil {
			return apperr.Validation("局部编辑 mask 版本已变化")
		}
		out = snapshot{
			taskRow: task, SourceBytes: sourceBytes, SourceMIME: sourceMIME, MaskBytes: maskBytes,
			SourcePath: sourcePath, SourceName: source.OriginalFilename, ImageType: source.ImageTypeKey, Display: source.DisplayName,
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
		var raw any
		if requestJSON != nil {
			b, err := json.Marshal(requestJSON)
			if err != nil {
				return err
			}
			raw = b
		}
		if _, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE local_image_edit_tasks SET
				progress_phase = $3, provider_name = COALESCE(NULLIF($4, ''), provider_name), updated_at = NOW()
			WHERE id = $1 AND active_attempt_id = $2
		`, taskID, attemptID, phase, providerName); err != nil {
			return err
		}
		_, err = pfdb.Exec(ctx, pgxTx, `
			UPDATE local_image_edit_provider_attempts SET
				phase = $3, provider_name = COALESCE(NULLIF($4, ''), provider_name),
				request_json = COALESCE($5, request_json), updated_at = NOW()
			WHERE task_id = $1 AND attempt_id = $2
		`, taskID, attemptID, phase, providerName, raw)
		return err
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
		if _, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE local_image_edit_tasks SET
				status = 'succeeded', active_attempt_id = NULL, progress_phase = 'provider_result_received',
				failure_reason = NULL, is_retryable = FALSE, finished_at = NOW(), result_asset_id = $2,
				provider_model = $3, provider_response_id = NULLIF($4, ''), provider_status = NULLIF($5, ''),
				updated_at = NOW()
			WHERE id = $1 AND status = 'running' AND active_attempt_id = $6
		`, snap.ID, asset.ID, result.Model, result.ResponseID, result.ProviderStatus, attemptID); err != nil {
			return err
		}
		_, err = pfdb.Exec(ctx, pgxTx, `
			UPDATE local_image_edit_provider_attempts SET
				phase = 'succeeded', effect_result = 'applied',
				provider_model = $3, provider_response_id = NULLIF($4, ''), provider_status = NULLIF($5, ''),
				updated_at = NOW()
			WHERE task_id = $1 AND attempt_id = $2
		`, snap.ID, attemptID, result.Model, result.ResponseID, result.ProviderStatus)
		return err
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
		if _, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE local_image_edit_tasks SET
				status = $3, active_attempt_id = NULL, progress_phase = $4, failure_reason = $5,
				is_retryable = $6, finished_at = NOW(), provider_status = NULLIF($7, ''),
				provider_response_id = NULLIF($8, ''), updated_at = NOW()
			WHERE id = $1 AND active_attempt_id = $2 AND status = 'running'
		`, taskID, attemptID, status, phase, detail, retryable, providerStatus, responseID); err != nil {
			return err
		}
		_, err = pfdb.Exec(ctx, pgxTx, `
			UPDATE local_image_edit_provider_attempts SET
				phase = $3, effect_result = $4, detail = $5,
				provider_status = NULLIF($6, ''), provider_response_id = NULLIF($7, ''), updated_at = NOW()
			WHERE task_id = $1 AND attempt_id = $2
		`, taskID, attemptID, phase, effect, detail, providerStatus, responseID)
		return err
	})
}

func lockFenced(ctx context.Context, tx *gorm.DB, taskID, attemptID string) (taskRow, bool, error) {
	row, err := scanTask(pfdb.QueryRow(ctx, tx, `
		SELECT `+taskSelect+` FROM local_image_edit_tasks
		WHERE id = $1 AND status = 'running' AND active_attempt_id = $2
		FOR UPDATE
	`, taskID, attemptID))
	if errors.Is(err, sqldb.ErrNoRows) {
		return taskRow{}, false, nil
	}
	if err != nil {
		return taskRow{}, false, err
	}
	var attemptExists string
	err = pfdb.QueryRow(ctx, tx, `
		SELECT id FROM local_image_edit_provider_attempts WHERE task_id = $1 AND attempt_id = $2 FOR UPDATE
	`, taskID, attemptID).Scan(&attemptExists)
	if errors.Is(err, sqldb.ErrNoRows) {
		return taskRow{}, false, apperr.Conflict("局部编辑 attempt 审计记录不存在")
	}
	return row, err == nil, err
}

func markStaleClaimed(ctx context.Context, tx *gorm.DB, task taskRow) error {
	if task.ActiveAttemptID != nil {
		n, err := pfdb.Exec(ctx, tx, `
			UPDATE local_image_edit_provider_attempts SET
				phase = 'failed', effect_result = 'failed',
				detail = 'worker claim 已过期，provider boundary 尚未开始', updated_at = NOW()
			WHERE task_id = $1 AND attempt_id = $2 AND effect_result = 'pending'
		`, task.ID, *task.ActiveAttemptID)
		if err != nil {
			return err
		}
		if n != 1 {
			return apperr.Conflict("局部编辑 provider effect 已知晓，不能安全重入队")
		}
	}
	_, err := pfdb.Exec(ctx, tx, `
		UPDATE local_image_edit_tasks SET
			status = 'queued', active_attempt_id = NULL, progress_phase = NULL,
			started_at = NULL, updated_at = NOW()
		WHERE id = $1
	`, task.ID)
	return err
}

func markUnknownLocked(ctx context.Context, tx *gorm.DB, task taskRow, detail string) error {
	if task.ActiveAttemptID != nil {
		_, _ = pfdb.Exec(ctx, tx, `
			UPDATE local_image_edit_provider_attempts SET
				phase = 'unknown', effect_result = 'unknown', detail = $3, updated_at = NOW()
			WHERE task_id = $1 AND attempt_id = $2
		`, task.ID, *task.ActiveAttemptID, detail)
	}
	_, err := pfdb.Exec(ctx, tx, `
		UPDATE local_image_edit_tasks SET
			status = 'unknown', active_attempt_id = NULL, progress_phase = 'unknown_provider_effect',
			failure_reason = $2, is_retryable = FALSE, finished_at = NOW(), updated_at = NOW()
		WHERE id = $1
	`, task.ID, detail)
	return err
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
