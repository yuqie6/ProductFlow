package product

import (
	"context"
	"errors"
	"strings"

	sqldb "database/sql"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
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
	rows, err := pfdb.Query(ctx, tx, `
		SELECT id FROM product_image_assets
		WHERE product_id = $1 AND id = ANY($2)
	`, productID, unique)
	if err != nil {
		return err
	}
	defer rows.Close()
	found := map[string]struct{}{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		found[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
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
	_, err := pfdb.Exec(ctx, tx, `UPDATE products SET updated_at = NOW() WHERE id = $1`, productID)
	return err
}

func LoadImage(ctx context.Context, q *gorm.DB, assetID string) (ImageAsset, error) {
	return loadAsset(ctx, q, assetID)
}

func LoadImageForUpdate(ctx context.Context, tx *gorm.DB, assetID string) (ImageAsset, error) {
	var asset ImageAsset
	err := pfdb.QueryRow(ctx, tx, `
		SELECT a.id, a.product_id, a.media_object_id, a.origin_type, a.display_name, a.original_filename,
		       a.image_type_key, a.user_folder_id, a.parent_asset_id, a.source_image_session_asset_id,
		       a.source_library_asset_id, m.mime_type, m.byte_size, m.width, m.height, m.verification_status,
		       m.storage_path, a.created_at, a.updated_at
		FROM product_image_assets a
		JOIN media_objects m ON m.id = a.media_object_id
		WHERE a.id = $1
		FOR UPDATE OF a
	`, assetID).Scan(
		&asset.ID, &asset.ProductID, &asset.MediaObjectID, &asset.OriginType, &asset.DisplayName, &asset.OriginalFilename,
		&asset.ImageTypeKey, &asset.UserFolderID, &asset.ParentAssetID, &asset.SourceImageSessionAsset,
		&asset.SourceLibraryAsset, &asset.MIMEType, &asset.ByteSize, &asset.Width, &asset.Height, &asset.VerificationStatus,
		&asset.StoragePath, &asset.CreatedAt, &asset.UpdatedAt,
	)
	if errors.Is(err, sqldb.ErrNoRows) {
		return ImageAsset{}, apperr.NotFound("商品图片不存在")
	}
	return asset, err
}

func LookupByLibrarySource(ctx context.Context, tx *gorm.DB, productID, libraryAssetID string) (ImageAsset, bool, error) {
	var id string
	err := pfdb.QueryRow(ctx, tx, `
		SELECT id FROM product_image_assets
		WHERE product_id = $1 AND source_library_asset_id = $2
	`, productID, libraryAssetID).Scan(&id)
	if errors.Is(err, sqldb.ErrNoRows) {
		return ImageAsset{}, false, nil
	}
	if err != nil {
		return ImageAsset{}, false, err
	}
	asset, err := loadAsset(ctx, tx, id)
	return asset, true, err
}

func LoadByLibrarySources(ctx context.Context, tx *gorm.DB, productID string, libraryIDs []string) (map[string]ImageAsset, error) {
	out := map[string]ImageAsset{}
	if len(libraryIDs) == 0 {
		return out, nil
	}
	rows, err := pfdb.Query(ctx, tx, `
		SELECT a.id, a.product_id, a.media_object_id, a.origin_type, a.display_name, a.original_filename,
		       a.image_type_key, a.user_folder_id, a.parent_asset_id, a.source_image_session_asset_id,
		       a.source_library_asset_id, m.mime_type, m.byte_size, m.width, m.height, m.verification_status,
		       m.storage_path, a.created_at, a.updated_at
		FROM product_image_assets a
		JOIN media_objects m ON m.id = a.media_object_id
		WHERE a.product_id = $1 AND a.source_library_asset_id = ANY($2)
	`, productID, libraryIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	assets, err := scanAssets(rows)
	if err != nil {
		return nil, err
	}
	for _, asset := range assets {
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
	_, err := pfdb.Exec(ctx, tx, `
		INSERT INTO product_image_assets (
			id, product_id, media_object_id, origin_type, display_name, original_filename,
			source_library_asset_id, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
	`, id, in.ProductID, in.MediaObjectID, origin, display, original, in.SourceLibraryAssetID)
	if err != nil {
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
