package localedit

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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
	"github.com/yuqie6/productflow/internal/product"
	"gorm.io/gorm"
)

type Service struct {
	DB       *gorm.DB
	Media    media.Store
	Provider Provider
}

func (s Service) provider() Provider {
	if s.Provider != nil {
		return s.Provider
	}
	return MockProvider{}
}

func (s Service) Capability() CapabilityResponse {
	return s.provider().Capability().Response()
}

func (s Service) Create(ctx context.Context, productID, sourceAssetID, targetNodeID string, draft Draft, maskPNG []byte) (TaskResponse, error) {
	var taskID string
	var compensation storage.Compensation
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if err := lockProduct(ctx, pgxTx, productID); err != nil {
			return err
		}
		source, sha, err := lockSource(ctx, pgxTx, productID, sourceAssetID)
		if err != nil {
			return err
		}
		target, err := validateTarget(ctx, pgxTx, productID, source.ID, targetNodeID)
		if err != nil {
			return err
		}
		refs, err := lockReferences(ctx, pgxTx, productID, draft.ReferenceAssetIDs)
		if err != nil {
			return err
		}
		if source.Width == nil || source.Height == nil {
			return apperr.Validation("局部编辑源图片缺少尺寸")
		}
		normalized, err := normalizeMask(*source.Width, *source.Height, maskPNG, draft.MaskGeometry)
		if err != nil {
			return err
		}
		taskID = clockid.New()
		obj, err := s.Media.Stage(ctx, pgxTx, normalized, "image/png", &compensation)
		if err != nil {
			return err
		}
		geoJSON, err := json.Marshal(draft.MaskGeometry)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		row := schema.LocalImageEditTasks{
			ID: taskID, ProductID: productID, SourceAssetID: source.ID, SourceMediaSHA256: sha,
			MaskMediaObjectID: obj.ID, TargetGraphID: target.GraphID, TargetNodeID: target.NodeID,
			TargetGraphRevision: target.Revision, SourceArtifactID: target.ArtifactID,
			SourceArtifactAssetID: target.AssetID, SourceArtifactInputDigest: target.InputDigest,
			Operation: draft.Operation, Instruction: draft.Instruction, SourceText: draft.SourceText,
			ReplacementText: draft.ReplacementText, MaskGeometryJSON: string(geoJSON),
			Status: "draft", Revision: 1, Attempts: 0, IsRetryable: true, CreatedAt: now, UpdatedAt: now,
		}
		if err := pgxTx.Create(&row).Error; err != nil {
			return err
		}
		if err := replaceReferences(ctx, pgxTx, taskID, refs); err != nil {
			return err
		}
		return pgxTx.Model(&schema.Products{}).Where("id = ?", productID).Updates(map[string]any{"updated_at": now}).Error
	})
	if err != nil {
		compensation.Rollback()
		return TaskResponse{}, err
	}
	compensation.Release()
	return s.Get(ctx, productID, taskID, true)
}

func (s Service) Update(ctx context.Context, productID, taskID string, expectedRevision int, draft Draft, maskPNG []byte) (TaskResponse, error) {
	var compensation storage.Compensation
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		task, err := loadTaskForUpdate(ctx, pgxTx, productID, taskID)
		if err != nil {
			return err
		}
		if err := lockProduct(ctx, pgxTx, productID); err != nil {
			return err
		}
		if task.Status != "draft" {
			return apperr.Conflict("局部编辑任务已提交，intent、mask 和 references 不可修改")
		}
		if task.Revision != expectedRevision {
			return apperr.Conflict("局部编辑 draft 已变化，请刷新后重试")
		}
		source, _, err := lockSource(ctx, pgxTx, productID, task.SourceAssetID)
		if err != nil {
			return err
		}
		refs, err := lockReferences(ctx, pgxTx, productID, draft.ReferenceAssetIDs)
		if err != nil {
			return err
		}
		geoJSON, err := json.Marshal(draft.MaskGeometry)
		if err != nil {
			return err
		}
		maskID := task.MaskMediaID
		if len(maskPNG) > 0 {
			if source.Width == nil || source.Height == nil {
				return apperr.Validation("局部编辑源图片缺少尺寸")
			}
			normalized, err := normalizeMask(*source.Width, *source.Height, maskPNG, draft.MaskGeometry)
			if err != nil {
				return err
			}
			obj, err := s.Media.Stage(ctx, pgxTx, normalized, "image/png", &compensation)
			if err != nil {
				return err
			}
			maskID = obj.ID
		}
		if err := pgxTx.Model(&schema.LocalImageEditTasks{}).Where("id = ?", taskID).Updates(map[string]any{
			"operation":            draft.Operation,
			"instruction":          draft.Instruction,
			"source_text":          draft.SourceText,
			"replacement_text":     draft.ReplacementText,
			"mask_geometry_json":   string(geoJSON),
			"mask_media_object_id": maskID,
			"revision":             gorm.Expr("revision + 1"),
			"updated_at":           time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
		if err := replaceReferences(ctx, pgxTx, taskID, refs); err != nil {
			return err
		}
		return pgxTx.Model(&schema.Products{}).Where("id = ?", productID).Updates(map[string]any{"updated_at": time.Now().UTC()}).Error
	})
	if err != nil {
		compensation.Rollback()
		return TaskResponse{}, err
	}
	compensation.Release()
	return s.Get(ctx, productID, taskID, true)
}

