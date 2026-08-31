package localedit

import (
	"context"
	"encoding/json"

	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/product"
	"gorm.io/gorm"
)

// serializeTask 投影任务。includeAudit 为 true 才查 attempts/adoption，列表路径不要开以免放大查询。
func serializeTask(ctx context.Context, tx *gorm.DB, row taskRow, includeAudit bool) (TaskResponse, error) {
	source, err := product.LoadAssetRow(ctx, tx, row.SourceAssetID)
	if err != nil {
		return TaskResponse{}, err
	}
	var geo MaskGeometry
	if err := json.Unmarshal(row.GeometryJSON, &geo); err != nil {
		return TaskResponse{}, err
	}
	if geo.TransformDirection == "" {
		geo.TransformDirection = "viewport_to_source"
	}
	refs := make([]product.AssetResponse, 0, len(row.ReferenceIDs))
	for _, id := range row.ReferenceIDs {
		asset, err := product.LoadAssetRow(ctx, tx, id)
		if err != nil {
			return TaskResponse{}, err
		}
		refs = append(refs, product.SerializeAsset(asset))
	}
	cancelable := row.Status == "draft" || row.Status == "queued" || row.Status == "running"
	out := TaskResponse{
		ID: row.ID, ProductID: row.ProductID, Status: row.Status, Revision: row.Revision,
		Operation: row.Operation, Instruction: row.Instruction, SourceText: row.SourceText, ReplacementText: row.ReplacementText,
		MaskGeometry: geo, SourceMediaSHA256: row.SourceSHA, SourceAsset: product.SerializeAsset(source),
		References: refs, ReferenceAssetIDs: append([]string{}, row.ReferenceIDs...),
		TargetGraphID: row.TargetGraphID, TargetNodeID: row.TargetNodeID, TargetGraphRevision: row.TargetRevision,
		SourceArtifactID: row.SourceArtifactID, SourceArtifactAssetID: row.SourceArtifactAssetID,
		SourceArtifactInputDigest: row.SourceArtifactDigest, IdempotencyKey: row.IdempotencyKey, RequestHash: row.RequestHash,
		RequestedProviderName: row.RequestedProvider, RequestedLocalEditMode: row.RequestedMode,
		Attempts: row.Attempts, ActiveAttemptID: row.ActiveAttemptID, ProgressPhase: row.ProgressPhase,
		FailureReason: row.FailureReason, IsRetryable: row.IsRetryable, IsCancelable: cancelable,
		ProviderName: row.ProviderName, ProviderModel: row.ProviderModel, ProviderResponseID: row.ProviderResponseID,
		ProviderStatus: row.ProviderStatus, ProviderAttempts: []AttemptResponse{}, AdoptionEvents: []AdoptionEventResponse{},
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, QueuedAt: row.QueuedAt, StartedAt: row.StartedAt, FinishedAt: row.FinishedAt,
	}
	if row.ResultAssetID != nil {
		result, err := product.LoadAssetRow(ctx, tx, *row.ResultAssetID)
		if err != nil {
			return TaskResponse{}, err
		}
		resp := product.SerializeAsset(result)
		out.ResultAsset = &resp
	}
	if !includeAudit {
		return out, nil
	}
	attempts, err := listAttempts(ctx, tx, row.ID)
	if err != nil {
		return TaskResponse{}, err
	}
	events, err := listEvents(ctx, tx, row.ID)
	if err != nil {
		return TaskResponse{}, err
	}
	out.ProviderAttempts = attempts
	out.AdoptionEvents = events
	return out, nil
}

// listAttempts 按时间列出该任务的 provider 尝试。空结果是空切片不是 nil。
func listAttempts(ctx context.Context, tx *gorm.DB, taskID string) ([]AttemptResponse, error) {
	var rows []schema.LocalImageEditProviderAttempts
	if err := tx.Where("task_id = ?", taskID).Order("attempt_number DESC, id DESC").Limit(50).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]AttemptResponse, 0, len(rows))
	for _, row := range rows {
		item := AttemptResponse{
			ID: row.ID, AttemptID: row.AttemptID, AttemptNumber: row.AttemptNumber, OperationKey: row.OperationKey,
			Phase: row.Phase, EffectResult: row.EffectResult, ProviderName: row.ProviderName,
			ProviderModel: row.ProviderModel, ProviderResponseID: row.ProviderResponseID, ProviderStatus: row.ProviderStatus,
			Detail: row.Detail, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}
		if row.LateResultAssetID != nil {
			asset, err := product.LoadAssetRow(ctx, tx, *row.LateResultAssetID)
			if err != nil {
				return nil, err
			}
			resp := product.SerializeAsset(asset)
			item.LateResultAsset = &resp
		}
		out = append(out, item)
	}
	return out, nil
}

func listEvents(ctx context.Context, tx *gorm.DB, taskID string) ([]AdoptionEventResponse, error) {
	var rows []schema.LocalImageEditAdoptionEvents
	if err := tx.WithContext(ctx).Where("task_id = ?", taskID).Order("created_at DESC, id DESC").Limit(50).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]AdoptionEventResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, AdoptionEventResponse{
			ID: row.ID, TaskID: row.TaskID, GraphID: row.GraphID, NodeID: row.NodeID, EventType: row.EventType,
			FromArtifactID: row.FromArtifactID, ToArtifactID: row.ToArtifactID, RelatedEventID: row.RelatedEventID,
			CreatedAt: row.CreatedAt,
		})
	}
	return out, nil
}
