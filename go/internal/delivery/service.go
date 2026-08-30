package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/graph"
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

type Service struct {
	DB    *gorm.DB
	Media media.Store
}

type SubmitResult struct {
	Job     JobResponse
	Created bool
	Queued  bool
}

func (s Service) Submit(ctx context.Context, sourceAssetID string, specRaw map[string]any) (SubmitResult, error) {
	normalized, err := NormalizeSpec(specRaw)
	if err != nil {
		return SubmitResult{}, err
	}
	var created bool
	var jobID string
	var queued bool
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		source, err := productLoad(ctx, pgxTx, sourceAssetID)
		if err != nil {
			if apperr.IsNotFound(err) {
				return apperr.NotFound("交付派生原图不存在")
			}
			return err
		}
		if err := validateSource(ctx, pgxTx, source); err != nil {
			return err
		}
		existing, err := loadBySourceHash(ctx, pgxTx, source.ID, normalized.Hash)
		if err != nil {
			return err
		}
		if existing != nil {
			jobID = existing.ID
			created = false
			queued = existing.Status == "queued" || existing.Status == "running"
			return nil
		}
		id := clockid.New()
		specJSON, err := json.Marshal(normalized.Payload)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		row := schema.DeliveryRenditionJobs{
			ID: id, ProductID: source.ProductID, SourceAssetID: source.ID,
			SpecSchemaVersion: specSchemaVersion, SpecJSON: string(specJSON), SpecHash: normalized.Hash,
			Status: "queued", Attempts: 0, IsRetryable: true, CreatedAt: now, UpdatedAt: now,
		}
		if err := pgxTx.Create(&row).Error; err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				dup, loadErr := loadBySourceHash(ctx, pgxTx, source.ID, normalized.Hash)
				if loadErr != nil {
					return loadErr
				}
				if dup != nil {
					jobID = dup.ID
					created = false
					queued = dup.Status == "queued" || dup.Status == "running"
					return nil
				}
			}
			return err
		}
		if _, err := queue.StageForActor(ctx, pgxTx, queue.ActorDelivery, id, 0); err != nil {
			return err
		}
		jobID = id
		created = true
		queued = true
		return nil
	})
	if err != nil {
		return SubmitResult{}, err
	}
	resp, err := s.Get(ctx, jobID)
	if err != nil {
		return SubmitResult{}, err
	}
	return SubmitResult{Job: resp, Created: created, Queued: queued}, nil
}

func (s Service) Get(ctx context.Context, jobID string) (JobResponse, error) {
	var out JobResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		row, err := loadJob(ctx, pgxTx, jobID)
		if err != nil {
			return err
		}
		out, err = s.serialize(ctx, pgxTx, row)
		return err
	})
	return out, err
}

func (s Service) List(ctx context.Context, sourceAssetID string) (JobListResponse, error) {
	var out JobListResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if _, err := productLoad(ctx, pgxTx, sourceAssetID); err != nil {
			if apperr.IsNotFound(err) {
				return apperr.NotFound("交付派生原图不存在")
			}
			return err
		}
		var collected []schema.DeliveryRenditionJobs
		if err := pgxTx.Where("source_asset_id = ?", sourceAssetID).Order("created_at DESC, id DESC").Find(&collected).Error; err != nil {
			return err
		}
		items := make([]JobResponse, 0, len(collected))
		for _, model := range collected {
			item, err := s.serialize(ctx, pgxTx, jobFromModel(model))
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		out.Items = items
		return nil
	})
	return out, err
}

func (s Service) Retry(ctx context.Context, jobID string) (JobResponse, error) {
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		row, err := loadJobForUpdate(ctx, pgxTx, jobID)
		if err != nil {
			return err
		}
		if row.Status != "failed" {
			return apperr.Conflict("只有失败的交付派生任务可以重试")
		}
		if !row.IsRetryable {
			return apperr.Conflict("该交付派生任务不可重试")
		}
		if err := pgxTx.Model(&schema.DeliveryRenditionJobs{}).Where("id = ?", jobID).Updates(map[string]any{
			"status":            "queued",
			"active_attempt_id": nil,
			"failure_reason":    nil,
			"started_at":        nil,
			"finished_at":       nil,
			"is_retryable":      true,
			"updated_at":        time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
		_, err = queue.Requeue(ctx, pgxTx, queue.DeliveryKey(queue.ActorDelivery, jobID), queue.ActorDelivery, jobID, nil, nil, false)
		return err
	})
	if err != nil {
		return JobResponse{}, err
	}
	return s.Get(ctx, jobID)
}

// QueueAfterImageSuccess 图运行图片成功后按节点 DeliverySpec 入队。
// 无 DeliverySpec 或规格不合法时跳过；INSERT / StageForActor 失败必须返回 error。
func (s Service) QueueAfterImageSuccess(ctx context.Context, pgxTx *gorm.DB, nodeID, sourceAssetID string) error {
	raw, err := graph.NodeConfigJSON(ctx, pgxTx, nodeID)
	if err != nil {
		return err
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil
	}
	specRaw, ok := config["delivery_spec"].(map[string]any)
	if !ok || specRaw == nil {
		return nil
	}
	normalized, err := NormalizeSpec(specRaw)
	if err != nil {
		return nil
	}
	source, err := productLoad(ctx, pgxTx, sourceAssetID)
	if err != nil {
		return err
	}
	if err := validateSource(ctx, pgxTx, source); err != nil {
		return nil
	}
	existing, err := loadBySourceHash(ctx, pgxTx, source.ID, normalized.Hash)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}
	id := clockid.New()
	specJSON, err := json.Marshal(normalized.Payload)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	row := schema.DeliveryRenditionJobs{
		ID: id, ProductID: source.ProductID, SourceAssetID: source.ID,
		SpecSchemaVersion: specSchemaVersion, SpecJSON: string(specJSON), SpecHash: normalized.Hash,
		Status: "queued", Attempts: 0, IsRetryable: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := pgxTx.Create(&row).Error; err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			dup, loadErr := loadBySourceHash(ctx, pgxTx, source.ID, normalized.Hash)
			if loadErr != nil {
				return loadErr
			}
			if dup != nil {
				return nil
			}
		}
		return err
	}
	_, err = queue.StageForActor(ctx, pgxTx, queue.ActorDelivery, id, 0)
	return err
}

