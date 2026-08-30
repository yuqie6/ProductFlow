package product

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

const (
	galleryMoveMaxAssets = 100
	entityIDMaxLen       = 36
)

type GalleryAssetMove struct {
	AssetID          string
	ExpectedFolderID *string
}

type folderRow struct {
	ID        string
	Name      string
	SortOrder int
}

func normalizeFolderName(value string) (string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return "", apperr.Validation("文件夹名称不能为空")
	}
	if utf8.RuneCountInString(normalized) > 120 {
		return "", apperr.Validation("文件夹名称不能超过 120 个字符")
	}
	return normalized, nil
}

func normalizeDisplayName(value string) (string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return "", apperr.Validation("图片显示名不能为空")
	}
	if utf8.RuneCountInString(normalized) > 255 {
		return "", apperr.Validation("图片显示名不能超过 255 个字符")
	}
	return normalized, nil
}

func folderMaxSort(ctx context.Context, tx *gorm.DB, productID string) (int, error) {
	var maxSort *int
	if err := tx.WithContext(ctx).Model(&schema.ProductAssetFolders{}).
		Where("product_id = ?", productID).
		Select("MAX(sort_order)").
		Scan(&maxSort).Error; err != nil {
		return 0, err
	}
	if maxSort == nil {
		return 0, nil
	}
	return *maxSort + 1, nil
}

// CreateGalleryFolder 创建一层用户文件夹；不移动资产，也不改商品 updated_at。
func (s Service) CreateGalleryFolder(ctx context.Context, productID, name string) (GalleryFolderMutation, error) {
	var out GalleryFolderMutation
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		normalized, err := normalizeFolderName(name)
		if err != nil {
			return err
		}
		if _, err := loadProductForUpdate(ctx, pgxTx, productID); err != nil {
			return err
		}
		sortOrder, err := folderMaxSort(ctx, pgxTx, productID)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		rec := schema.ProductAssetFolders{
			ID:        clockid.New(),
			ProductID: productID,
			Name:      normalized,
			SortOrder: sortOrder,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := pgxTx.WithContext(ctx).Create(&rec).Error; err != nil {
			if uniqueViolation(err) {
				return apperr.Conflict("当前商品已存在同名文件夹")
			}
			return err
		}
		out = GalleryFolderMutation{ID: rec.ID, Name: rec.Name, SortOrder: rec.SortOrder}
		return nil
	})
	return out, err
}

// CreateGalleryFolderWithID 按 Agent 预分配的 folder_id 创建；ID 已存在则 409。
func (s Service) CreateGalleryFolderWithID(ctx context.Context, productID, folderID, name string) (GalleryFolderMutation, error) {
	var out GalleryFolderMutation
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		normalized, err := normalizeFolderName(name)
		if err != nil {
			return err
		}
		id := strings.TrimSpace(folderID)
		if id == "" || len(id) > 36 {
			return apperr.Validation("文件夹 ID 无效")
		}
		if _, err := loadProductForUpdate(ctx, pgxTx, productID); err != nil {
			return err
		}
		var existing schema.ProductAssetFolders
		err = pgxTx.WithContext(ctx).Select("id").Where("id = ?", id).Take(&existing).Error
		if err == nil {
			return apperr.Conflict("文件夹 ID 已存在")
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		sortOrder, err := folderMaxSort(ctx, pgxTx, productID)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		rec := schema.ProductAssetFolders{
			ID:        id,
			ProductID: productID,
			Name:      normalized,
			SortOrder: sortOrder,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := pgxTx.WithContext(ctx).Create(&rec).Error; err != nil {
			if uniqueViolation(err) {
				return apperr.Conflict("当前商品已存在同名文件夹")
			}
			return err
		}
		out = GalleryFolderMutation{ID: rec.ID, Name: rec.Name, SortOrder: rec.SortOrder}
		return nil
	})
	return out, err
}

// RenameGalleryFolder 用 expected_name 做乐观锁；同名冲突 409。
func (s Service) RenameGalleryFolder(ctx context.Context, productID, folderID, expectedName, name string) (GalleryFolderMutation, error) {
	var out GalleryFolderMutation
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if _, err := loadProductForUpdate(ctx, pgxTx, productID); err != nil {
			return err
		}
		expected, err := normalizeFolderName(expectedName)
		if err != nil {
			return err
		}
		normalized, err := normalizeFolderName(name)
		if err != nil {
			return err
		}
		folder, err := loadFolderForUpdate(ctx, pgxTx, productID, folderID)
		if err != nil {
			return err
		}
		if folder.Name != expected {
			return apperr.Conflict("文件夹名称已被其他操作修改")
		}
		if folder.Name != normalized {
			err = pgxTx.WithContext(ctx).Model(&schema.ProductAssetFolders{}).Where("id = ?", folderID).Updates(map[string]any{
				"name":       normalized,
				"updated_at": time.Now().UTC(),
			}).Error
			if uniqueViolation(err) {
				return apperr.Conflict("当前商品已存在同名文件夹")
			}
			if err != nil {
				return err
			}
			folder.Name = normalized
		}
		out = GalleryFolderMutation{ID: folder.ID, Name: folder.Name, SortOrder: folder.SortOrder}
		return nil
	})
	return out, err
}

