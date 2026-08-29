package library

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/tx"
)

func (s Service) Get(ctx context.Context, assetID string) (Asset, error) {
	var asset Asset
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		var err error
		asset, err = s.loadAsset(ctx, pgxTx, assetID)
		return err
	})
	return asset, err
}

func (s Service) Bootstrap(ctx context.Context) (Bootstrap, error) {
	var out Bootstrap
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		if err := pgxTx.QueryRow(ctx, `SELECT COUNT(*) FROM media_library_assets`).Scan(&out.TotalCount); err != nil {
			return err
		}
		if err := pgxTx.QueryRow(ctx, `SELECT COUNT(*) FROM media_library_assets WHERE is_archived = FALSE`).Scan(&out.ActiveCount); err != nil {
			return err
		}
		out.ArchivedCount = out.TotalCount - out.ActiveCount
		if err := pgxTx.QueryRow(ctx, `
			SELECT COUNT(*) FROM media_library_assets WHERE is_archived = FALSE AND folder_id IS NULL
		`).Scan(&out.UnorganizedCount); err != nil {
			return err
		}
		folderRows, err := pgxTx.Query(ctx, `
			SELECT f.id, f.name, COUNT(a.id)
			FROM media_library_folders f
			LEFT JOIN media_library_assets a ON a.folder_id = f.id AND a.is_archived = FALSE
			GROUP BY f.id
			ORDER BY f.name, f.id
		`)
		if err != nil {
			return err
		}
		defer folderRows.Close()
		out.Folders = []Folder{}
		for folderRows.Next() {
			var folder Folder
			if err := folderRows.Scan(&folder.ID, &folder.Name, &folder.Count); err != nil {
				return err
			}
			out.Folders = append(out.Folders, folder)
		}
		if err := folderRows.Err(); err != nil {
			return err
		}
		tagRows, err := pgxTx.Query(ctx, `
			SELECT t.id, t.name, COUNT(a.id)
			FROM media_library_tags t
			LEFT JOIN media_library_asset_tags at ON at.tag_id = t.id
			LEFT JOIN media_library_assets a ON a.id = at.asset_id AND a.is_archived = FALSE
			GROUP BY t.id
			ORDER BY t.name, t.id
		`)
		if err != nil {
			return err
		}
		defer tagRows.Close()
		out.Tags = []Tag{}
		for tagRows.Next() {
			var tag Tag
			if err := tagRows.Scan(&tag.ID, &tag.Name, &tag.Count); err != nil {
				return err
			}
			out.Tags = append(out.Tags, tag)
		}
		return tagRows.Err()
	})
	return out, err
}

func (s Service) List(ctx context.Context, in ListFilter) (ListResponse, error) {
	if in.Limit < 1 {
		in.Limit = 20
	}
	if in.Limit > 100 {
		in.Limit = 100
	}
	search := normalizeSearch(in.Search)
	sourceType := strings.TrimSpace(in.SourceType)
	folderID := strings.TrimSpace(in.FolderID)
	tag := ""
	if strings.TrimSpace(in.Tag) != "" {
		tag = strings.TrimSpace(in.Tag)
	}
	signature := filterSignature(search, sourceType, in.IncludeArchived, folderID, tag)
	asOf := s.now()
	var cursorCreated *time.Time
	var cursorID string
	if strings.TrimSpace(in.Cursor) != "" {
		created, id, decodedAsOf, err := decodeCursor(in.Cursor, signature)
		if err != nil {
			return ListResponse{}, err
		}
		cursorCreated = &created
		cursorID = id
		asOf = decodedAsOf
	}
	var page ListResponse
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		sql := assetSelect
		args := []any{}
		where := []string{}
		if !in.IncludeArchived {
			where = append(where, "a.is_archived = FALSE")
		}
		if search != "" {
			args = append(args, "%"+search+"%")
			where = append(where, fmt.Sprintf("(a.display_name ILIKE $%d OR a.original_filename ILIKE $%d)", len(args), len(args)))
		}
		if sourceType != "" {
			args = append(args, sourceType)
			where = append(where, fmt.Sprintf("a.source_type = $%d", len(args)))
		}
		if folderID != "" {
			args = append(args, folderID)
			where = append(where, fmt.Sprintf("a.folder_id = $%d", len(args)))
		}
		if tag != "" {
			sql += `
				JOIN media_library_asset_tags at ON at.asset_id = a.id
				JOIN media_library_tags tg ON tg.id = at.tag_id`
			args = append(args, tag)
			where = append(where, fmt.Sprintf("tg.normalized_name = $%d", len(args)))
		}
		if cursorCreated != nil {
			args = append(args, *cursorCreated, cursorID)
			where = append(where, fmt.Sprintf("(a.created_at < $%d OR (a.created_at = $%d AND a.id < $%d))", len(args)-1, len(args)-1, len(args)))
		}
		if len(where) > 0 {
			sql += " WHERE " + strings.Join(where, " AND ")
		}
		sql += " ORDER BY a.created_at DESC, a.id DESC"
		args = append(args, in.Limit+1)
		sql += fmt.Sprintf(" LIMIT $%d", len(args))
		items, err := loadAssets(ctx, pgxTx, sql, args...)
		if err != nil {
			return err
		}
		hasMore := len(items) > in.Limit
		if hasMore {
			items = items[:in.Limit]
		}
		page.Items, err = serializeAssets(items)
		if err != nil {
			return err
		}
		if hasMore && len(items) > 0 {
			next, err := encodeCursor(items[len(items)-1].CreatedAt, items[len(items)-1].ID, signature, asOf)
			if err != nil {
				return err
			}
			page.NextCursor = &next
		}
		return nil
	})
	return page, err
}

func encodeCursor(createdAt time.Time, assetID, signature string, asOf time.Time) (string, error) {
	raw, err := canonicalJSON(map[string]any{
		"v":      2,
		"sort":   "created_desc",
		"filter": signature,
		"key":    pythonISOFormat(createdAt),
		"id":     assetID,
		"as_of":  pythonISOFormat(asOf),
	})
	if err != nil {
		return "", err
	}
	return strings.TrimRight(base64.URLEncoding.EncodeToString(raw), "="), nil
}

func decodeCursor(cursor, signature string) (time.Time, string, time.Time, error) {
	invalid := apperr.Validation("素材库分页游标无效或与当前筛选条件不匹配")
	padded := cursor + strings.Repeat("=", (4-len(cursor)%4)%4)
	raw, err := base64.URLEncoding.DecodeString(padded)
	if err != nil {
		return time.Time{}, "", time.Time{}, invalid
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return time.Time{}, "", time.Time{}, invalid
	}
	if asIntOr(payload["v"], 0) != 2 || payload["sort"] != "created_desc" || payload["filter"] != signature {
		return time.Time{}, "", time.Time{}, invalid
	}
	key, _ := payload["key"].(string)
	id, _ := payload["id"].(string)
	asOfRaw, _ := payload["as_of"].(string)
	if key == "" || id == "" || asOfRaw == "" {
		return time.Time{}, "", time.Time{}, invalid
	}
	created, err := parseDateTime(key)
	if err != nil {
		return time.Time{}, "", time.Time{}, invalid
	}
	asOf, err := parseDateTime(asOfRaw)
	if err != nil {
		return time.Time{}, "", time.Time{}, invalid
	}
	return created, id, asOf, nil
}

func asIntOr(v any, fallback int) int {
	n, err := asInt(v)
	if err != nil {
		return fallback
	}
	return n
}
