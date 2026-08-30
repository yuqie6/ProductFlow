package product

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
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

type galleryScanRow struct {
	ID                        string    `gorm:"column:id"`
	ProductID                 string    `gorm:"column:product_id"`
	MediaObjectID             string    `gorm:"column:media_object_id"`
	OriginType                string    `gorm:"column:origin_type"`
	DisplayName               string    `gorm:"column:display_name"`
	OriginalFilename          string    `gorm:"column:original_filename"`
	ImageTypeKey              *string   `gorm:"column:image_type_key"`
	UserFolderID              *string   `gorm:"column:user_folder_id"`
	ParentAssetID             *string   `gorm:"column:parent_asset_id"`
	SourceImageSessionAssetID *string   `gorm:"column:source_image_session_asset_id"`
	SourceLibraryAssetID      *string   `gorm:"column:source_library_asset_id"`
	MIMEType                  string    `gorm:"column:mime_type"`
	ByteSize                  *int64    `gorm:"column:byte_size"`
	Width                     *int      `gorm:"column:width"`
	Height                    *int      `gorm:"column:height"`
	VerificationStatus        string    `gorm:"column:verification_status"`
	StoragePath               string    `gorm:"column:storage_path"`
	CreatedAt                 time.Time `gorm:"column:created_at"`
	UpdatedAt                 time.Time `gorm:"column:updated_at"`
	SortName                  string    `gorm:"column:gallery_sort_name"`
	ImageTypeTitle            *string   `gorm:"column:image_type_title"`
	GenWorkflowID             *string   `gorm:"column:gen_workflow_id"`
	GenNodeID                 *string   `gorm:"column:gen_node_id"`
	GenNodeRunID              *string   `gorm:"column:gen_node_run_id"`
	GenVisualID               *string   `gorm:"column:gen_visual_version_id"`
	RenditionJobID            *string   `gorm:"column:rendition_job_id"`
	RenditionSrcID            *string   `gorm:"column:source_asset_id"`
	RenditionSpec             *string   `gorm:"column:spec_json"`
	RenditionStat             *string   `gorm:"column:status"`
	FolderName                *string   `gorm:"column:user_folder_name"`
}

const gallerySelectColumns = `
		a.id, a.product_id, a.media_object_id, a.origin_type, a.display_name, a.original_filename,
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
		f.name AS user_folder_name`

func galleryQuery(tx *gorm.DB) *gorm.DB {
	return tx.Table("product_image_assets AS a").
		Select(gallerySelectColumns).
		Joins("JOIN media_objects m ON m.id = a.media_object_id").
		Joins("LEFT JOIN product_asset_folders f ON f.id = a.user_folder_id").
		Joins("LEFT JOIN workflow_graph_artifacts direct_gen ON direct_gen.product_image_asset_id = a.id AND direct_gen.artifact_type = 'image'").
		Joins("LEFT JOIN workflow_graph_nodes direct_node ON direct_node.id = direct_gen.node_id").
		Joins("LEFT JOIN delivery_rendition_jobs rend ON rend.result_asset_id = a.id").
		Joins("LEFT JOIN product_image_assets rend_src ON rend_src.id = rend.source_asset_id").
		Joins("LEFT JOIN workflow_graph_artifacts source_gen ON source_gen.product_image_asset_id = rend_src.id AND source_gen.artifact_type = 'image'").
		Joins("LEFT JOIN workflow_graph_nodes source_node ON source_node.id = source_gen.node_id")
}

