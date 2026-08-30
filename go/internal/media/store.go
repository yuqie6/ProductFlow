package media

import (
	"context"
	"errors"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"gorm.io/gorm"
)

const StatusVerified = "verified"
const StatusMissing = "missing"

type Object struct {
	ID                 string
	StoragePath        string
	MIMEType           string
	ByteSize           int
	Width              int
	Height             int
	SHA256             string
	VerificationStatus string
	CreatedAt          time.Time
	VerifiedAt         time.Time
}

type Store struct {
	Files storage.Local
}

func objectFromSchema(rec schema.MediaObjects) Object {
	obj := Object{
		ID:                 rec.ID,
		StoragePath:        rec.StoragePath,
		MIMEType:           rec.MIMEType,
		VerificationStatus: rec.VerificationStatus,
		CreatedAt:          rec.CreatedAt,
	}
	if rec.ByteSize != nil {
		obj.ByteSize = int(*rec.ByteSize)
	}
	if rec.Width != nil {
		obj.Width = *rec.Width
	}
	if rec.Height != nil {
		obj.Height = *rec.Height
	}
	if rec.SHA256 != nil {
		obj.SHA256 = *rec.SHA256
	}
	if rec.VerifiedAt != nil {
		obj.VerifiedAt = *rec.VerifiedAt
	}
	return obj
}

func (s Store) Stage(ctx context.Context, tx *gorm.DB, content []byte, expectedMIME string, compensation *storage.Compensation) (Object, error) {
	verified, err := Inspect(content, expectedMIME)
	if err != nil {
		return Object{}, err
	}
	id := clockid.New()
	path, err := s.Files.WriteMedia(id, ExtensionForMIME(verified.MIMEType), content, compensation)
	if err != nil {
		return Object{}, apperr.Internal("写入媒体文件失败")
	}
	now := time.Now().UTC()
	byteSize := int64(verified.ByteSize)
	width := verified.Width
	height := verified.Height
	sha := verified.SHA256
	rec := schema.MediaObjects{
		ID:                 id,
		StoragePath:        path,
		MIMEType:           verified.MIMEType,
		ByteSize:           &byteSize,
		Width:              &width,
		Height:             &height,
		SHA256:             &sha,
		VerificationStatus: StatusVerified,
		CreatedAt:          now,
		VerifiedAt:         &now,
	}
	if err := tx.WithContext(ctx).Create(&rec).Error; err != nil {
		return Object{}, err
	}
	return objectFromSchema(rec), nil
}

func (s Store) Get(ctx context.Context, q *gorm.DB, id string) (Object, error) {
	var rec schema.MediaObjects
	err := q.WithContext(ctx).Where("id = ?", id).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Object{}, apperr.NotFound("媒体对象不存在")
	}
	if err != nil {
		return Object{}, err
	}
	return objectFromSchema(rec), nil
}

func (s Store) MarkMissingByStoragePath(ctx context.Context, db *gorm.DB, storagePath string) {
	if db == nil || storagePath == "" {
		return
	}
	_ = db.WithContext(ctx).Model(&schema.MediaObjects{}).
		Where("storage_path = ? AND verification_status <> ?", storagePath, StatusMissing).
		Updates(map[string]any{"verification_status": StatusMissing}).Error
}
