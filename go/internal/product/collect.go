package product

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
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
	OriginType           string // upload|workflow_generation|image_session_attach|local_edit
	DisplayName          string
	OriginalFilename     string
	SourceLibraryAssetID string
}

// Lock 以 FOR UPDATE 锁住商品行，供 graph 与图库命令串行化。
// 商品不存在返回 NotFound。
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

// GraphGuard 供 graph.Service / WriteTx 注入，SQL 留在本包。
type GraphGuard struct{}

// Lock 实现 graph.ProductGuard：锁商品行。SQL 留在本包，避免 graph import product 循环。
// 商品不存在返回 NotFound。
func (GraphGuard) Lock(ctx context.Context, tx *gorm.DB, productID string) error {
	_, err := Lock(ctx, tx, productID)
	return err
}

// HasAssets 确认 ids 都属于该商品，供 graph 校验 image_asset 绑定。
// 缺任一返回 Validation；空 ids 返回 nil。
func (GraphGuard) HasAssets(ctx context.Context, tx *gorm.DB, productID string, ids []string) error {
	return AssetIDsExist(ctx, tx, productID, ids)
}

// Touch 只把 products.updated_at 设为现在，供全局图库收藏写回商品列表排序。
// 不改封面、facts、图 revision。调用方须已在同一事务里改过资产。不要用它代替 Lock。
// 商品不存在时 Updates 仍返回 nil，不报 NotFound。
func Touch(ctx context.Context, tx *gorm.DB, productID string) error {
	return tx.WithContext(ctx).Model(&schema.Products{}).Where("id = ?", productID).Updates(map[string]any{
		"updated_at": time.Now().UTC(),
	}).Error
}

// LoadImage 按 ProductImageAsset id 读取身份。找不到返回 NotFound。
func LoadImage(ctx context.Context, q *gorm.DB, assetID string) (ImageAsset, error) {
	return loadAsset(ctx, q, assetID)
}

// LoadImageForUpdate 以 FOR UPDATE 锁住 product_image_assets 行再 join 读回，供全局图库把素材写回已有商品图。
// 找不到或跨商返回统一 404。不要和 LoadImage（不锁）或 gallery 的 loadAssetForUpdate（还校验 product_id）搞混。
func LoadImageForUpdate(ctx context.Context, tx *gorm.DB, assetID string) (ImageAsset, error) {
	var row assetJoinRow
	query := assetJoinQuery(tx.WithContext(ctx)).
		Clauses(pfdb.ForUpdateOf("a")).
		Joins("JOIN products p ON p.id = a.product_id").
		Where("a.id = ?", assetID)
	query = auth.ScopeMerchant(ctx, query, "p.merchant_id")
	err := query.Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ImageAsset{}, auth.NotFoundCrossMerchant()
	}
	if err != nil {
		return ImageAsset{}, err
	}
	return imageAssetFromJoin(row), nil
}

// LookupByLibrarySource 按全局素材 id 查找本商品已收藏的身份。未收藏时 found=false。
// 未收藏返回 false, nil；库错误原样返回。
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

// LoadByLibrarySources 批量按全局素材 id 映射到本商品图片身份。
// 库查询失败原样返回；未命中的 id 不写入 map。
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

// InsertCollected 登记一条共享 MediaObject 的商品图片身份，不复制 bytes。
// 插入或回读失败原样返回。
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

// UniqueViolation 识别 PostgreSQL 23505，供收藏去重。
func UniqueViolation(err error) bool {
	return uniqueViolation(err)
}

// SerializeAssets 把 ImageAsset 身份列表投成 Web AssetResponse（含推导的下载 URL，不含存储路径）。
// 供全局图库收藏接口与本包内部共用。改字段会碰到商品图 JSON 合同。不要回传 StoragePath。
func SerializeAssets(assets []ImageAsset) []AssetResponse {
	return serializeAssets(assets)
}
