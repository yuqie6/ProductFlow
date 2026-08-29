package media

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	_ "golang.org/x/image/webp"
)

type Verified struct {
	MIMEType string
	ByteSize int
	Width    int
	Height   int
	SHA256   string
}

func Inspect(content []byte, expectedMIME string) (Verified, error) {
	if len(content) == 0 {
		return Verified{}, apperr.Validation("图片内容不能为空")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return Verified{}, apperr.Validation("图片内容不是可解码的 PNG、JPEG 或 WEBP")
	}
	mime, ok := formatMIME(format)
	if !ok {
		return Verified{}, apperr.Validation("图片格式仅支持 PNG、JPEG 或 WEBP")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return Verified{}, apperr.Validation("图片尺寸无效")
	}
	if expected := NormalizeMIME(expectedMIME); expected != "" && expected != mime {
		return Verified{}, apperr.Validation("图片内容格式与声明媒体类型不一致")
	}
	sum := sha256.Sum256(content)
	return Verified{
		MIMEType: mime,
		ByteSize: len(content),
		Width:    cfg.Width,
		Height:   cfg.Height,
		SHA256:   hex.EncodeToString(sum[:]),
	}, nil
}

func formatMIME(format string) (string, bool) {
	switch strings.ToLower(format) {
	case "png":
		return "image/png", true
	case "jpeg":
		return "image/jpeg", true
	case "webp":
		return "image/webp", true
	default:
		return "", false
	}
}

func ExtensionForMIME(mime string) string {
	switch mime {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	default:
		return ".bin"
	}
}

func FormatByteSize(size int) string {
	if size >= 1024*1024 {
		return strings.ReplaceAll(fmt.Sprintf("%.1fMB", float64(size)/(1024*1024)), ".0MB", "MB")
	}
	if size >= 1024 {
		return strings.ReplaceAll(fmt.Sprintf("%.1fKB", float64(size)/1024), ".0KB", "KB")
	}
	return fmt.Sprintf("%dB", size)
}
