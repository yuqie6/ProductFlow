package product

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

// CollectedInput 把全局素材登记为商品图片身份，共享 MediaObject，不复制 bytes。
type CollectedInput struct {
	ProductID            string
	MediaObjectID        string
	OriginType           string
	DisplayName          string
	OriginalFilename     string
	SourceLibraryAssetID string
}

func Lock(ctx context.Context, tx *gorm.DB, productID string) (Product, error) {
	return loadProductForUpdate(ctx, tx, productID)
}

// AssetIDsExist 确认 ids 都属于该商品；缺任一则校验失败。
func AssetIDsExist(ctx context.Context, tx *gorm.DB, productID string, ids []string) error {
	wanted := map[string]struct{}{}
	for _, id := range ids {
		if id != "" {
			wanted[id] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	unique := make([]string, 0, len(wanted))
	for id := range wanted {
		unique = append(unique, id)
	}
	var found []schema.ProductImageAssets
	err := tx.WithContext(ctx).Select("id").
		Where("product_id = ? AND id IN ?", productID, unique).
		Find(&found).Error
	if err != nil {
		return err
	}
	if len(found) != len(wanted) {
		return apperr.Validation("节点绑定了不属于该商品的图片")
	}
	return nil
}

// GraphGuard 供 graph.Service / StageNew 注入，SQL 留在本包。
type GraphGuard struct{}

func (GraphGuard) Lock(ctx context.Context, tx *gorm.DB, productID string) error {
	_, err := Lock(ctx, tx, productID)
	return err
}

func (GraphGuard) HasAssets(ctx context.Context, tx *gorm.DB, productID string, ids []string) error {
	return AssetIDsExist(ctx, tx, productID, ids)
}

func Touch(ctx context.Context, tx *gorm.DB, productID string) error {
	return tx.WithContext(ctx).Model(&schema.Products{}).Where("id = ?", productID).Updates(map[string]any{
		"updated_at": time.Now().UTC(),
	}).Error
}

func LoadImage(ctx context.Context, q *gorm.DB, assetID string) (ImageAsset, error) {
	return loadAsset(ctx, q, assetID)
}

func LoadImageForUpdate(ctx context.Context, tx *gorm.DB, assetID string) (ImageAsset, error) {
	var row assetJoinRow
	err := assetJoinQuery(tx.WithContext(ctx)).
		Clauses(pfdb.ForUpdateOf("a")).
		Where("a.id = ?", assetID).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ImageAsset{}, apperr.NotFound("商品图片不存在")
	}
	if err != nil {
		return ImageAsset{}, err
	}
	return imageAssetFromJoin(row), nil
}

func LookupByLibrarySource(ctx context.Context, tx *gorm.DB, productID, libraryAssetID string) (ImageAsset, bool, error) {
	var rec schema.ProductImageAssets
	err := tx.WithContext(ctx).Select("id").
		Where("product_id = ? AND source_library_asset_id = ?", productID, libraryAssetID).
		Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ImageAsset{}, false, nil
	}
	if err != nil {
		return ImageAsset{}, false, err
	}
	asset, err := loadAsset(ctx, tx, rec.ID)
	return asset, true, err
}

func LoadByLibrarySources(ctx context.Context, tx *gorm.DB, productID string, libraryIDs []string) (map[string]ImageAsset, error) {
	out := map[string]ImageAsset{}
	if len(libraryIDs) == 0 {
		return out, nil
	}
	var rows []assetJoinRow
	err := assetJoinQuery(tx.WithContext(ctx)).
		Where("a.product_id = ? AND a.source_library_asset_id IN ?", productID, libraryIDs).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		asset := imageAssetFromJoin(row)
		if asset.SourceLibraryAsset != nil {
			out[*asset.SourceLibraryAsset] = asset
		}
	}
	return out, nil
}

func InsertCollected(ctx context.Context, tx *gorm.DB, in CollectedInput) (ImageAsset, error) {
	id := clockid.New()
	original := strings.TrimSpace(in.OriginalFilename)
	if original == "" {
		original = "image"
	}
	display := strings.TrimSpace(in.DisplayName)
	if display == "" {
		display = original
	}
	if n := []rune(display); len(n) > 255 {
		display = string(n[:255])
	}
	if n := []rune(original); len(n) > 255 {
		original = string(n[:255])
	}
	origin := strings.TrimSpace(in.OriginType)
	if origin == "" {
		origin = "upload"
	}
	now := time.Now().UTC()
	sourceID := in.SourceLibraryAssetID
	rec := schema.ProductImageAssets{
		ID:                   id,
		ProductID:            in.ProductID,
		MediaObjectID:        in.MediaObjectID,
		OriginType:           origin,
		DisplayName:          display,
		OriginalFilename:     original,
		SourceLibraryAssetID: &sourceID,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	if err := tx.WithContext(ctx).Create(&rec).Error; err != nil {
		return ImageAsset{}, err
	}
	return loadAsset(ctx, tx, id)
}

func UniqueViolation(err error) bool {
	return uniqueViolation(err)
}

func SerializeAssets(assets []ImageAsset) []AssetResponse {
	return serializeAssets(assets)
}
