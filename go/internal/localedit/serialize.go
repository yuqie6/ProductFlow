package localedit

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/yuqie6/productflow/internal/product"
)

func serializeTask(ctx context.Context, tx pgx.Tx, row taskRow, includeAudit bool) (TaskResponse, error) {
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

func listAttempts(ctx context.Context, tx pgx.Tx, taskID string) ([]AttemptResponse, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, attempt_id, attempt_number, operation_key, phase, effect_result, provider_name,
		       provider_model, provider_response_id, provider_status, late_result_asset_id, detail, created_at, updated_at
		FROM local_image_edit_provider_attempts
		WHERE task_id = $1
		ORDER BY attempt_number DESC, id DESC
		LIMIT 50
	`, taskID)
	if err != nil {
		return nil, err
	}
	type attemptScan struct {
		item   AttemptResponse
		lateID *string
	}
	var scanned []attemptScan
	for rows.Next() {
		var item attemptScan
		if err := rows.Scan(
			&item.item.ID, &item.item.AttemptID, &item.item.AttemptNumber, &item.item.OperationKey, &item.item.Phase, &item.item.EffectResult, &item.item.ProviderName,
			&item.item.ProviderModel, &item.item.ProviderResponseID, &item.item.ProviderStatus, &item.lateID, &item.item.Detail, &item.item.CreatedAt, &item.item.UpdatedAt,
		); err != nil {
			rows.Close()
			return nil, err
		}
		scanned = append(scanned, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]AttemptResponse, 0, len(scanned))
	for _, item := range scanned {
		if item.lateID != nil {
			asset, err := product.LoadAssetRow(ctx, tx, *item.lateID)
			if err != nil {
				return nil, err
			}
			resp := product.SerializeAsset(asset)
			item.item.LateResultAsset = &resp
		}
		out = append(out, item.item)
	}
	return out, nil
}

func listEvents(ctx context.Context, tx pgx.Tx, taskID string) ([]AdoptionEventResponse, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, task_id, graph_id, node_id, event_type, from_artifact_id, to_artifact_id, related_event_id, created_at
		FROM local_image_edit_adoption_events
		WHERE task_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 50
	`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdoptionEventResponse{}
	for rows.Next() {
		var item AdoptionEventResponse
		if err := rows.Scan(
			&item.ID, &item.TaskID, &item.GraphID, &item.NodeID, &item.EventType,
			&item.FromArtifactID, &item.ToArtifactID, &item.RelatedEventID, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
