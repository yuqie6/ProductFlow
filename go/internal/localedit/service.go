package localedit

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	sqldb "database/sql"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/canonjson"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
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
		if _, err := pfdb.Exec(ctx, pgxTx, `
			INSERT INTO local_image_edit_tasks (
				id, product_id, source_asset_id, source_media_sha256, mask_media_object_id,
				target_graph_id, target_node_id, target_graph_revision,
				source_artifact_id, source_artifact_asset_id, source_artifact_input_digest,
				operation, instruction, source_text, replacement_text, mask_geometry_json,
				status, revision, attempts, is_retryable, created_at, updated_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16,
				'draft', 1, 0, TRUE, NOW(), NOW()
			)
		`, taskID, productID, source.ID, sha, obj.ID,
			target.GraphID, target.NodeID, target.Revision,
			target.ArtifactID, target.AssetID, target.InputDigest,
			draft.Operation, draft.Instruction, draft.SourceText, draft.ReplacementText, geoJSON); err != nil {
			return err
		}
		if err := replaceReferences(ctx, pgxTx, taskID, refs); err != nil {
			return err
		}
		_, err = pfdb.Exec(ctx, pgxTx, `UPDATE products SET updated_at = NOW() WHERE id = $1`, productID)
		return err
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
		if _, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE local_image_edit_tasks SET
				operation = $2, instruction = $3, source_text = $4, replacement_text = $5,
				mask_geometry_json = $6, mask_media_object_id = $7, revision = revision + 1, updated_at = NOW()
			WHERE id = $1
		`, taskID, draft.Operation, draft.Instruction, draft.SourceText, draft.ReplacementText, geoJSON, maskID); err != nil {
			return err
		}
		if err := replaceReferences(ctx, pgxTx, taskID, refs); err != nil {
			return err
		}
		_, err = pfdb.Exec(ctx, pgxTx, `UPDATE products SET updated_at = NOW() WHERE id = $1`, productID)
		return err
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
		var exists string
		err := pfdb.QueryRow(ctx, pgxTx, `SELECT id FROM products WHERE id = $1`, productID).Scan(&exists)
		if errors.Is(err, sqldb.ErrNoRows) {
			return apperr.NotFound("商品不存在")
		}
		if err != nil {
			return err
		}
		rows, err := pfdb.Query(ctx, pgxTx, `
			SELECT `+taskSelect+`
			FROM local_image_edit_tasks
			WHERE product_id = $1
			ORDER BY created_at DESC, id DESC
			LIMIT $2
		`, productID, limit)
		if err != nil {
			return err
		}
		var collected []taskRow
		for rows.Next() {
			row, err := scanTask(rows)
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
		items := make([]TaskResponse, 0, len(collected))
		for _, row := range collected {
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
		var existingID string
		err = pfdb.QueryRow(ctx, pgxTx, `
			SELECT id FROM local_image_edit_tasks
			WHERE product_id = $1 AND idempotency_key = $2
			FOR UPDATE
		`, productID, key).Scan(&existingID)
		if err == nil {
			existing, err := loadTask(ctx, pgxTx, productID, existingID)
			if err != nil {
				return err
			}
			if existing.RequestHash == nil || *existing.RequestHash != hash {
				return apperr.Conflict("相同 idempotency key 不能提交不同的局部编辑请求")
			}
			_, err = queue.Stage(ctx, pgxTx, queue.DeliveryKey(queue.ActorLocalEdit, existing.ID), queue.ActorLocalEdit, existing.ID, map[string]any{
				"task_id": existing.ID, "request_hash": hash,
			}, nil)
			return err
		}
		if !errors.Is(err, sqldb.ErrNoRows) {
			return err
		}
		if task.Status != "draft" {
			return apperr.Conflict("局部编辑任务已提交，不能重复提交新的 idempotency key")
		}
		if _, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE local_image_edit_tasks SET
				status = 'queued', idempotency_key = $2, request_hash = $3,
				requested_provider_name = $4, requested_local_edit_mode = $5,
				progress_phase = 'queued', queued_at = NOW(), failure_reason = NULL,
				is_retryable = TRUE, updated_at = NOW()
			WHERE id = $1
		`, taskID, key, hash, providerName, mode); err != nil {
			return err
		}
		if _, err := queue.Stage(ctx, pgxTx, queue.DeliveryKey(queue.ActorLocalEdit, taskID), queue.ActorLocalEdit, taskID, map[string]any{
			"task_id": taskID, "request_hash": hash,
		}, nil); err != nil {
			return err
		}
		_, err = pfdb.Exec(ctx, pgxTx, `UPDATE products SET updated_at = NOW() WHERE id = $1`, productID)
		return err
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
		if _, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE local_image_edit_tasks SET
				status = 'queued', active_attempt_id = NULL, progress_phase = 'queued',
				failure_reason = NULL, is_retryable = TRUE, queued_at = NOW(),
				started_at = NULL, finished_at = NULL, updated_at = NOW()
			WHERE id = $1
		`, taskID); err != nil {
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
			_, _ = pfdb.Exec(ctx, pgxTx, `
				UPDATE local_image_edit_provider_attempts SET
					phase = $3, effect_result = $4, detail = $5, updated_at = NOW()
				WHERE task_id = $1 AND attempt_id = $2
			`, taskID, *task.ActiveAttemptID, phase, result, detail)
		}
		_, err = pfdb.Exec(ctx, pgxTx, `
			UPDATE local_image_edit_tasks SET
				status = 'cancelled', active_attempt_id = NULL, progress_phase = 'cancelled',
				failure_reason = $2, is_retryable = FALSE, finished_at = NOW(), updated_at = NOW()
			WHERE id = $1
		`, taskID, cancelReason)
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
		_, err = pfdb.Exec(ctx, pgxTx, `
			INSERT INTO local_image_edit_adoption_events (
				id, product_id, task_id, graph_id, node_id, event_type,
				from_artifact_id, to_artifact_id, created_at
			) VALUES ($1, $2, $3, $4, $5, 'adopt', $6, $7, NOW())
		`, clockid.New(), productID, task.ID, *task.TargetGraphID, *task.TargetNodeID, *task.SourceArtifactID, artifactID)
		return err
	})
	if err != nil {
		return TaskResponse{}, err
	}
	return s.Get(ctx, productID, taskID, true)
}

func (s Service) Revert(ctx context.Context, productID, taskID, eventID, expectedArtifactID string) (TaskResponse, error) {
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		var eventTask, eventType, graphID, nodeID, fromID, toID, eventProduct string
		err := pfdb.QueryRow(ctx, pgxTx, `
			SELECT product_id, task_id, event_type, graph_id, node_id, from_artifact_id, to_artifact_id
			FROM local_image_edit_adoption_events WHERE id = $1 AND task_id = $2 FOR UPDATE
		`, eventID, taskID).Scan(&eventProduct, &eventTask, &eventType, &graphID, &nodeID, &fromID, &toID)
		if errors.Is(err, sqldb.ErrNoRows) || eventType != "adopt" {
			return apperr.NotFound("局部编辑 adoption 事件不存在")
		}
		if err != nil {
			return err
		}
		if eventProduct != productID {
			return apperr.NotFound("局部编辑 adoption 事件不存在")
		}
		if err := lockProduct(ctx, pgxTx, productID); err != nil {
			return err
		}
		if err := graph.RevertNodeCurrentArtifact(ctx, pgxTx, productID, graphID, nodeID, expectedArtifactID, toID, fromID); err != nil {
			return err
		}
		_, err = pfdb.Exec(ctx, pgxTx, `
			INSERT INTO local_image_edit_adoption_events (
				id, product_id, task_id, graph_id, node_id, event_type,
				from_artifact_id, to_artifact_id, related_event_id, created_at
			) VALUES ($1, $2, $3, $4, $5, 'revert', $6, $7, $8, NOW())
		`, clockid.New(), productID, taskID, graphID, nodeID, toID, fromID, eventID)
		return err
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
	var maskSHA string
	err := pfdb.QueryRow(ctx, tx, `SELECT sha256 FROM media_objects WHERE id = $1`, task.MaskMediaID).Scan(&maskSHA)
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
