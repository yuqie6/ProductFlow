package localedit

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/product"
)

const taskSelect = `
	id, product_id, source_asset_id, source_media_sha256, mask_media_object_id,
	target_graph_id, target_node_id, target_graph_revision,
	source_artifact_id, source_artifact_asset_id, source_artifact_input_digest,
	operation, instruction, source_text, replacement_text, mask_geometry_json,
	requested_provider_name, requested_local_edit_mode, status, revision,
	idempotency_key, request_hash, attempts, active_attempt_id, progress_phase,
	failure_reason, is_retryable, provider_name, provider_model, provider_response_id,
	provider_status, result_asset_id, created_at, updated_at, queued_at, started_at, finished_at
`

type taskRow struct {
	ID                    string
	ProductID             string
	SourceAssetID         string
	SourceSHA             string
	MaskMediaID           string
	TargetGraphID         *string
	TargetNodeID          *string
	TargetRevision        *int
	SourceArtifactID      *string
	SourceArtifactAssetID *string
	SourceArtifactDigest  *string
	Operation             string
	Instruction           *string
	SourceText            *string
	ReplacementText       *string
	GeometryJSON          []byte
	RequestedProvider     *string
	RequestedMode         *string
	Status                string
	Revision              int
	IdempotencyKey        *string
	RequestHash           *string
	Attempts              int
	ActiveAttemptID       *string
	ProgressPhase         *string
	FailureReason         *string
	IsRetryable           bool
	ProviderName          *string
	ProviderModel         *string
	ProviderResponseID    *string
	ProviderStatus        *string
	ResultAssetID         *string
	CreatedAt             time.Time
	UpdatedAt             time.Time
	QueuedAt              *time.Time
	StartedAt             *time.Time
	FinishedAt            *time.Time
	ReferenceIDs          []string
}

