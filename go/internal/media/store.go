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

// StatusVerified 表示字节已解码并写入 media_objects。
const StatusVerified = "verified"

// StatusMissing 表示磁盘上的原图已找不到（media_objects.verification_status）。
// 与 StatusVerified 互斥。列表仍可展示身份；download 应 404，不要当「未归档」。
const StatusMissing = "missing"

// Object 是 MediaObject 的应用层投影。ByteSize/Width/Height/SHA256 在库内为 nil 时这里是零值。
type Object struct {
	ID                 string
	StoragePath        string // STORAGE_ROOT 相对路径，不进 HTTP
	MIMEType           string
	ByteSize           int // 库内为 null 时这里是 0；用 VerificationStatus 判断是否核验过
	Width              int
	Height             int
	SHA256             string // 库内为 null 时这里是空串
	VerificationStatus string // verified 或 missing
	CreatedAt          time.Time
	VerifiedAt         time.Time
}

// Store 把校验后的字节写入 [storage.Local] 并登记 media_objects。
type Store struct {
	Files storage.Local // STORAGE_ROOT 本地落盘；变体也走这里
}

// objectFromSchema 把库行收成应用投影。可空列（ByteSize/宽高/SHA256/VerifiedAt）为 nil 时写成零值，
// 调用方用 VerificationStatus 判断是否核验过，不要用 Width==0 当「没有尺寸」。
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

// Stage 解码、落盘并插入 verified 行。写文件失败返回 500「写入媒体文件失败」。
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

// Get 按 id 读取 MediaObject。不存在返回 404「媒体对象不存在」。
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

// MarkMissingByStoragePath 把匹配路径且尚未 missing 的行标为 missing。更新 error 被吞掉。
func (s Store) MarkMissingByStoragePath(ctx context.Context, db *gorm.DB, storagePath string) {
	if db == nil || storagePath == "" {
		return
	}
	_ = db.WithContext(ctx).Model(&schema.MediaObjects{}).
		Where("storage_path = ? AND verification_status <> ?", storagePath, StatusMissing).
		Updates(map[string]any{"verification_status": StatusMissing}).Error
}
