package delivery

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
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
		e.fail(ctx, jobID, attemptID, err, false)
		return nil
	}
	if !claimed.ok {
		return nil
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
		n, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE delivery_rendition_jobs SET
				status = 'running', attempts = attempts + 1, active_attempt_id = $2,
				failure_reason = NULL, started_at = NOW(), finished_at = NULL, updated_at = NOW()
			WHERE id = $1 AND status = 'queued'
		`, jobID, attemptID)
		if err != nil {
			return err
		}
		if n != 1 {
			return nil
		}
		row, err := loadJob(ctx, pgxTx, jobID)
		if err != nil {
			return err
		}
		source, err := product.LoadAssetRow(ctx, pgxTx, row.SourceAssetID)
		if err != nil {
			return err
		}
		if source.VerificationStatus != media.StatusVerified {
			return apperr.Validation("交付派生原图媒体尚未通过核验")
		}
		spec, err := specFromJSON(row.SpecJSON)
		if err != nil {
			return err
		}
		out = claim{
			ok: true, jobID: jobID, attemptID: attemptID, productID: row.ProductID,
			sourceID: source.ID, sourcePath: source.StoragePath, sourceMIME: source.MIMEType,
			sourceName: source.DisplayName, imageTypeKey: source.ImageTypeKey, spec: spec,
		}
		if source.ByteSize != nil {
			out.sourceBytes = *source.ByteSize
		}
		var sha string
		_ = pfdb.QueryRow(ctx, pgxTx, `SELECT sha256 FROM media_objects WHERE id = $1`, source.MediaObjectID).Scan(&sha)
		out.sourceSHA = sha
		if source.ByteSize == nil || sha == "" {
			return apperr.Validation("交付派生原图缺少核验元数据")
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
		n, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE delivery_rendition_jobs SET
				result_asset_id = $2, status = 'succeeded', active_attempt_id = NULL,
				failure_reason = NULL, finished_at = NOW(), is_retryable = FALSE, updated_at = NOW()
			WHERE id = $1 AND status = 'running' AND active_attempt_id = $3
		`, claimed.jobID, asset.ID, claimed.attemptID)
		if err != nil {
			return err
		}
		if n != 1 {
			return nil
		}
		_, _ = pfdb.Exec(ctx, pgxTx, `UPDATE products SET updated_at = NOW() WHERE id = $1`, claimed.productID)
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
	detail := unexpectedFailure
	if reason != nil {
		detail = reason.Error()
		if len(detail) > 1000 {
			detail = detail[:1000]
		}
	}
	_ = tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		_, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE delivery_rendition_jobs SET
				status = 'failed', active_attempt_id = NULL, is_retryable = $3,
				failure_reason = $4, finished_at = NOW(), updated_at = NOW()
			WHERE id = $1 AND status = 'running' AND active_attempt_id = $2
		`, jobID, attemptID, retryable, detail)
		return err
	})
}
