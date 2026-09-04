package product

import (
	"context"

	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"gorm.io/gorm"
)

// ReadAssetBytes 按商品图片身份读回已核验媒体字节，供图运行把参考图交给 provider。
// 图不属于该商品或文件未通过核验返回 Validation；媒体对象不存在返回 NotFound。
func (s Service) ReadAssetBytes(ctx context.Context, tx *gorm.DB, productID, assetID string) ([]byte, string, string, error) {
	q := tx
	if q == nil {
		q = s.DB
	}
	asset, err := loadAssetForProduct(ctx, q, productID, assetID)
	if err != nil {
		return nil, "", "", err
	}
	content, err := s.Media.ReadVerified(ctx, q, asset.MediaObjectID)
	if err != nil {
		if re, ok := media.AsReadError(err); ok {
			if re.Kind == media.ReadNotFound {
				return nil, "", "", apperr.NotFound("媒体对象不存在")
			}
			return nil, "", "", apperr.Validation("参考图文件不可读取")
		}
		return nil, "", "", err
	}
	mime := content.Verified.MIMEType
	if mime == "" {
		mime = asset.MIMEType
	}
	filename := asset.OriginalFilename
	if filename == "" {
		filename = asset.DisplayName
	}
	return content.Bytes, mime, filename, nil
}
