package product

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

const assetJoinSelect = `a.id, a.product_id, a.media_object_id, a.origin_type, a.display_name, a.original_filename,
		       a.image_type_key, a.user_folder_id, a.parent_asset_id, a.source_image_session_asset_id,
		       a.source_library_asset_id, m.mime_type, m.byte_size, m.width, m.height, m.verification_status,
		       m.storage_path, a.created_at, a.updated_at`

type assetJoinRow struct {
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
}

type productListRow struct {
	ID                 string    `gorm:"column:id"`
	Name               string    `gorm:"column:name"`
	Category           *string   `gorm:"column:category"`
	Price              *string   `gorm:"column:price"`
	CoverImageAssetID  *string   `gorm:"column:cover_image_asset_id"`
	CoverImageFilename *string   `gorm:"column:cover_image_filename"`
	CreatedAt          time.Time `gorm:"column:created_at"`
	UpdatedAt          time.Time `gorm:"column:updated_at"`
}

func assetJoinQuery(tx *gorm.DB) *gorm.DB {
	return tx.Table("product_image_assets AS a").
		Select(assetJoinSelect).
		Joins("JOIN media_objects m ON m.id = a.media_object_id")
}

// imageAssetFromJoin 把 join 行的 Source*AssetID 与 int64 ByteSize 收成领域身份；StoragePath 仅供本包读字节。
func imageAssetFromJoin(row assetJoinRow) ImageAsset {
	return ImageAsset{
		ID:                      row.ID,
		ProductID:               row.ProductID,
		MediaObjectID:           row.MediaObjectID,
		OriginType:              row.OriginType,
		DisplayName:             row.DisplayName,
		OriginalFilename:        row.OriginalFilename,
		ImageTypeKey:            row.ImageTypeKey,
		UserFolderID:            row.UserFolderID,
		ParentAssetID:           row.ParentAssetID,
		SourceImageSessionAsset: row.SourceImageSessionAssetID,
		SourceLibraryAsset:      row.SourceLibraryAssetID,
		MIMEType:                row.MIMEType,
		ByteSize:                int64PtrToIntPtr(row.ByteSize),
		Width:                   row.Width,
		Height:                  row.Height,
		VerificationStatus:      row.VerificationStatus,
		StoragePath:             row.StoragePath,
		CreatedAt:               row.CreatedAt,
		UpdatedAt:               row.UpdatedAt,
	}
}

func int64PtrToIntPtr(v *int64) *int {
	if v == nil {
		return nil
	}
	n := int(*v)
	return &n
}

func productFromSchema(rec schema.Products) Product {
	row := Product{
		ID:                rec.ID,
		Name:              rec.Name,
		Category:          rec.Category,
		Price:             rec.Price,
		SourceNote:        rec.SourceNote,
		CoverImageAssetID: rec.CoverImageAssetID,
		IntakeVersion:     rec.IntakeSchemaVersion,
		FactSetVersionID:  rec.CurrentFactSetVersionID,
		CreatedAt:         rec.CreatedAt,
		UpdatedAt:         rec.UpdatedAt,
	}
	if rec.IntakeJSON != nil && *rec.IntakeJSON != "" {
		row.IntakeJSON = json.RawMessage(*rec.IntakeJSON)
	}
	return row
}

func conversationFromSchema(rec schema.AgentConversations) Conversation {
	return Conversation{
		ID:           rec.ID,
		ScopeType:    rec.ScopeType,
		SessionID:    rec.SessionID,
		ProductID:    rec.ProductID,
		HarnessRunID: rec.HarnessRunID,
		Status:       rec.Status,
		CreatedAt:    rec.CreatedAt,
		UpdatedAt:    rec.UpdatedAt,
	}
}

