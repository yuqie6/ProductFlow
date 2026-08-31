// Package media 负责 MediaObject 字节校验、落盘与变体下载。
//
// 职责：不可变媒体身份（哈希+字节）。商品图、图库、交付、会话附件都指向它，不各存一份路径字符串。
// 调用时机：上传 Validate* → Store.Put；下载走 ServeVariant（preview/thumbnail 缺则现生成）。
// 副作用：写 media_objects 行和 STORAGE_ROOT 文件。删除要先确认无引用，见 prune。
// 错误：类型/像素/体积超限 Validation 或 TooLarge。不要把存储路径泄漏到 HTTP 合同。
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

// Verified 是解码后的图片元数据。SHA256 是内容的 hex 摘要。
type Verified struct {
	MIMEType string
	ByteSize int // 解码后的内容长度
	Width    int
	Height   int
	SHA256   string // 内容 hex 摘要，不是 media_objects.id
}

// Inspect 解码 PNG/JPEG/WEBP 并校验尺寸。expectedMIME 非空且与真实类型不一致时返回 400。
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

// ExtensionForMIME 把已规范化 MIME 映射到扩展名；未知类型返回 .bin。
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

// FormatByteSize 把字节数格式化为 B/KB/MB，给上传超限文案使用。
func FormatByteSize(size int) string {
	if size >= 1024*1024 {
		return strings.ReplaceAll(fmt.Sprintf("%.1fMB", float64(size)/(1024*1024)), ".0MB", "MB")
	}
	if size >= 1024 {
		return strings.ReplaceAll(fmt.Sprintf("%.1fKB", float64(size)/1024), ".0KB", "KB")
	}
	return fmt.Sprintf("%dB", size)
}
