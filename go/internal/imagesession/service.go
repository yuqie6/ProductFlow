package imagesession

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/settings"
)

type Service struct {
	Pool     *pgxpool.Pool
	Media    media.Store
	Settings interface {
		settings.RuntimeReader
		settings.LimitsReader
	}
}

func (s Service) maxDimension(ctx context.Context) int {
	if s.Settings == nil {
		return defaultMaxDimension
	}
	runtime, err := s.Settings.Runtime(ctx)
	if err != nil {
		return defaultMaxDimension
	}
	if runtime.ImageGenerationMaxDimension > 0 {
		return runtime.ImageGenerationMaxDimension
	}
	return defaultMaxDimension
}

func (s Service) allowedToolFields(ctx context.Context) []string {
	if s.Settings == nil {
		return []string{"model", "quality", "output_format", "output_compression", "moderation", "action", "input_fidelity", "partial_images"}
	}
	runtime, err := s.Settings.Runtime(ctx)
	if err != nil || len(runtime.ImageToolAllowedFields) == 0 {
		return []string{"model", "quality", "output_format", "output_compression", "moderation", "action", "input_fidelity", "partial_images"}
	}
	return runtime.ImageToolAllowedFields
}

func (s Service) List(ctx context.Context) (ListResponse, error) {
	var out ListResponse
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		rows, err := pgxTx.Query(ctx, `
			SELECT id, title, created_at, updated_at FROM image_sessions ORDER BY updated_at DESC, id DESC
		`)
		if err != nil {
			return err
		}
		var sessions []sessionRow
		for rows.Next() {
			var sess sessionRow
			if err := rows.Scan(&sess.ID, &sess.Title, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
				rows.Close()
				return err
			}
			sessions = append(sessions, sess)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		items := make([]SummaryResponse, 0, len(sessions))
		for _, sess := range sessions {
			item, err := s.serializeSummary(ctx, pgxTx, sess)
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

func (s Service) Create(ctx context.Context, title *string) (DetailResponse, error) {
	normalized := defaultTitle
	if title != nil {
		trimmed := strings.TrimSpace(*title)
		if trimmed != "" {
			if len([]rune(trimmed)) > 255 {
				return DetailResponse{}, apperr.Validation("请求体无效")
			}
			normalized = trimmed
		}
	}
	id := clockid.New()
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		_, err := pgxTx.Exec(ctx, `
			INSERT INTO image_sessions (id, title, created_at, updated_at) VALUES ($1, $2, NOW(), NOW())
		`, id, normalized)
		return err
	})
	if err != nil {
		return DetailResponse{}, err
	}
	return s.Get(ctx, id)
}

func (s Service) Get(ctx context.Context, sessionID string) (DetailResponse, error) {
	var out DetailResponse
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		detail, err := s.loadDetail(ctx, pgxTx, sessionID)
		out = detail
		return err
	})
	return out, err
}

func (s Service) Status(ctx context.Context, sessionID string) (StatusResponse, error) {
	var out StatusResponse
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		sess, err := loadSession(ctx, pgxTx, sessionID)
		if err != nil {
			return err
		}
		detail, err := s.loadDetail(ctx, pgxTx, sessionID)
		if err != nil {
			return err
		}
		var latestRoundID, latestGroup *string
		if len(detail.Rounds) > 0 {
			last := detail.Rounds[len(detail.Rounds)-1]
			latestRoundID = &last.ID
			latestGroup = last.GenerationGroupID
		}
		active := false
		for _, task := range detail.GenerationTasks {
			if task.Status == "queued" || task.Status == "running" {
				active = true
				break
			}
		}
		out = StatusResponse{
			ID: sess.ID, Title: sess.Title, RoundsCount: len(detail.Rounds),
			LatestRoundID: latestRoundID, LatestGenerationGroupID: latestGroup,
			HasActiveGenerationTask: active, GenerationTasks: detail.GenerationTasks,
			CreatedAt: sess.CreatedAt, UpdatedAt: sess.UpdatedAt,
		}
		return nil
	})
	return out, err
}

func (s Service) Update(ctx context.Context, sessionID, title string) (DetailResponse, error) {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		trimmed = defaultTitle
	}
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		if _, err := loadSession(ctx, pgxTx, sessionID); err != nil {
			return err
		}
		_, err := pgxTx.Exec(ctx, `UPDATE image_sessions SET title = $2, updated_at = NOW() WHERE id = $1`, sessionID, trimmed)
		return err
	})
	if err != nil {
		return DetailResponse{}, err
	}
	return s.Get(ctx, sessionID)
}

