package media

import (
	"context"
	"errors"
	"time"

	sqldb "database/sql"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
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
	var createdAt, verifiedAt time.Time
	err = pfdb.QueryRow(ctx, tx, `
		INSERT INTO media_objects (
			id, storage_path, mime_type, byte_size, width, height, sha256,
			verification_status, created_at, verified_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, 'verified', NOW(), NOW())
		RETURNING created_at, verified_at
	`, id, path, verified.MIMEType, verified.ByteSize, verified.Width, verified.Height, verified.SHA256).Scan(&createdAt, &verifiedAt)
	if err != nil {
		return Object{}, err
	}
	return Object{
		ID:                 id,
		StoragePath:        path,
		MIMEType:           verified.MIMEType,
		ByteSize:           verified.ByteSize,
		Width:              verified.Width,
		Height:             verified.Height,
		SHA256:             verified.SHA256,
		VerificationStatus: StatusVerified,
		CreatedAt:          createdAt,
		VerifiedAt:         verifiedAt,
	}, nil
}

func (s Store) Get(ctx context.Context, q *gorm.DB, id string) (Object, error) {
	var obj Object
	var verifiedAt *time.Time
	err := pfdb.QueryRow(ctx, q, `
		SELECT id, storage_path, mime_type, byte_size, width, height, sha256,
		       verification_status, created_at, verified_at
		FROM media_objects WHERE id = $1
	`, id).Scan(
		&obj.ID, &obj.StoragePath, &obj.MIMEType, &obj.ByteSize, &obj.Width, &obj.Height, &obj.SHA256,
		&obj.VerificationStatus, &obj.CreatedAt, &verifiedAt,
	)
	if errors.Is(err, sqldb.ErrNoRows) {
		return Object{}, apperr.NotFound("媒体对象不存在")
	}
	if err != nil {
		return Object{}, err
	}
	if verifiedAt != nil {
		obj.VerifiedAt = *verifiedAt
	}
	return obj, nil
}