func (s Service) Get(ctx context.Context, productID, taskID string, includeAudit bool) (TaskResponse, error) {
	var out TaskResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		row, err := loadTask(ctx, pgxTx, productID, taskID)
		if err != nil {
			return err
		}
		out, err = serializeTask(ctx, pgxTx, row, includeAudit)
		return err
	})
	return out, err
}

func (s Service) List(ctx context.Context, productID string, limit int) (TaskListResponse, error) {
	if limit < 1 || limit > 100 {
		return TaskListResponse{}, apperr.Validation("局部编辑任务 limit 必须在 1 到 100 之间")
	}
	var out TaskListResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		var productRow schema.Products
		err := pgxTx.Where("id = ?", productID).Take(&productRow).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperr.NotFound("商品不存在")
		}
		if err != nil {
			return err
		}
		var collected []schema.LocalImageEditTasks
		if err := pgxTx.Where("product_id = ?", productID).Order("created_at DESC, id DESC").Limit(limit).Find(&collected).Error; err != nil {
			return err
		}
		items := make([]TaskResponse, 0, len(collected))
		for _, model := range collected {
			row := taskFromModel(model)
			ids, err := listReferenceIDs(ctx, pgxTx, row.ID)
			if err != nil {
				return err
			}
			row.ReferenceIDs = ids
			item, err := serializeTask(ctx, pgxTx, row, false)
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

func (s Service) Submit(ctx context.Context, productID, taskID, idempotencyKey string) (TaskResponse, error) {
	cap := s.provider().Capability()
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		task, err := loadTaskForUpdate(ctx, pgxTx, productID, taskID)
		if err != nil {
			return err
		}
		if err := validateCapability(task, cap); err != nil {
			return err
		}
		if cap.Mode == "" {
			reason := cap.Reason
			if reason == "" {
				reason = "当前图片 provider 不支持局部编辑"
			}
			return apperr.Validation(reason)
		}
		key, err := normalizeIdempotency(idempotencyKey)
		if err != nil {
			return err
		}
		providerName, err := normalizeIntent(cap.ProviderName, "provider name")
		if err != nil {
			return err
		}
		mode, err := normalizeIntent(cap.Mode, "local edit mode")
		if err != nil {
			return err
		}
		if mode != modeMasked {
			return apperr.Validation("局部编辑 provider 能力模式不受支持")
		}
		if err := lockProduct(ctx, pgxTx, productID); err != nil {
			return err
		}
		hash, err := requestHash(ctx, pgxTx, task, providerName, mode)
		if err != nil {
			return err
		}
		var existing schema.LocalImageEditTasks
		err = pgxTx.Clauses(pfdb.ForUpdate()).Where("product_id = ? AND idempotency_key = ?", productID, key).Take(&existing).Error
		if err == nil {
			loaded, err := loadTask(ctx, pgxTx, productID, existing.ID)
			if err != nil {
				return err
			}
			if loaded.RequestHash == nil || *loaded.RequestHash != hash {
				return apperr.Conflict("相同 idempotency key 不能提交不同的局部编辑请求")
			}
			_, err = queue.Stage(ctx, pgxTx, queue.DeliveryKey(queue.ActorLocalEdit, loaded.ID), queue.ActorLocalEdit, loaded.ID, map[string]any{
				"task_id": loaded.ID, "request_hash": hash,
			}, nil)
			return err
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if task.Status != "draft" {
			return apperr.Conflict("局部编辑任务已提交，不能重复提交新的 idempotency key")
		}
		now := time.Now().UTC()
		if err := pgxTx.Model(&schema.LocalImageEditTasks{}).Where("id = ?", taskID).Updates(map[string]any{
			"status":                    "queued",
			"idempotency_key":           key,
			"request_hash":              hash,
			"requested_provider_name":   providerName,
			"requested_local_edit_mode": mode,
			"progress_phase":            "queued",
			"queued_at":                 now,
			"failure_reason":            nil,
			"is_retryable":              true,
			"updated_at":                now,
		}).Error; err != nil {
			return err
		}
		if _, err := queue.Stage(ctx, pgxTx, queue.DeliveryKey(queue.ActorLocalEdit, taskID), queue.ActorLocalEdit, taskID, map[string]any{
			"task_id": taskID, "request_hash": hash,
		}, nil); err != nil {
			return err
		}
		return pgxTx.Model(&schema.Products{}).Where("id = ?", productID).Updates(map[string]any{"updated_at": now}).Error
	})
	if err != nil {
		return TaskResponse{}, err
	}
	return s.Get(ctx, productID, taskID, true)
}