type targetSnapshot struct {
	GraphID     *string
	NodeID      *string
	Revision    *int
	ArtifactID  *string
	AssetID     *string
	InputDigest *string
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTask(row rowScanner) (taskRow, error) {
	var t taskRow
	err := row.Scan(
		&t.ID, &t.ProductID, &t.SourceAssetID, &t.SourceSHA, &t.MaskMediaID,
		&t.TargetGraphID, &t.TargetNodeID, &t.TargetRevision,
		&t.SourceArtifactID, &t.SourceArtifactAssetID, &t.SourceArtifactDigest,
		&t.Operation, &t.Instruction, &t.SourceText, &t.ReplacementText, &t.GeometryJSON,
		&t.RequestedProvider, &t.RequestedMode, &t.Status, &t.Revision,
		&t.IdempotencyKey, &t.RequestHash, &t.Attempts, &t.ActiveAttemptID, &t.ProgressPhase,
		&t.FailureReason, &t.IsRetryable, &t.ProviderName, &t.ProviderModel, &t.ProviderResponseID,
		&t.ProviderStatus, &t.ResultAssetID, &t.CreatedAt, &t.UpdatedAt, &t.QueuedAt, &t.StartedAt, &t.FinishedAt,
	)
	return t, err
}

func loadTask(ctx context.Context, tx pgx.Tx, productID, taskID string) (taskRow, error) {
	row, err := scanTask(tx.QueryRow(ctx, `SELECT `+taskSelect+` FROM local_image_edit_tasks WHERE id = $1 AND product_id = $2`, taskID, productID))
	if errors.Is(err, pgx.ErrNoRows) {
		return taskRow{}, apperr.NotFound("局部编辑任务不存在")
	}
	if err != nil {
		return taskRow{}, err
	}
	ids, err := listReferenceIDs(ctx, tx, row.ID)
	if err != nil {
		return taskRow{}, err
	}
	row.ReferenceIDs = ids
	return row, nil
}

func loadTaskForUpdate(ctx context.Context, tx pgx.Tx, productID, taskID string) (taskRow, error) {
	row, err := scanTask(tx.QueryRow(ctx, `SELECT `+taskSelect+` FROM local_image_edit_tasks WHERE id = $1 AND product_id = $2 FOR UPDATE`, taskID, productID))
	if errors.Is(err, pgx.ErrNoRows) {
		return taskRow{}, apperr.NotFound("局部编辑任务不存在")
	}
	if err != nil {
		return taskRow{}, err
	}
	ids, err := listReferenceIDs(ctx, tx, row.ID)
	if err != nil {
		return taskRow{}, err
	}
	row.ReferenceIDs = ids
	return row, nil
}

func loadTaskByID(ctx context.Context, tx pgx.Tx, taskID string) (taskRow, error) {
	row, err := scanTask(tx.QueryRow(ctx, `SELECT `+taskSelect+` FROM local_image_edit_tasks WHERE id = $1 FOR UPDATE`, taskID))
	if errors.Is(err, pgx.ErrNoRows) {
		return taskRow{}, apperr.NotFound("局部编辑任务不存在")
	}
	if err != nil {
		return taskRow{}, err
	}
	ids, err := listReferenceIDs(ctx, tx, row.ID)
	if err != nil {
		return taskRow{}, err
	}
	row.ReferenceIDs = ids
	return row, nil
}

func listReferenceIDs(ctx context.Context, tx pgx.Tx, taskID string) ([]string, error) {
	rows, err := tx.Query(ctx, `
		SELECT asset_id FROM local_image_edit_task_references WHERE task_id = $1 ORDER BY sort_order ASC, asset_id ASC
	`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func lockProduct(ctx context.Context, tx pgx.Tx, productID string) error {
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM products WHERE id = $1 FOR UPDATE`, productID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound("商品不存在")
	}
	return err
}

func lockSource(ctx context.Context, tx pgx.Tx, productID, assetID string) (product.ImageAsset, string, error) {
	var id string
	err := tx.QueryRow(ctx, `
		SELECT id FROM product_image_assets WHERE id = $1 AND product_id = $2 FOR UPDATE
	`, assetID, productID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return product.ImageAsset{}, "", apperr.NotFound("商品图片不存在")
	}
	if err != nil {
		return product.ImageAsset{}, "", err
	}
	asset, err := product.LoadAssetRow(ctx, tx, assetID)
	if err != nil {
		return product.ImageAsset{}, "", err
	}
	var sha string
	var status string
	if err := tx.QueryRow(ctx, `SELECT sha256, verification_status FROM media_objects WHERE id = $1`, asset.MediaObjectID).Scan(&sha, &status); err != nil {
		return product.ImageAsset{}, "", apperr.Validation("局部编辑源图片必须是已核验图片")
	}
	if status != media.StatusVerified {
		return product.ImageAsset{}, "", apperr.Validation("局部编辑源图片必须是已核验图片")
	}
	if sha == "" || asset.Width == nil || asset.Height == nil {
		return product.ImageAsset{}, "", apperr.Validation("局部编辑源图片缺少不可变媒体快照")
	}
	return asset, sha, nil
}

func validateTarget(ctx context.Context, tx pgx.Tx, productID, sourceAssetID, targetNodeID string) (targetSnapshot, error) {
	if targetNodeID == "" {
		return targetSnapshot{}, nil
	}
	var graphID, nodeType string
	var artifactID *string
	err := tx.QueryRow(ctx, `
		SELECT n.graph_id, n.node_type, n.current_artifact_id
		FROM workflow_graph_nodes n
		JOIN workflow_graphs g ON g.id = n.graph_id
		WHERE n.id = $1 AND g.product_id = $2 AND g.active = TRUE
		FOR UPDATE OF n
	`, targetNodeID, productID).Scan(&graphID, &nodeType, &artifactID)
	if errors.Is(err, pgx.ErrNoRows) || nodeType != "image_generation" {
		return targetSnapshot{}, apperr.Conflict("局部编辑 target 必须是当前商品 active graph 的 image_generation 节点")
	}
	if err != nil {
		return targetSnapshot{}, err
	}
	if artifactID == nil {
		return targetSnapshot{}, apperr.Conflict("局部编辑 target 必须有当前 image artifact")
	}
	var artifactType string
	var assetID *string
	var digest *string
	err = tx.QueryRow(ctx, `
		SELECT artifact_type, product_image_asset_id, input_digest FROM workflow_graph_artifacts WHERE id = $1
	`, *artifactID).Scan(&artifactType, &assetID, &digest)
	if err != nil || artifactType != "image" || assetID == nil {
		return targetSnapshot{}, apperr.Conflict("局部编辑 target 必须有当前 image artifact")
	}
	if *assetID != sourceAssetID {
		return targetSnapshot{}, apperr.Conflict("局部编辑 source asset 必须等于 target 当前 artifact asset")
	}
	if digest == nil || len(*digest) != 64 {
		return targetSnapshot{}, apperr.Conflict("局部编辑 target 当前 artifact 缺少 input digest")
	}
	var revision int
	if err := tx.QueryRow(ctx, `SELECT revision FROM workflow_graphs WHERE id = $1`, graphID).Scan(&revision); err != nil {
		return targetSnapshot{}, apperr.Conflict("局部编辑 target graph 不存在")
	}
	nodeID := targetNodeID
	return targetSnapshot{
		GraphID: &graphID, NodeID: &nodeID, Revision: &revision,
		ArtifactID: artifactID, AssetID: assetID, InputDigest: digest,
	}, nil
}

func lockReferences(ctx context.Context, tx pgx.Tx, productID string, ids []string) ([]string, error) {
	if len(ids) > maxReferences {
		return nil, apperr.Validation("局部编辑参考图不能超过 6 张")
	}
	if len(ids) == 0 {
		return []string{}, nil
	}
	seen := map[string]struct{}{}
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			return nil, apperr.Validation("局部编辑参考图不能重复")
		}
		seen[id] = struct{}{}
	}
	rows, err := tx.Query(ctx, `
		SELECT a.id FROM product_image_assets a
		JOIN media_objects m ON m.id = a.media_object_id
		WHERE a.product_id = $1 AND a.id = ANY($2)
		FOR UPDATE OF a
	`, productID, ids)
	if err != nil {
		return nil, err
	}
	found := map[string]struct{}{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		found[id] = struct{}{}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(found) != len(ids) {
		return nil, apperr.NotFound("局部编辑参考图不存在")
	}
	for _, id := range ids {
		asset, err := product.LoadAssetRow(ctx, tx, id)
		if err != nil {
			return nil, apperr.NotFound("局部编辑参考图不存在")
		}
		var sha string
		err = tx.QueryRow(ctx, `
			SELECT sha256 FROM media_objects
			WHERE id = $1 AND verification_status = 'verified' AND byte_size > 0 AND width > 0 AND height > 0 AND length(sha256) = 64
		`, asset.MediaObjectID).Scan(&sha)
		if err != nil {
			return nil, apperr.Validation("局部编辑参考图必须是有完整核验元数据的图片")
		}
	}
	return ids, nil
}

func replaceReferences(ctx context.Context, tx pgx.Tx, taskID string, ids []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM local_image_edit_task_references WHERE task_id = $1`, taskID); err != nil {
		return err
	}
	for i, id := range ids {
		if _, err := tx.Exec(ctx, `
			INSERT INTO local_image_edit_task_references (task_id, asset_id, sort_order) VALUES ($1, $2, $3)
		`, taskID, id, i); err != nil {
			return err
		}
	}
	return nil
}
