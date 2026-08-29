package delivery

import (
	"encoding/json"
	"fmt"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/canonjson"
)

const (
	specSchemaVersion   = 1
	maxTotalPixels      = 64 * 1024 * 1024
	sourceMissingDetail = "交付派生原图文件缺失或已变化"
	unexpectedFailure   = "交付图处理失败，请重试"
	exportMaxJobs       = 100
	exportMaxBytes      = 512 * 1024 * 1024
)

var qualityLadder = []int{95, 90, 85, 80, 75, 70, 60, 50, 40, 30, 20, 10, 5, 1}

var formatMIME = map[string]string{
	"png":  "image/png",
	"jpeg": "image/jpeg",
	"webp": "image/webp",
}

var formatExt = map[string]string{
	"png":  ".png",
	"jpeg": ".jpg",
	"webp": ".webp",
}

// Spec 是确定性交付派生合同。
type Spec struct {
	Width           int     `json:"width"`
	Height          int     `json:"height"`
	Format          string  `json:"format"`
	MaxByteSize     *int    `json:"max_byte_size"`
	Fit             string  `json:"fit"`
	BackgroundColor *string `json:"background_color"`
	CropAnchor      *string `json:"crop_anchor"`
}

type NormalizedSpec struct {
	Spec    Spec
	Payload map[string]any
	Hash    string
}

func NormalizeSpec(raw map[string]any) (NormalizedSpec, error) {
	if raw == nil {
		return NormalizedSpec{}, apperr.Validation("DeliverySpec 不符合 schema")
	}
	known := map[string]struct{}{
		"width": {}, "height": {}, "format": {}, "max_byte_size": {},
		"fit": {}, "background_color": {}, "crop_anchor": {},
	}
	for key := range raw {
		if _, ok := known[key]; !ok {
			return NormalizedSpec{}, apperr.Validation("DeliverySpec 不符合 schema")
		}
	}
	width, ok := asInt(raw["width"])
	if !ok || width < 1 || width > 16384 {
		return NormalizedSpec{}, apperr.Validation("DeliverySpec 不符合 schema")
	}
	height, ok := asInt(raw["height"])
	if !ok || height < 1 || height > 16384 {
		return NormalizedSpec{}, apperr.Validation("DeliverySpec 不符合 schema")
	}
	format, _ := raw["format"].(string)
	if format != "png" && format != "jpeg" && format != "webp" {
		return NormalizedSpec{}, apperr.Validation("DeliverySpec 不符合 schema")
	}
	fit, _ := raw["fit"].(string)
	if fit != "contain" && fit != "cover" {
		return NormalizedSpec{}, apperr.Validation("DeliverySpec 不符合 schema")
	}
	if width*height > maxTotalPixels {
		return NormalizedSpec{}, apperr.Validation(fmt.Sprintf("交付规格总像素不能超过 %d", maxTotalPixels))
	}
	var maxByte *int
	if rawSize, exists := raw["max_byte_size"]; exists && rawSize != nil {
		n, ok := asInt(rawSize)
		if !ok || n < 1 {
			return NormalizedSpec{}, apperr.Validation("DeliverySpec 不符合 schema")
		}
		maxByte = &n
	}
	var background *string
	if rawColor, exists := raw["background_color"]; exists && rawColor != nil {
		s, ok := rawColor.(string)
		if !ok || !hexColor(s) {
			return NormalizedSpec{}, apperr.Validation("DeliverySpec 不符合 schema")
		}
		background = &s
	}
	var crop *string
	if rawCrop, exists := raw["crop_anchor"]; exists && rawCrop != nil {
		s, ok := rawCrop.(string)
		if !ok || (s != "center" && s != "top" && s != "bottom" && s != "left" && s != "right") {
			return NormalizedSpec{}, apperr.Validation("DeliverySpec 不符合 schema")
		}
		crop = &s
	}
	if fit == "contain" && crop != nil {
		return NormalizedSpec{}, apperr.Validation("contain 交付规格不能指定 crop_anchor")
	}
	if fit == "cover" && background != nil {
		return NormalizedSpec{}, apperr.Validation("cover 交付规格不能指定 background_color")
	}
	spec := Spec{
		Width: width, Height: height, Format: format, MaxByteSize: maxByte,
		Fit: fit, BackgroundColor: background, CropAnchor: crop,
	}
	payload := specPayload(spec)
	hash, err := canonjson.SHA256Hex(payload)
	if err != nil {
		return NormalizedSpec{}, err
	}
	return NormalizedSpec{Spec: spec, Payload: payload, Hash: hash}, nil
}

func specPayload(spec Spec) map[string]any {
	var maxByte any
	if spec.MaxByteSize != nil {
		maxByte = *spec.MaxByteSize
	}
	var bg any
	if spec.BackgroundColor != nil {
		bg = *spec.BackgroundColor
	}
	var crop any
	if spec.CropAnchor != nil {
		crop = *spec.CropAnchor
	}
	return map[string]any{
		"width":            spec.Width,
		"height":           spec.Height,
		"format":           spec.Format,
		"max_byte_size":    maxByte,
		"fit":              spec.Fit,
		"background_color": bg,
		"crop_anchor":      crop,
	}
}

func specFromJSON(raw []byte) (Spec, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Spec{}, apperr.Validation("交付派生任务中的 DeliverySpec 已损坏")
	}
	normalized, err := NormalizeSpec(payload)
	if err != nil {
		return Spec{}, apperr.Validation("交付派生任务中的 DeliverySpec 已损坏")
	}
	return normalized.Spec, nil
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		if n != float64(int(n)) {
			return 0, false
		}
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	default:
		return 0, false
	}
}

func hexColor(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	for i := 1; i < 7; i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'A' || c > 'F') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
