package localedit

import (
	"context"
	"errors"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/product"
	"gorm.io/gorm"
)

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

func taskFromModel(m schema.LocalImageEditTasks) taskRow {
	return taskRow{
		ID: m.ID, ProductID: m.ProductID, SourceAssetID: m.SourceAssetID, SourceSHA: m.SourceMediaSHA256,
		MaskMediaID: m.MaskMediaObjectID, TargetGraphID: m.TargetGraphID, TargetNodeID: m.TargetNodeID,
		TargetRevision: m.TargetGraphRevision, SourceArtifactID: m.SourceArtifactID,
		SourceArtifactAssetID: m.SourceArtifactAssetID, SourceArtifactDigest: m.SourceArtifactInputDigest,
		Operation: m.Operation, Instruction: m.Instruction, SourceText: m.SourceText, ReplacementText: m.ReplacementText,
		GeometryJSON: []byte(m.MaskGeometryJSON), RequestedProvider: m.RequestedProviderName, RequestedMode: m.RequestedLocalEditMode,
		Status: m.Status, Revision: m.Revision, IdempotencyKey: m.IdempotencyKey, RequestHash: m.RequestHash,
		Attempts: m.Attempts, ActiveAttemptID: m.ActiveAttemptID, ProgressPhase: m.ProgressPhase,
		FailureReason: m.FailureReason, IsRetryable: m.IsRetryable, ProviderName: m.ProviderName,
		ProviderModel: m.ProviderModel, ProviderResponseID: m.ProviderResponseID, ProviderStatus: m.ProviderStatus,
		ResultAssetID: m.ResultAssetID, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
		QueuedAt: m.QueuedAt, StartedAt: m.StartedAt, FinishedAt: m.FinishedAt,
	}
}

func loadTask(ctx context.Context, tx *gorm.DB, productID, taskID string) (taskRow, error) {
	if err := requireProduct(ctx, tx, productID); err != nil {
		return taskRow{}, err
	}
	var row schema.LocalImageEditTasks
	err := tx.Where("id = ? AND product_id = ?", taskID, productID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return taskRow{}, apperr.NotFound("局部编辑任务不存在")
	}
	if err != nil {
		return taskRow{}, err
	}
	out := taskFromModel(row)
	ids, err := listReferenceIDs(ctx, tx, out.ID)
	if err != nil {
		return taskRow{}, err
	}
	out.ReferenceIDs = ids
	return out, nil
}

func loadTaskForUpdate(ctx context.Context, tx *gorm.DB, productID, taskID string) (taskRow, error) {
	if err := requireProduct(ctx, tx, productID); err != nil {
		return taskRow{}, err
	}
	var row schema.LocalImageEditTasks
	err := tx.Clauses(pfdb.ForUpdate()).Where("id = ? AND product_id = ?", taskID, productID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return taskRow{}, apperr.NotFound("局部编辑任务不存在")
	}
	if err != nil {
		return taskRow{}, err
	}
	out := taskFromModel(row)
	ids, err := listReferenceIDs(ctx, tx, out.ID)
	if err != nil {
		return taskRow{}, err
	}
	out.ReferenceIDs = ids
	return out, nil
}

func loadTaskByID(ctx context.Context, tx *gorm.DB, taskID string) (taskRow, error) {
	var row schema.LocalImageEditTasks
	err := tx.Clauses(pfdb.ForUpdate()).Where("id = ?", taskID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return taskRow{}, apperr.NotFound("局部编辑任务不存在")
	}
	if err != nil {
		return taskRow{}, err
	}
	out := taskFromModel(row)
	ids, err := listReferenceIDs(ctx, tx, out.ID)
	if err != nil {
		return taskRow{}, err
	}
	out.ReferenceIDs = ids
	return out, nil
}

func listReferenceIDs(ctx context.Context, tx *gorm.DB, taskID string) ([]string, error) {
	var refs []schema.LocalImageEditTaskReferences
	if err := tx.WithContext(ctx).Where("task_id = ?", taskID).Order("sort_order ASC, asset_id ASC").Find(&refs).Error; err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(refs))
	for _, r := range refs {
		ids = append(ids, r.AssetID)
	}
	return ids, nil
}

func requireProduct(ctx context.Context, tx *gorm.DB, productID string) error {
	var row schema.Products
	err := auth.ScopeMerchant(ctx, tx.WithContext(ctx).Where("id = ?", productID), "merchant_id").Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return auth.NotFoundCrossMerchant()
	}
	return err
}

