package media

import (
	"strings"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

type Limits struct {
	MaxImageBytes      int
	MaxPixels          int
	AllowedMIMETypes   map[string]struct{}
	MaxBatchFiles      int
	MaxBatchBytes      int
	MaxReferenceImages int
}

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

func (l Limits) Allows(mime string) bool {
	_, ok := l.AllowedMIMETypes[mime]
	return ok
}

func (l Limits) RejectReferenceCount(count int) error {
	if count > l.MaxReferenceImages {
		return apperr.Validationf("参考图最多上传 %d 张", l.MaxReferenceImages)
	}
	return nil
}
