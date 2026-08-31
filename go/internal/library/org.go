package library

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

// CreateFolder 创建一层用户文件夹；同名已存在时返回已有行且 Created=false。
// 名称为空或超长返回 Validation。
func (s Service) CreateFolder(ctx context.Context, name string) (FolderMutation, error) {
	display, err := normalizeName(name, kindFolder)
	if err != nil {
		return FolderMutation{}, err
	}
	key, err := normalizeKey(display, kindFolder)
	if err != nil {
		return FolderMutation{}, err
	}
	var out FolderMutation
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		var existing schema.MediaLibraryFolders
		scanErr := pgxTx.WithContext(ctx).Select("id, name").Where("normalized_name = ?", key).Take(&existing).Error
		if scanErr == nil {
			out = FolderMutation{ID: existing.ID, Name: existing.Name, Created: false}
			return nil
		}
		if !errors.Is(scanErr, gorm.ErrRecordNotFound) {
			return scanErr
		}
		id := clockid.New()
		now := time.Now().UTC()
		err := pgxTx.WithContext(ctx).Create(&schema.MediaLibraryFolders{
			ID:             id,
			Name:           display,
			NormalizedName: key,
			CreatedAt:      now,
			UpdatedAt:      now,
		}).Error
		if uniqueViolation(err) {
			scanErr = pgxTx.WithContext(ctx).Select("id, name").Where("normalized_name = ?", key).Take(&existing).Error
			if scanErr != nil {
				return scanErr
			}
			out = FolderMutation{ID: existing.ID, Name: existing.Name, Created: false}
			return nil
		}
		if err != nil {
			return err
		}
		out = FolderMutation{ID: id, Name: display, Created: true}
		return nil
	})
	return out, err
}

// RenameFolder 按期望旧名重命名一层用户文件夹。
// 调用时机：HTTP PATCH /folders/:folder_id。副作用只改 media_library_folders，不改素材 revision。
// 找不到文件夹 NotFound；expectedName 对不上 Conflict；新名非法 Validation。
// 禁区：不要顺手改 folder_id 下的素材。
func (s Service) RenameFolder(ctx context.Context, folderID, expectedName, name string) (Folder, error) {
	var folder Folder
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		locked, err := lockFolder(ctx, pgxTx, folderID)
		if err != nil {
			return err
		}
		expected, err := normalizeName(expectedName, kindFolder)
		if err != nil {
			return err
		}
		if expected != locked.Name {
			return apperr.Conflict("文件夹名称已被其他操作修改")
		}
		display, err := normalizeName(name, kindFolder)
		if err != nil {
			return err
		}
		key, err := normalizeKey(display, kindFolder)
		if err != nil {
			return err
		}
		if locked.Name != display {
			err = pgxTx.WithContext(ctx).Model(&schema.MediaLibraryFolders{}).Where("id = ?", locked.ID).Updates(map[string]any{
				"name":            display,
				"normalized_name": key,
				"updated_at":      time.Now().UTC(),
			}).Error
			if err != nil {
				return err
			}
		}
		folder = Folder{ID: locked.ID, Name: display}
		return nil
	})
	return folder, err
}

// DeleteFolder 只删除文件夹组织，不删除素材，也不打断节点或 lineage 引用。
// 找不到文件夹返回 NotFound。
func (s Service) DeleteFolder(ctx context.Context, folderID string) (int, error) {
	var moved int
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		folder, err := lockFolder(ctx, pgxTx, folderID)
		if err != nil {
			return err
		}
		var n int64
		if err := pgxTx.WithContext(ctx).Model(&schema.MediaLibraryAssets{}).Where("folder_id = ?", folder.ID).Count(&n).Error; err != nil {
			return err
		}
		moved = int(n)
		if err := pgxTx.WithContext(ctx).Model(&schema.MediaLibraryAssets{}).Where("folder_id = ?", folder.ID).Updates(map[string]any{
			"folder_id":  nil,
			"updated_at": time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
		return pgxTx.WithContext(ctx).Where("id = ?", folder.ID).Delete(&schema.MediaLibraryFolders{}).Error
	})
	return moved, err
}