// insertProduct 写入商品行；封面与 fact 由调用方随后设置。
func insertProduct(ctx context.Context, tx *gorm.DB, name string, category, price, sourceNote *string) (Product, error) {
	id := clockid.New()
	now := time.Now().UTC()
	rec := schema.Products{
		ID:         id,
		Name:       name,
		Category:   category,
		Price:      price,
		SourceNote: sourceNote,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := tx.WithContext(ctx).Create(&rec).Error; err != nil {
		return Product{}, err
	}
	return productFromSchema(rec), nil
}

func assignCover(ctx context.Context, tx *gorm.DB, productID, assetID string) error {
	return tx.WithContext(ctx).Model(&schema.Products{}).Where("id = ?", productID).Updates(map[string]any{
		"cover_image_asset_id": assetID,
		"updated_at":           time.Now().UTC(),
	}).Error
}

func setIntake(ctx context.Context, tx *gorm.DB, productID string, payload []byte) error {
	updates := map[string]any{
		"intake_schema_version": 1,
		"updated_at":            time.Now().UTC(),
	}
	if payload == nil {
		updates["intake_json"] = nil
	} else {
		s := string(payload)
		updates["intake_json"] = s
	}
	return tx.WithContext(ctx).Model(&schema.Products{}).Where("id = ?", productID).Updates(updates).Error
}

func setCurrentFactSet(ctx context.Context, tx *gorm.DB, productID, factSetID string) error {
	return tx.WithContext(ctx).Model(&schema.Products{}).Where("id = ?", productID).Updates(map[string]any{
		"current_fact_set_version_id": factSetID,
		"updated_at":                  time.Now().UTC(),
	}).Error
}

func insertAsset(ctx context.Context, tx *gorm.DB, productID, mediaID, filename string) (ImageAsset, error) {
	return insertAssetOrigin(ctx, tx, productID, mediaID, filename, "upload", nil)
}

// AssetIdentityInput 写入一条商品图片身份。Origin 空则按 upload。
type AssetIdentityInput struct {
	ProductID                 string
	MediaID                   string
	Filename                  string
	Origin                    string  // 空则按 upload
	ImageTypeKey              *string // 图种闭集；nil 表示未分类
	ParentAssetID             *string
	SourceImageSessionAssetID *string
	DisplayName               string // 空则回落 Filename
}

func insertAssetOrigin(ctx context.Context, tx *gorm.DB, productID, mediaID, filename, origin string, imageTypeKey *string) (ImageAsset, error) {
	return InsertAssetIdentity(ctx, tx, AssetIdentityInput{
		ProductID:    productID,
		MediaID:      mediaID,
		Filename:     filename,
		Origin:       origin,
		ImageTypeKey: imageTypeKey,
	})
}

// InsertAssetIdentity 写入一条商品图片身份，可带 parent / 会话来源。
func InsertAssetIdentity(ctx context.Context, tx *gorm.DB, in AssetIdentityInput) (ImageAsset, error) {
	id := clockid.New()
	display := strings.TrimSpace(in.DisplayName)
	if display == "" {
		display = strings.TrimSpace(in.Filename)
	}
	if display == "" {
		display = "image"
	}
	if len([]rune(display)) > 255 {
		display = string([]rune(display)[:255])
	}
	original := strings.TrimSpace(in.Filename)
	if original == "" {
		original = display
	}
	if len([]rune(original)) > 255 {
		original = string([]rune(original)[:255])
	}
	now := time.Now().UTC()
	rec := schema.ProductImageAssets{
		ID:                        id,
		ProductID:                 in.ProductID,
		MediaObjectID:             in.MediaID,
		OriginType:                in.Origin,
		DisplayName:               display,
		OriginalFilename:          original,
		ImageTypeKey:              in.ImageTypeKey,
		ParentAssetID:             in.ParentAssetID,
		SourceImageSessionAssetID: in.SourceImageSessionAssetID,
		CreatedAt:                 now,
		UpdatedAt:                 now,
	}
	if err := tx.WithContext(ctx).Create(&rec).Error; err != nil {
		return ImageAsset{}, err
	}
	return ImageAsset{
		ID:                      rec.ID,
		ProductID:               rec.ProductID,
		MediaObjectID:           rec.MediaObjectID,
		OriginType:              rec.OriginType,
		DisplayName:             rec.DisplayName,
		OriginalFilename:        rec.OriginalFilename,
		ImageTypeKey:            rec.ImageTypeKey,
		ParentAssetID:           rec.ParentAssetID,
		SourceImageSessionAsset: rec.SourceImageSessionAssetID,
		VerificationStatus:      "verified",
		CreatedAt:               rec.CreatedAt,
		UpdatedAt:               rec.UpdatedAt,
	}, nil
}

func loadProduct(ctx context.Context, tx *gorm.DB, id string) (Product, error) {
	return scanProduct(ctx, tx, id, false)
}

func loadProductForUpdate(ctx context.Context, tx *gorm.DB, id string) (Product, error) {
	return scanProduct(ctx, tx, id, true)
}

func scanProduct(ctx context.Context, tx *gorm.DB, id string, forUpdate bool) (Product, error) {
	q := tx.WithContext(ctx).Where("id = ?", id)
	if forUpdate {
		q = q.Clauses(pfdb.ForUpdate())
	}
	var rec schema.Products
	err := q.Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Product{}, apperr.NotFound("商品不存在")
	}
	if err != nil {
		return Product{}, err
	}
	return productFromSchema(rec), nil
}

