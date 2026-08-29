package storage

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

type Variant string

const (
	VariantOriginal  Variant = "original"
	VariantPreview   Variant = "preview"
	VariantThumbnail Variant = "thumbnail"
)

var variantMaxEdge = map[Variant]int{
	VariantPreview:   1600,
	VariantThumbnail: 320,
}

func ParseVariant(raw string) (Variant, error) {
	switch strings.TrimSpace(raw) {
	case "", "original":
		return VariantOriginal, nil
	case "preview":
		return VariantPreview, nil
	case "thumbnail":
		return VariantThumbnail, nil
	default:
		return "", fmt.Errorf("不支持的图片变体")
	}
}

type ResolvedFile struct {
	AbsPath   string
	MediaType string
	Filename  string
}

func (s Local) ResolveForVariant(relativePath, fallbackMediaType string, variant Variant) (ResolvedFile, error) {
	original, err := s.Resolve(relativePath)
	if err != nil {
		return ResolvedFile{}, err
	}
	if variant == VariantOriginal {
		return ResolvedFile{
			AbsPath:   original,
			MediaType: guessMediaType(original, fallbackMediaType),
			Filename:  filepath.Base(original),
		}, nil
	}
	variantPath := s.variantPath(original, variant)
	if _, err := os.Stat(variantPath); err != nil {
		if err := s.generateVariant(original, variantPath, variant); err != nil {
			return ResolvedFile{
				AbsPath:   original,
				MediaType: guessMediaType(original, fallbackMediaType),
				Filename:  filepath.Base(original),
			}, nil
		}
	}
	return ResolvedFile{
		AbsPath:   variantPath,
		MediaType: guessMediaType(variantPath, fallbackMediaType),
		Filename:  filepath.Base(variantPath),
	}, nil
}

func (s Local) DeleteWithVariants(relativePath string) error {
	original, err := s.Resolve(relativePath)
	if err != nil {
		return err
	}
	for _, variant := range []Variant{VariantPreview, VariantThumbnail} {
		_ = os.Remove(s.variantPath(original, variant))
	}
	_ = os.Remove(original)
	_ = os.Remove(filepath.Dir(s.variantPath(original, VariantPreview)))
	return nil
}

func (s Local) warmVariants(relativePath string) error {
	for _, variant := range []Variant{VariantPreview, VariantThumbnail} {
		if _, err := s.ResolveForVariant(relativePath, "application/octet-stream", variant); err != nil {
			return err
		}
	}
	return nil
}

func (s Local) variantPath(originalAbs string, variant Variant) string {
	dir := filepath.Dir(originalAbs)
	stem := strings.TrimSuffix(filepath.Base(originalAbs), filepath.Ext(originalAbs))
	return filepath.Join(dir, ".variants", stem+"."+string(variant)+".jpg")
}

func (s Local) generateVariant(originalAbs, variantAbs string, variant Variant) error {
	maxEdge := variantMaxEdge[variant]
	if maxEdge <= 0 {
		return fmt.Errorf("原图不需要派生缩略图")
	}
	srcFile, err := os.Open(originalAbs)
	if err != nil {
		return err
	}
	defer srcFile.Close()
	src, _, err := image.Decode(srcFile)
	if err != nil {
		return err
	}
	resized := thumbnail(src, maxEdge)
	if err := os.MkdirAll(filepath.Dir(variantAbs), 0o755); err != nil {
		return err
	}
	tmp := variantAbs + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	rgb := flattenRGB(resized)
	err = jpeg.Encode(out, rgb, &jpeg.Options{Quality: 86})
	closeErr := out.Close()
	if err != nil || closeErr != nil {
		_ = os.Remove(tmp)
		if err != nil {
			return err
		}
		return closeErr
	}
	return os.Rename(tmp, variantAbs)
}

func thumbnail(src image.Image, maxEdge int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return src
	}
	if w <= maxEdge && h <= maxEdge {
		return src
	}
	ratio := float64(maxEdge) / float64(w)
	if rh := float64(maxEdge) / float64(h); rh < ratio {
		ratio = rh
	}
	nw := int(float64(w)*ratio + 0.5)
	nh := int(float64(h)*ratio + 0.5)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	return dst
}

func flattenRGB(src image.Image) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Over)
	return dst
}

func guessMediaType(path, fallback string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	default:
		if fallback != "" {
			return fallback
		}
		return "application/octet-stream"
	}
}

func VariantDownloadName(originalFilename string, variant Variant, resolvedSuffix string) string {
	if variant == VariantOriginal {
		return originalFilename
	}
	stem := strings.TrimSuffix(filepath.Base(originalFilename), filepath.Ext(originalFilename))
	if stem == "" {
		stem = "image"
	}
	if resolvedSuffix == "" {
		resolvedSuffix = ".jpg"
	}
	if !strings.HasPrefix(resolvedSuffix, ".") {
		resolvedSuffix = "." + resolvedSuffix
	}
	return stem + "-" + string(variant) + resolvedSuffix
}

func ImageURLs(baseDownloadURL string) (download, preview, thumbnail string) {
	return baseDownloadURL, baseDownloadURL + "?variant=preview", baseDownloadURL + "?variant=thumbnail"
}
