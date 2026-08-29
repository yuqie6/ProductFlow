// Package library 实现全局素材库：/api/media-library 的列表、组织、保存、收录与工作流子图库关联。
// LibraryOrganizationDraft 确认应用函数属于本包，HTTP 挂在全局 Agent 对话上，随 P10 接线。
// 保存与收录共享 MediaObject，不复制 bytes；归档只改可见性。
package library

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
)

// Service 拥有全局素材库的保存、组织、收录与工作流子图库关联。
type Service struct {
	Pool  *pgxpool.Pool
	Media media.Store
	Now   func() time.Time
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

const assetSelect = `
	SELECT a.id, a.media_object_id, a.source_type, a.source_id, a.display_name, a.folder_id,
	       f.name, a.original_filename, a.revision, a.is_archived, a.archived_at, a.provenance_hash,
	       m.mime_type, m.byte_size, m.width, m.height, m.verification_status, m.storage_path,
	       a.created_at, a.updated_at, a.provenance_json, a.source_product_asset_id, a.source_image_session_asset_id,
	       m.sha256, src.media_object_id, src.origin_type, src.product_id, sess.media_object_id
	FROM media_library_assets a
	LEFT JOIN media_objects m ON m.id = a.media_object_id
	LEFT JOIN media_library_folders f ON f.id = a.folder_id
	LEFT JOIN product_image_assets src ON src.id = a.source_product_asset_id
	LEFT JOIN image_session_assets sess ON sess.id = a.source_image_session_asset_id
`

type scanner interface {
	Scan(dest ...any) error
}

func scanAsset(row scanner) (Asset, error) {
	var a Asset
	var provenance []byte
	var mime, storagePath, status, sha *string
	var byteSize, width, height *int
	err := row.Scan(
		&a.ID, &a.MediaObjectID, &a.SourceType, &a.SourceID, &a.DisplayName, &a.FolderID,
		&a.FolderName, &a.OriginalFilename, &a.Revision, &a.IsArchived, &a.ArchivedAt, &a.ProvenanceHash,
		&mime, &byteSize, &width, &height, &status, &storagePath,
		&a.CreatedAt, &a.UpdatedAt, &provenance, &a.SourceProductAssetID, &a.SourceSessionAssetID,
		&sha, &a.SourceProductMediaID, &a.SourceProductOrigin, &a.SourceProductProductID, &a.SourceSessionMediaID,
	)
	if err != nil {
		return Asset{}, err
	}
	if mime != nil {
		a.MIMEType = *mime
	}
	a.ByteSize, a.Width, a.Height = byteSize, width, height
	if status != nil {
		a.VerificationStatus = *status
	}
	if storagePath != nil {
		a.StoragePath = *storagePath
	}
	if sha != nil {
		a.SHA256 = *sha
	}
	if len(provenance) > 0 {
		_ = json.Unmarshal(provenance, &a.ProvenanceJSON)
	}
	return a, nil
}

func (s Service) loadAsset(ctx context.Context, q interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, id string) (Asset, error) {
	asset, err := scanAsset(q.QueryRow(ctx, assetSelect+` WHERE a.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Asset{}, apperr.NotFound("素材库资产不存在")
	}
	if err != nil {
		return Asset{}, err
	}
	tags, err := loadTags(ctx, q, []string{asset.ID})
	if err != nil {
		return Asset{}, err
	}
	asset.Tags = tags[asset.ID]
	if asset.Tags == nil {
		asset.Tags = []Tag{}
	}
	return asset, nil
}

func loadAssets(ctx context.Context, q interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, sql string, args ...any) ([]Asset, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Asset{}
	ids := []string{}
	for rows.Next() {
		asset, err := scanAsset(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, asset)
		ids = append(ids, asset.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	tags, err := loadTags(ctx, q, ids)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Tags = tags[items[i].ID]
		if items[i].Tags == nil {
			items[i].Tags = []Tag{}
		}
	}
	return items, nil
}

type tagQuery interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func loadTags(ctx context.Context, q tagQuery, assetIDs []string) (map[string][]Tag, error) {
	out := map[string][]Tag{}
	if len(assetIDs) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `
		SELECT at.asset_id, t.id, t.name
		FROM media_library_asset_tags at
		JOIN media_library_tags t ON t.id = at.tag_id
		WHERE at.asset_id = ANY($1)
		ORDER BY t.name, t.id
	`, assetIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var assetID string
		var tag Tag
		if err := rows.Scan(&assetID, &tag.ID, &tag.Name); err != nil {
			return nil, err
		}
		out[assetID] = append(out[assetID], tag)
	}
	return out, rows.Err()
}

func insertLibraryAsset(ctx context.Context, tx pgx.Tx, in Asset, p Provenance) (string, error) {
	id := clockid.New()
	hash, err := provenanceHash(p)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(storeProvenance(p))
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO media_library_assets (
			id, media_object_id, source_type, source_id, source_image_session_asset_id, source_product_asset_id,
			provenance_json, provenance_hash, revision, display_name, original_filename, folder_id,
			is_archived, archived_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,1,$9,$10,$11,FALSE,NULL,NOW(),NOW())
	`, id, in.MediaObjectID, in.SourceType, in.SourceID, in.SourceSessionAssetID, in.SourceProductAssetID,
		raw, hash, in.DisplayName, in.OriginalFilename, in.FolderID)
	return id, err
}

func findBySource(ctx context.Context, tx pgx.Tx, sourceType, sourceID string) (string, bool, error) {
	var id string
	err := tx.QueryRow(ctx, `
		SELECT id FROM media_library_assets WHERE source_type = $1 AND source_id = $2
	`, sourceType, sourceID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return id, true, nil
}

func lockFolder(ctx context.Context, tx pgx.Tx, folderID string) (Folder, error) {
	var folder Folder
	err := tx.QueryRow(ctx, `
		SELECT id, name FROM media_library_folders WHERE id = $1 FOR UPDATE
	`, folderID).Scan(&folder.ID, &folder.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return Folder{}, apperr.NotFound("素材库文件夹不存在")
	}
	return folder, err
}

func lockTag(ctx context.Context, tx pgx.Tx, tagID string) (Tag, error) {
	var tag Tag
	err := tx.QueryRow(ctx, `
		SELECT id, name FROM media_library_tags WHERE id = $1 FOR UPDATE
	`, tagID).Scan(&tag.ID, &tag.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return Tag{}, apperr.NotFound("素材库标签不存在")
	}
	return tag, err
}

func getFolder(ctx context.Context, tx pgx.Tx, folderID string) (Folder, error) {
	var folder Folder
	err := tx.QueryRow(ctx, `SELECT id, name FROM media_library_folders WHERE id = $1`, folderID).Scan(&folder.ID, &folder.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return Folder{}, apperr.NotFound("文件夹不存在")
	}
	return folder, err
}

func lockLibraryAssets(ctx context.Context, tx pgx.Tx, ids []string) ([]Asset, error) {
	rows, err := tx.Query(ctx, `
		SELECT id FROM media_library_assets WHERE id = ANY($1) ORDER BY id FOR UPDATE
	`, ids)
	if err != nil {
		return nil, err
	}
	locked := 0
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		locked++
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if locked != len(ids) {
		return nil, apperr.NotFound("素材库资产不存在")
	}
	return loadAssets(ctx, tx, assetSelect+` WHERE a.id = ANY($1)`, ids)
}

func reloadInOrder(ctx context.Context, tx pgx.Tx, ids []string) ([]Asset, error) {
	items, err := loadAssets(ctx, tx, assetSelect+` WHERE a.id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	byID := map[string]Asset{}
	for _, item := range items {
		byID[item.ID] = item
	}
	out := make([]Asset, 0, len(ids))
	for _, id := range ids {
		asset, ok := byID[id]
		if !ok {
			return nil, apperr.NotFound("素材库资产不存在")
		}
		out = append(out, asset)
	}
	return out, nil
}

func loadSessionAsset(ctx context.Context, tx pgx.Tx, id string) (sessionRow, error) {
	var row sessionRow
	var verifiedAt *time.Time
	err := tx.QueryRow(ctx, `
		SELECT a.id, a.kind::text, a.original_filename, a.created_at,
		       m.id, m.storage_path, m.mime_type, m.byte_size, m.width, m.height, m.sha256,
		       m.verification_status, m.created_at, m.verified_at
		FROM image_session_assets a
		JOIN media_objects m ON m.id = a.media_object_id
		WHERE a.id = $1
		FOR UPDATE OF a
	`, id).Scan(
		&row.ID, &row.Kind, &row.OriginalFilename, &row.CreatedAt,
		&row.Media.ID, &row.Media.StoragePath, &row.Media.MIMEType, &row.Media.ByteSize, &row.Media.Width, &row.Media.Height, &row.Media.SHA256,
		&row.Media.VerificationStatus, &row.Media.CreatedAt, &verifiedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return sessionRow{}, apperr.NotFound("会话图片不存在")
	}
	if err != nil {
		return sessionRow{}, err
	}
	if verifiedAt != nil {
		row.Media.VerifiedAt = *verifiedAt
	}
	return row, nil
}

func requireWorkflow(ctx context.Context, tx pgx.Tx, productID, workflowID string, forUpdate bool) error {
	sql := `SELECT id FROM workflow_graphs WHERE id = $1 AND product_id = $2`
	if forUpdate {
		sql += ` FOR UPDATE`
	}
	var id string
	err := tx.QueryRow(ctx, sql, workflowID, productID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound("工作流不存在")
	}
	return err
}