func (s Service) Retry(ctx context.Context, productID, taskID string, expectedRevision *int) (TaskResponse, error) {
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		task, err := loadTaskForUpdate(ctx, pgxTx, productID, taskID)
		if err != nil {
			return err
		}
		if expectedRevision != nil && task.Revision != *expectedRevision {
			return apperr.Conflict("局部编辑任务已变化，请刷新后重试")
		}
		if task.Status != "failed" || !task.IsRetryable {
			return apperr.Conflict("只有可重试的 failed 局部编辑任务才能重试")
		}
		if task.RequestHash == nil || task.IdempotencyKey == nil {
			return apperr.Conflict("局部编辑任务缺少不可变请求身份，不能重试")
		}
		if err := pgxTx.Model(&schema.LocalImageEditTasks{}).Where("id = ?", taskID).Updates(map[string]any{
			"status":            "queued",
			"active_attempt_id": nil,
			"progress_phase":    "queued",
			"failure_reason":    nil,
			"is_retryable":      true,
			"queued_at":         time.Now().UTC(),
			"started_at":        nil,
			"finished_at":       nil,
			"updated_at":        time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
		_, err = queue.Requeue(ctx, pgxTx, queue.DeliveryKey(queue.ActorLocalEdit, taskID), queue.ActorLocalEdit, taskID, map[string]any{
			"task_id": taskID, "request_hash": *task.RequestHash,
		}, nil, false)
		return err
	})
	if err != nil {
		return TaskResponse{}, err
	}
	return s.Get(ctx, productID, taskID, true)
}

func (s Service) Cancel(ctx context.Context, productID, taskID string, expectedRevision *int) (TaskResponse, error) {
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		task, err := loadTaskForUpdate(ctx, pgxTx, productID, taskID)
		if err != nil {
			return err
		}
		if expectedRevision != nil && task.Revision != *expectedRevision {
			return apperr.Conflict("局部编辑任务已变化，请刷新后重试")
		}
		switch task.Status {
		case "succeeded", "failed", "unknown":
			return apperr.Conflict("已完成的局部编辑任务不能取消")
		case "cancelled":
			return nil
		}
		if task.ActiveAttemptID != nil {
			phase := "failed"
			result := "failed"
			if task.ProgressPhase != nil && *task.ProgressPhase != "claimed" {
				phase = "unknown"
				result = "unknown"
			}
			detail := "局部编辑任务已取消；provider boundary 之后的结果只能作为审计"
			_ = pgxTx.Model(&schema.LocalImageEditProviderAttempts{}).
				Where("task_id = ? AND attempt_id = ?", taskID, *task.ActiveAttemptID).
				Updates(map[string]any{
					"phase": phase, "effect_result": result, "detail": detail, "updated_at": time.Now().UTC(),
				}).Error
		}
		now := time.Now().UTC()
		err = pgxTx.Model(&schema.LocalImageEditTasks{}).Where("id = ?", taskID).Updates(map[string]any{
			"status":            "cancelled",
			"active_attempt_id": nil,
			"progress_phase":    "cancelled",
			"failure_reason":    cancelReason,
			"is_retryable":      false,
			"finished_at":       now,
			"updated_at":        now,
		}).Error
		return err
	})
	if err != nil {
		return TaskResponse{}, err
	}
	return s.Get(ctx, productID, taskID, true)
}