func (s Service) Delete(ctx context.Context, sessionID string) error {
	var deleted []media.Deleted
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		if _, err := loadSession(ctx, pgxTx, sessionID); err != nil {
			return err
		}
		rows, err := pgxTx.Query(ctx, `SELECT id, media_object_id FROM image_session_assets WHERE session_id = $1`, sessionID)
		if err != nil {
			return err
		}
		var assetIDs, mediaIDs []string
		for rows.Next() {
			var assetID, mediaID string
			if err := rows.Scan(&assetID, &mediaID); err != nil {
				rows.Close()
				return err
			}
			assetIDs = append(assetIDs, assetID)
			mediaIDs = append(mediaIDs, mediaID)
		}
		rows.Close()
		if len(assetIDs) > 0 {
			_, _ = pgxTx.Exec(ctx, `
				UPDATE product_image_assets SET source_image_session_asset_id = NULL
				WHERE source_image_session_asset_id = ANY($1)
			`, assetIDs)
		}
		if _, err := pgxTx.Exec(ctx, `DELETE FROM image_sessions WHERE id = $1`, sessionID); err != nil {
			return err
		}
		deleted, err = media.PruneUnreferenced(ctx, pgxTx, mediaIDs)
		return err
	})
	if err != nil {
		return err
	}
	for _, item := range deleted {
		_ = s.Media.Files.DeleteWithVariants(item.StoragePath)
	}
	return nil
}

func (s Service) AddReferences(ctx context.Context, sessionID string, uploads []product.Upload) (DetailResponse, error) {
	var compensation storage.Compensation
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		if _, err := loadSession(ctx, pgxTx, sessionID); err != nil {
			return err
		}
		for _, up := range uploads {
			obj, err := s.Media.Stage(ctx, pgxTx, up.Content, up.MIMEType, &compensation)
			if err != nil {
				return err
			}
			if _, err := pgxTx.Exec(ctx, `
				INSERT INTO image_session_assets (
					id, session_id, kind, original_filename, mime_type, storage_path, media_object_id, created_at
				) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
			`, clockid.New(), sessionID, kindReference, up.Filename, obj.MIMEType, obj.StoragePath, obj.ID); err != nil {
				return err
			}
		}
		_, err := pgxTx.Exec(ctx, `UPDATE image_sessions SET updated_at = NOW() WHERE id = $1`, sessionID)
		return err
	})
	if err != nil {
		compensation.Rollback()
		return DetailResponse{}, err
	}
	compensation.Release()
	return s.Get(ctx, sessionID)
}

func (s Service) DeleteReference(ctx context.Context, sessionID, assetID string) (DetailResponse, error) {
	var deleted []media.Deleted
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		if _, err := loadSession(ctx, pgxTx, sessionID); err != nil {
			return err
		}
		var kind, mediaID string
		err := pgxTx.QueryRow(ctx, `
			SELECT kind, media_object_id FROM image_session_assets WHERE id = $1 AND session_id = $2
		`, assetID, sessionID).Scan(&kind, &mediaID)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound("会话参考图不存在")
		}
		if err != nil {
			return err
		}
		if kind != kindReference {
			return apperr.Validation("只能删除会话参考图")
		}
		_, _ = pgxTx.Exec(ctx, `
			UPDATE product_image_assets SET source_image_session_asset_id = NULL WHERE source_image_session_asset_id = $1
		`, assetID)
		if _, err := pgxTx.Exec(ctx, `DELETE FROM image_session_assets WHERE id = $1`, assetID); err != nil {
			return err
		}
		_, _ = pgxTx.Exec(ctx, `UPDATE image_sessions SET updated_at = NOW() WHERE id = $1`, sessionID)
		deleted, err = media.PruneUnreferenced(ctx, pgxTx, []string{mediaID})
		return err
	})
	if err != nil {
		return DetailResponse{}, err
	}
	for _, item := range deleted {
		_ = s.Media.Files.DeleteWithVariants(item.StoragePath)
	}
	return s.Get(ctx, sessionID)
}

