package library

import (
	"context"
	"time"
	"unicode/utf8"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

// ListWorkflow 列出工作流子图库关联；关联行不复制媒体 bytes。
// 工作流不存在返回 NotFound。
func (s Service) ListWorkflow(ctx context.Context, productID, workflowID string, limit int) (WorkflowList, error) {
	if limit < 1 {
		limit = maxWorkflow
	}
	if limit > maxWorkflow {
		limit = maxWorkflow
	}
	var out WorkflowList
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if err := requireWorkflow(ctx, pgxTx, productID, workflowID, false); err != nil {
			return err
		}
		var err error
		out, err = s.listWorkflowTx(ctx, pgxTx, productID, workflowID, limit)
		return err
	})
	return out, err
}

type workflowLinkScan struct {
	ID        string    `gorm:"column:media_library_asset_id"`
	LinkedAt  time.Time `gorm:"column:created_at"`
	CollectID *string   `gorm:"column:collect_id"`
	SourceID  *string   `gorm:"column:source_id"`
}

// listWorkflowTx 列出工作流子图库关联。ProductImageAssetID 优先用本商品收藏行，否则回落到源商品图。
func (s Service) listWorkflowTx(ctx context.Context, pgxTx *gorm.DB, productID, workflowID string, limit int) (WorkflowList, error) {
	var rows []workflowLinkScan
	err := pgxTx.WithContext(ctx).Table("workflow_media_library_assets AS w").
		Select("w.media_library_asset_id, w.created_at, lib.id AS collect_id, src.id AS source_id").
		Joins("JOIN media_library_assets a ON a.id = w.media_library_asset_id").
		Joins("LEFT JOIN product_image_assets lib ON lib.source_library_asset_id = a.id AND lib.product_id = ?", productID).
		Joins("LEFT JOIN product_image_assets src ON src.id = a.source_product_asset_id AND src.product_id = ?", productID).
		Where("w.workflow_id = ?", workflowID).
		Order("w.created_at DESC, w.media_library_asset_id DESC").
		Limit(limit).
		Scan(&rows).Error
	if err != nil {
		return WorkflowList{}, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	assets, err := reloadInOrder(ctx, pgxTx, uniqueKeepOrder(ids))
	if err != nil {
		return WorkflowList{}, err
	}
	byID := map[string]Asset{}
	for _, asset := range assets {
		byID[asset.ID] = asset
	}
	out := WorkflowList{WorkflowID: workflowID, Items: []WorkflowItem{}}
	for _, it := range rows {
		serialized, err := serializeAsset(byID[it.ID])
		if err != nil {
			return WorkflowList{}, err
		}
		productAssetID := it.CollectID
		if productAssetID == nil {
			productAssetID = it.SourceID
		}
		out.Items = append(out.Items, WorkflowItem{
			Asset:               serialized,
			ProductImageAssetID: productAssetID,
			LinkedAt:            it.LinkedAt,
		})
	}
	return out, nil
}

// SyncWorkflow 重写工作流子图库关联集合，不复制媒体 bytes。
// 未选素材、ID 无效或重复返回 Validation；工作流或素材不存在返回 NotFound。
func (s Service) SyncWorkflow(ctx context.Context, productID, workflowID string, libraryIDs []string) (WorkflowList, error) {
	if len(libraryIDs) == 0 {
		return WorkflowList{}, apperr.Validation("至少选择一个素材")
	}
	if len(libraryIDs) > maxWorkflow {
		return WorkflowList{}, apperr.Validationf("一次最多关联 %d 个素材", maxWorkflow)
	}
	seen := map[string]struct{}{}
	uniqueIDs := make([]string, 0, len(libraryIDs))
	for _, id := range libraryIDs {
		if id == "" || utf8.RuneCountInString(id) > 36 {
			return WorkflowList{}, apperr.Validation("素材 ID 无效")
		}
		if _, ok := seen[id]; ok {
			return WorkflowList{}, apperr.Validation("关联请求包含重复素材")
		}
		seen[id] = struct{}{}
		uniqueIDs = append(uniqueIDs, id)
	}
	var out WorkflowList
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if err := requireWorkflow(ctx, pgxTx, productID, workflowID, false); err != nil {
			return err
		}
		assets, err := loadAssets(ctx, pgxTx, libraryAssetQuery(pgxTx).Where("a.id IN ?", uniqueIDs))
		if err != nil {
			return err
		}
		if len(assets) != len(uniqueIDs) {
			return apperr.NotFound("素材库资产不存在")
		}
		for _, asset := range assets {
			if _, err := validateForUse(asset); err != nil {
				return err
			}
		}
		sameProduct := map[string]struct{}{}
		for _, asset := range assets {
			if asset.SourceProductProductID != nil && *asset.SourceProductProductID == productID {
				sameProduct[asset.ID] = struct{}{}
			}
		}
		toCollect := make([]string, 0)
		for _, id := range uniqueIDs {
			if _, ok := sameProduct[id]; !ok {
				toCollect = append(toCollect, id)
			}
		}
		if len(toCollect) > 0 {
			if _, err := s.collectTx(ctx, pgxTx, productID, toCollect, ""); err != nil {
				return err
			}
		}
		if err := requireWorkflow(ctx, pgxTx, productID, workflowID, true); err != nil {
			return err
		}
		existing := map[string]struct{}{}
		var linked []schema.WorkflowMediaLibraryAssets
		if err := pgxTx.WithContext(ctx).Select("media_library_asset_id").
			Where("workflow_id = ? AND media_library_asset_id IN ?", workflowID, uniqueIDs).
			Find(&linked).Error; err != nil {
			return err
		}
		for _, row := range linked {
			existing[row.MediaLibraryAssetID] = struct{}{}
		}
		now := time.Now().UTC()
		for _, id := range uniqueIDs {
			if _, ok := existing[id]; ok {
				continue
			}
			if err := pgxTx.WithContext(ctx).Create(&schema.WorkflowMediaLibraryAssets{
				WorkflowID:          workflowID,
				MediaLibraryAssetID: id,
				CreatedAt:           now,
			}).Error; err != nil {
				return err
			}
		}
		out, err = s.listWorkflowTx(ctx, pgxTx, productID, workflowID, maxWorkflow)
		return err
	})
	return out, err
}

// RemoveWorkflow 删除一条工作流子图库关联，不删除全局素材。
// 工作流不属于该商品或不存在返回 NotFound「工作流不存在」；关联行不存在返回 NotFound「工作流素材关联不存在」。
func (s Service) RemoveWorkflow(ctx context.Context, productID, workflowID, libraryAssetID string) error {
	return tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if err := requireWorkflow(ctx, pgxTx, productID, workflowID, false); err != nil {
			return err
		}
		res := pgxTx.WithContext(ctx).
			Where("workflow_id = ? AND media_library_asset_id = ?", workflowID, libraryAssetID).
			Delete(&schema.WorkflowMediaLibraryAssets{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return apperr.NotFound("工作流素材关联不存在")
		}
		return nil
	})
}

func uniqueKeepOrder(ids []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
