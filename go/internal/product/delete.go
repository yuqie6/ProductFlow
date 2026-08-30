package product

import (
	"context"
	"errors"

	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

// DeleteProduct 在无 running GraphRun 时删商品；视觉体系若被其他商品占用则 409。
func (s Service) DeleteProduct(ctx context.Context, productID string) error {
	var files []media.Deleted
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if _, err := loadProductForUpdate(ctx, pgxTx, productID); err != nil {
			return err
		}
		var running schema.WorkflowGraphRuns
		err := pgxTx.WithContext(ctx).Table("workflow_graph_runs AS r").
			Select("r.id").
			Joins("JOIN workflow_graphs g ON g.id = r.graph_id").
			Where("g.product_id = ? AND r.status = ?", productID, "running").
			Take(&running).Error
		if err == nil {
			return apperr.Validation("商品工作流运行中，稍后删除")
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var assets []schema.ProductImageAssets
		if err := pgxTx.WithContext(ctx).Select("media_object_id").Where("product_id = ?", productID).Find(&assets).Error; err != nil {
			return err
		}
		mediaIDs := make([]string, 0, len(assets))
		for _, asset := range assets {
			mediaIDs = append(mediaIDs, asset.MediaObjectID)
		}
		removable, err := prepareVisualSystemCleanup(ctx, pgxTx, productID)
		if err != nil {
			return err
		}
		if err := deleteRestrictChildren(ctx, pgxTx, productID); err != nil {
			return err
		}
		if err := pgxTx.WithContext(ctx).Where("id = ?", productID).Delete(&schema.Products{}).Error; err != nil {
			return err
		}
		if err := deleteOwnedVisualVersions(ctx, pgxTx, removable); err != nil {
			return err
		}
		files, err = media.PruneUnreferenced(ctx, pgxTx, mediaIDs)
		return err
	})
	if err != nil {
		return err
	}
	for _, file := range files {
		_ = s.Media.Files.DeleteWithVariants(file.StoragePath)
	}
	s.Media.Files.RemoveEmptyProductDirs(productID)
	return nil
}

func deleteRestrictChildren(ctx context.Context, tx *gorm.DB, productID string) error {
	if err := tx.WithContext(ctx).Where("product_id = ?", productID).Delete(&schema.ProductImageFidelityChecks{}).Error; err != nil {
		return err
	}
	if err := tx.WithContext(ctx).Where("product_id = ?", productID).Delete(&schema.DeliveryRenditionJobs{}).Error; err != nil {
		return err
	}
	if err := tx.WithContext(ctx).Where("product_id = ?", productID).Delete(&schema.LocalImageEditAdoptionEvents{}).Error; err != nil {
		return err
	}
	return nil
}

type visualVersionRef struct {
	versionID      string
	visualSystemID string
}

func prepareVisualSystemCleanup(ctx context.Context, tx *gorm.DB, productID string) ([]visualVersionRef, error) {
	owned, err := collectOwnedVisualVersionIDs(ctx, tx, productID)
	if err != nil {
		return nil, err
	}
	referenced, err := collectReferencedVisualVersionIDs(ctx, tx, productID)
	if err != nil {
		return nil, err
	}
	for versionID := range referenced {
		if _, ok := owned[versionID]; !ok {
			return nil, apperr.Conflict("商品图片仍被其他视觉体系版本引用，不能删除商品")
		}
	}
	removable := make([]visualVersionRef, 0)
	for versionID := range owned {
		var rec schema.VisualSystemVersions
		err := tx.WithContext(ctx).Select("visual_system_id").Where("id = ?", versionID).Take(&rec).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		external, err := visualVersionHasExternalConsumer(ctx, tx, productID, versionID)
		if err != nil {
			return nil, err
		}
		if external {
			if _, usedHere := referenced[versionID]; usedHere {
				return nil, apperr.Conflict("商品视觉体系仍被其他商品使用，不能删除其参考图片")
			}
			continue
		}
		if err := tx.WithContext(ctx).Where("visual_system_version_id = ?", versionID).Delete(&schema.VisualSystemVersionReferences{}).Error; err != nil {
			return nil, err
		}
		removable = append(removable, visualVersionRef{versionID: versionID, visualSystemID: rec.VisualSystemID})
	}
	return removable, nil
}