// DeleteGalleryFolder 删文件夹并把其中资产移回未整理；不删 MediaObject。
func (s Service) DeleteGalleryFolder(ctx context.Context, productID, folderID, expectedName string) (DeleteGalleryFolderResponse, error) {
	var out DeleteGalleryFolderResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if _, err := loadProductForUpdate(ctx, pgxTx, productID); err != nil {
			return err
		}
		expected, err := normalizeFolderName(expectedName)
		if err != nil {
			return err
		}
		folder, err := loadFolderForUpdate(ctx, pgxTx, productID, folderID)
		if err != nil {
			return err
		}
		if folder.Name != expected {
			return apperr.Conflict("文件夹名称已被其他操作修改")
		}
		res := pgxTx.WithContext(ctx).Model(&schema.ProductImageAssets{}).
			Where("product_id = ? AND user_folder_id = ?", productID, folderID).
			Updates(map[string]any{
				"user_folder_id": nil,
				"updated_at":     time.Now().UTC(),
			})
		if res.Error != nil {
			return res.Error
		}
		if err := pgxTx.WithContext(ctx).Where("id = ?", folderID).Delete(&schema.ProductAssetFolders{}).Error; err != nil {
			return err
		}
		out = DeleteGalleryFolderResponse{FolderID: folderID, MovedToUnorganizedCount: int(res.RowsAffected)}
		return nil
	})
	return out, err
}

// RenameGalleryAsset 只改 display_name，不改 original_filename 或商品 updated_at。
func (s Service) RenameGalleryAsset(ctx context.Context, productID, assetID, expectedDisplayName, displayName string) (GalleryAssetResponse, error) {
	var out GalleryAssetResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if _, err := loadProductForUpdate(ctx, pgxTx, productID); err != nil {
			return err
		}
		expected, err := normalizeDisplayName(expectedDisplayName)
		if err != nil {
			return err
		}
		normalized, err := normalizeDisplayName(displayName)
		if err != nil {
			return err
		}
		asset, err := loadAssetForUpdate(ctx, pgxTx, productID, assetID)
		if err != nil {
			return err
		}
		if asset.DisplayName != expected {
			return apperr.Conflict("图片显示名已被其他操作修改")
		}
		if asset.DisplayName != normalized {
			if err := pgxTx.WithContext(ctx).Model(&schema.ProductImageAssets{}).Where("id = ?", assetID).Updates(map[string]any{
				"display_name": normalized,
				"updated_at":   time.Now().UTC(),
			}).Error; err != nil {
				return err
			}
		}
		detail, err := s.loadGalleryAssetInTx(ctx, pgxTx, productID, assetID)
		if err != nil {
			return err
		}
		out = detail
		return nil
	})
	return out, err
}

