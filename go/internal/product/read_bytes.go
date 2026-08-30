package product

import (
	"context"
	"os"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"gorm.io/gorm"
)

// ReadAssetBytes 按商品图片身份读回媒体字节，供图运行把参考图交给 provider。
func (s Service) ReadAssetBytes(ctx context.Context, tx *gorm.DB, assetID string) ([]byte, string, string, error) {
	q := tx
	if q == nil {
		q = s.DB
	}
	asset, err := LoadAssetRow(ctx, q, assetID)
	if err != nil {
		return nil, "", "", err
	}
	obj, err := s.Media.Get(ctx, q, asset.MediaObjectID)
	if err != nil {
		return nil, "", "", err
	}
	abs, err := s.Media.Files.Resolve(obj.StoragePath)
	if err != nil {
		return nil, "", "", apperr.Validation("参考图文件不可读取")
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, "", "", apperr.Validation("参考图文件不可读取")
	}
	mime := obj.MIMEType
	if mime == "" {
		mime = asset.MIMEType
	}
	filename := asset.OriginalFilename
	if filename == "" {
		filename = asset.DisplayName
	}
	return data, mime, filename, nil
}
