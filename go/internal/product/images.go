package product

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/tx"
)

// SetCover 把展示封面指向一张属于该商品的已核验图片，不改 facts 或节点绑定。
func (s Service) SetCover(ctx context.Context, productID, assetID string) (Detail, error) {
	var detail Detail
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
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
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		if _, err := loadProduct(ctx, pgxTx, productID); err != nil {
			return err
		}
		_, err := pgxTx.Exec(ctx, `UPDATE products SET cover_image_asset_id = NULL, updated_at = NOW() WHERE id = $1`, productID)
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
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
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
			_, err = pgxTx.Exec(ctx, `UPDATE products SET updated_at = NOW() WHERE id = $1`, productID)
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
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		asset, err := loadAsset(ctx, pgxTx, assetID)
		if err != nil {
			return err
		}
		if err := ensureAssetNotReferenced(ctx, pgxTx, assetID); err != nil {
			return err
		}
		if _, err := pgxTx.Exec(ctx, `DELETE FROM product_image_assets WHERE id = $1`, assetID); err != nil {
			return err
		}
		if _, err := pgxTx.Exec(ctx, `UPDATE products SET updated_at = NOW() WHERE id = $1`, asset.ProductID); err != nil {
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

func ensureAssetNotReferenced(ctx context.Context, tx pgx.Tx, assetID string) error {
	checks := []struct {
		sql    string
		detail string
	}{
		{`SELECT 1 FROM products WHERE cover_image_asset_id = $1 LIMIT 1`, "商品图片仍被设为封面，不能删除"},
		{`SELECT 1 FROM product_image_assets WHERE parent_asset_id = $1 LIMIT 1`, "商品图片仍有派生图片，不能删除"},
		{`SELECT 1 FROM workflow_graph_nodes WHERE bound_image_asset_id = $1 LIMIT 1`, "商品图片仍被工作流节点绑定，不能删除"},
		{`SELECT 1 FROM visual_system_version_references WHERE asset_id = $1 LIMIT 1`, "商品图片仍被视觉体系版本引用，不能删除"},
		{`SELECT 1 FROM workflow_graph_artifacts WHERE product_image_asset_id = $1 LIMIT 1`, "商品图片仍被工作流生成历史作为结果引用，不能删除"},
		{`SELECT 1 FROM delivery_rendition_jobs WHERE source_asset_id = $1 OR result_asset_id = $1 LIMIT 1`, "商品图片仍被交付派生任务引用，不能删除"},
		{`SELECT 1 FROM local_image_edit_tasks WHERE source_asset_id = $1 OR source_artifact_asset_id = $1 OR result_asset_id = $1 LIMIT 1`, "商品图片仍被局部编辑任务的源图或结果引用，不能删除"},
		{`SELECT 1 FROM local_image_edit_task_references WHERE asset_id = $1 LIMIT 1`, "商品图片仍被局部编辑任务作为参考图引用，不能删除"},
		{`SELECT 1 FROM local_image_edit_provider_attempts WHERE late_result_asset_id = $1 LIMIT 1`, "商品图片仍被局部编辑迟到结果审计引用，不能删除"},
		{`SELECT 1 FROM product_image_fidelity_checks WHERE asset_id = $1 LIMIT 1`, "商品图片仍有人工保真检查历史，不能删除"},
	}
	for _, check := range checks {
		var one int
		err := tx.QueryRow(ctx, check.sql, assetID).Scan(&one)
		if err == nil {
			return apperr.Conflict(check.detail)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
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
