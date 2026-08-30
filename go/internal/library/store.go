package library

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

// Service 拥有全局素材库的保存、组织、收录与工作流子图库关联。
type Service struct {
	DB    *gorm.DB
	Media media.Store
	Now   func() time.Time
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

const libraryAssetSelect = `a.id, a.media_object_id, a.source_type, a.source_id, a.display_name, a.folder_id,
	       f.name AS folder_name, a.original_filename, a.revision, a.is_archived, a.archived_at, a.provenance_hash,
	       m.mime_type, m.byte_size, m.width, m.height, m.verification_status, m.storage_path,
	       a.created_at, a.updated_at, a.provenance_json, a.source_product_asset_id, a.source_image_session_asset_id,
	       m.sha256, src.media_object_id AS source_product_media_id, src.origin_type AS source_product_origin,
	       src.product_id AS source_product_product_id, sess.media_object_id AS source_session_media_id`

type libraryAssetScan struct {
	ID                     string     `gorm:"column:id"`
	MediaObjectID          string     `gorm:"column:media_object_id"`
	SourceType             string     `gorm:"column:source_type"`
	SourceID               string     `gorm:"column:source_id"`
	DisplayName            string     `gorm:"column:display_name"`
	FolderID               *string    `gorm:"column:folder_id"`
	FolderName             *string    `gorm:"column:folder_name"`
	OriginalFilename       string     `gorm:"column:original_filename"`
	Revision               int        `gorm:"column:revision"`
	IsArchived             bool       `gorm:"column:is_archived"`
	ArchivedAt             *time.Time `gorm:"column:archived_at"`
	ProvenanceHash         string     `gorm:"column:provenance_hash"`
	MIMEType               *string    `gorm:"column:mime_type"`
	ByteSize               *int64     `gorm:"column:byte_size"`
	Width                  *int       `gorm:"column:width"`
	Height                 *int       `gorm:"column:height"`
	VerificationStatus     *string    `gorm:"column:verification_status"`
	StoragePath            *string    `gorm:"column:storage_path"`
	CreatedAt              time.Time  `gorm:"column:created_at"`
	UpdatedAt              time.Time  `gorm:"column:updated_at"`
	ProvenanceJSON         string     `gorm:"column:provenance_json"`
	SourceProductAssetID   *string    `gorm:"column:source_product_asset_id"`
	SourceSessionAssetID   *string    `gorm:"column:source_image_session_asset_id"`
	SHA256                 *string    `gorm:"column:sha256"`
	SourceProductMediaID   *string    `gorm:"column:source_product_media_id"`
	SourceProductOrigin    *string    `gorm:"column:source_product_origin"`
	SourceProductProductID *string    `gorm:"column:source_product_product_id"`
	SourceSessionMediaID   *string    `gorm:"column:source_session_media_id"`
}

func libraryAssetQuery(tx *gorm.DB) *gorm.DB {
	return tx.Table("media_library_assets AS a").
		Select(libraryAssetSelect).
		Joins("LEFT JOIN media_objects m ON m.id = a.media_object_id").
		Joins("LEFT JOIN media_library_folders f ON f.id = a.folder_id").
		Joins("LEFT JOIN product_image_assets src ON src.id = a.source_product_asset_id").
		Joins("LEFT JOIN image_session_assets sess ON sess.id = a.source_image_session_asset_id")
}

func assetFromScan(row libraryAssetScan) Asset {
	a := Asset{
		ID:                     row.ID,
		MediaObjectID:          row.MediaObjectID,
		SourceType:             row.SourceType,
		SourceID:               row.SourceID,
		DisplayName:            row.DisplayName,
		FolderID:               row.FolderID,
		FolderName:             row.FolderName,
		OriginalFilename:       row.OriginalFilename,
		Revision:               row.Revision,
		IsArchived:             row.IsArchived,
		ArchivedAt:             row.ArchivedAt,
		ProvenanceHash:         row.ProvenanceHash,
		CreatedAt:              row.CreatedAt,
		UpdatedAt:              row.UpdatedAt,
		SourceProductAssetID:   row.SourceProductAssetID,
		SourceSessionAssetID:   row.SourceSessionAssetID,
		SourceProductMediaID:   row.SourceProductMediaID,
		SourceProductOrigin:    row.SourceProductOrigin,
		SourceProductProductID: row.SourceProductProductID,
		SourceSessionMediaID:   row.SourceSessionMediaID,
	}
	if row.MIMEType != nil {
		a.MIMEType = *row.MIMEType
	}
	if row.ByteSize != nil {
		n := int(*row.ByteSize)
		a.ByteSize = &n
	}
	a.Width, a.Height = row.Width, row.Height
	if row.VerificationStatus != nil {
		a.VerificationStatus = *row.VerificationStatus
	}
	if row.StoragePath != nil {
		a.StoragePath = *row.StoragePath
	}
	if row.SHA256 != nil {
		a.SHA256 = *row.SHA256
	}
	if row.ProvenanceJSON != "" {
		_ = json.Unmarshal([]byte(row.ProvenanceJSON), &a.ProvenanceJSON)
	}
	return a
}

func (s Service) loadAsset(ctx context.Context, q *gorm.DB, id string) (Asset, error) {
	items, err := loadAssets(ctx, q, libraryAssetQuery(q.WithContext(ctx)).Where("a.id = ?", id))
	if err != nil {
		return Asset{}, err
	}
	if len(items) == 0 {
		return Asset{}, apperr.NotFound("素材库资产不存在")
	}
	return items[0], nil
}

func loadAssets(ctx context.Context, tx *gorm.DB, q *gorm.DB) ([]Asset, error) {
	var rows []libraryAssetScan
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]Asset, 0, len(rows))
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		items = append(items, assetFromScan(row))
		ids = append(ids, row.ID)
	}
	tags, err := loadTags(ctx, tx, ids)
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

