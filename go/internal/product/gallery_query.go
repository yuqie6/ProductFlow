package product

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	sqldb "database/sql"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

const (
	galleryCursorVersion       = 1
	galleryDefaultLimit        = 50
	galleryMaxLimit            = 100
	galleryRecentDays          = 30
	galleryUnclassifiedTypeKey = "__unclassified__"
)

var (
	galleryBusinessKey = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	generatedOrigins   = []string{"workflow_generation", "image_session_attach"}
	originTypeOrder    = []string{"upload", "workflow_generation", "image_session_attach", "local_edit"}
)

type galleryCursor struct {
	sort       string
	filterHash string
	key        string
	assetID    string
	asOf       *time.Time
}

type galleryRow struct {
	asset          ImageAsset
	sortName       string
	imageTypeTitle *string
	genWorkflowID  *string
	genNodeID      *string
	genNodeRunID   *string
	genVisualID    *string
	renditionJobID *string
	renditionSrcID *string
	renditionSpec  []byte
	renditionStat  *string
	folderName     *string
}

const gallerySelectSQL = `
		SELECT a.id, a.product_id, a.media_object_id, a.origin_type, a.display_name, a.original_filename,
		       a.image_type_key, a.user_folder_id, a.parent_asset_id, a.source_image_session_asset_id,
		       a.source_library_asset_id, m.mime_type, m.byte_size, m.width, m.height, m.verification_status,
		       m.storage_path, a.created_at, a.updated_at,
		       LOWER(a.display_name) AS gallery_sort_name,
		       COALESCE(direct_node.title, source_node.title) AS image_type_title,
		       COALESCE(direct_gen.graph_id, source_gen.graph_id) AS gen_workflow_id,
		       COALESCE(direct_gen.node_id, source_gen.node_id) AS gen_node_id,
		       COALESCE(direct_gen.node_run_id, source_gen.node_run_id) AS gen_node_run_id,
		       COALESCE(direct_gen.payload_json->>'visual_system_version_id', source_gen.payload_json->>'visual_system_version_id') AS gen_visual_version_id,
		       rend.id AS rendition_job_id,
		       rend.source_asset_id,
		       rend.spec_json,
		       rend.status,
		       f.name AS user_folder_name
		FROM product_image_assets a
		JOIN media_objects m ON m.id = a.media_object_id
		LEFT JOIN product_asset_folders f ON f.id = a.user_folder_id
		LEFT JOIN workflow_graph_artifacts direct_gen
		  ON direct_gen.product_image_asset_id = a.id AND direct_gen.artifact_type = 'image'
		LEFT JOIN workflow_graph_nodes direct_node ON direct_node.id = direct_gen.node_id
		LEFT JOIN delivery_rendition_jobs rend ON rend.result_asset_id = a.id
		LEFT JOIN product_image_assets rend_src ON rend_src.id = rend.source_asset_id
		LEFT JOIN workflow_graph_artifacts source_gen
		  ON source_gen.product_image_asset_id = rend_src.id AND source_gen.artifact_type = 'image'
		LEFT JOIN workflow_graph_nodes source_node ON source_node.id = source_gen.node_id
`

