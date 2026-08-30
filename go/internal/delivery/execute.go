package delivery

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"github.com/yuqie6/productflow/internal/product"
	"gorm.io/gorm"
)

type Executor struct {
	DB    *gorm.DB
	Media media.Store
}

// Execute 本地渲染已有原图；失败不改源资产，也不标 unknown。
func (e Executor) Execute(ctx context.Context, jobID string) error {
	attemptID := clockid.New()
	claimed, err := e.claim(ctx, jobID, attemptID)
	if err != nil {
		return err
	}
	if !claimed.ok {
		return e.releaseIdle(ctx, jobID)
	}
	sourceBytes, err := readStorage(e.Media.Files, claimed.sourcePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			e.fail(ctx, jobID, attemptID, apperr.Validation(sourceMissingDetail), false)
			return nil
		}
		e.fail(ctx, jobID, attemptID, apperr.Validation(unexpectedFailure), true)
		return nil
	}
	meta, err := media.Inspect(sourceBytes, claimed.sourceMIME)
	if err != nil || meta.ByteSize != claimed.sourceBytes || meta.SHA256 != claimed.sourceSHA {
		e.fail(ctx, jobID, attemptID, apperr.Validation(sourceMissingDetail), false)
		return nil
	}
	rendered, err := Render(sourceBytes, claimed.spec)
	if err != nil {
		retryable := true
		var ae apperr.Error
		if errors.As(err, &ae) && ae.Status == 400 {
			retryable = false
		}
		e.fail(ctx, jobID, attemptID, err, retryable)
		return nil
	}
	if err := e.persist(ctx, claimed, rendered); err != nil {
		e.fail(ctx, jobID, attemptID, apperr.Validation(unexpectedFailure), true)
		return nil
	}
	return nil
}

// releaseIdle 在 claim 不到 queued 行时决定信封命运：别人正在跑则 ErrBusy；业务已终态或行不存在则 nil，让 Consume 标 CONSUMED，避免 dispatcher 无限重投。
func (e Executor) releaseIdle(ctx context.Context, jobID string) error {
	var row schema.DeliveryRenditionJobs
	err := e.DB.WithContext(ctx).Where("id = ?", jobID).Take(&row).Error
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

type claim struct {
	ok           bool
	jobID        string
	attemptID    string
	productID    string
	sourceID     string
	sourcePath   string
	sourceMIME   string
	sourceBytes  int
	sourceSHA    string
	sourceName   string
	imageTypeKey *string
	spec         Spec
}

func (e Executor) claim(ctx context.Context, jobID, attemptID string) (claim, error) {
	var out claim
	err := tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		now := time.Now().UTC()
		res := pgxTx.Model(&schema.DeliveryRenditionJobs{}).
			Where("id = ? AND status = ?", jobID, "queued").
			Updates(map[string]any{
				"status":            "running",
				"attempts":          gorm.Expr("attempts + 1"),
				"active_attempt_id": attemptID,
				"failure_reason":    nil,
				"started_at":        now,
				"finished_at":       nil,
				"updated_at":        now,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return nil
		}
		row, err := loadJob(ctx, pgxTx, jobID)
		if err != nil {
			return failClaimIfClient(ctx, pgxTx, jobID, attemptID, err)
		}
		source, err := product.LoadAssetRow(ctx, pgxTx, row.SourceAssetID)
		if err != nil {
			return failClaimIfClient(ctx, pgxTx, jobID, attemptID, err)
		}
		if source.VerificationStatus != media.StatusVerified {
			return failClaimIfClient(ctx, pgxTx, jobID, attemptID, apperr.Validation("交付派生原图媒体尚未通过核验"))
		}
		spec, err := specFromJSON(row.SpecJSON)
		if err != nil {
			return failClaimIfClient(ctx, pgxTx, jobID, attemptID, err)
		}
		sha, err := loadMediaSHA256(ctx, pgxTx, source.MediaObjectID)
		if err != nil {
			return failClaimIfClient(ctx, pgxTx, jobID, attemptID, err)
		}
		if source.ByteSize == nil || sha == "" {
			return failClaimIfClient(ctx, pgxTx, jobID, attemptID, apperr.Validation("交付派生原图缺少核验元数据"))
		}
		out = claim{
			ok: true, jobID: jobID, attemptID: attemptID, productID: row.ProductID,
			sourceID: source.ID, sourcePath: source.StoragePath, sourceMIME: source.MIMEType,
			sourceName: source.DisplayName, imageTypeKey: source.ImageTypeKey, spec: spec,
			sourceBytes: *source.ByteSize, sourceSHA: sha,
		}
		return nil
	})
	return out, err
}

