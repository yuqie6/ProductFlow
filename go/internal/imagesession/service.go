package imagesession

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/settings"
	"gorm.io/gorm"
)

type Service struct {
	DB       *gorm.DB
	Media    media.Store
	Settings interface {
		settings.RuntimeReader
		settings.LimitsReader
	}
	Reconciler interface {
		ReconcileResponse(ctx context.Context, responseID string) (string, error)
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
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		var sessions []schema.ImageSessions
		if err := pgxTx.Order("updated_at DESC, id DESC").Find(&sessions).Error; err != nil {
			return err
		}
		items := make([]SummaryResponse, 0, len(sessions))
		for _, sess := range sessions {
			item, err := s.serializeSummary(ctx, pgxTx, sessionFromModel(sess))
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
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		now := time.Now().UTC()
		row := schema.ImageSessions{ID: id, Title: normalized, CreatedAt: now, UpdatedAt: now}
		return pgxTx.Create(&row).Error
	})
	if err != nil {
		return DetailResponse{}, err
	}
	return s.Get(ctx, id)
}

func (s Service) Get(ctx context.Context, sessionID string) (DetailResponse, error) {
	var out DetailResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		detail, err := s.loadDetail(ctx, pgxTx, sessionID)
		out = detail
		return err
	})
	return out, err
}

func (s Service) Status(ctx context.Context, sessionID string) (StatusResponse, error) {
	var out StatusResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
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
	if trimmed == "" || len([]rune(trimmed)) > 255 {
		return DetailResponse{}, apperr.Validation("请求体无效")
	}
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if _, err := loadSession(ctx, pgxTx, sessionID); err != nil {
			return err
		}
		return pgxTx.Model(&schema.ImageSessions{}).Where("id = ?", sessionID).Updates(map[string]any{
			"title": trimmed, "updated_at": time.Now().UTC(),
		}).Error
	})
	if err != nil {
		return DetailResponse{}, err
	}
	return s.Get(ctx, sessionID)
}

func (s Service) Delete(ctx context.Context, sessionID string) error {
	var deleted []media.Deleted
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if _, err := loadSession(ctx, pgxTx, sessionID); err != nil {
			return err
		}
		var assets []schema.ImageSessionAssets
		if err := pgxTx.Where("session_id = ?", sessionID).Find(&assets).Error; err != nil {
			return err
		}
		var assetIDs, mediaIDs []string
		for _, a := range assets {
			assetIDs = append(assetIDs, a.ID)
			mediaIDs = append(mediaIDs, a.MediaObjectID)
		}
		if len(assetIDs) > 0 {
			_ = pgxTx.Model(&schema.ProductImageAssets{}).
				Where("source_image_session_asset_id IN ?", assetIDs).
				Updates(map[string]any{"source_image_session_asset_id": nil}).Error
		}
		if err := pgxTx.Where("id = ?", sessionID).Delete(&schema.ImageSessions{}).Error; err != nil {
			return err
		}
		var pruneErr error
		deleted, pruneErr = media.PruneUnreferenced(ctx, pgxTx, mediaIDs)
		return pruneErr
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
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if _, err := loadSession(ctx, pgxTx, sessionID); err != nil {
			return err
		}
		for _, up := range uploads {
			obj, err := s.Media.Stage(ctx, pgxTx, up.Content, up.MIMEType, &compensation)
			if err != nil {
				return err
			}
			now := time.Now().UTC()
			row := schema.ImageSessionAssets{
				ID: clockid.New(), SessionID: sessionID, Kind: kindReference,
				OriginalFilename: up.Filename, MIMEType: obj.MIMEType, StoragePath: obj.StoragePath,
				MediaObjectID: obj.ID, CreatedAt: now,
			}
			if err := pgxTx.Create(&row).Error; err != nil {
				return err
			}
		}
		return pgxTx.Model(&schema.ImageSessions{}).Where("id = ?", sessionID).Updates(map[string]any{
			"updated_at": time.Now().UTC(),
		}).Error
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
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if _, err := loadSession(ctx, pgxTx, sessionID); err != nil {
			return err
		}
		var asset schema.ImageSessionAssets
		err := pgxTx.Where("id = ? AND session_id = ?", assetID, sessionID).Take(&asset).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperr.NotFound("会话参考图不存在")
		}
		if err != nil {
			return err
		}
		if asset.Kind != kindReference {
			return apperr.Validation("只能删除会话参考图")
		}
		_ = pgxTx.Model(&schema.ProductImageAssets{}).
			Where("source_image_session_asset_id = ?", assetID).
			Updates(map[string]any{"source_image_session_asset_id": nil}).Error
		if err := pgxTx.Where("id = ?", assetID).Delete(&schema.ImageSessionAssets{}).Error; err != nil {
			return err
		}
		_ = pgxTx.Model(&schema.ImageSessions{}).Where("id = ?", sessionID).Updates(map[string]any{"updated_at": time.Now().UTC()}).Error
		deleted, err = media.PruneUnreferenced(ctx, pgxTx, []string{asset.MediaObjectID})
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
	if utf8.RuneCountInString(prompt) > 4000 {
		return DetailResponse{}, apperr.Validation("提示词不能超过 4000 个字符")
	}
	count := 1
	if req.GenerationCount != nil {
		count = *req.GenerationCount
	}
	size := req.Size
	if strings.TrimSpace(size) == "" {
		size = "1024x1024"
	}
	normalizedSize, err := normalizeSize(size, s.maxDimension(ctx))
	if err != nil {
		return DetailResponse{}, err
	}
	if err := validateToolOptions(req.ToolOptions); err != nil {
		return DetailResponse{}, err
	}
	toolOpts := filterToolOptions(req.ToolOptions, s.allowedToolFields(ctx))
	var taskID string
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
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
		now := time.Now().UTC()
		refsStr := string(refJSON)
		var toolPtr *string
		if toolJSON != nil {
			s := string(toolJSON.([]byte))
			toolPtr = &s
		}
		row := schema.ImageSessionGenerationTasks{
			ID: taskID, SessionID: sessionID, Status: "queued", Prompt: prompt, Size: normalizedSize,
			BaseAssetID: baseID, SelectedReferenceAssetIds: &refsStr, ToolOptions: toolPtr,
			GenerationCount: count, CompletedCandidates: 0, Attempts: 0, IsRetryable: true, CreatedAt: now,
		}
		if err := pgxTx.Create(&row).Error; err != nil {
			return fmt.Errorf("insert generation task: %w", err)
		}
		if err := pgxTx.Model(&schema.ImageSessions{}).Where("id = ?", sessionID).Updates(map[string]any{
			"updated_at": now,
		}).Error; err != nil {
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
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
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
		now := time.Now().UTC()
		if err := pgxTx.Model(&schema.ImageSessionGenerationTasks{}).Where("id = ?", taskID).Updates(map[string]any{
			"status":                   "queued",
			"active_attempt_id":        nil,
			"failure_reason":           nil,
			"started_at":               nil,
			"finished_at":              nil,
			"active_candidate_index":   nil,
			"progress_phase":           "manual_retry_queued",
			"progress_updated_at":      now,
			"provider_response_id":     nil,
			"provider_response_status": nil,
			"progress_metadata":        nil,
			"is_retryable":             true,
		}).Error; err != nil {
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
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
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
		now := time.Now().UTC()
		err = pgxTx.Model(&schema.ImageSessionGenerationTasks{}).Where("id = ?", taskID).Updates(map[string]any{
			"status":              "cancelled",
			"active_attempt_id":   nil,
			"failure_reason":      cancelledReason,
			"finished_at":         now,
			"progress_phase":      "cancelled",
			"progress_updated_at": now,
			"is_retryable":        false,
		}).Error
		return err
	})
	if err != nil {
		return DetailResponse{}, err
	}
	return s.Get(ctx, sessionID)
}