func (s Service) serialize(ctx context.Context, q *gorm.DB, row jobRow) (JobResponse, error) {
	spec, err := specFromJSON(row.SpecJSON)
	if err != nil {
		return JobResponse{}, err
	}
	out := JobResponse{
		ID: row.ID, ProductID: row.ProductID, SourceAssetID: row.SourceAssetID,
		DeliverySpec: spec, Status: row.Status, Attempts: row.Attempts,
		IsRetryable: row.IsRetryable, FailureReason: row.FailureReason,
		CreatedAt: row.CreatedAt, StartedAt: row.StartedAt, FinishedAt: row.FinishedAt, UpdatedAt: row.UpdatedAt,
	}
	if row.ResultAssetID != nil {
		asset, err := product.LoadAssetRow(ctx, q, *row.ResultAssetID)
		if err != nil {
			return JobResponse{}, err
		}
		resp := product.SerializeAsset(asset)
		out.ResultAsset = &resp
	}
	return out, nil
}

func productLoad(ctx context.Context, q *gorm.DB, assetID string) (product.ImageAsset, error) {
	return product.LoadAssetRow(ctx, q, assetID)
}

func validateSource(ctx context.Context, tx *gorm.DB, source product.ImageAsset) error {
	if source.ParentAssetID != nil {
		return apperr.Validation("交付派生不能以已有派生图作为原图")
	}
	if source.VerificationStatus != media.StatusVerified {
		return apperr.Validation("交付派生原图媒体尚未通过核验")
	}
	ok, err := graph.HasImageArtifactForAsset(ctx, tx, source.ID)
	if err != nil {
		return err
	}
	if !ok {
		return apperr.Validation("交付派生只接受成功的工作流生成原图")
	}
	return nil
}

func loadJob(ctx context.Context, q *gorm.DB, jobID string) (jobRow, error) {
	var row schema.DeliveryRenditionJobs
	err := q.WithContext(ctx).Where("id = ?", jobID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return jobRow{}, apperr.NotFound("交付派生任务不存在")
	}
	if err != nil {
		return jobRow{}, err
	}
	return jobFromModel(row), nil
}

func loadJobForUpdate(ctx context.Context, tx *gorm.DB, jobID string) (jobRow, error) {
	var row schema.DeliveryRenditionJobs
	err := tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Where("id = ?", jobID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return jobRow{}, apperr.NotFound("交付派生任务不存在")
	}
	if err != nil {
		return jobRow{}, err
	}
	return jobFromModel(row), nil
}

func loadBySourceHash(ctx context.Context, tx *gorm.DB, sourceID, hash string) (*jobRow, error) {
	var row schema.DeliveryRenditionJobs
	err := tx.WithContext(ctx).Where("source_asset_id = ? AND spec_hash = ?", sourceID, hash).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := jobFromModel(row)
	return &out, nil
}

func loadMediaSHA256(ctx context.Context, q *gorm.DB, mediaObjectID string) (string, error) {
	var row schema.MediaObjects
	err := q.WithContext(ctx).Where("id = ?", mediaObjectID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if row.SHA256 == nil {
		return "", nil
	}
	return *row.SHA256, nil
}

func jobFromModel(m schema.DeliveryRenditionJobs) jobRow {
	return jobRow{
		ID: m.ID, ProductID: m.ProductID, SourceAssetID: m.SourceAssetID, ResultAssetID: m.ResultAssetID,
		SpecSchemaVersion: m.SpecSchemaVersion, SpecJSON: []byte(m.SpecJSON), SpecHash: m.SpecHash,
		Status: m.Status, Attempts: m.Attempts, IsRetryable: m.IsRetryable, FailureReason: m.FailureReason,
		CreatedAt: m.CreatedAt, StartedAt: m.StartedAt, FinishedAt: m.FinishedAt, UpdatedAt: m.UpdatedAt,
		ActiveAttempt: m.ActiveAttemptID,
	}
}

func readStorage(files storage.Local, rel string) ([]byte, error) {
	abs, err := files.Resolve(rel)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(abs)
}

func trimFilename(stem, suffix string) string {
	runes := []rune(stem)
	max := 255 - len([]rune(suffix))
	if max < 1 {
		max = 1
	}
	if len(runes) > max {
		runes = runes[:max]
	}
	return string(runes) + suffix
}

func displayRendition(sourceDisplay string, spec Spec) string {
	base := strings.TrimSpace(sourceDisplay)
	if base == "" {
		base = "image"
	}
	return base + " " + strconv.Itoa(spec.Width) + "x" + strconv.Itoa(spec.Height) + " " + strings.ToUpper(spec.Format)
}