func loadTags(ctx context.Context, tx *gorm.DB, assetIDs []string) (map[string][]Tag, error) {
	out := map[string][]Tag{}
	if len(assetIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		AssetID string `gorm:"column:asset_id"`
		ID      string `gorm:"column:id"`
		Name    string `gorm:"column:name"`
	}
	err := tx.WithContext(ctx).
		Table("media_library_asset_tags AS at").
		Select("at.asset_id, t.id, t.name").
		Joins("JOIN media_library_tags t ON t.id = at.tag_id").
		Where("at.asset_id IN ?", assetIDs).
		Order("t.name, t.id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.AssetID] = append(out[row.AssetID], Tag{ID: row.ID, Name: row.Name})
	}
	return out, nil
}

func insertLibraryAsset(ctx context.Context, tx *gorm.DB, in Asset, p Provenance) (string, error) {
	id := clockid.New()
	hash, err := provenanceHash(p)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(storeProvenance(p))
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	rec := schema.MediaLibraryAssets{
		ID:                        id,
		MediaObjectID:             in.MediaObjectID,
		SourceType:                in.SourceType,
		SourceID:                  in.SourceID,
		SourceImageSessionAssetID: in.SourceSessionAssetID,
		SourceProductAssetID:      in.SourceProductAssetID,
		ProvenanceJSON:            string(raw),
		ProvenanceHash:            hash,
		Revision:                  1,
		DisplayName:               in.DisplayName,
		OriginalFilename:          in.OriginalFilename,
		FolderID:                  in.FolderID,
		IsArchived:                false,
		CreatedAt:                 now,
		UpdatedAt:                 now,
	}
	if err := tx.WithContext(ctx).Create(&rec).Error; err != nil {
		return "", err
	}
	return id, nil
}

func findBySource(ctx context.Context, tx *gorm.DB, sourceType, sourceID string) (string, bool, error) {
	var rec schema.MediaLibraryAssets
	err := tx.WithContext(ctx).Select("id").Where("source_type = ? AND source_id = ?", sourceType, sourceID).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return rec.ID, true, nil
}

func lockFolder(ctx context.Context, tx *gorm.DB, folderID string) (Folder, error) {
	var rec schema.MediaLibraryFolders
	err := tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Select("id, name").Where("id = ?", folderID).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Folder{}, apperr.NotFound("素材库文件夹不存在")
	}
	if err != nil {
		return Folder{}, err
	}
	return Folder{ID: rec.ID, Name: rec.Name}, nil
}

func lockTag(ctx context.Context, tx *gorm.DB, tagID string) (Tag, error) {
	var rec schema.MediaLibraryTags
	err := tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Select("id, name").Where("id = ?", tagID).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Tag{}, apperr.NotFound("素材库标签不存在")
	}
	if err != nil {
		return Tag{}, err
	}
	return Tag{ID: rec.ID, Name: rec.Name}, nil
}