// GalleryBootstrap 返回系统目录计数与用户文件夹；cover_image_asset_id 只是展示元数据。
func (s Service) GalleryBootstrap(ctx context.Context, productID string) (GalleryBootstrap, error) {
	var out GalleryBootstrap
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		product, err := loadProduct(ctx, pgxTx, productID)
		if err != nil {
			return err
		}
		asOf := s.now()
		var allCount, unorganized, recent int
		if err := pfdb.QueryRow(ctx, pgxTx, `SELECT COUNT(*) FROM product_image_assets WHERE product_id = $1`, productID).Scan(&allCount); err != nil {
			return err
		}
		if err := pfdb.QueryRow(ctx, pgxTx, `
			SELECT COUNT(*) FROM product_image_assets WHERE product_id = $1 AND user_folder_id IS NULL
		`, productID).Scan(&unorganized); err != nil {
			return err
		}
		if err := pfdb.QueryRow(ctx, pgxTx, `
			SELECT COUNT(*) FROM product_image_assets
			WHERE product_id = $1 AND origin_type = ANY($2) AND created_at >= $3
		`, productID, generatedOrigins, asOf.Add(-galleryRecentDays*24*time.Hour)).Scan(&recent); err != nil {
			return err
		}
		originCounts, err := loadOriginCounts(ctx, pgxTx, productID)
		if err != nil {
			return err
		}
		imageTypes, err := loadImageTypeCounts(ctx, pgxTx, productID)
		if err != nil {
			return err
		}
		folders, err := loadFolderCounts(ctx, pgxTx, productID)
		if err != nil {
			return err
		}
		generated := originCounts["workflow_generation"] + originCounts["image_session_attach"]
		origins := make([]GalleryOrigin, 0)
		for _, origin := range originTypeOrder {
			if originCounts[origin] > 0 {
				origins = append(origins, GalleryOrigin{OriginType: origin, Count: originCounts[origin]})
			}
		}
		out = GalleryBootstrap{
			ProductID:         product.ID,
			CoverImageAssetID: product.CoverImageAssetID,
			SystemDirectories: []GallerySystemDirectory{
				{Kind: "all", Count: allCount},
				{Kind: "recent_generated", Count: recent},
				{Kind: "uploads", Count: originCounts["upload"]},
				{Kind: "generated", Count: generated},
				{Kind: "unorganized", Count: unorganized},
			},
			ImageTypes:       imageTypes,
			Origins:          origins,
			UserFolders:      folders,
			UnorganizedCount: unorganized,
		}
		return nil
	})
	return out, err
}

type GalleryListInput struct {
	DirectoryKind string
	DirectoryKey  string
	Query         string
	Sort          string
	After         string
	Limit         int
}

// ListGalleryAssets 按系统目录或用户文件夹分页列出商品图，cursor 绑定筛选条件。
func (s Service) ListGalleryAssets(ctx context.Context, productID string, in GalleryListInput) (GalleryAssetPage, error) {
	var page GalleryAssetPage
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if _, err := loadProduct(ctx, pgxTx, productID); err != nil {
			return err
		}
		kind, key, err := normalizeDirectory(ctx, pgxTx, productID, in.DirectoryKind, in.DirectoryKey)
		if err != nil {
			return err
		}
		query := strings.TrimSpace(in.Query)
		if utf8.RuneCountInString(query) > 255 {
			return apperr.Validation("图库搜索词不能超过 255 个字符")
		}
		limit := in.Limit
		if limit == 0 {
			limit = galleryDefaultLimit
		}
		if limit < 1 || limit > galleryMaxLimit {
			return apperr.Validationf("图库分页 limit 必须在 1 到 %d 之间", galleryMaxLimit)
		}
		sort := in.Sort
		if sort == "" {
			sort = "created_desc"
		}
		if !validGallerySort(sort) {
			return apperr.Validation("图库排序无效")
		}
		filterHash, err := galleryFilterHash(productID, kind, key, query, sort)
		if err != nil {
			return err
		}
		var cursor *galleryCursor
		if strings.TrimSpace(in.After) != "" {
			decoded, err := decodeGalleryCursor(in.After)
			if err != nil {
				return err
			}
			cursor = &decoded
			if cursor.sort != sort || cursor.filterHash != filterHash {
				return apperr.Validation("图库分页 cursor 与当前查询条件不匹配")
			}
		}
		var asOf *time.Time
		if kind == "recent_generated" {
			if cursor != nil && cursor.asOf != nil {
				asOf = cursor.asOf
			} else {
				now := s.now()
				asOf = &now
			}
			if asOf == nil {
				return apperr.Validation("图库分页 cursor 缺少最近生成时间锚点")
			}
		} else if cursor != nil && cursor.asOf != nil {
			return apperr.Validation("图库分页 cursor 与当前目录不匹配")
		}
		if cursor != nil && (sort == "created_asc" || sort == "created_desc") {
			if _, err := parseCursorTime(cursor.key); err != nil {
				return err
			}
		}
		sql, args := buildGalleryListSQL(productID, kind, key, query, sort, cursor, asOf, limit+1)
		rows, err := pfdb.Query(ctx, pgxTx, sql, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		var collected []galleryRow
		for rows.Next() {
			row, err := scanGalleryRow(rows)
			if err != nil {
				return err
			}
			collected = append(collected, row)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		hasMore := len(collected) > limit
		if hasMore {
			collected = collected[:limit]
		}
		items := make([]GalleryAssetResponse, 0, len(collected))
		for _, row := range collected {
			items = append(items, projectGalleryAsset(row))
		}
		page = GalleryAssetPage{Items: items}
		if hasMore && len(collected) > 0 {
			last := collected[len(collected)-1]
			sortKey := last.sortName
			if sort == "created_asc" || sort == "created_desc" {
				sortKey = last.asset.CreatedAt.UTC().Format(time.RFC3339Nano)
			}
			encoded, err := encodeGalleryCursor(galleryCursor{
				sort:       sort,
				filterHash: filterHash,
				key:        sortKey,
				assetID:    last.asset.ID,
				asOf:       asOf,
			})
			if err != nil {
				return err
			}
			page.NextCursor = &encoded
		}
		return nil
	})
	return page, err
}

