package product

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/yuqie6/productflow/internal/media"
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

// GalleryArchive 是 POST .../download-archive 200 的内存 ZIP，不落库。
// Filename 来自商品名；Bytes 是已核验原图。最多 100 张、合计 512MiB。不要当成 MediaObject。
type GalleryArchive struct {
	Filename string
	Bytes    []byte // 已核验原图打成的 ZIP，不落库
}

// BuildGalleryArchive 按资产 id 打包已核验原图；ZIP 条目名来自显示名。
func (s Service) BuildGalleryArchive(ctx context.Context, productID string, assetIDs []string) (GalleryArchive, error) {
	normalized, err := normalizeArchiveIDs(assetIDs)
	if err != nil {
		return GalleryArchive{}, err
	}
	var archive GalleryArchive
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
		ordered := make([]ImageAsset, 0, len(normalized))
		total := 0
		for _, id := range normalized {
			asset, ok := byID[id]
			if !ok {
				return apperr.NotFound("商品图片不存在")
			}
			if asset.VerificationStatus != media.StatusVerified {
				return apperr.Validation("只有已核验图片可以批量下载")
			}
			if asset.ByteSize != nil {
				total += *asset.ByteSize
			}
			ordered = append(ordered, asset)
		}
		if total > galleryArchiveMaxBytes {
			return apperr.Validation("批量下载图片总大小不能超过 512 MiB")
		}
		var buf bytes.Buffer
		writer := zip.NewWriter(&buf)
		used := map[string]struct{}{}
		for _, asset := range ordered {
			abs, err := s.Media.Files.Resolve(asset.StoragePath)
			if err != nil {
				return apperr.NotFound("商品图片文件不存在")
			}
			file, err := os.Open(abs)
			if err != nil {
				return apperr.NotFound("商品图片文件不存在")
			}
			entryName, err := archiveEntryName(asset, used)
			if err != nil {
				_ = file.Close()
				return err
			}
			entry, err := writer.Create(entryName)
			if err != nil {
				_ = file.Close()
				return err
			}
			if _, err := io.Copy(entry, file); err != nil {
				_ = file.Close()
				return err
			}
			_ = file.Close()
		}
		if err := writer.Close(); err != nil {
			return err
		}
		archive = GalleryArchive{
			Filename: cleanFilename(product.Name, "product") + "-images.zip",
			Bytes:    buf.Bytes(),
		}
		return nil
	})
	return archive, err
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
