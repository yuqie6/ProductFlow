package product

import (
	"context"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/mediaarchive"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

const (
	galleryArchiveMaxAssets = 100
	galleryArchiveMaxBytes  = 512 * 1024 * 1024
)

var invalidFilenameChars = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]`)

var archiveExtensions = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
}

// GalleryArchive 指向已写好的临时 ZIP，不落库。
// 调用方（HTTP downloadArchive）必须 FileAttachment 之后删 Path。Filename 给 Content-Disposition。
type GalleryArchive struct {
	Path     string
	Filename string
}

type frozenGalleryFile struct {
	name        string
	mediaID     string
	storagePath string
	mime        string
	byteSize    int
	width       int
	height      int
	sha256      string
}

// BuildGalleryArchive 按资产 id 打包已核验原图；ZIP 条目名来自显示名。
// 事务内只冻结条目与期望身份；事务外逐文件 ReadVerified 再写入临时 ZIP。
// 数量非法或未核验返回 Validation；缺图或缺文件返回 NotFound。
func (s Service) BuildGalleryArchive(ctx context.Context, productID string, assetIDs []string) (GalleryArchive, error) {
	normalized, err := normalizeArchiveIDs(assetIDs)
	if err != nil {
		return GalleryArchive{}, err
	}
	var (
		filename string
		files    []frozenGalleryFile
	)
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		product, err := loadProduct(ctx, pgxTx, productID)
		if err != nil {
			return err
		}
		assets, err := loadAssetsByIDs(ctx, pgxTx, productID, normalized)
		if err != nil {
			return err
		}
		if len(assets) != len(normalized) {
			return apperr.NotFound("商品图片不存在")
		}
		byID := map[string]ImageAsset{}
		for _, asset := range assets {
			byID[asset.ID] = asset
		}
		used := map[string]struct{}{}
		total := 0
		ordered := make([]frozenGalleryFile, 0, len(normalized))
		for _, id := range normalized {
			asset, ok := byID[id]
			if !ok {
				return apperr.NotFound("商品图片不存在")
			}
			if asset.VerificationStatus != media.StatusVerified {
				return apperr.Validation("只有已核验图片可以批量下载")
			}
			obj, err := s.Media.Get(ctx, pgxTx, asset.MediaObjectID)
			if err != nil {
				return mapGalleryArchiveRead(err)
			}
			if obj.VerificationStatus != media.StatusVerified {
				return apperr.Validation("只有已核验图片可以批量下载")
			}
			if obj.ByteSize <= 0 || strings.TrimSpace(obj.MIMEType) == "" {
				return apperr.Validation("只有已核验图片可以批量下载")
			}
			total += obj.ByteSize
			if total > galleryArchiveMaxBytes {
				return apperr.Validation("批量下载图片总大小不能超过 512 MiB")
			}
			entryName, err := archiveEntryName(asset, used)
			if err != nil {
				return err
			}
			ordered = append(ordered, frozenGalleryFile{
				name:        entryName,
				mediaID:     obj.ID,
				storagePath: obj.StoragePath,
				mime:        obj.MIMEType,
				byteSize:    obj.ByteSize,
				width:       obj.Width,
				height:      obj.Height,
				sha256:      obj.SHA256,
			})
		}
		files = ordered
		filename = cleanFilename(product.Name, "product") + "-images.zip"
		return nil
	})
	if err != nil {
		return GalleryArchive{}, err
	}

	w, err := mediaarchive.Begin(ctx, mediaarchive.Options{
		Filename: filename,
		Pattern:  "gallery-archive-*.zip",
	})
	if err != nil {
		return GalleryArchive{}, err
	}
	defer w.Abort()
	for _, f := range files {
		content, err := s.Media.ReadVerified(ctx, s.DB, f.mediaID)
		if err != nil {
			return GalleryArchive{}, mapGalleryArchiveRead(err)
		}
		meta := content.Verified
		if content.Object.StoragePath != f.storagePath ||
			meta.MIMEType != f.mime ||
			meta.ByteSize != f.byteSize ||
			(f.sha256 != "" && meta.SHA256 != f.sha256) ||
			meta.Width != f.width ||
			meta.Height != f.height {
			return GalleryArchive{}, apperr.Conflict("商品图片在打包时发生变化")
		}
		if err := w.Add(ctx, mediaarchive.File{Name: f.name, Data: content.Bytes}); err != nil {
			return GalleryArchive{}, err
		}
	}
	out, err := w.Finish()
	if err != nil {
		return GalleryArchive{}, err
	}
	return GalleryArchive{Path: out.Path, Filename: out.Filename}, nil
}

func mapGalleryArchiveRead(err error) error {
	if apperr.IsNotFound(err) {
		return apperr.NotFound("商品图片文件不存在")
	}
	if re, ok := media.AsReadError(err); ok {
		switch re.Kind {
		case media.ReadNotVerified:
			return apperr.Validation("只有已核验图片可以批量下载")
		case media.ReadNotFound, media.ReadMissingFile, media.ReadCorrupt, media.ReadIO, media.ReadIdentity:
			return apperr.NotFound("商品图片文件不存在")
		}
	}
	return err
}

func normalizeArchiveIDs(assetIDs []string) ([]string, error) {
	if len(assetIDs) < 1 || len(assetIDs) > galleryArchiveMaxAssets {
		return nil, apperr.Validationf("批量下载必须选择 1 到 %d 张图片", galleryArchiveMaxAssets)
	}
	out := make([]string, 0, len(assetIDs))
	seen := map[string]struct{}{}
	for _, raw := range assetIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			return nil, apperr.Validation("图片 ID 不能为空")
		}
		if _, ok := seen[id]; ok {
			return nil, apperr.Validation("批量下载不能包含重复图片 ID")
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

func archiveEntryName(asset ImageAsset, names map[string]struct{}) (string, error) {
	ext := archiveExtensions[asset.MIMEType]
	if ext == "" {
		return "", apperr.Validation("批量下载包含不支持的图片格式")
	}
	base := cleanFilename(asset.DisplayName, "image")
	if strings.HasSuffix(strings.ToLower(base), ext) {
		base = base[:len(base)-len(ext)]
		if base == "" {
			base = "image"
		}
	}
	candidate := base + ext
	if _, ok := names[strings.ToLower(candidate)]; ok {
		shortID := asset.ID
		if utf8.RuneCountInString(shortID) >= 8 {
			shortID = asset.ID[:8]
		}
		candidate = base + "-" + shortID + ext
	}
	for {
		folded := strings.ToLower(candidate)
		if _, ok := names[folded]; !ok {
			names[folded] = struct{}{}
			return candidate, nil
		}
		candidate = base + "-" + asset.ID + ext
	}
}

func cleanFilename(value, fallback string) string {
	cleaned := invalidFilenameChars.ReplaceAllString(value, "_")
	cleaned = strings.TrimSpace(strings.Trim(cleaned, "."))
	cleaned = regexp.MustCompile(`\s+`).ReplaceAllString(cleaned, " ")
	if cleaned == "" {
		cleaned = fallback
	}
	if utf8.RuneCountInString(cleaned) > 180 {
		cleaned = string([]rune(cleaned)[:180])
	}
	return cleaned
}