func (s Service) Adopt(ctx context.Context, productID, taskID, expectedArtifactID string) (TaskResponse, error) {
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		task, err := loadTaskForUpdate(ctx, pgxTx, productID, taskID)
		if err != nil {
			return err
		}
		if task.Status != "succeeded" || task.ResultAssetID == nil {
			return apperr.Conflict("只有成功且有结果资产的局部编辑任务才能 adoption")
		}
		if task.TargetGraphID == nil || task.TargetNodeID == nil || task.SourceArtifactID == nil {
			return apperr.Conflict("局部编辑任务没有可 adoption 的目标 artifact")
		}
		if err := lockProduct(ctx, pgxTx, productID); err != nil {
			return err
		}
		if task.SourceArtifactAssetID == nil || *task.SourceArtifactAssetID != task.SourceAssetID {
			return apperr.Conflict("局部编辑 source artifact 与任务快照不一致")
		}
		result, err := product.LoadAssetRow(ctx, pgxTx, *task.ResultAssetID)
		if err != nil || result.ProductID != productID {
			return apperr.Conflict("局部编辑 lineage 记录不完整")
		}
		lineage, err := graph.LoadArtifactLineage(ctx, pgxTx, *task.SourceArtifactID)
		if err != nil {
			return err
		}
		sourceDigest := lineage.InputDigest
		sourceRevision := lineage.GraphRevision
		payload := map[string]any{
			"kind": "local_image_edit", "task_id": task.ID, "source_asset_id": task.SourceAssetID,
			"source_artifact_id": *task.SourceArtifactID, "source_artifact_input_digest": sourceDigest,
			"source_graph_revision": sourceRevision, "result_asset_id": result.ID,
			"operation": task.Operation, "mask_media_object_id": task.MaskMediaID, "task_revision": task.Revision,
		}
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		hash, err := canonjson.SHA256Hex(payload)
		if err != nil {
			return err
		}
		providerName := "local_edit"
		if task.ProviderName != nil && *task.ProviderName != "" {
			providerName = *task.ProviderName
		}
		artifactID, err := graph.AdoptImageArtifact(ctx, pgxTx, graph.AdoptImageArtifactInput{
			ProductID:                 productID,
			GraphID:                   *task.TargetGraphID,
			NodeID:                    *task.TargetNodeID,
			ExpectedCurrentArtifactID: expectedArtifactID,
			SourceArtifactID:          *task.SourceArtifactID,
			ExpectedSourceAssetID:     *task.SourceArtifactAssetID,
			ResultAssetID:             result.ID,
			PayloadJSON:               payloadJSON,
			PayloadHash:               hash,
			ProviderName:              providerName,
			ProviderModel:             task.ProviderModel,
		})
		if err != nil {
			return err
		}
		event := schema.LocalImageEditAdoptionEvents{
			ID: clockid.New(), ProductID: productID, TaskID: task.ID,
			GraphID: *task.TargetGraphID, NodeID: *task.TargetNodeID, EventType: "adopt",
			FromArtifactID: *task.SourceArtifactID, ToArtifactID: artifactID, CreatedAt: time.Now().UTC(),
		}
		return pgxTx.Create(&event).Error
	})
	if err != nil {
		return TaskResponse{}, err
	}
	return s.Get(ctx, productID, taskID, true)
}