func loadAssetsByIDs(ctx context.Context, tx *gorm.DB, productID string, ids []string) ([]ImageAsset, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []assetJoinRow
	err := assetJoinQuery(tx.WithContext(ctx)).
		Where("a.product_id = ? AND a.id IN ?", productID, ids).
		Order("a.created_at ASC, a.id ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]ImageAsset, 0, len(rows))
	for _, row := range rows {
		out = append(out, imageAssetFromJoin(row))
	}
	return out, nil
}

func loadAsset(ctx context.Context, q *gorm.DB, assetID string) (ImageAsset, error) {
	var row assetJoinRow
	err := assetJoinQuery(q.WithContext(ctx)).Where("a.id = ?", assetID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ImageAsset{}, apperr.NotFound("商品图片不存在")
	}
	if err != nil {
		return ImageAsset{}, err
	}
	return imageAssetFromJoin(row), nil
}

func loadAssetForProduct(ctx context.Context, q *gorm.DB, productID, assetID string) (ImageAsset, error) {
	var row assetJoinRow
	err := assetJoinQuery(q.WithContext(ctx)).Where("a.product_id = ? AND a.id = ?", productID, assetID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ImageAsset{}, apperr.Validation("参考图不属于该商品")
	}
	if err != nil {
		return ImageAsset{}, err
	}
	return imageAssetFromJoin(row), nil
}

// LoadAssetRow 供 delivery / localedit 读取商品图片身份。
func LoadAssetRow(ctx context.Context, q *gorm.DB, assetID string) (ImageAsset, error) {
	return loadAsset(ctx, q, assetID)
}

// listProducts 封面 URL 由 ProductImageAsset id 推导；未知 sort 回落到 updated_desc。
func listProducts(ctx context.Context, tx *gorm.DB, page, pageSize int, q, sort string) ([]Summary, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	order := "p.updated_at DESC, p.id DESC"
	switch sort {
	case "created_desc":
		order = "p.created_at DESC, p.id DESC"
	case "name_asc":
		order = "LOWER(p.name) ASC, p.name ASC, p.id ASC"
	}
	base := func() *gorm.DB {
		db := tx.WithContext(ctx).Table("products AS p")
		if strings.TrimSpace(q) != "" {
			db = db.Where("p.name ILIKE ?", "%"+escapeLike(strings.TrimSpace(q))+"%")
		}
		return db
	}
	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	var rows []productListRow
	err := base().
		Select("p.id, p.name, p.category, p.price, p.cover_image_asset_id, a.original_filename AS cover_image_filename, p.created_at, p.updated_at").
		Joins("LEFT JOIN product_image_assets a ON a.id = p.cover_image_asset_id").
		Order(order).
		Offset(offset).
		Limit(pageSize).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	items := make([]Summary, 0, len(rows))
	for _, row := range rows {
		item := Summary{
			ID:                 row.ID,
			Name:               row.Name,
			Category:           row.Category,
			Price:              row.Price,
			CoverImageAssetID:  row.CoverImageAssetID,
			CoverImageFilename: row.CoverImageFilename,
			CreatedAt:          row.CreatedAt,
			UpdatedAt:          row.UpdatedAt,
		}
		if item.CoverImageAssetID != nil {
			download, preview, thumb := assetURLs(*item.CoverImageAssetID)
			item.CoverImageDownloadURL = &download
			item.CoverImagePreviewURL = &preview
			item.CoverImageThumbnailURL = &thumb
		}
		items = append(items, item)
	}
	return items, int(total), nil
}

// insertFactSet 追加不可变 fact 版本；version 取当前 MAX+1，不覆盖旧行。
func insertFactSet(ctx context.Context, tx *gorm.DB, productID string, facts []map[string]any) (string, int, error) {
	payload := map[string]any{
		"schema_version":    1,
		"source_product_id": productID,
		"facts":             facts,
	}
	raw, err := canonicalJSON(payload)
	if err != nil {
		return "", 0, err
	}
	sum := sha256Hex(raw)
	var version int
	err = tx.WithContext(ctx).Model(&schema.ProductFactSetVersions{}).
		Where("product_id = ?", productID).
		Select("COALESCE(MAX(version), 0)").
		Scan(&version).Error
	if err != nil {
		return "", 0, err
	}
	version++
	id := clockid.New()
	rec := schema.ProductFactSetVersions{
		ID:          id,
		ProductID:   productID,
		Version:     version,
		PayloadJSON: string(raw),
		PayloadHash: sum,
		CreatedAt:   time.Now().UTC(),
	}
	if err := tx.WithContext(ctx).Create(&rec).Error; err != nil {
		return "", 0, err
	}
	if err := setCurrentFactSet(ctx, tx, productID, id); err != nil {
		return "", 0, err
	}
	return id, version, nil
}

func loadConversationByKey(ctx context.Context, tx *gorm.DB, key string) (Conversation, error) {
	var rec schema.AgentConversations
	err := tx.WithContext(ctx).Where("creation_idempotency_key = ?", key).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Conversation{}, err
	}
	if err != nil {
		return Conversation{}, err
	}
	return conversationFromSchema(rec), nil
}