// CreateTag 创建标签；同名已存在时返回已有行且 Created=false。
// 名称为空或超长返回 Validation。
func (s Service) CreateTag(ctx context.Context, name string) (TagMutation, error) {
	display, err := normalizeName(name, kindTag)
	if err != nil {
		return TagMutation{}, err
	}
	key, err := normalizeKey(display, kindTag)
	if err != nil {
		return TagMutation{}, err
	}
	var out TagMutation
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		var existing schema.MediaLibraryTags
		scanErr := pgxTx.WithContext(ctx).Select("id, name").Where("normalized_name = ?", key).Take(&existing).Error
		if scanErr == nil {
			out = TagMutation{ID: existing.ID, Name: existing.Name, Created: false}
			return nil
		}
		if !errors.Is(scanErr, gorm.ErrRecordNotFound) {
			return scanErr
		}
		id := clockid.New()
		now := time.Now().UTC()
		err := pgxTx.WithContext(ctx).Create(&schema.MediaLibraryTags{
			ID:             id,
			Name:           display,
			NormalizedName: key,
			CreatedAt:      now,
			UpdatedAt:      now,
		}).Error
		if uniqueViolation(err) {
			if err := pgxTx.WithContext(ctx).Select("id, name").Where("normalized_name = ?", key).Take(&existing).Error; err != nil {
				return err
			}
			out = TagMutation{ID: existing.ID, Name: existing.Name, Created: false}
			return nil
		}
		if err != nil {
			return err
		}
		out = TagMutation{ID: id, Name: display, Created: true}
		return nil
	})
	return out, err
}

// RenameTag 按期望旧名重命名标签定义。
// 调用时机：HTTP PATCH /tags/:tag_id。已关联素材跟着新名走，因为关联的是 tag_id。
// 找不到 NotFound；expectedName 对不上 Conflict。不要把改名当成新建标签。
func (s Service) RenameTag(ctx context.Context, tagID, expectedName, name string) (Tag, error) {
	var tag Tag
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		locked, err := lockTag(ctx, pgxTx, tagID)
		if err != nil {
			return err
		}
		expected, err := normalizeName(expectedName, kindTag)
		if err != nil {
			return err
		}
		if expected != locked.Name {
			return apperr.Conflict("标签名称已被其他操作修改")
		}
		display, err := normalizeName(name, kindTag)
		if err != nil {
			return err
		}
		key, err := normalizeKey(display, kindTag)
		if err != nil {
			return err
		}
		if err := pgxTx.WithContext(ctx).Model(&schema.MediaLibraryTags{}).Where("id = ?", locked.ID).Updates(map[string]any{
			"name":            display,
			"normalized_name": key,
			"updated_at":      time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
		tag = Tag{ID: locked.ID, Name: display}
		return nil
	})
	return tag, err
}

// DeleteTag 删除标签定义并解除素材关联，返回解除条数。
// 调用时机：HTTP DELETE /tags/:tag_id。不删 MediaLibraryAsset，不碰 MediaObject。
// 找不到标签 NotFound。返回值不是「删了多少素材」。
func (s Service) DeleteTag(ctx context.Context, tagID string) (int, error) {
	var removed int
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		tag, err := lockTag(ctx, pgxTx, tagID)
		if err != nil {
			return err
		}
		var n int64
		if err := pgxTx.WithContext(ctx).Model(&schema.MediaLibraryAssetTags{}).Where("tag_id = ?", tag.ID).Count(&n).Error; err != nil {
			return err
		}
		removed = int(n)
		if err := pgxTx.WithContext(ctx).Where("tag_id = ?", tag.ID).Delete(&schema.MediaLibraryAssetTags{}).Error; err != nil {
			return err
		}
		return pgxTx.WithContext(ctx).Where("id = ?", tag.ID).Delete(&schema.MediaLibraryTags{}).Error
	})
	return removed, err
}

