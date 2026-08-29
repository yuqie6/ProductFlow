package library

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/tx"
)

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
	err = tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		var existing Folder
		scanErr := pgxTx.QueryRow(ctx, `
			SELECT id, name FROM media_library_folders WHERE normalized_name = $1
		`, key).Scan(&existing.ID, &existing.Name)
		if scanErr == nil {
			out = FolderMutation{ID: existing.ID, Name: existing.Name, Created: false}
			return nil
		}
		if !errors.Is(scanErr, pgx.ErrNoRows) {
			return scanErr
		}
		id := clockid.New()
		_, err := pgxTx.Exec(ctx, `
			INSERT INTO media_library_folders (id, name, normalized_name, created_at, updated_at)
			VALUES ($1, $2, $3, NOW(), NOW())
		`, id, display, key)
		if uniqueViolation(err) {
			scanErr = pgxTx.QueryRow(ctx, `
				SELECT id, name FROM media_library_folders WHERE normalized_name = $1
			`, key).Scan(&existing.ID, &existing.Name)
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

func (s Service) RenameFolder(ctx context.Context, folderID, expectedName, name string) (Folder, error) {
	var folder Folder
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
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
			_, err = pgxTx.Exec(ctx, `
				UPDATE media_library_folders SET name = $1, normalized_name = $2, updated_at = NOW() WHERE id = $3
			`, display, key, locked.ID)
			if err != nil {
				return err
			}
		}
		folder = Folder{ID: locked.ID, Name: display}
		return nil
	})
	return folder, err
}

func (s Service) DeleteFolder(ctx context.Context, folderID string) (int, error) {
	var moved int
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		folder, err := lockFolder(ctx, pgxTx, folderID)
		if err != nil {
			return err
		}
		if err := pgxTx.QueryRow(ctx, `SELECT COUNT(*) FROM media_library_assets WHERE folder_id = $1`, folder.ID).Scan(&moved); err != nil {
			return err
		}
		if _, err := pgxTx.Exec(ctx, `
			UPDATE media_library_assets SET folder_id = NULL, updated_at = NOW() WHERE folder_id = $1
		`, folder.ID); err != nil {
			return err
		}
		_, err = pgxTx.Exec(ctx, `DELETE FROM media_library_folders WHERE id = $1`, folder.ID)
		return err
	})
	return moved, err
}

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
	err = tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		var existing Tag
		scanErr := pgxTx.QueryRow(ctx, `
			SELECT id, name FROM media_library_tags WHERE normalized_name = $1
		`, key).Scan(&existing.ID, &existing.Name)
		if scanErr == nil {
			out = TagMutation{ID: existing.ID, Name: existing.Name, Created: false}
			return nil
		}
		if !errors.Is(scanErr, pgx.ErrNoRows) {
			return scanErr
		}
		id := clockid.New()
		_, err := pgxTx.Exec(ctx, `
			INSERT INTO media_library_tags (id, name, normalized_name, created_at, updated_at)
			VALUES ($1, $2, $3, NOW(), NOW())
		`, id, display, key)
		if uniqueViolation(err) {
			if err := pgxTx.QueryRow(ctx, `
				SELECT id, name FROM media_library_tags WHERE normalized_name = $1
			`, key).Scan(&existing.ID, &existing.Name); err != nil {
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

func (s Service) RenameTag(ctx context.Context, tagID, expectedName, name string) (Tag, error) {
	var tag Tag
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
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
		_, err = pgxTx.Exec(ctx, `
			UPDATE media_library_tags SET name = $1, normalized_name = $2, updated_at = NOW() WHERE id = $3
		`, display, key, locked.ID)
		if err != nil {
			return err
		}
		tag = Tag{ID: locked.ID, Name: display}
		return nil
	})
	return tag, err
}

func (s Service) DeleteTag(ctx context.Context, tagID string) (int, error) {
	var removed int
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		tag, err := lockTag(ctx, pgxTx, tagID)
		if err != nil {
			return err
		}
		if err := pgxTx.QueryRow(ctx, `SELECT COUNT(*) FROM media_library_asset_tags WHERE tag_id = $1`, tag.ID).Scan(&removed); err != nil {
			return err
		}
		if _, err := pgxTx.Exec(ctx, `DELETE FROM media_library_asset_tags WHERE tag_id = $1`, tag.ID); err != nil {
			return err
		}
		_, err = pgxTx.Exec(ctx, `DELETE FROM media_library_tags WHERE id = $1`, tag.ID)
		return err
	})
	return removed, err
}

func (s Service) MoveAssets(ctx context.Context, assetIDs []string, folderID *string, expected map[string]int) ([]Asset, error) {
	ids, err := validateOrgIDs(assetIDs)
	if err != nil {
		return nil, err
	}
	if err := coverExpected(ids, expected); err != nil {
		return nil, err
	}
	var out []Asset
	err = tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
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
			if _, err := pgxTx.Exec(ctx, `
				UPDATE media_library_assets SET folder_id = $1, revision = revision + 1, updated_at = $2 WHERE id = $3
			`, folderID, now, asset.ID); err != nil {
				return err
			}
		}
		out, err = reloadInOrder(ctx, pgxTx, ids)
		return err
	})
	return out, err
}

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
	err = tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		tagIDs := map[string]string{}
		if len(keys) > 0 {
			rows, err := pgxTx.Query(ctx, `SELECT id, normalized_name FROM media_library_tags WHERE normalized_name = ANY($1)`, keys)
			if err != nil {
				return err
			}
			for rows.Next() {
				var id, key string
				if err := rows.Scan(&id, &key); err != nil {
					rows.Close()
					return err
				}
				tagIDs[key] = id
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
			for _, key := range keys {
				if _, ok := tagIDs[key]; ok {
					continue
				}
				id := clockid.New()
				_, err := pgxTx.Exec(ctx, `
					INSERT INTO media_library_tags (id, name, normalized_name, created_at, updated_at)
					VALUES ($1, $2, $3, NOW(), NOW())
				`, id, displays[key], key)
				if uniqueViolation(err) {
					var existing string
					if err := pgxTx.QueryRow(ctx, `SELECT id FROM media_library_tags WHERE normalized_name = $1`, key).Scan(&existing); err != nil {
						return err
					}
					tagIDs[key] = existing
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
			if _, err := pgxTx.Exec(ctx, `DELETE FROM media_library_asset_tags WHERE asset_id = $1`, asset.ID); err != nil {
				return err
			}
			for _, key := range keys {
				if _, err := pgxTx.Exec(ctx, `
					INSERT INTO media_library_asset_tags (asset_id, tag_id, created_at) VALUES ($1, $2, NOW())
				`, asset.ID, tagIDs[key]); err != nil {
					return err
				}
			}
			if _, err := pgxTx.Exec(ctx, `
				UPDATE media_library_assets SET revision = revision + 1, updated_at = $1 WHERE id = $2
			`, now, asset.ID); err != nil {
				return err
			}
		}
		out, err = reloadInOrder(ctx, pgxTx, ids)
		return err
	})
	return out, err
}

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