// GetGalleryAsset 返回单张商品图及其生成/交付 lineage。
func (s Service) GetGalleryAsset(ctx context.Context, productID, assetID string) (GalleryAssetResponse, error) {
	var out GalleryAssetResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if _, err := loadProduct(ctx, pgxTx, productID); err != nil {
			return err
		}
		row, err := pfdb.Query(ctx, pgxTx, gallerySelectSQL+` WHERE a.product_id = $1 AND a.id = $2`, productID, assetID)
		if err != nil {
			return err
		}
		defer row.Close()
		if !row.Next() {
			return apperr.NotFound("商品图片不存在")
		}
		scanned, err := scanGalleryRow(row)
		if err != nil {
			return err
		}
		out = projectGalleryAsset(scanned)
		return nil
	})
	return out, err
}

func loadOriginCounts(ctx context.Context, tx *gorm.DB, productID string) (map[string]int, error) {
	rows, err := pfdb.Query(ctx, tx, `
		SELECT origin_type, COUNT(*) FROM product_image_assets WHERE product_id = $1 GROUP BY origin_type
	`, productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var origin string
		var count int
		if err := rows.Scan(&origin, &count); err != nil {
			return nil, err
		}
		out[origin] = count
	}
	return out, rows.Err()
}

func loadImageTypeCounts(ctx context.Context, tx *gorm.DB, productID string) ([]GalleryImageType, error) {
	rows, err := pfdb.Query(ctx, tx, `
		SELECT image_type_key, COUNT(*) FROM product_image_assets
		WHERE product_id = $1 GROUP BY image_type_key ORDER BY image_type_key NULLS FIRST
	`, productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GalleryImageType{}
	for rows.Next() {
		var key *string
		var count int
		if err := rows.Scan(&key, &count); err != nil {
			return nil, err
		}
		item := GalleryImageType{Count: count, ImageTypeKey: key}
		if key == nil {
			item.DirectoryKey = galleryUnclassifiedTypeKey
			item.Title = "未分类"
		} else {
			item.DirectoryKey = *key
			item.Title = *key
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func loadFolderCounts(ctx context.Context, tx *gorm.DB, productID string) ([]GalleryFolder, error) {
	rows, err := pfdb.Query(ctx, tx, `
		SELECT f.id, f.name, f.sort_order, COUNT(a.id)
		FROM product_asset_folders f
		LEFT JOIN product_image_assets a ON a.user_folder_id = f.id
		WHERE f.product_id = $1
		GROUP BY f.id
		ORDER BY f.sort_order, f.name, f.id
	`, productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GalleryFolder{}
	for rows.Next() {
		var folder GalleryFolder
		if err := rows.Scan(&folder.ID, &folder.Name, &folder.SortOrder, &folder.Count); err != nil {
			return nil, err
		}
		out = append(out, folder)
	}
	return out, rows.Err()
}

func normalizeDirectory(ctx context.Context, tx *gorm.DB, productID, kind, key string) (string, *string, error) {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "all"
	}
	switch kind {
	case "all", "recent_generated", "uploads", "generated", "image_type", "source", "unorganized", "user_folder":
	default:
		return "", nil, apperr.Validation("图库目录无效")
	}
	normalizedKey := strings.TrimSpace(key)
	keyed := kind == "image_type" || kind == "source" || kind == "user_folder"
	if keyed && normalizedKey == "" {
		return "", nil, apperr.Validation("当前图库目录必须提供 directory_key")
	}
	if !keyed && normalizedKey != "" {
		return "", nil, apperr.Validation("当前图库目录不接受 directory_key")
	}
	switch kind {
	case "image_type":
		if normalizedKey != galleryUnclassifiedTypeKey && (len(normalizedKey) > 80 || !galleryBusinessKey.MatchString(normalizedKey)) {
			return "", nil, apperr.Validation("图片类型 directory_key 无效")
		}
	case "source":
		ok := false
		for _, origin := range originTypeOrder {
			if origin == normalizedKey {
				ok = true
				break
			}
		}
		if !ok {
			return "", nil, apperr.Validation("图片来源 directory_key 无效")
		}
	case "user_folder":
		var exists string
		err := pfdb.QueryRow(ctx, tx, `
			SELECT id FROM product_asset_folders WHERE id = $1 AND product_id = $2
		`, normalizedKey, productID).Scan(&exists)
		if errors.Is(err, sqldb.ErrNoRows) {
			return "", nil, apperr.NotFound("商品图片文件夹不存在")
		}
		if err != nil {
			return "", nil, err
		}
	}
	if normalizedKey == "" {
		return kind, nil, nil
	}
	return kind, &normalizedKey, nil
}

func validGallerySort(sort string) bool {
	switch sort {
	case "created_desc", "created_asc", "name_asc", "name_desc":
		return true
	default:
		return false
	}
}

func buildGalleryListSQL(productID, kind string, key *string, query, sort string, cursor *galleryCursor, asOf *time.Time, limit int) (string, []any) {
	args := []any{productID}
	where := []string{"a.product_id = $1"}
	switch kind {
	case "recent_generated":
		args = append(args, generatedOrigins, asOf.Add(-galleryRecentDays*24*time.Hour))
		where = append(where, fmt.Sprintf("a.origin_type = ANY($%d) AND a.created_at >= $%d", len(args)-1, len(args)))
	case "uploads":
		args = append(args, "upload")
		where = append(where, fmt.Sprintf("a.origin_type = $%d", len(args)))
	case "generated":
		args = append(args, generatedOrigins)
		where = append(where, fmt.Sprintf("a.origin_type = ANY($%d)", len(args)))
	case "image_type":
		if key != nil && *key == galleryUnclassifiedTypeKey {
			where = append(where, "a.image_type_key IS NULL")
		} else {
			args = append(args, *key)
			where = append(where, fmt.Sprintf("a.image_type_key = $%d", len(args)))
		}
	case "source":
		args = append(args, *key)
		where = append(where, fmt.Sprintf("a.origin_type = $%d", len(args)))
	case "unorganized":
		where = append(where, "a.user_folder_id IS NULL")
	case "user_folder":
		args = append(args, *key)
		where = append(where, fmt.Sprintf("a.user_folder_id = $%d", len(args)))
	}
	if query != "" {
		pattern := "%" + escapeLike(query) + "%"
		args = append(args, pattern)
		idx := strconv.Itoa(len(args))
		where = append(where, "(a.display_name ILIKE $"+idx+" ESCAPE '\\' OR a.original_filename ILIKE $"+idx+" ESCAPE '\\')")
	}
	if cursor != nil {
		if sort == "created_asc" || sort == "created_desc" {
			parsed, _ := parseCursorTime(cursor.key)
			args = append(args, parsed, cursor.assetID)
			col := "a.created_at"
			idIdx := strconv.Itoa(len(args))
			keyIdx := strconv.Itoa(len(args) - 1)
			if sort == "created_asc" {
				where = append(where, fmt.Sprintf("(%s > $%s OR (%s = $%s AND a.id > $%s))", col, keyIdx, col, keyIdx, idIdx))
			} else {
				where = append(where, fmt.Sprintf("(%s < $%s OR (%s = $%s AND a.id < $%s))", col, keyIdx, col, keyIdx, idIdx))
			}
		} else {
			args = append(args, cursor.key, cursor.assetID)
			idIdx := strconv.Itoa(len(args))
			keyIdx := strconv.Itoa(len(args) - 1)
			if sort == "name_asc" {
				where = append(where, fmt.Sprintf("(LOWER(a.display_name) > $%s OR (LOWER(a.display_name) = $%s AND a.id > $%s))", keyIdx, keyIdx, idIdx))
			} else {
				where = append(where, fmt.Sprintf("(LOWER(a.display_name) < $%s OR (LOWER(a.display_name) = $%s AND a.id < $%s))", keyIdx, keyIdx, idIdx))
			}
		}
	}
	order := "a.created_at DESC, a.id DESC"
	switch sort {
	case "created_asc":
		order = "a.created_at ASC, a.id ASC"
	case "name_asc":
		order = "LOWER(a.display_name) ASC, a.id ASC"
	case "name_desc":
		order = "LOWER(a.display_name) DESC, a.id DESC"
	}
	args = append(args, limit)
	sql := gallerySelectSQL + " WHERE " + strings.Join(where, " AND ") + " ORDER BY " + order + " LIMIT $" + strconv.Itoa(len(args))
	return sql, args
}

func parseCursorTime(raw string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.999999999-07:00", "2006-01-02T15:04:05-07:00"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, apperr.Validation("图库分页 cursor 无效")
}

func scanGalleryRow(rows *sqldb.Rows) (galleryRow, error) {
	var row galleryRow
	err := rows.Scan(
		&row.asset.ID, &row.asset.ProductID, &row.asset.MediaObjectID, &row.asset.OriginType, &row.asset.DisplayName, &row.asset.OriginalFilename,
		&row.asset.ImageTypeKey, &row.asset.UserFolderID, &row.asset.ParentAssetID, &row.asset.SourceImageSessionAsset,
		&row.asset.SourceLibraryAsset, &row.asset.MIMEType, &row.asset.ByteSize, &row.asset.Width, &row.asset.Height, &row.asset.VerificationStatus,
		&row.asset.StoragePath, &row.asset.CreatedAt, &row.asset.UpdatedAt,
		&row.sortName, &row.imageTypeTitle, &row.genWorkflowID, &row.genNodeID, &row.genNodeRunID, &row.genVisualID,
		&row.renditionJobID, &row.renditionSrcID, &row.renditionSpec, &row.renditionStat, &row.folderName,
	)
	return row, err
}

func projectGalleryAsset(row galleryRow) GalleryAssetResponse {
	item := GalleryAssetResponse{
		AssetResponse:  serializeAsset(row.asset),
		UserFolderName: row.folderName,
	}
	title := row.imageTypeTitle
	if title == nil {
		title = row.asset.ImageTypeKey
	}
	item.ImageTypeTitle = title
	if row.genWorkflowID != nil {
		item.Generation = &GalleryGenerationSummary{
			WorkflowID:            *row.genWorkflowID,
			NodeID:                deref(row.genNodeID),
			NodeRunID:             deref(row.genNodeRunID),
			VisualSystemVersionID: row.genVisualID,
		}
	}
	if row.renditionJobID != nil {
		spec := map[string]any{}
		if len(row.renditionSpec) > 0 {
			_ = json.Unmarshal(row.renditionSpec, &spec)
		}
		item.Rendition = &GalleryRenditionSummary{
			JobID:         *row.renditionJobID,
			SourceAssetID: deref(row.renditionSrcID),
			DeliverySpec:  spec,
			Status:        deref(row.renditionStat),
		}
	}
	return item
}

func galleryFilterHash(productID, kind string, key *string, query, sort string) (string, error) {
	var directoryKey any
	if key != nil {
		directoryKey = *key
	}
	raw, err := canonicalJSON(map[string]any{
		"product_id":     productID,
		"directory_kind": kind,
		"directory_key":  directoryKey,
		"query":          query,
		"sort":           sort,
	})
	if err != nil {
		return "", err
	}
	return sha256Hex(raw), nil
}

func encodeGalleryCursor(cursor galleryCursor) (string, error) {
	payload := map[string]any{
		"v":      galleryCursorVersion,
		"sort":   cursor.sort,
		"filter": cursor.filterHash,
		"key":    cursor.key,
		"id":     cursor.assetID,
	}
	if cursor.asOf != nil {
		payload["as_of"] = cursor.asOf.UTC().Format(time.RFC3339Nano)
	}
	raw, err := canonicalJSON(payload)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeGalleryCursor(value string) (galleryCursor, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" || len(normalized) > 4096 {
		return galleryCursor{}, apperr.Validation("图库分页 cursor 无效")
	}
	raw, err := base64.RawURLEncoding.DecodeString(normalized)
	if err != nil {
		padded := normalized
		if m := len(normalized) % 4; m != 0 {
			padded += strings.Repeat("=", 4-m)
		}
		raw, err = base64.URLEncoding.DecodeString(padded)
		if err != nil {
			return galleryCursor{}, apperr.Validation("图库分页 cursor 无效")
		}
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return galleryCursor{}, apperr.Validation("图库分页 cursor 无效")
	}
	allowed := map[string]struct{}{"v": {}, "sort": {}, "filter": {}, "key": {}, "id": {}, "as_of": {}}
	for field := range decoded {
		if _, ok := allowed[field]; !ok {
			return galleryCursor{}, apperr.Validation("图库分页 cursor 无效")
		}
	}
	for _, required := range []string{"v", "sort", "filter", "key", "id"} {
		if _, ok := decoded[required]; !ok {
			return galleryCursor{}, apperr.Validation("图库分页 cursor 无效")
		}
	}
	version, ok := asInt(decoded["v"])
	if !ok || version != galleryCursorVersion {
		return galleryCursor{}, apperr.Validation("图库分页 cursor 无效")
	}
	sort, _ := decoded["sort"].(string)
	filter, _ := decoded["filter"].(string)
	key, _ := decoded["key"].(string)
	id, _ := decoded["id"].(string)
	if !validGallerySort(sort) || filter == "" || len(filter) != 64 || key == "" || id == "" {
		return galleryCursor{}, apperr.Validation("图库分页 cursor 无效")
	}
	cursor := galleryCursor{sort: sort, filterHash: filter, key: key, assetID: id}
	if asOfRaw, ok := decoded["as_of"]; ok && asOfRaw != nil {
		asOfStr, _ := asOfRaw.(string)
		parsed, err := parseCursorTime(asOfStr)
		if err != nil {
			return galleryCursor{}, apperr.Validation("图库分页 cursor 无效")
		}
		cursor.asOf = &parsed
	}
	return cursor, nil
}

func asInt(v any) (int, bool) {
	switch typed := v.(type) {
	case float64:
		return int(typed), true
	case json.Number:
		n, err := typed.Int64()
		return int(n), err == nil
	case int:
		return typed, true
	default:
		return 0, false
	}
}