func loadConversationByID(ctx context.Context, tx *gorm.DB, id string) (Conversation, error) {
	row, _, _, err := scanConversation(ctx, tx, id, false)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Conversation{}, apperr.NotFound("Agent 商品工作空间不存在")
	}
	return row, err
}

func loadConversationIntakeForUpdate(ctx context.Context, tx *gorm.DB, id string) (Conversation, *string, *string, error) {
	row, key, hash, err := scanConversation(ctx, tx, id, true)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Conversation{}, nil, nil, apperr.NotFound("Agent 商品工作空间不存在")
	}
	return row, key, hash, err
}

func scanConversation(ctx context.Context, tx *gorm.DB, id string, forUpdate bool) (Conversation, *string, *string, error) {
	q := tx.WithContext(ctx).Where("id = ?", id)
	if forUpdate {
		q = q.Clauses(pfdb.ForUpdate())
	}
	var rec schema.AgentConversations
	err := q.Take(&rec).Error
	if err != nil {
		return Conversation{}, nil, nil, err
	}
	return conversationFromSchema(rec), rec.IntakeIdempotencyKey, rec.IntakeRequestHash, nil
}

func setConversationIntake(ctx context.Context, tx *gorm.DB, conversationID, key, requestHash string) error {
	return tx.WithContext(ctx).Model(&schema.AgentConversations{}).Where("id = ?", conversationID).Updates(map[string]any{
		"intake_idempotency_key": key,
		"intake_request_hash":    requestHash,
		"status":                 "collecting",
		"updated_at":             time.Now().UTC(),
	}).Error
}

func setSourceNote(ctx context.Context, tx *gorm.DB, productID string, sourceNote *string) error {
	return tx.WithContext(ctx).Model(&schema.Products{}).Where("id = ?", productID).Updates(map[string]any{
		"source_note": sourceNote,
		"updated_at":  time.Now().UTC(),
	}).Error
}

func conversationRequestHash(ctx context.Context, tx *gorm.DB, id string) (string, error) {
	var rec schema.AgentConversations
	err := tx.WithContext(ctx).Select("creation_request_hash").Where("id = ?", id).Take(&rec).Error
	if err != nil {
		return "", err
	}
	if rec.CreationRequestHash == nil {
		return "", nil
	}
	return *rec.CreationRequestHash, nil
}

func escapeLike(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(q)
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