func getFolder(ctx context.Context, tx *gorm.DB, folderID string) (Folder, error) {
	var rec schema.MediaLibraryFolders
	err := tx.WithContext(ctx).Select("id, name").Where("id = ?", folderID).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Folder{}, apperr.NotFound("文件夹不存在")
	}
	if err != nil {
		return Folder{}, err
	}
	return Folder{ID: rec.ID, Name: rec.Name}, nil
}

func lockLibraryAssets(ctx context.Context, tx *gorm.DB, ids []string) ([]Asset, error) {
	if len(ids) == 0 {
		return []Asset{}, nil
	}
	var locked []schema.MediaLibraryAssets
	err := tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).
		Select("id").
		Where("id IN ?", ids).
		Order("id").
		Find(&locked).Error
	if err != nil {
		return nil, err
	}
	if len(locked) != len(ids) {
		return nil, apperr.NotFound("素材库资产不存在")
	}
	return loadAssets(ctx, tx, libraryAssetQuery(tx).Where("a.id IN ?", ids))
}

func reloadInOrder(ctx context.Context, tx *gorm.DB, ids []string) ([]Asset, error) {
	if len(ids) == 0 {
		return []Asset{}, nil
	}
	items, err := loadAssets(ctx, tx, libraryAssetQuery(tx).Where("a.id IN ?", ids))
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

type sessionAssetScan struct {
	ID                 string     `gorm:"column:id"`
	Kind               string     `gorm:"column:kind"`
	OriginalFilename   string     `gorm:"column:original_filename"`
	CreatedAt          time.Time  `gorm:"column:created_at"`
	MediaID            string     `gorm:"column:media_id"`
	StoragePath        string     `gorm:"column:storage_path"`
	MIMEType           string     `gorm:"column:mime_type"`
	ByteSize           *int64     `gorm:"column:byte_size"`
	Width              *int       `gorm:"column:width"`
	Height             *int       `gorm:"column:height"`
	SHA256             *string    `gorm:"column:sha256"`
	VerificationStatus string     `gorm:"column:verification_status"`
	MediaCreatedAt     time.Time  `gorm:"column:media_created_at"`
	VerifiedAt         *time.Time `gorm:"column:verified_at"`
}

func loadSessionAsset(ctx context.Context, tx *gorm.DB, id string) (sessionRow, error) {
	var row sessionAssetScan
	err := tx.WithContext(ctx).Table("image_session_assets AS a").
		Select(`a.id, a.kind::text AS kind, a.original_filename, a.created_at,
		       m.id AS media_id, m.storage_path, m.mime_type, m.byte_size, m.width, m.height, m.sha256,
		       m.verification_status, m.created_at AS media_created_at, m.verified_at`).
		Joins("JOIN media_objects m ON m.id = a.media_object_id").
		Clauses(pfdb.ForUpdateOf("a")).
		Where("a.id = ?", id).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return sessionRow{}, apperr.NotFound("会话图片不存在")
	}
	if err != nil {
		return sessionRow{}, err
	}
	obj := media.Object{
		ID:                 row.MediaID,
		StoragePath:        row.StoragePath,
		MIMEType:           row.MIMEType,
		VerificationStatus: row.VerificationStatus,
		CreatedAt:          row.MediaCreatedAt,
	}
	if row.ByteSize != nil {
		obj.ByteSize = int(*row.ByteSize)
	}
	if row.Width != nil {
		obj.Width = *row.Width
	}
	if row.Height != nil {
		obj.Height = *row.Height
	}
	if row.SHA256 != nil {
		obj.SHA256 = *row.SHA256
	}
	if row.VerifiedAt != nil {
		obj.VerifiedAt = *row.VerifiedAt
	}
	return sessionRow{
		ID:               row.ID,
		Kind:             row.Kind,
		OriginalFilename: row.OriginalFilename,
		CreatedAt:        row.CreatedAt,
		Media:            obj,
	}, nil
}

func requireWorkflow(ctx context.Context, tx *gorm.DB, productID, workflowID string, forUpdate bool) error {
	q := tx.WithContext(ctx).Select("id").Where("id = ? AND product_id = ?", workflowID, productID)
	if forUpdate {
		q = q.Clauses(pfdb.ForUpdate())
	}
	var rec schema.WorkflowGraphs
	err := q.Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apperr.NotFound("工作流不存在")
	}
	return err
}
