// Package imagesession 实现连续生图会话，职责与 WorkflowGraphRun 和 AgentTask 分离。
//
// 职责：一轮会话里反复 chat 出候选图，用户再 attach 进商品图库。不是画布 GraphRun，也不是 Goal。
// 调用时机：HTTP 创建/生成/取消；worker 执行 image_session 任务。Retry 只接受 failed，unknown 不能走 Retry。
// 副作用：写 image_sessions、rounds、generation_tasks、provider_effects、session assets。
// 错误：会话不存在 NotFound。Generate 在已有任务 running 时仍可再入队，不会 Conflict。
// Delete 会拆掉商品图上的 source_image_session_asset_id，不因运行中任务拒绝。
// 禁区：不要把会话状态写进 workflow_graphs；不要把 attach 当成节点绑定的唯一路径。
package imagesession

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/auth"
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

// Service 拥有连续生图会话、参考图、生成任务与 attach 到商品。
// Pool 给 SSE LISTEN；Media 写 bytes。Settings 为 nil 时上限走内置默认。
// 不要在这里写 workflow_graphs 或 AgentTask。
type Service struct {
	DB    *gorm.DB      // 命令事务
	Pool  *pgxpool.Pool // SSE LISTEN；会话状态推送
	Media media.Store   // 写会话素材 bytes
	// Settings 为 nil 时生图上限与 tool 字段走内置默认。
	Settings interface {
		settings.RuntimeReader
		settings.LimitsReader
	}
	// Reconciler 对账供应商原请求；nil 则跳过 ReconcileResponse，不可证明时保持 unknown。
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

// List 按 updated_at、id 倒序返回一页会话摘要。
// 调用时机：HTTP GET /api/image-sessions。无写入。空库返回 Items=[]、NextCursor=null。
func (s Service) List(ctx context.Context, after string, limit int) (ListResponse, error) {
	if limit == 0 {
		limit = imageSessionListDefaultLimit
	}
	if limit < 1 || limit > imageSessionListMaxLimit {
		return ListResponse{}, apperr.Validation("会话列表 limit 必须在 1 到 100 之间")
	}
	cursor, cursorAt, hasCursor := decodeImageSessionListCursor(after)
	if strings.TrimSpace(after) != "" && !hasCursor {
		return ListResponse{}, apperr.Validation("会话列表游标无效")
	}

	var out ListResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		query := auth.ScopeMerchant(ctx, pgxTx.Select("id", "title", "created_at", "updated_at"), "merchant_id")
		if hasCursor {
			query = query.Where("updated_at < ? OR (updated_at = ? AND id < ?)", cursorAt, cursorAt, cursor.ID)
		}
		var sessions []schema.ImageSessions
		if err := query.Order("updated_at DESC, id DESC").Limit(limit + 1).Find(&sessions).Error; err != nil {
			return err
		}
		hasMore := len(sessions) > limit
		if hasMore {
			sessions = sessions[:limit]
		}
		items, err := serializeSessionSummaries(ctx, pgxTx, sessions)
		if err != nil {
			return err
		}
		out.Items = items
		if hasMore {
			last := sessions[len(sessions)-1]
			next, err := encodeImageSessionListCursor(last.UpdatedAt, last.ID)
			if err != nil {
				return err
			}
			out.NextCursor = &next
		}
		return nil
	})
	return out, err
}

// Create 新建连续生图会话，然后 Get 详情。
// 调用时机：HTTP POST /api/image-sessions。title 为 nil/空白用「未命名会话」；超 255 字 Validation。
// 副作用：插入 image_sessions 一行。不创建生成任务。
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
	merchantID := auth.ResolveMerchantID(ctx)
		now := time.Now().UTC()
		row := schema.ImageSessions{ID: id, MerchantID: merchantID, Title: normalized, CreatedAt: now, UpdatedAt: now}
		return pgxTx.Create(&row).Error
	})
	if err != nil {
		return DetailResponse{}, err
	}
	return s.Get(ctx, id)
}

// Get 读取会话首屏详情（参考图、最新一轮页、活动任务）。找不到返回 NotFound。
// 调用时机：HTTP GET 详情，以及 Create/Update/Generate 成功后的回读。无写入。完整历史请用 History。
func (s Service) Get(ctx context.Context, sessionID string) (DetailResponse, error) {
	var out DetailResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		detail, err := s.loadDetail(ctx, pgxTx, sessionID)
		out = detail
		return err
	})
	return out, err
}

// History 按 created_at、id 倒序返回一页已落盘轮次。after 是不透明游标。
// 调用时机：HTTP GET /history。找不到会话 NotFound；游标无效 Validation。
func (s Service) History(ctx context.Context, sessionID, after string, limit int) (HistoryResponse, error) {
	if limit == 0 {
		limit = imageSessionHistoryDefaultLimit
	}
	if limit < 1 || limit > imageSessionHistoryMaxLimit {
		return HistoryResponse{}, apperr.Validation("会话历史 limit 必须在 1 到 100 之间")
	}
	cursor, cursorAt, hasCursor := decodeImageSessionHistoryCursor(after)
	if strings.TrimSpace(after) != "" && !hasCursor {
		return HistoryResponse{}, apperr.Validation("会话历史游标无效")
	}
	var out HistoryResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		page, err := s.loadHistory(ctx, pgxTx, sessionID, cursor, cursorAt, hasCursor, limit)
		out = page
		return err
	})
	if err != nil {
		return HistoryResponse{}, err
	}
	if out.Items == nil {
		out.Items = []RoundResponse{}
	}
	return out, nil
}