func (s Service) Generate(ctx context.Context, sessionID string, req GenerateRequest) (DetailResponse, error) {
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return DetailResponse{}, apperr.Validation("提示词不能为空")
	}
	if len([]rune(prompt)) > 4000 {
		return DetailResponse{}, apperr.Validation("提示词不能为空")
	}
	count := req.GenerationCount
	if count == 0 {
		count = 1
	}
	size := req.Size
	if strings.TrimSpace(size) == "" {
		size = "1024x1024"
	}
	normalizedSize, err := normalizeSize(size, s.maxDimension(ctx))
	if err != nil {
		return DetailResponse{}, err
	}
	toolOpts := filterToolOptions(req.ToolOptions, s.allowedToolFields(ctx))
	var taskID string
	err = tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		if _, err := graph.GenerationCapacityAvailable(ctx, pgxTx); err != nil {
			return fmt.Errorf("generation capacity: %w", err)
		}
		sessAssets, err := listAssets(ctx, pgxTx, sessionID)
		if err != nil {
			return fmt.Errorf("list assets: %w", err)
		}
		if len(sessAssets) == 0 {
			if _, err := loadSession(ctx, pgxTx, sessionID); err != nil {
				return err
			}
		}
		baseID, refs, err := validateGeneration(ctx, pgxTx, sessionID, req.BaseAssetID, req.SelectedReferenceAssetIDs, count)
		if err != nil {
			return err
		}
		if refs == nil {
			refs = []string{}
		}
		taskID = clockid.New()
		refJSON, err := json.Marshal(refs)
		if err != nil {
			return err
		}
		var toolJSON any
		if toolOpts != nil {
			raw, err := json.Marshal(toolOpts)
			if err != nil {
				return err
			}
			toolJSON = raw
		}
		if _, err := pgxTx.Exec(ctx, `
			INSERT INTO image_session_generation_tasks (
				id, session_id, status, prompt, size, base_asset_id, selected_reference_asset_ids,
				tool_options, generation_count, completed_candidates, attempts, is_retryable, created_at
			) VALUES ($1, $2, 'queued', $3, $4, $5, $6, $7, $8, 0, 0, TRUE, NOW())
		`, taskID, sessionID, prompt, normalizedSize, baseID, refJSON, toolJSON, count); err != nil {
			return fmt.Errorf("insert generation task: %w", err)
		}
		if _, err := pgxTx.Exec(ctx, `UPDATE image_sessions SET updated_at = NOW() WHERE id = $1`, sessionID); err != nil {
			return fmt.Errorf("touch session: %w", err)
		}
		if _, err := queue.StageForActor(ctx, pgxTx, queue.ActorImageSession, taskID, 0); err != nil {
			return fmt.Errorf("stage dispatch: %w", err)
		}
		return nil
	})
	if err != nil {
		return DetailResponse{}, err
	}
	return s.Get(ctx, sessionID)
}

func (s Service) Retry(ctx context.Context, sessionID, taskID string) (DetailResponse, error) {
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		if _, err := loadSession(ctx, pgxTx, sessionID); err != nil {
			return err
		}
		task, err := loadTask(ctx, pgxTx, sessionID, taskID)
		if err != nil {
			return err
		}
		if task.Status != "failed" {
			return apperr.Validation("只有失败的生成任务可以重试")
		}
		if !task.IsRetryable {
			return apperr.Validation("该生成任务不可重试")
		}
		if _, err := pgxTx.Exec(ctx, `
			UPDATE image_session_generation_tasks SET
				status = 'queued', active_attempt_id = NULL, failure_reason = NULL,
				started_at = NULL, finished_at = NULL, active_candidate_index = NULL,
				progress_phase = 'manual_retry_queued', progress_updated_at = NOW(),
				provider_response_id = NULL, provider_response_status = NULL,
				progress_metadata = NULL, is_retryable = TRUE
			WHERE id = $1
		`, taskID); err != nil {
			return err
		}
		_, err = queue.Requeue(ctx, pgxTx, queue.DeliveryKey(queue.ActorImageSession, taskID), queue.ActorImageSession, taskID, nil, nil, false)
		return err
	})
	if err != nil {
		return DetailResponse{}, err
	}
	return s.Get(ctx, sessionID)
}

func (s Service) Cancel(ctx context.Context, sessionID, taskID string) (DetailResponse, error) {
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		if _, err := loadSession(ctx, pgxTx, sessionID); err != nil {
			return err
		}
		task, err := loadTask(ctx, pgxTx, sessionID, taskID)
		if err != nil {
			return err
		}
		if task.Status == "cancelled" {
			return nil
		}
		if task.Status == "succeeded" || task.Status == "failed" || task.Status == "unknown" {
			return apperr.Validation("已结束的生成任务不能取消")
		}
		_, err = pgxTx.Exec(ctx, `
			UPDATE image_session_generation_tasks SET
				status = 'cancelled', active_attempt_id = NULL, failure_reason = $2,
				finished_at = NOW(), progress_phase = 'cancelled', progress_updated_at = NOW(),
				is_retryable = FALSE
			WHERE id = $1
		`, taskID, cancelledReason)
		return err
	})
	if err != nil {
		return DetailResponse{}, err
	}
	return s.Get(ctx, sessionID)
}

