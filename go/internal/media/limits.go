package media

import (
	"strings"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

// Limits 是上传闸门。AllowedMIMETypes 为 nil 或空时 [Allows] 一律拒绝。
type Limits struct {
	MaxImageBytes      int                 // 单张上限；超则 413
	MaxPixels          int                 // 宽×高上限
	AllowedMIMETypes   map[string]struct{} // nil 或空时 Allows 一律拒绝
	MaxBatchFiles      int                 // 单批张数上限
	MaxBatchBytes      int                 // 单批总字节上限
	MaxReferenceImages int                 // 参考图张数上限
}

// DefaultLimits 返回与 config 默认值一致的闸门（10MiB、16M 像素、png/jpeg/webp、批量 20 张/50MiB、参考图 6 张）。
func DefaultLimits() Limits {
	return Limits{
		MaxImageBytes:      10 * 1024 * 1024,
		MaxPixels:          16_000_000,
		AllowedMIMETypes:   map[string]struct{}{"image/png": {}, "image/jpeg": {}, "image/webp": {}},
		MaxBatchFiles:      20,
		MaxBatchBytes:      50 * 1024 * 1024,
		MaxReferenceImages: 6,
	}
}

// ParseMIMEList 解析逗号分隔 MIME；空串回退 [DefaultLimits] 的允许集。
func ParseMIMEList(raw string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		item := strings.ToLower(strings.TrimSpace(part))
		if item != "" {
			out[item] = struct{}{}
		}
	}
	if len(out) == 0 {
		return DefaultLimits().AllowedMIMETypes
	}
	return out
}

// Allows 报告 mime 是否在 AllowedMIMETypes 里。map 为 nil 或空时一律 false（拒绝）。
// 调用时机：ValidateUpload。比较前调用方应已把 mime 正规化；本方法不做大小写折叠。
func (l Limits) Allows(mime string) bool {
	_, ok := l.AllowedMIMETypes[mime]
	return ok
}

// RejectReferenceCount 在 count 超过 MaxReferenceImages 时返回 400。
func (l Limits) RejectReferenceCount(count int) error {
	if count > l.MaxReferenceImages {
		return apperr.Validationf("参考图最多上传 %d 张", l.MaxReferenceImages)
	}
	return nil
}
