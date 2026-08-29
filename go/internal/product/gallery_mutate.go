package product

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/tx"
)

const galleryMoveMaxAssets = 100

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

// CreateGalleryFolder 创建一层用户文件夹；不移动资产，也不改商品 updated_at。
func (s Service) CreateGalleryFolder(ctx context.Context, productID, name string) (GalleryFolderMutation, error) {
	var out GalleryFolderMutation
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		normalized, err := normalizeFolderName(name)
		if err != nil {
			return err
		}
		if _, err := loadProductForUpdate(ctx, pgxTx, productID); err != nil {
			return err
		}
		var maxSort *int
		if err := pgxTx.QueryRow(ctx, `
			SELECT MAX(sort_order) FROM product_asset_folders WHERE product_id = $1
		`, productID).Scan(&maxSort); err != nil {
			return err
		}
		sortOrder := 0
		if maxSort != nil {
			sortOrder = *maxSort + 1
		}
		id := clockid.New()
		err = pgxTx.QueryRow(ctx, `
			INSERT INTO product_asset_folders (id, product_id, name, sort_order, created_at, updated_at)
			VALUES ($1, $2, $3, $4, NOW(), NOW())
			RETURNING id, name, sort_order
		`, id, productID, normalized, sortOrder).Scan(&out.ID, &out.Name, &out.SortOrder)
		if uniqueViolation(err) {
			return apperr.Conflict("当前商品已存在同名文件夹")
		}
		return err
	})
	return out, err
}

// RenameGalleryFolder 用 expected_name 做乐观锁；同名冲突 409。
func (s Service) RenameGalleryFolder(ctx context.Context, productID, folderID, expectedName, name string) (GalleryFolderMutation, error) {
	var out GalleryFolderMutation
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
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
			_, err = pgxTx.Exec(ctx, `
				UPDATE product_asset_folders SET name = $1, updated_at = NOW() WHERE id = $2
			`, normalized, folderID)
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
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
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
		tag, err := pgxTx.Exec(ctx, `
			UPDATE product_image_assets SET user_folder_id = NULL, updated_at = NOW()
			WHERE product_id = $1 AND user_folder_id = $2
		`, productID, folderID)
		if err != nil {
			return err
		}
		if _, err := pgxTx.Exec(ctx, `DELETE FROM product_asset_folders WHERE id = $1`, folderID); err != nil {
			return err
		}
		out = DeleteGalleryFolderResponse{FolderID: folderID, MovedToUnorganizedCount: int(tag.RowsAffected())}
		return nil
	})
	return out, err
}

// RenameGalleryAsset 只改 display_name，不改 original_filename 或商品 updated_at。
func (s Service) RenameGalleryAsset(ctx context.Context, productID, assetID, expectedDisplayName, displayName string) (GalleryAssetResponse, error) {
	var out GalleryAssetResponse
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
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
			_, err = pgxTx.Exec(ctx, `
				UPDATE product_image_assets SET display_name = $1, updated_at = NOW() WHERE id = $2
			`, normalized, assetID)
			if err != nil {
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
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		normalized, err := normalizeMoves(moves)
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
			rows, err := pgxTx.Query(ctx, `
				SELECT id FROM product_asset_folders WHERE product_id = $1 AND id = ANY($2) FOR UPDATE
			`, productID, ids)
			if err != nil {
				return err
			}
			found := map[string]struct{}{}
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return err
				}
				found[id] = struct{}{}
			}
			rows.Close()
			if err := rows.Err(); err != nil {
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
		rows, err := pgxTx.Query(ctx, `
			SELECT id, user_folder_id FROM product_image_assets
			WHERE product_id = $1 AND id = ANY($2)
			ORDER BY id FOR UPDATE
		`, productID, assetIDs)
		if err != nil {
			return err
		}
		type locked struct {
			id       string
			folderID *string
		}
		var lockedAssets []locked
		for rows.Next() {
			var item locked
			if err := rows.Scan(&item.id, &item.folderID); err != nil {
				rows.Close()
				return err
			}
			lockedAssets = append(lockedAssets, item)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(lockedAssets) != len(assetIDs) {
			return apperr.NotFound("商品图片不存在")
		}
		for _, asset := range lockedAssets {
			if !sameOptionalString(asset.folderID, expected[asset.id]) {
				return apperr.Conflict("图片所在文件夹已被其他操作修改")
			}
		}
		for _, assetID := range assetIDs {
			_, err := pgxTx.Exec(ctx, `
				UPDATE product_image_assets SET user_folder_id = $1, updated_at = NOW() WHERE id = $2 AND (user_folder_id IS DISTINCT FROM $1)
			`, target, assetID)
			if err != nil {
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

func (s Service) loadGalleryAssetInTx(ctx context.Context, tx pgx.Tx, productID, assetID string) (GalleryAssetResponse, error) {
	rows, err := tx.Query(ctx, gallerySelectSQL+` WHERE a.product_id = $1 AND a.id = $2`, productID, assetID)
	if err != nil {
		return GalleryAssetResponse{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return GalleryAssetResponse{}, apperr.NotFound("商品图片不存在")
	}
	row, err := scanGalleryRow(rows)
	if err != nil {
		return GalleryAssetResponse{}, err
	}
	return projectGalleryAsset(row), nil
}

func loadFolderForUpdate(ctx context.Context, tx pgx.Tx, productID, folderID string) (folderRow, error) {
	var folder folderRow
	err := tx.QueryRow(ctx, `
		SELECT id, name, sort_order FROM product_asset_folders
		WHERE id = $1 AND product_id = $2 FOR UPDATE
	`, folderID, productID).Scan(&folder.ID, &folder.Name, &folder.SortOrder)
	if errors.Is(err, pgx.ErrNoRows) {
		return folderRow{}, apperr.NotFound("商品图片文件夹不存在")
	}
	return folder, err
}

func loadAssetForUpdate(ctx context.Context, tx pgx.Tx, productID, assetID string) (ImageAsset, error) {
	asset, err := loadAsset(ctx, tx, assetID)
	if err != nil {
		return ImageAsset{}, err
	}
	if asset.ProductID != productID {
		return ImageAsset{}, apperr.NotFound("商品图片不存在")
	}
	_, err = tx.Exec(ctx, `SELECT id FROM product_image_assets WHERE id = $1 FOR UPDATE`, assetID)
	if err != nil {
		return ImageAsset{}, err
	}
	return loadAsset(ctx, tx, assetID)
}

func normalizeMoves(moves []GalleryAssetMove) ([]GalleryAssetMove, error) {
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
		if _, ok := seen[assetID]; ok {
			return nil, apperr.Validation("移动列表不能包含重复图片 ID")
		}
		seen[assetID] = struct{}{}
		var expected *string
		if move.ExpectedFolderID != nil {
			trimmed := strings.TrimSpace(*move.ExpectedFolderID)
			if trimmed == "" {
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