func (e Executor) persist(ctx context.Context, claimed claim, rendered Rendered) error {
	var compensation storage.Compensation
	err := tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		row, err := loadJobForUpdate(ctx, pgxTx, claimed.jobID)
		if err != nil {
			return err
		}
		if row.Status != "running" || row.ActiveAttempt == nil || *row.ActiveAttempt != claimed.attemptID {
			return nil
		}
		source, err := product.LoadAssetRow(ctx, pgxTx, claimed.sourceID)
		if err != nil {
			return err
		}
		obj, err := e.Media.Stage(ctx, pgxTx, rendered.Bytes, rendered.Metadata.MIMEType, &compensation)
		if err != nil {
			return err
		}
		ext := formatExt[claimed.spec.Format]
		suffix := "-" + strconv.Itoa(claimed.spec.Width) + "x" + strconv.Itoa(claimed.spec.Height) + ext
		stem := strings.TrimSuffix(source.OriginalFilename, filepath.Ext(source.OriginalFilename))
		filename := trimFilename(stem, suffix)
		asset, err := product.InsertAssetIdentity(ctx, pgxTx, product.AssetIdentityInput{
			ProductID:     claimed.productID,
			MediaID:       obj.ID,
			Filename:      filename,
			Origin:        "workflow_generation",
			ImageTypeKey:  claimed.imageTypeKey,
			ParentAssetID: &source.ID,
			DisplayName:   displayRendition(source.DisplayName, claimed.spec),
		})
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		res := pgxTx.Model(&schema.DeliveryRenditionJobs{}).
			Where("id = ? AND status = ? AND active_attempt_id = ?", claimed.jobID, "running", claimed.attemptID).
			Updates(map[string]any{
				"result_asset_id":   asset.ID,
				"status":            "succeeded",
				"active_attempt_id": nil,
				"failure_reason":    nil,
				"finished_at":       now,
				"is_retryable":      false,
				"updated_at":        now,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return nil
		}
		_ = pgxTx.Model(&schema.Products{}).Where("id = ?", claimed.productID).Updates(map[string]any{"updated_at": now}).Error
		return nil
	})
	if err != nil {
		compensation.Rollback()
		return err
	}
	compensation.Release()
	return nil
}

func (e Executor) fail(ctx context.Context, jobID, attemptID string, reason error, retryable bool) {
	_ = tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		return failJob(ctx, pgxTx, jobID, attemptID, reason, retryable)
	})
}

func failClaimIfClient(ctx context.Context, pgxTx *gorm.DB, jobID, attemptID string, err error) error {
	var ae apperr.Error
	if !errors.As(err, &ae) || ae.Status < 400 || ae.Status >= 500 {
		return err
	}
	if failErr := failJob(ctx, pgxTx, jobID, attemptID, err, false); failErr != nil {
		return failErr
	}
	return nil
}

func failJob(ctx context.Context, pgxTx *gorm.DB, jobID, attemptID string, reason error, retryable bool) error {
	detail := unexpectedFailure
	if reason != nil {
		detail = reason.Error()
		if len(detail) > 1000 {
			detail = detail[:1000]
		}
	}
	now := time.Now().UTC()
	return pgxTx.WithContext(ctx).Model(&schema.DeliveryRenditionJobs{}).
		Where("id = ? AND status = ? AND active_attempt_id = ?", jobID, "running", attemptID).
		Updates(map[string]any{
			"status":            "failed",
			"active_attempt_id": nil,
			"is_retryable":      retryable,
			"failure_reason":    detail,
			"finished_at":       now,
			"updated_at":        now,
		}).Error
}