func galleryRowFromScan(row galleryScanRow) galleryRow {
	out := galleryRow{
		asset: imageAssetFromJoin(assetJoinRow{
			ID:                        row.ID,
			ProductID:                 row.ProductID,
			MediaObjectID:             row.MediaObjectID,
			OriginType:                row.OriginType,
			DisplayName:               row.DisplayName,
			OriginalFilename:          row.OriginalFilename,
			ImageTypeKey:              row.ImageTypeKey,
			UserFolderID:              row.UserFolderID,
			ParentAssetID:             row.ParentAssetID,
			SourceImageSessionAssetID: row.SourceImageSessionAssetID,
			SourceLibraryAssetID:      row.SourceLibraryAssetID,
			MIMEType:                  row.MIMEType,
			ByteSize:                  row.ByteSize,
			Width:                     row.Width,
			Height:                    row.Height,
			VerificationStatus:        row.VerificationStatus,
			StoragePath:               row.StoragePath,
			CreatedAt:                 row.CreatedAt,
			UpdatedAt:                 row.UpdatedAt,
		}),
		sortName:       row.SortName,
		imageTypeTitle: row.ImageTypeTitle,
		genWorkflowID:  row.GenWorkflowID,
		genNodeID:      row.GenNodeID,
		genNodeRunID:   row.GenNodeRunID,
		genVisualID:    row.GenVisualID,
		renditionJobID: row.RenditionJobID,
		renditionSrcID: row.RenditionSrcID,
		renditionStat:  row.RenditionStat,
		folderName:     row.FolderName,
	}
	if row.RenditionSpec != nil {
		out.renditionSpec = []byte(*row.RenditionSpec)
	}
	return out
}

// GalleryBootstrap 返回系统目录计数与用户文件夹；cover_image_asset_id 只是展示元数据。
func (s Service) GalleryBootstrap(ctx context.Context, productID string) (GalleryBootstrap, error) {
	var out GalleryBootstrap
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		product, err := loadProduct(ctx, pgxTx, productID)
		if err != nil {
			return err
		}
		asOf := s.now()
		var allCount, unorganized, recent int64
		if err := pgxTx.WithContext(ctx).Model(&schema.ProductImageAssets{}).Where("product_id = ?", productID).Count(&allCount).Error; err != nil {
			return err
		}
		if err := pgxTx.WithContext(ctx).Model(&schema.ProductImageAssets{}).
			Where("product_id = ? AND user_folder_id IS NULL", productID).Count(&unorganized).Error; err != nil {
			return err
		}
		if err := pgxTx.WithContext(ctx).Model(&schema.ProductImageAssets{}).
			Where("product_id = ? AND origin_type IN ? AND created_at >= ?", productID, generatedOrigins, asOf.Add(-galleryRecentDays*24*time.Hour)).
			Count(&recent).Error; err != nil {
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
				{Kind: "all", Count: int(allCount)},
				{Kind: "recent_generated", Count: int(recent)},
				{Kind: "uploads", Count: originCounts["upload"]},
				{Kind: "generated", Count: generated},
				{Kind: "unorganized", Count: int(unorganized)},
			},
			ImageTypes:       imageTypes,
			Origins:          origins,
			UserFolders:      folders,
			UnorganizedCount: int(unorganized),
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
		q := applyGalleryListFilters(galleryQuery(pgxTx.WithContext(ctx)), productID, kind, key, query, sort, cursor, asOf, limit+1)
		var scanned []galleryScanRow
		if err := q.Scan(&scanned).Error; err != nil {
			return err
		}
		collected := make([]galleryRow, 0, len(scanned))
		for _, row := range scanned {
			collected = append(collected, galleryRowFromScan(row))
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
		row, err := loadGalleryRow(ctx, pgxTx, productID, assetID)
		if err != nil {
			return err
		}
		out = projectGalleryAsset(row)
		return nil
	})
	return out, err
}

func loadGalleryRow(ctx context.Context, tx *gorm.DB, productID, assetID string) (galleryRow, error) {
	var row galleryScanRow
	err := galleryQuery(tx.WithContext(ctx)).
		Where("a.product_id = ? AND a.id = ?", productID, assetID).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return galleryRow{}, apperr.NotFound("商品图片不存在")
	}
	if err != nil {
		return galleryRow{}, err
	}
	return galleryRowFromScan(row), nil
}

func loadOriginCounts(ctx context.Context, tx *gorm.DB, productID string) (map[string]int, error) {
	var rows []struct {
		OriginType string `gorm:"column:origin_type"`
		Count      int64  `gorm:"column:count"`
	}
	err := tx.WithContext(ctx).Model(&schema.ProductImageAssets{}).
		Select("origin_type, COUNT(*) AS count").
		Where("product_id = ?", productID).
		Group("origin_type").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for _, row := range rows {
		out[row.OriginType] = int(row.Count)
	}
	return out, nil
}