// MoveGalleryAssets 只改 user_folder_id；expected_folder_id 与当前值不一致则整批失败。
func (s Service) MoveGalleryAssets(ctx context.Context, productID string, moves []GalleryAssetMove, folderID *string) (GalleryAssetPage, error) {
	var page GalleryAssetPage
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		normalized, err := NormalizeMoves(moves)
		if err != nil {
			return err
		}
		if _, err := loadProductForUpdate(ctx, pgxTx, productID); err != nil {
			return err
		}
		folderIDs := map[string]struct{}{}
		var target *string
		if folderID != nil {
			trimmed := strings.TrimSpace(*folderID)
			if trimmed == "" {
				return apperr.Validation("目标文件夹 ID 无效")
			}
			target = &trimmed
			folderIDs[trimmed] = struct{}{}
		}
		for _, move := range normalized {
			if move.ExpectedFolderID != nil {
				folderIDs[*move.ExpectedFolderID] = struct{}{}
			}
		}
		if len(folderIDs) > 0 {
			ids := make([]string, 0, len(folderIDs))
			for id := range folderIDs {
				ids = append(ids, id)
			}
			var found []schema.ProductAssetFolders
			if err := pgxTx.WithContext(ctx).Clauses(pfdb.ForUpdate()).
				Select("id").
				Where("product_id = ? AND id IN ?", productID, ids).
				Find(&found).Error; err != nil {
				return err
			}
			if len(found) != len(folderIDs) {
				return apperr.NotFound("商品图片文件夹不存在")
			}
		}
		assetIDs := make([]string, 0, len(normalized))
		expected := map[string]*string{}
		for _, move := range normalized {
			assetIDs = append(assetIDs, move.AssetID)
			expected[move.AssetID] = move.ExpectedFolderID
		}
		var lockedAssets []schema.ProductImageAssets
		if err := pgxTx.WithContext(ctx).Clauses(pfdb.ForUpdate()).
			Select("id, user_folder_id").
			Where("product_id = ? AND id IN ?", productID, assetIDs).
			Order("id").
			Find(&lockedAssets).Error; err != nil {
			return err
		}
		if len(lockedAssets) != len(assetIDs) {
			return apperr.NotFound("商品图片不存在")
		}
		for _, asset := range lockedAssets {
			if !sameOptionalString(asset.UserFolderID, expected[asset.ID]) {
				return apperr.Conflict("图片所在文件夹已被其他操作修改")
			}
		}
		now := time.Now().UTC()
		for _, assetID := range assetIDs {
			if err := pgxTx.WithContext(ctx).Model(&schema.ProductImageAssets{}).
				Where("id = ? AND user_folder_id IS DISTINCT FROM ?", assetID, target).
				Updates(map[string]any{
					"user_folder_id": target,
					"updated_at":     now,
				}).Error; err != nil {
				return err
			}
		}
		items := make([]GalleryAssetResponse, 0, len(assetIDs))
		for _, assetID := range assetIDs {
			item, err := s.loadGalleryAssetInTx(ctx, pgxTx, productID, assetID)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		page = GalleryAssetPage{Items: items}
		return nil
	})
	return page, err
}

func (s Service) loadGalleryAssetInTx(ctx context.Context, tx *gorm.DB, productID, assetID string) (GalleryAssetResponse, error) {
	row, err := loadGalleryRow(ctx, tx, productID, assetID)
	if err != nil {
		return GalleryAssetResponse{}, err
	}
	return projectGalleryAsset(row), nil
}

func loadFolderForUpdate(ctx context.Context, tx *gorm.DB, productID, folderID string) (folderRow, error) {
	var rec schema.ProductAssetFolders
	err := tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).
		Select("id, name, sort_order").
		Where("id = ? AND product_id = ?", folderID, productID).
		Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return folderRow{}, apperr.NotFound("商品图片文件夹不存在")
	}
	if err != nil {
		return folderRow{}, err
	}
	return folderRow{ID: rec.ID, Name: rec.Name, SortOrder: rec.SortOrder}, nil
}

func loadAssetForUpdate(ctx context.Context, tx *gorm.DB, productID, assetID string) (ImageAsset, error) {
	asset, err := loadAsset(ctx, tx, assetID)
	if err != nil {
		return ImageAsset{}, err
	}
	if asset.ProductID != productID {
		return ImageAsset{}, apperr.NotFound("商品图片不存在")
	}
	var rec schema.ProductImageAssets
	if err := tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Where("id = ?", assetID).Take(&rec).Error; err != nil {
		return ImageAsset{}, err
	}
	return loadAsset(ctx, tx, assetID)
}

// NormalizeMoves 校验 1–100 条移动项：asset_id 去空白、非空、最长 36、不重复；expected_folder_id 可选且同样最长 36。
func NormalizeMoves(moves []GalleryAssetMove) ([]GalleryAssetMove, error) {
	if len(moves) < 1 || len(moves) > galleryMoveMaxAssets {
		return nil, apperr.Validationf("单次必须移动 1 到 %d 张图片", galleryMoveMaxAssets)
	}
	seen := map[string]struct{}{}
	out := make([]GalleryAssetMove, 0, len(moves))
	for _, move := range moves {
		assetID := strings.TrimSpace(move.AssetID)
		if assetID == "" {
			return nil, apperr.Validation("图片 ID 不能为空")
		}
		if len(assetID) > entityIDMaxLen {
			return nil, apperr.Validation("图片 ID 无效")
		}
		if _, ok := seen[assetID]; ok {
			return nil, apperr.Validation("移动列表不能包含重复图片 ID")
		}
		seen[assetID] = struct{}{}
		var expected *string
		if move.ExpectedFolderID != nil {
			trimmed := strings.TrimSpace(*move.ExpectedFolderID)
			if trimmed == "" || len(trimmed) > entityIDMaxLen {
				return nil, apperr.Validation("expected_folder_id 无效")
			}
			expected = &trimmed
		}
		out = append(out, GalleryAssetMove{AssetID: assetID, ExpectedFolderID: expected})
	}
	return out, nil
}

func sameOptionalString(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