func (s Service) Attach(ctx context.Context, sessionID, assetID, productID string) (product.AssetResponse, error) {
	var out product.AssetResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
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
		var productRow schema.Products
		err = pgxTx.Where("id = ?", productID).Take(&productRow).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperr.NotFound("商品不存在")
		}
		if err != nil {
			return err
		}
		var mediaObj schema.MediaObjects
		if err := pgxTx.Where("id = ?", asset.MediaObjectID).Take(&mediaObj).Error; err != nil {
			return apperr.Conflict("会话图片引用的媒体对象不存在")
		}
		if mediaObj.VerificationStatus == media.StatusMissing {
			return apperr.Validation("会话图片文件缺失，不能附加到商品")
		}
		var existing schema.ProductImageAssets
		err = pgxTx.Where("product_id = ? AND source_image_session_asset_id = ?", productID, asset.ID).Take(&existing).Error
		if err == nil {
			loaded, err := product.LoadAssetRow(ctx, pgxTx, existing.ID)
			if err != nil {
				return err
			}
			out = product.SerializeAsset(loaded)
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
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
	var asset schema.ImageSessionAssets
	err := s.DB.WithContext(ctx).Where("id = ?", assetID).Take(&asset).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return assetRow{}, apperr.NotFound("会话图片不存在")
	}
	if err != nil {
		return assetRow{}, err
	}
	return assetFromModels(s.DB.WithContext(ctx), asset)
}

func (s Service) Reconcile(ctx context.Context, sessionID, taskID string, candidateStart int) (EffectResponse, error) {
	var out EffectResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
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
		state := "unsupported"
		detail := "当前图片会话 provider 不支持查询原请求"
		if s.Reconciler != nil && effect.ProviderResponseID != nil && strings.TrimSpace(*effect.ProviderResponseID) != "" {
			result, recErr := s.Reconciler.ReconcileResponse(ctx, *effect.ProviderResponseID)
			if recErr != nil {
				state = "unknown"
				detail = recErr.Error()
			} else {
				switch result {
				case "applied":
					state = "applied"
					detail = ""
				case "failed":
					state = "not_applied"
					detail = "供应商记录显示图片请求未完成"
				case "unknown":
					state = "unknown"
					detail = "供应商 response 没有足够的图片结果证据"
				default:
					state = "unsupported"
				}
			}
		}
		effectResult := effect.EffectResult
		if state == "applied" {
			effectResult = "applied"
		}
		if state == "not_applied" {
			effectResult = "failed"
		}
		var detailPtr *string
		if detail != "" {
			detailPtr = &detail
		}
		if err := pgxTx.Model(&schema.ImageSessionProviderEffects{}).Where("id = ?", effect.ID).Updates(map[string]any{
			"reconciliation_state": state,
			"effect_result":        effectResult,
			"detail":               detailPtr,
			"updated_at":           now,
		}).Error; err != nil {
			return err
		}
		effect.ReconciliationState = state
		effect.EffectResult = effectResult
		if detail == "" {
			effect.Detail = nil
		} else {
			effect.Detail = &detail
		}
		effect.UpdatedAt = now
		out = effect
		return nil
	})
	return out, err
}
