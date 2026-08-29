package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"github.com/yuqie6/productflow/internal/product"
)

type Service struct {
	Pool  *pgxpool.Pool
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
	err = tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
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
		_, err = pgxTx.Exec(ctx, `
			INSERT INTO delivery_rendition_jobs (
				id, product_id, source_asset_id, spec_schema_version, spec_json, spec_hash,
				status, attempts, is_retryable, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, 'queued', 0, TRUE, NOW(), NOW())
		`, id, source.ProductID, source.ID, specSchemaVersion, specJSON, normalized.Hash)
		if err != nil {
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
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
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
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		if _, err := productLoad(ctx, pgxTx, sourceAssetID); err != nil {
			if apperr.IsNotFound(err) {
				return apperr.NotFound("交付派生原图不存在")
			}
			return err
		}
		rows, err := pgxTx.Query(ctx, `
			SELECT id, product_id, source_asset_id, result_asset_id, spec_json, spec_hash, status,
			       attempts, is_retryable, failure_reason, created_at, started_at, finished_at, updated_at, active_attempt_id
			FROM delivery_rendition_jobs
			WHERE source_asset_id = $1
			ORDER BY created_at DESC, id DESC
		`, sourceAssetID)
		if err != nil {
			return err
		}
		var collected []jobRow
		for rows.Next() {
			row, err := scanJob(rows)
			if err != nil {
				rows.Close()
				return err
			}
			collected = append(collected, row)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		items := make([]JobResponse, 0, len(collected))
		for _, row := range collected {
			item, err := s.serialize(ctx, pgxTx, row)
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
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
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
		if _, err := pgxTx.Exec(ctx, `
			UPDATE delivery_rendition_jobs SET
				status = 'queued', active_attempt_id = NULL, failure_reason = NULL,
				started_at = NULL, finished_at = NULL, is_retryable = TRUE, updated_at = NOW()
			WHERE id = $1
		`, jobID); err != nil {
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

// QueueAfterImageSuccess 图运行图片成功后按节点 DeliverySpec 入队；失败不影响已成功资产。
func (s Service) QueueAfterImageSuccess(ctx context.Context, pgxTx pgx.Tx, nodeID, sourceAssetID string) error {
	var raw json.RawMessage
	err := pgxTx.QueryRow(ctx, `SELECT config_json FROM workflow_graph_nodes WHERE id = $1`, nodeID).Scan(&raw)
	if err != nil {
		return nil
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
		return nil
	}
	if err := validateSource(ctx, pgxTx, source); err != nil {
		return nil
	}
	existing, err := loadBySourceHash(ctx, pgxTx, source.ID, normalized.Hash)
	if err != nil || existing != nil {
		return nil
	}
	id := clockid.New()
	specJSON, err := json.Marshal(normalized.Payload)
	if err != nil {
		return nil
	}
	if _, err := pgxTx.Exec(ctx, `
		INSERT INTO delivery_rendition_jobs (
			id, product_id, source_asset_id, spec_schema_version, spec_json, spec_hash,
			status, attempts, is_retryable, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, 'queued', 0, TRUE, NOW(), NOW())
	`, id, source.ProductID, source.ID, specSchemaVersion, specJSON, normalized.Hash); err != nil {
		return nil
	}
	_, _ = queue.StageForActor(ctx, pgxTx, queue.ActorDelivery, id, 0)
	return nil
}

func (s Service) serialize(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, row jobRow) (JobResponse, error) {
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

func productLoad(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, assetID string) (product.ImageAsset, error) {
	return product.LoadAssetRow(ctx, q, assetID)
}

func validateSource(ctx context.Context, tx pgx.Tx, source product.ImageAsset) error {
	if source.ParentAssetID != nil {
		return apperr.Validation("交付派生不能以已有派生图作为原图")
	}
	if source.VerificationStatus != media.StatusVerified {
		return apperr.Validation("交付派生原图媒体尚未通过核验")
	}
	var artifactID string
	err := tx.QueryRow(ctx, `
		SELECT id FROM workflow_graph_artifacts
		WHERE product_image_asset_id = $1 AND artifact_type = 'image'
		LIMIT 1
	`, source.ID).Scan(&artifactID)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.Validation("交付派生只接受成功的工作流生成原图")
	}
	return err
}

func loadJob(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, jobID string) (jobRow, error) {
	row, err := scanJobRow(q.QueryRow(ctx, `
		SELECT id, product_id, source_asset_id, result_asset_id, spec_json, spec_hash, status,
		       attempts, is_retryable, failure_reason, created_at, started_at, finished_at, updated_at, active_attempt_id
		FROM delivery_rendition_jobs WHERE id = $1
	`, jobID))
	if errors.Is(err, pgx.ErrNoRows) {
		return jobRow{}, apperr.NotFound("交付派生任务不存在")
	}
	return row, err
}

func loadJobForUpdate(ctx context.Context, tx pgx.Tx, jobID string) (jobRow, error) {
	row, err := scanJobRow(tx.QueryRow(ctx, `
		SELECT id, product_id, source_asset_id, result_asset_id, spec_json, spec_hash, status,
		       attempts, is_retryable, failure_reason, created_at, started_at, finished_at, updated_at, active_attempt_id
		FROM delivery_rendition_jobs WHERE id = $1 FOR UPDATE
	`, jobID))
	if errors.Is(err, pgx.ErrNoRows) {
		return jobRow{}, apperr.NotFound("交付派生任务不存在")
	}
	return row, err
}

func loadBySourceHash(ctx context.Context, tx pgx.Tx, sourceID, hash string) (*jobRow, error) {
	row, err := scanJobRow(tx.QueryRow(ctx, `
		SELECT id, product_id, source_asset_id, result_asset_id, spec_json, spec_hash, status,
		       attempts, is_retryable, failure_reason, created_at, started_at, finished_at, updated_at, active_attempt_id
		FROM delivery_rendition_jobs WHERE source_asset_id = $1 AND spec_hash = $2
	`, sourceID, hash))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanJob(rows pgx.Rows) (jobRow, error) {
	return scanJobRow(rows)
}

func scanJobRow(row rowScanner) (jobRow, error) {
	var j jobRow
	err := row.Scan(
		&j.ID, &j.ProductID, &j.SourceAssetID, &j.ResultAssetID, &j.SpecJSON, &j.SpecHash, &j.Status,
		&j.Attempts, &j.IsRetryable, &j.FailureReason, &j.CreatedAt, &j.StartedAt, &j.FinishedAt, &j.UpdatedAt, &j.ActiveAttempt,
	)
	return j, err
}

func (s Service) files() storage.Local {
	return s.Media.Files
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