func (s Service) Attach(ctx context.Context, sessionID, assetID, productID string) (product.AssetResponse, error) {
	var out product.AssetResponse
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		if _, err := loadSession(ctx, pgxTx, sessionID); err != nil {
			return err
		}
		asset, err := loadAsset(ctx, pgxTx, sessionID, assetID)
		if err != nil {
			return err
		}
		if asset.Kind != kindGenerated {
			return apperr.Validation("只有生成结果可以附加到商品")
		}
		var exists string
		err = pgxTx.QueryRow(ctx, `SELECT id FROM products WHERE id = $1`, productID).Scan(&exists)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound("商品不存在")
		}
		if err != nil {
			return err
		}
		var verification string
		if err := pgxTx.QueryRow(ctx, `SELECT verification_status FROM media_objects WHERE id = $1`, asset.MediaObjectID).Scan(&verification); err != nil {
			return apperr.Conflict("会话图片引用的媒体对象不存在")
		}
		if verification == media.StatusMissing {
			return apperr.Validation("会话图片文件缺失，不能附加到商品")
		}
		var existingID string
		err = pgxTx.QueryRow(ctx, `
			SELECT id FROM product_image_assets
			WHERE product_id = $1 AND source_image_session_asset_id = $2
		`, productID, asset.ID).Scan(&existingID)
		if err == nil {
			loaded, err := product.LoadAssetRow(ctx, pgxTx, existingID)
			if err != nil {
				return err
			}
			out = product.SerializeAsset(loaded)
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		created, err := product.InsertAssetIdentity(ctx, pgxTx, product.AssetIdentityInput{
			ProductID:                 productID,
			MediaID:                   asset.MediaObjectID,
			Filename:                  asset.OriginalFilename,
			Origin:                    "image_session_attach",
			SourceImageSessionAssetID: &asset.ID,
		})
		if err != nil {
			return err
		}
		loaded, err := product.LoadAssetRow(ctx, pgxTx, created.ID)
		if err != nil {
			return err
		}
		out = product.SerializeAsset(loaded)
		return nil
	})
	return out, err
}

func (s Service) AssetDownload(ctx context.Context, assetID string) (assetRow, error) {
	var row assetRow
	err := s.Pool.QueryRow(ctx, `
		SELECT a.id, a.session_id, a.kind, a.original_filename, a.mime_type, m.storage_path, a.media_object_id, a.created_at
		FROM image_session_assets a
		JOIN media_objects m ON m.id = a.media_object_id
		WHERE a.id = $1
	`, assetID).Scan(&row.ID, &row.SessionID, &row.Kind, &row.OriginalFilename, &row.MIMEType, &row.StoragePath, &row.MediaObjectID, &row.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return assetRow{}, apperr.NotFound("会话图片不存在")
	}
	return row, err
}

func (s Service) Reconcile(ctx context.Context, sessionID, taskID string, candidateStart int) (EffectResponse, error) {
	var out EffectResponse
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		task, err := loadTask(ctx, pgxTx, sessionID, taskID)
		if err != nil {
			if apperr.IsNotFound(err) {
				return apperr.Conflict("连续生图任务不存在")
			}
			return err
		}
		effect, err := loadEffect(ctx, pgxTx, task.ID, candidateStart)
		if err != nil {
			return err
		}
		if task.Status != "unknown" && effect.EffectResult != "unknown" {
			return apperr.Conflict("只有 unknown 连续生图任务可以执行 provider effect 对账")
		}
		if effect.EffectResult == "applied" || effect.EffectResult == "failed" {
			out = effect
			return nil
		}
		now := time.Now().UTC()
		detail := "当前图片会话 provider 不支持查询原请求"
		if _, err := pgxTx.Exec(ctx, `
			UPDATE image_session_provider_effects SET
				reconciliation_state = 'unsupported', detail = $2, updated_at = $3
			WHERE id = $1
		`, effect.ID, detail, now); err != nil {
			return err
		}
		effect.ReconciliationState = "unsupported"
		effect.Detail = &detail
		effect.UpdatedAt = now
		out = effect
		return nil
	})
	return out, err
}