func loadImageTypeCounts(ctx context.Context, tx *gorm.DB, productID string) ([]GalleryImageType, error) {
	var rows []struct {
		Key   *string `gorm:"column:image_type_key"`
		Count int64   `gorm:"column:count"`
	}
	err := tx.WithContext(ctx).Model(&schema.ProductImageAssets{}).
		Select("image_type_key, COUNT(*) AS count").
		Where("product_id = ?", productID).
		Group("image_type_key").
		Order("image_type_key NULLS FIRST").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := []GalleryImageType{}
	for _, row := range rows {
		item := GalleryImageType{Count: int(row.Count), ImageTypeKey: row.Key}
		if row.Key == nil {
			item.DirectoryKey = galleryUnclassifiedTypeKey
			item.Title = "未分类"
		} else {
			item.DirectoryKey = *row.Key
			item.Title = *row.Key
		}
		out = append(out, item)
	}
	return out, nil
}

func loadFolderCounts(ctx context.Context, tx *gorm.DB, productID string) ([]GalleryFolder, error) {
	var rows []struct {
		ID        string `gorm:"column:id"`
		Name      string `gorm:"column:name"`
		SortOrder int    `gorm:"column:sort_order"`
		Count     int64  `gorm:"column:count"`
	}
	err := tx.WithContext(ctx).Table("product_asset_folders AS f").
		Select("f.id, f.name, f.sort_order, COUNT(a.id) AS count").
		Joins("LEFT JOIN product_image_assets a ON a.user_folder_id = f.id").
		Where("f.product_id = ?", productID).
		Group("f.id").
		Order("f.sort_order, f.name, f.id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]GalleryFolder, 0, len(rows))
	for _, row := range rows {
		out = append(out, GalleryFolder{ID: row.ID, Name: row.Name, SortOrder: row.SortOrder, Count: int(row.Count)})
	}
	return out, nil
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
		var rec schema.ProductAssetFolders
		err := tx.WithContext(ctx).Select("id").Where("id = ? AND product_id = ?", normalizedKey, productID).Take(&rec).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
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

func applyGalleryListFilters(q *gorm.DB, productID, kind string, key *string, query, sort string, cursor *galleryCursor, asOf *time.Time, limit int) *gorm.DB {
	q = q.Where("a.product_id = ?", productID)
	switch kind {
	case "recent_generated":
		q = q.Where("a.origin_type IN ? AND a.created_at >= ?", generatedOrigins, asOf.Add(-galleryRecentDays*24*time.Hour))
	case "uploads":
		q = q.Where("a.origin_type = ?", "upload")
	case "generated":
		q = q.Where("a.origin_type IN ?", generatedOrigins)
	case "image_type":
		if key != nil && *key == galleryUnclassifiedTypeKey {
			q = q.Where("a.image_type_key IS NULL")
		} else {
			q = q.Where("a.image_type_key = ?", *key)
		}
	case "source":
		q = q.Where("a.origin_type = ?", *key)
	case "unorganized":
		q = q.Where("a.user_folder_id IS NULL")
	case "user_folder":
		q = q.Where("a.user_folder_id = ?", *key)
	}
	if query != "" {
		pattern := "%" + escapeLike(query) + "%"
		q = q.Where("(a.display_name ILIKE ? ESCAPE '\\' OR a.original_filename ILIKE ? ESCAPE '\\')", pattern, pattern)
	}
	if cursor != nil {
		switch sort {
		case "created_asc":
			parsed, _ := parseCursorTime(cursor.key)
			q = q.Where("(a.created_at > ? OR (a.created_at = ? AND a.id > ?))", parsed, parsed, cursor.assetID)
		case "created_desc":
			parsed, _ := parseCursorTime(cursor.key)
			q = q.Where("(a.created_at < ? OR (a.created_at = ? AND a.id < ?))", parsed, parsed, cursor.assetID)
		case "name_asc":
			q = q.Where("(LOWER(a.display_name) > ? OR (LOWER(a.display_name) = ? AND a.id > ?))", cursor.key, cursor.key, cursor.assetID)
		default:
			q = q.Where("(LOWER(a.display_name) < ? OR (LOWER(a.display_name) = ? AND a.id < ?))", cursor.key, cursor.key, cursor.assetID)
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
	return q.Order(order).Limit(limit)
}

func parseCursorTime(raw string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.999999999-07:00", "2006-01-02T15:04:05-07:00"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, apperr.Validation("图库分页 cursor 无效")
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
