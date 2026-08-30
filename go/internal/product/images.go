package product

import (
	"context"
	"errors"
	"time"

	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

// SetCover 把展示封面指向一张属于该商品的已核验图片，不改 facts 或节点绑定。
func (s Service) SetCover(ctx context.Context, productID, assetID string) (Detail, error) {
	var detail Detail
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if _, err := loadProduct(ctx, pgxTx, productID); err != nil {
			return err
		}
		asset, err := loadAsset(ctx, pgxTx, assetID)
		if err != nil {
			return err
		}
		if asset.ProductID != productID {
			return apperr.Validation("封面图片不属于当前商品")
		}
		if asset.VerificationStatus == media.StatusMissing {
			return apperr.Validation("缺失的媒体文件不能设为封面")
		}
		if err := assignCover(ctx, pgxTx, productID, assetID); err != nil {
			return err
		}
		product, err := loadProduct(ctx, pgxTx, productID)
		if err != nil {
			return err
		}
		detail = serializeDetail(product)
		return nil
	})
	return detail, err
}

// ClearCover 只清展示封面，不删资产。
func (s Service) ClearCover(ctx context.Context, productID string) (Detail, error) {
	var detail Detail
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if _, err := loadProduct(ctx, pgxTx, productID); err != nil {
			return err
		}
		err := pgxTx.WithContext(ctx).Model(&schema.Products{}).Where("id = ?", productID).Updates(map[string]any{
			"cover_image_asset_id": nil,
			"updated_at":           time.Now().UTC(),
		}).Error
		if err != nil {
			return err
		}
		product, err := loadProduct(ctx, pgxTx, productID)
		if err != nil {
			return err
		}
		detail = serializeDetail(product)
		return nil
	})
	return detail, err
}

// AddImages 追加上传到商品图片身份；尚无封面时写入第一张为展示图。
func (s Service) AddImages(ctx context.Context, productID string, uploads []Upload) ([]ImageAsset, error) {
	if len(uploads) == 0 {
		return nil, apperr.Validation("至少上传一张商品图片")
	}
	if len(uploads) > 6 {
		return nil, apperr.Validation("单次最多上传 6 张商品图片")
	}
	var created []ImageAsset
	var compensation storage.Compensation
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		product, err := loadProductForUpdate(ctx, pgxTx, productID)
		if err != nil {
			return err
		}
		ids := make([]string, 0, len(uploads))
		for _, upload := range uploads {
			obj, err := s.Media.Stage(ctx, pgxTx, upload.Content, upload.MIMEType, &compensation)
			if err != nil {
				return err
			}
			asset, err := insertAsset(ctx, pgxTx, productID, obj.ID, upload.Filename)
			if err != nil {
				return err
			}
			ids = append(ids, asset.ID)
		}
		if product.CoverImageAssetID == nil {
			if err := assignCover(ctx, pgxTx, productID, ids[0]); err != nil {
				return err
			}
		} else {
			err = pgxTx.WithContext(ctx).Model(&schema.Products{}).Where("id = ?", productID).Updates(map[string]any{
				"updated_at": time.Now().UTC(),
			}).Error
			if err != nil {
				return err
			}
		}
		assets, err := loadAssetsByIDs(ctx, pgxTx, productID, ids)
		if err != nil {
			return err
		}
		created = assetsInOrder(assets, ids)
		compensation.Release()
		return nil
	})
	if err != nil {
		compensation.Rollback()
		return nil, err
	}
	return created, nil
}