// MoveAssets 把素材移入一层用户文件夹，或 folderID 为 nil/空串时移到未整理。
// 调用时机：HTTP POST /organize/move。每条素材 revision+1。
// expected 必须覆盖全部 assetIDs，对不上 Conflict；文件夹不存在 NotFound。
// 禁区：不要复制媒体、不要改商品图库文件夹。
func (s Service) MoveAssets(ctx context.Context, assetIDs []string, folderID *string, expected map[string]int) ([]Asset, error) {
	ids, err := validateOrgIDs(assetIDs)
	if err != nil {
		return nil, err
	}
	if err := coverExpected(ids, expected); err != nil {
		return nil, err
	}
	var out []Asset
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if folderID != nil && *folderID != "" {
			if _, err := lockFolder(ctx, pgxTx, *folderID); err != nil {
				return err
			}
		} else {
			folderID = nil
		}
		assets, err := lockLibraryAssets(ctx, pgxTx, ids)
		if err != nil {
			return err
		}
		now := s.now()
		for _, asset := range assets {
			if err := checkRevision(asset, expected); err != nil {
				return err
			}
			if err := pgxTx.WithContext(ctx).Model(&schema.MediaLibraryAssets{}).Where("id = ?", asset.ID).Updates(map[string]any{
				"folder_id":  folderID,
				"revision":   gorm.Expr("revision + 1"),
				"updated_at": now,
			}).Error; err != nil {
				return err
			}
		}
		out, err = reloadInOrder(ctx, pgxTx, ids)
		return err
	})
	return out, err
}

// RenameAsset 按 expected revision 改素材显示名。
// 名称为空或超长返回 Validation；素材不存在返回 NotFound；名称或 revision 已变返回 Conflict。
func (s Service) RenameAsset(ctx context.Context, assetID, expectedName string, expectedRevision int, name string) (Asset, error) {
	display := whitespace.ReplaceAllString(strings.TrimSpace(name), " ")
	if display == "" {
		return Asset{}, apperr.Validation("素材名称不能为空")
	}
	if len([]rune(display)) > maxFilename {
		return Asset{}, apperr.Validationf("素材名称不能超过 %d 个字符", maxFilename)
	}
	expected := whitespace.ReplaceAllString(strings.TrimSpace(expectedName), " ")
	var out Asset
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		assets, err := lockLibraryAssets(ctx, pgxTx, []string{assetID})
		if err != nil {
			return err
		}
		if len(assets) == 0 {
			return apperr.NotFound("素材不存在")
		}
		asset := assets[0]
		if expected != "" && asset.DisplayName != expected {
			return apperr.Conflict("素材名称已被其他操作修改")
		}
		if err := checkRevision(asset, map[string]int{assetID: expectedRevision}); err != nil {
			return err
		}
		if err := pgxTx.WithContext(ctx).Model(&schema.MediaLibraryAssets{}).Where("id = ?", asset.ID).Updates(map[string]any{
			"display_name": display,
			"revision":     gorm.Expr("revision + 1"),
			"updated_at":   s.now(),
		}).Error; err != nil {
			return err
		}
		out, err = s.loadAsset(ctx, pgxTx, asset.ID)
		return err
	})
	return out, err
}