func (s Service) Revert(ctx context.Context, productID, taskID, eventID, expectedArtifactID string) (TaskResponse, error) {
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		var event schema.LocalImageEditAdoptionEvents
		err := pgxTx.Clauses(pfdb.ForUpdate()).Where("id = ? AND task_id = ?", eventID, taskID).Take(&event).Error
		if errors.Is(err, gorm.ErrRecordNotFound) || event.EventType != "adopt" {
			return apperr.NotFound("局部编辑 adoption 事件不存在")
		}
		if err != nil {
			return err
		}
		if event.ProductID != productID {
			return apperr.NotFound("局部编辑 adoption 事件不存在")
		}
		if err := lockProduct(ctx, pgxTx, productID); err != nil {
			return err
		}
		if err := graph.RevertNodeCurrentArtifact(ctx, pgxTx, productID, event.GraphID, event.NodeID, expectedArtifactID, event.ToArtifactID, event.FromArtifactID); err != nil {
			return err
		}
		revert := schema.LocalImageEditAdoptionEvents{
			ID: clockid.New(), ProductID: productID, TaskID: taskID,
			GraphID: event.GraphID, NodeID: event.NodeID, EventType: "revert",
			FromArtifactID: event.ToArtifactID, ToArtifactID: event.FromArtifactID,
			RelatedEventID: &eventID, CreatedAt: time.Now().UTC(),
		}
		return pgxTx.Create(&revert).Error
	})
	if err != nil {
		return TaskResponse{}, err
	}
	return s.Get(ctx, productID, taskID, true)
}

func validateCapability(task taskRow, cap Capability) error {
	if !cap.Supported || cap.Mode == "" {
		reason := cap.Reason
		if reason == "" {
			reason = "当前图片 provider 不支持局部编辑"
		}
		return apperr.Validation(reason)
	}
	supported := false
	for _, op := range cap.Operations {
		if op == task.Operation {
			supported = true
			break
		}
	}
	if !supported {
		return apperr.Validation("当前图片 provider 不支持该局部编辑操作")
	}
	if len(task.ReferenceIDs) > cap.MaxReferenceImages {
		return apperr.Validation("局部编辑参考图数量超过当前图片 provider 能力")
	}
	return nil
}

func normalizeIdempotency(value string) (string, error) {
	key := strings.TrimSpace(value)
	if key == "" {
		return "", apperr.Validation("局部编辑 idempotency key 不能为空")
	}
	if len([]rune(key)) > 120 {
		return "", apperr.Validation("局部编辑 idempotency key 不能超过 120 个字符")
	}
	return key, nil
}

func normalizeIntent(value, label string) (string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" || len(normalized) > 80 {
		return "", apperr.Validation("局部编辑 " + label + " 无效")
	}
	return normalized, nil
}

func requestHash(ctx context.Context, tx *gorm.DB, task taskRow, providerName, mode string) (string, error) {
	var mask schema.MediaObjects
	err := tx.WithContext(ctx).Where("id = ?", task.MaskMediaID).Take(&mask).Error
	maskSHA := ""
	if mask.SHA256 != nil {
		maskSHA = *mask.SHA256
	}
	if err != nil || maskSHA == "" {
		return "", apperr.Conflict("局部编辑 mask 媒体快照不存在")
	}
	var geo any
	if err := json.Unmarshal(task.GeometryJSON, &geo); err != nil {
		return "", apperr.Conflict("局部编辑 mask 媒体快照不存在")
	}
	return canonjson.SHA256Hex(map[string]any{
		"task_revision":       task.Revision,
		"source_asset_id":     task.SourceAssetID,
		"source_media_sha256": task.SourceSHA,
		"operation":           task.Operation,
		"instruction":         task.Instruction,
		"source_text":         task.SourceText,
		"replacement_text":    task.ReplacementText,
		"mask_media_sha256":   maskSHA,
		"mask_geometry":       geo,
		"reference_asset_ids": task.ReferenceIDs,
		"provider_intent":     map[string]any{"provider_name": providerName, "local_edit_mode": mode},
		"target": map[string]any{
			"graph_id": task.TargetGraphID, "node_id": task.TargetNodeID, "graph_revision": task.TargetRevision,
			"source_artifact_id": task.SourceArtifactID, "source_artifact_asset_id": task.SourceArtifactAssetID,
			"source_artifact_input_digest": task.SourceArtifactDigest,
		},
	})
}