// DeleteAsset 在无封面/节点/生成/交付/局部编辑引用时删除商品图片身份，commit 后再清无引用文件。
func (s Service) DeleteAsset(ctx context.Context, assetID string) error {
	var files []media.Deleted
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		asset, err := loadAsset(ctx, pgxTx, assetID)
		if err != nil {
			return err
		}
		if err := ensureAssetNotReferenced(ctx, pgxTx, assetID); err != nil {
			return err
		}
		if err := pgxTx.WithContext(ctx).Where("id = ?", assetID).Delete(&schema.ProductImageAssets{}).Error; err != nil {
			return err
		}
		if err := pgxTx.WithContext(ctx).Model(&schema.Products{}).Where("id = ?", asset.ProductID).Updates(map[string]any{
			"updated_at": time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
		files, err = media.PruneUnreferenced(ctx, pgxTx, []string{asset.MediaObjectID})
		return err
	})
	if err != nil {
		return err
	}
	for _, file := range files {
		_ = s.Media.Files.DeleteWithVariants(file.StoragePath)
	}
	return nil
}

func ensureAssetNotReferenced(ctx context.Context, tx *gorm.DB, assetID string) error {
	type check struct {
		run    func() error
		detail string
	}
	checks := []check{
		{func() error {
			var rec schema.Products
			return tx.WithContext(ctx).Select("id").Where("cover_image_asset_id = ?", assetID).Take(&rec).Error
		}, "商品图片仍被设为封面，不能删除"},
		{func() error {
			var rec schema.ProductImageAssets
			return tx.WithContext(ctx).Select("id").Where("parent_asset_id = ?", assetID).Take(&rec).Error
		}, "商品图片仍有派生图片，不能删除"},
		{func() error {
			var rec schema.WorkflowGraphNodes
			return tx.WithContext(ctx).Select("id").Where("bound_image_asset_id = ?", assetID).Take(&rec).Error
		}, "商品图片仍被工作流节点绑定，不能删除"},
		{func() error {
			var rec schema.VisualSystemVersionReferences
			return tx.WithContext(ctx).Select("id").Where("asset_id = ?", assetID).Take(&rec).Error
		}, "商品图片仍被视觉体系版本引用，不能删除"},
		{func() error {
			var rec schema.WorkflowGraphArtifacts
			return tx.WithContext(ctx).Select("id").Where("product_image_asset_id = ?", assetID).Take(&rec).Error
		}, "商品图片仍被工作流生成历史作为结果引用，不能删除"},
		{func() error {
			var rec schema.DeliveryRenditionJobs
			return tx.WithContext(ctx).Select("id").Where("source_asset_id = ? OR result_asset_id = ?", assetID, assetID).Take(&rec).Error
		}, "商品图片仍被交付派生任务引用，不能删除"},
		{func() error {
			var rec schema.LocalImageEditTasks
			return tx.WithContext(ctx).Select("id").Where("source_asset_id = ? OR source_artifact_asset_id = ? OR result_asset_id = ?", assetID, assetID, assetID).Take(&rec).Error
		}, "商品图片仍被局部编辑任务的源图或结果引用，不能删除"},
		{func() error {
			var rec schema.LocalImageEditTaskReferences
			return tx.WithContext(ctx).Select("task_id").Where("asset_id = ?", assetID).Take(&rec).Error
		}, "商品图片仍被局部编辑任务作为参考图引用，不能删除"},
		{func() error {
			var rec schema.LocalImageEditProviderAttempts
			return tx.WithContext(ctx).Select("id").Where("late_result_asset_id = ?", assetID).Take(&rec).Error
		}, "商品图片仍被局部编辑迟到结果审计引用，不能删除"},
		{func() error {
			var rec schema.ProductImageFidelityChecks
			return tx.WithContext(ctx).Select("id").Where("asset_id = ?", assetID).Take(&rec).Error
		}, "商品图片仍有人工保真检查历史，不能删除"},
	}
	for _, item := range checks {
		err := item.run()
		if err == nil {
			return apperr.Conflict(item.detail)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	return nil
}

func assetsInOrder(assets []ImageAsset, ids []string) []ImageAsset {
	byID := make(map[string]ImageAsset, len(assets))
	for _, asset := range assets {
		byID[asset.ID] = asset
	}
	out := make([]ImageAsset, 0, len(ids))
	for _, id := range ids {
		if asset, ok := byID[id]; ok {
			out = append(out, asset)
		}
	}
	return out
}