// SetTags 整表替换素材标签集合：缺的标签会创建，空 tagNames 表示清光。
// 调用时机：HTTP POST /organize/tags。每条素材 revision+1。
// expected 必须覆盖全部 id，对不上 Conflict。不要做成增量 add/remove。
func (s Service) SetTags(ctx context.Context, assetIDs, tagNames []string, expected map[string]int) ([]Asset, error) {
	ids, err := validateOrgIDs(assetIDs)
	if err != nil {
		return nil, err
	}
	if err := coverExpected(ids, expected); err != nil {
		return nil, err
	}
	if len(tagNames) > 40 {
		return nil, apperr.Validation("请求体无效")
	}
	keys := []string{}
	seen := map[string]struct{}{}
	displays := map[string]string{}
	for _, name := range tagNames {
		display, err := normalizeName(name, kindTag)
		if err != nil {
			return nil, err
		}
		key, err := normalizeKey(display, kindTag)
		if err != nil {
			return nil, err
		}
		displays[key] = display
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	var out []Asset
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		tagIDs := map[string]string{}
		if len(keys) > 0 {
			var tags []schema.MediaLibraryTags
			if err := pgxTx.WithContext(ctx).Select("id, normalized_name").Where("normalized_name IN ?", keys).Find(&tags).Error; err != nil {
				return err
			}
			for _, tag := range tags {
				tagIDs[tag.NormalizedName] = tag.ID
			}
			now := time.Now().UTC()
			for _, key := range keys {
				if _, ok := tagIDs[key]; ok {
					continue
				}
				id := clockid.New()
				err := pgxTx.WithContext(ctx).Create(&schema.MediaLibraryTags{
					ID:             id,
					Name:           displays[key],
					NormalizedName: key,
					CreatedAt:      now,
					UpdatedAt:      now,
				}).Error
				if uniqueViolation(err) {
					var existing schema.MediaLibraryTags
					if err := pgxTx.WithContext(ctx).Select("id").Where("normalized_name = ?", key).Take(&existing).Error; err != nil {
						return err
					}
					tagIDs[key] = existing.ID
					continue
				}
				if err != nil {
					return err
				}
				tagIDs[key] = id
			}
		}
		assets, err := lockLibraryAssets(ctx, pgxTx, ids)
		if err != nil {
			return err
		}
		now := s.now()
		for _, asset := range assets {
			if err := checkRevision(asset, expected); err != nil {
				return err
			}
			if err := pgxTx.WithContext(ctx).Where("asset_id = ?", asset.ID).Delete(&schema.MediaLibraryAssetTags{}).Error; err != nil {
				return err
			}
			for _, key := range keys {
				if err := pgxTx.WithContext(ctx).Create(&schema.MediaLibraryAssetTags{
					AssetID:   asset.ID,
					TagID:     tagIDs[key],
					CreatedAt: now,
				}).Error; err != nil {
					return err
				}
			}
			if err := pgxTx.WithContext(ctx).Model(&schema.MediaLibraryAssets{}).Where("id = ?", asset.ID).Updates(map[string]any{
				"revision":   gorm.Expr("revision + 1"),
				"updated_at": now,
			}).Error; err != nil {
				return err
			}
		}
		out, err = reloadInOrder(ctx, pgxTx, ids)
		return err
	})
	return out, err
}

// validateOrgIDs 检查整理批量：1..maxOrgAssets、无重复、序列化后不超过 maxOrgBytes。
func validateOrgIDs(assetIDs []string) ([]string, error) {
	if len(assetIDs) < 1 || len(assetIDs) > maxOrgAssets {
		return nil, apperr.Validationf("单次最多整理 %d 个素材", maxOrgAssets)
	}
	seen := map[string]struct{}{}
	for _, id := range assetIDs {
		if _, ok := seen[id]; ok {
			return nil, apperr.Validation("整理请求包含重复素材 ID")
		}
		seen[id] = struct{}{}
	}
	raw, err := json.Marshal(assetIDs)
	if err != nil {
		return nil, err
	}
	if len(raw) > maxOrgBytes {
		return nil, apperr.Validation("素材整理请求过大")
	}
	return assetIDs, nil
}

func coverExpected(ids []string, expected map[string]int) error {
	if len(expected) != len(ids) {
		return apperr.Validation("expected_revision 必须覆盖全部素材")
	}
	for _, id := range ids {
		if _, ok := expected[id]; !ok {
			return apperr.Validation("expected_revision 必须覆盖全部素材")
		}
	}
	return nil
}

func checkRevision(asset Asset, expected map[string]int) error {
	got, ok := expected[asset.ID]
	if !ok || got != asset.Revision {
		return apperr.Conflict("素材库资产 revision 已变化")
	}
	return nil
}
