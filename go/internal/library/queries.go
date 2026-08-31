package library

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

// Get 按 id 读取全局素材身份（内部 Asset，不是 AssetResponse）。
// 调用时机：HTTP GET/download/archive/restore，以及收录前核验。
// 找不到返回 NotFound「素材库资产不存在」。不读磁盘 bytes；不要当工作流子图库关联用。
func (s Service) Get(ctx context.Context, assetID string) (Asset, error) {
	var asset Asset
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		var err error
		asset, err = s.loadAsset(ctx, pgxTx, assetID)
		return err
	})
	return asset, err
}

// Bootstrap 读图库首页计数、一层文件夹与标签，无写入。
// 调用时机：HTTP GET /bootstrap。文件夹 Count 只含未归档素材。
// 读库失败或 ctx 取消时返回 error。
func (s Service) Bootstrap(ctx context.Context) (Bootstrap, error) {
	var out Bootstrap
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		var total, active, unorganized int64
		if err := pgxTx.WithContext(ctx).Model(&schema.MediaLibraryAssets{}).Count(&total).Error; err != nil {
			return err
		}
		if err := pgxTx.WithContext(ctx).Model(&schema.MediaLibraryAssets{}).Where("is_archived = ?", false).Count(&active).Error; err != nil {
			return err
		}
		out.TotalCount = int(total)
		out.ActiveCount = int(active)
		out.ArchivedCount = out.TotalCount - out.ActiveCount
		if err := pgxTx.WithContext(ctx).Model(&schema.MediaLibraryAssets{}).
			Where("is_archived = ? AND folder_id IS NULL", false).Count(&unorganized).Error; err != nil {
			return err
		}
		out.UnorganizedCount = int(unorganized)
		var folders []struct {
			ID    string `gorm:"column:id"`
			Name  string `gorm:"column:name"`
			Count int64  `gorm:"column:count"`
		}
		if err := pgxTx.WithContext(ctx).Table("media_library_folders AS f").
			Select("f.id, f.name, COUNT(a.id) AS count").
			Joins("LEFT JOIN media_library_assets a ON a.folder_id = f.id AND a.is_archived = FALSE").
			Group("f.id").
			Order("f.name, f.id").
			Scan(&folders).Error; err != nil {
			return err
		}
		out.Folders = make([]Folder, 0, len(folders))
		for _, folder := range folders {
			out.Folders = append(out.Folders, Folder{ID: folder.ID, Name: folder.Name, Count: int(folder.Count)})
		}
		var tags []struct {
			ID    string `gorm:"column:id"`
			Name  string `gorm:"column:name"`
			Count int64  `gorm:"column:count"`
		}
		if err := pgxTx.WithContext(ctx).Table("media_library_tags AS t").
			Select("t.id, t.name, COUNT(a.id) AS count").
			Joins("LEFT JOIN media_library_asset_tags at ON at.tag_id = t.id").
			Joins("LEFT JOIN media_library_assets a ON a.id = at.asset_id AND a.is_archived = FALSE").
			Group("t.id").
			Order("t.name, t.id").
			Scan(&tags).Error; err != nil {
			return err
		}
		out.Tags = make([]Tag, 0, len(tags))
		for _, tag := range tags {
			out.Tags = append(out.Tags, Tag{ID: tag.ID, Name: tag.Name, Count: int(tag.Count)})
		}
		return nil
	})
	return out, err
}

// List 按 ListFilter 分页列出全局素材 HTTP 投影。
// 调用时机：HTTP GET /api/media-library。Limit 默认 20、上限 100。
// 游标与当前筛选签名不一致返回 Validation，禁止跨筛选续页。
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
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		q := libraryAssetQuery(pgxTx.WithContext(ctx))
		if !in.IncludeArchived {
			q = q.Where("a.is_archived = ?", false)
		}
		if search != "" {
			pattern := "%" + search + "%"
			q = q.Where("(a.display_name ILIKE ? OR a.original_filename ILIKE ?)", pattern, pattern)
		}
		if sourceType != "" {
			q = q.Where("a.source_type = ?", sourceType)
		}
		if folderID != "" {
			q = q.Where("a.folder_id = ?", folderID)
		}
		if tag != "" {
			q = q.Joins("JOIN media_library_asset_tags at ON at.asset_id = a.id").
				Joins("JOIN media_library_tags tg ON tg.id = at.tag_id").
				Where("tg.normalized_name = ?", tag)
		}
		if cursorCreated != nil {
			q = q.Where("(a.created_at < ? OR (a.created_at = ? AND a.id < ?))", *cursorCreated, *cursorCreated, cursorID)
		}
		q = q.Order("a.created_at DESC, a.id DESC").Limit(in.Limit + 1)
		items, err := loadAssets(ctx, pgxTx, q)
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

// decodeCursor 解开 v2 游标。sort/filter 与当前列表签名不一致时一律 400，避免跨筛选续页。
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