func deleteOwnedVisualVersions(ctx context.Context, tx *gorm.DB, versions []visualVersionRef) error {
	systemIDs := map[string]struct{}{}
	for _, version := range versions {
		if err := tx.WithContext(ctx).Where("id = ?", version.versionID).Delete(&schema.VisualSystemVersions{}).Error; err != nil {
			return err
		}
		systemIDs[version.visualSystemID] = struct{}{}
	}
	for systemID := range systemIDs {
		var remaining schema.VisualSystemVersions
		err := tx.WithContext(ctx).Select("id").Where("visual_system_id = ?", systemID).Take(&remaining).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := tx.WithContext(ctx).Where("id = ?", systemID).Delete(&schema.VisualSystems{}).Error; err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func collectOwnedVisualVersionIDs(ctx context.Context, tx *gorm.DB, productID string) (map[string]struct{}, error) {
	out := map[string]struct{}{}
	var nodeIDs []string
	err := tx.WithContext(ctx).Table("workflow_graph_nodes AS n").
		Select("DISTINCT n.config_json->>'visual_system_version_id'").
		Joins("JOIN workflow_graphs g ON g.id = n.graph_id").
		Where("g.product_id = ? AND NULLIF(n.config_json->>'visual_system_version_id', '') IS NOT NULL", productID).
		Scan(&nodeIDs).Error
	if err != nil {
		return nil, err
	}
	for _, id := range nodeIDs {
		if id != "" {
			out[id] = struct{}{}
		}
	}
	var artifactIDs []string
	err = tx.WithContext(ctx).Table("workflow_graph_artifacts AS a").
		Select("DISTINCT a.payload_json->>'visual_system_version_id'").
		Joins("JOIN workflow_graphs g ON g.id = a.graph_id").
		Where("g.product_id = ? AND NULLIF(a.payload_json->>'visual_system_version_id', '') IS NOT NULL", productID).
		Scan(&artifactIDs).Error
	if err != nil {
		return nil, err
	}
	for _, id := range artifactIDs {
		if id != "" {
			out[id] = struct{}{}
		}
	}
	return out, nil
}

func collectReferencedVisualVersionIDs(ctx context.Context, tx *gorm.DB, productID string) (map[string]struct{}, error) {
	var ids []string
	err := tx.WithContext(ctx).Table("visual_system_version_references AS r").
		Select("DISTINCT r.visual_system_version_id").
		Joins("JOIN product_image_assets a ON a.id = r.asset_id").
		Where("a.product_id = ?", productID).
		Scan(&ids).Error
	if err != nil {
		return nil, err
	}
	out := map[string]struct{}{}
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out, nil
}

func visualVersionHasExternalConsumer(ctx context.Context, tx *gorm.DB, productID, versionID string) (bool, error) {
	var node schema.WorkflowGraphNodes
	err := tx.WithContext(ctx).Table("workflow_graph_nodes AS n").
		Select("n.id").
		Joins("JOIN workflow_graphs g ON g.id = n.graph_id").
		Where("n.config_json->>'visual_system_version_id' = ? AND g.product_id <> ?", versionID, productID).
		Take(&node).Error
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	var artifact schema.WorkflowGraphArtifacts
	err = tx.WithContext(ctx).Table("workflow_graph_artifacts AS a").
		Select("a.id").
		Joins("JOIN workflow_graphs g ON g.id = a.graph_id").
		Where("a.payload_json->>'visual_system_version_id' = ? AND g.product_id <> ?", versionID, productID).
		Take(&artifact).Error
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	var recipe schema.WorkflowRecipeVersions
	err = tx.WithContext(ctx).Select("id").Where("preferred_visual_system_version_id = ?", versionID).Take(&recipe).Error
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	return false, nil
}