func lockProduct(ctx context.Context, tx *gorm.DB, productID string) error {
	var row schema.Products
	err := auth.ScopeMerchant(ctx, tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Where("id = ?", productID), "merchant_id").Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return auth.NotFoundCrossMerchant()
	}
	return err
}

// lockSource FOR UPDATE 锁商品图并返回 MediaObject 路径。
// 跨商/缺失统一 NotFoundCrossMerchant；图不属于该商品返回 404「商品图片不存在」。
func lockSource(ctx context.Context, tx *gorm.DB, productID, assetID string) (product.ImageAsset, string, error) {
	asset, err := product.LoadAssetRow(ctx, tx, assetID)
	if err != nil {
		return product.ImageAsset{}, "", err
	}
	if asset.ProductID != productID {
		return product.ImageAsset{}, "", apperr.NotFound("商品图片不存在")
	}
	var locked schema.ProductImageAssets
	err = tx.Clauses(pfdb.ForUpdate()).Where("id = ? AND product_id = ?", assetID, productID).Take(&locked).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return product.ImageAsset{}, "", apperr.NotFound("商品图片不存在")
	}
	if err != nil {
		return product.ImageAsset{}, "", err
	}
	var mediaObj schema.MediaObjects
	if err := tx.Where("id = ?", asset.MediaObjectID).Take(&mediaObj).Error; err != nil {
		return product.ImageAsset{}, "", apperr.Validation("局部编辑源图片必须是已核验图片")
	}
	if mediaObj.VerificationStatus != media.StatusVerified {
		return product.ImageAsset{}, "", apperr.Validation("局部编辑源图片必须是已核验图片")
	}
	sha := ""
	if mediaObj.SHA256 != nil {
		sha = *mediaObj.SHA256
	}
	if sha == "" || asset.Width == nil || asset.Height == nil {
		return product.ImageAsset{}, "", apperr.Validation("局部编辑源图片缺少不可变媒体快照")
	}
	return asset, sha, nil
}

// validateTarget 确认目标是本商品 live 图上的 image_generation 节点，且当前 artifact 仍是 sourceAssetID。
func validateTarget(ctx context.Context, tx *gorm.DB, productID, sourceAssetID, targetNodeID string) (targetSnapshot, error) {
	if targetNodeID == "" {
		return targetSnapshot{}, nil
	}
	target, err := graph.LockImageNodeTarget(ctx, tx, productID, targetNodeID)
	if err != nil {
		return targetSnapshot{}, err
	}
	if target.AssetID != sourceAssetID {
		return targetSnapshot{}, apperr.Conflict("局部编辑 source asset 必须等于 target 当前 artifact asset")
	}
	nodeID := target.NodeID
	artifactID := target.ArtifactID
	assetID := target.AssetID
	digest := target.InputDigest
	revision := target.Revision
	graphID := target.GraphID
	return targetSnapshot{
		GraphID: &graphID, NodeID: &nodeID, Revision: &revision,
		ArtifactID: &artifactID, AssetID: &assetID, InputDigest: &digest,
	}, nil
}

// lockReferences 按传入顺序锁参考图。重复 id 去重；任一不属于本商品返回 404。
func lockReferences(ctx context.Context, tx *gorm.DB, productID string, ids []string) ([]string, error) {
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
	var assets []schema.ProductImageAssets
	if err := tx.Clauses(pfdb.ForUpdate()).Where("product_id = ? AND id IN ?", productID, ids).Find(&assets).Error; err != nil {
		return nil, err
	}
	if len(assets) != len(ids) {
		return nil, apperr.NotFound("局部编辑参考图不存在")
	}
	for _, id := range ids {
		asset, err := product.LoadAssetRow(ctx, tx, id)
		if err != nil {
			return nil, apperr.NotFound("局部编辑参考图不存在")
		}
		var mediaObj schema.MediaObjects
		err = tx.Where("id = ? AND verification_status = ? AND byte_size > 0 AND width > 0 AND height > 0 AND length(sha256) = 64", asset.MediaObjectID, "verified").
			Take(&mediaObj).Error
		if err != nil {
			return nil, apperr.Validation("局部编辑参考图必须是有完整核验元数据的图片")
		}
	}
	return ids, nil
}

func replaceReferences(ctx context.Context, tx *gorm.DB, taskID string, ids []string) error {
	if err := tx.WithContext(ctx).Where("task_id = ?", taskID).Delete(&schema.LocalImageEditTaskReferences{}).Error; err != nil {
		return err
	}
	for i, id := range ids {
		row := schema.LocalImageEditTaskReferences{TaskID: taskID, AssetID: id, SortOrder: i}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}