// Status 读取会话轻量状态，含是否仍有 queued/running 任务。
// 调用时机：HTTP GET /status 与 SSE。找不到 NotFound。不要把本结果当 DetailResponse 用。
func (s Service) Status(ctx context.Context, sessionID string) (StatusResponse, error) {
	var out StatusResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		status, err := s.loadStatus(ctx, pgxTx, sessionID)
		out = status
		return err
	})
	return out, err
}

// Update 只改会话标题。调用时机：HTTP PATCH。空白或超长 Validation；找不到 NotFound。
// 不取消进行中的生成任务。
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

// Delete 删除会话及其素材；已附加到商品的引用会被断开，未再被引用的 MediaObject 会被清理。
// Delete 删除会话及其素材；已附加到商品的引用会被断开，未再被引用的 MediaObject 会被清理。
// 会话不存在返回 NotFound「连续生图会话不存在」。不因运行中任务拒绝。磁盘清理错误不回传。
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

// AddReferences 把已校验上传写成会话参考图（kind=reference_upload）。
// 调用时机：HTTP POST reference-images。找不到会话 NotFound。失败 Rollback 已 Stage 文件。
// 禁区：不要把参考图当 generated_image，也不能走 Attach。
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

// DeleteReference 只删参考图，不删已生成结果。
// 调用时机：HTTP DELETE reference-images/:asset_id。非参考图 Validation；找不到 NotFound。
// 副作用：断开商品图上的 source_image_session_asset_id，无引用则 Prune MediaObject。
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

// Generate 创建 queued 生成任务并写入 PENDING dispatch；HTTP 不直接入队 broker。
// 容量 admission 在 worker claim，入队不持 generation advisory、不增加 denied。
// 提示词空/超长、尺寸或 tool 非法返回 Validation；会话或图片不存在返回 NotFound。
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
		return publishSession(ctx, pgxTx, sessionID)
	})
	if err != nil {
		return DetailResponse{}, err
	}
	return s.Get(ctx, sessionID)
}

// Retry 把可重试的 failed 任务重新标 queued 并补 PENDING dispatch；unknown 不会被当成失败重试。
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
		if _, err := queue.Requeue(ctx, pgxTx, queue.DeliveryKey(queue.ActorImageSession, taskID), queue.ActorImageSession, taskID, nil, nil, false); err != nil {
			return err
		}
		return publishSession(ctx, pgxTx, sessionID)
	})
	if err != nil {
		return DetailResponse{}, err
	}
	return s.Get(ctx, sessionID)
}

// Cancel 取消尚未终态的生成任务（queued/running → cancelled）。
// 调用时机：HTTP POST .../cancel。已 succeeded/failed/unknown 返回 Validation；已 cancelled 幂等成功。
// HTTP 不入队 broker。unknown 任务不能当失败取消后再 Retry。
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
		if err != nil {
			return err
		}
		return publishSession(ctx, pgxTx, sessionID)
	})
	if err != nil {
		return DetailResponse{}, err
	}
	return s.Get(ctx, sessionID)
}

// Attach 把生成结果作为商品图片身份写入商品图库，复用同一 MediaObject，不复制 bytes。
// 非生成结果或文件缺失返回 Validation；会话/商品跨商或不存在统一 NotFoundCrossMerchant；媒体行缺失返回 Conflict。
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
		err = auth.ScopeMerchant(ctx, pgxTx.Where("id = ?", productID), "merchant_id").Take(&productRow).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return auth.NotFoundCrossMerchant()
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

// AssetDownload 读取会话素材行供下载；缺失与跨商统一 NotFoundCrossMerchant（F4 / B1）。
func (s Service) AssetDownload(ctx context.Context, assetID string) (assetRow, error) {
	var asset schema.ImageSessionAssets
	q := s.DB.WithContext(ctx).
		Table("image_session_assets AS a").
		Select("a.*").
		Joins("JOIN image_sessions AS s ON s.id = a.session_id").
		Where("a.id = ?", assetID)
	err := auth.ScopeMerchant(ctx, q, "s.merchant_id").Take(&asset).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return assetRow{}, auth.NotFoundCrossMerchant()
	}
	if err != nil {
		return assetRow{}, err
	}
	return assetFromModels(s.DB.WithContext(ctx), asset)
}

// Reconcile 对 unknown 生成任务查询供应商原请求；不可证明时保持 unknown。
// 会话跨商/缺失统一 NotFoundCrossMerchant；任务不存在或 effect 找不到返回 Conflict；非 unknown 状态也 Conflict。
func (s Service) Reconcile(ctx context.Context, sessionID, taskID string, candidateStart int) (EffectResponse, error) {
	var out EffectResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if _, err := loadSession(ctx, pgxTx, sessionID); err != nil {
			return err
		}
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
