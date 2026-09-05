package graph

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

const deliverySpecMaxTotalPixels = 64 * 1024 * 1024

var (
	aspectRatioPattern = regexp.MustCompile(`^[1-9][0-9]{0,2}:[1-9][0-9]{0,2}$`)
	hexColorPattern    = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
)

var defaultTemplateGenerationSpec = map[string]any{
	"aspect_ratio":       "1:1",
	"resolution_tier":    "high",
	"quality_intent":     "high",
	"reference_fidelity": "high",
	"background_intent":  "auto",
}

func resolveTemplateGenerationSpec(overrides map[string]any) (map[string]any, error) {
	payload := cloneMap(defaultTemplateGenerationSpec)
	for key, value := range overrides {
		payload[key] = cloneValue(value)
	}
	normalized, err := normalizeGenerationSpec(payload)
	if err != nil {
		return nil, apperr.Validation("出图设定无效")
	}
	return normalized, nil
}

// normalizeGenerationSpec owns model settings only; text belongs to the picture plan.
func normalizeGenerationSpec(value any) (map[string]any, error) {
	raw, ok := asMap(value)
	if !ok {
		return nil, apperr.Validation("generation_spec 无效")
	}
	known := map[string]struct{}{
		"aspect_ratio": {}, "resolution_tier": {}, "quality_intent": {},
		"reference_fidelity": {}, "background_intent": {},
	}
	for key := range raw {
		if _, ok := known[key]; !ok {
			return nil, apperr.Validation("generation_spec 无效")
		}
	}
	aspect, _ := raw["aspect_ratio"].(string)
	if !aspectRatioPattern.MatchString(aspect) {
		return nil, apperr.Validation("generation_spec 无效")
	}
	resolution := strOr(raw, "resolution_tier", "high")
	if !oneOf(resolution, "standard", "high", "ultra") {
		return nil, apperr.Validation("generation_spec 无效")
	}
	quality := strOr(raw, "quality_intent", "high")
	if !oneOf(quality, "draft", "standard", "high") {
		return nil, apperr.Validation("generation_spec 无效")
	}
	fidelity := strOr(raw, "reference_fidelity", "high")
	if !oneOf(fidelity, "low", "medium", "high") {
		return nil, apperr.Validation("generation_spec 无效")
	}
	background := strOr(raw, "background_intent", "auto")
	if !oneOf(background, "auto", "opaque", "transparent") {
		return nil, apperr.Validation("generation_spec 无效")
	}
	return map[string]any{
		"aspect_ratio":       aspect,
		"resolution_tier":    resolution,
		"quality_intent":     quality,
		"reference_fidelity": fidelity,
		"background_intent":  background,
	}, nil
}

// normalizeDeliverySpec 拒绝未知 key，宽高校 1–16384。非法返回 Validation。不进 image digest（withNoDigest）。
func normalizeDeliverySpec(value any) (map[string]any, error) {
	raw, ok := asMap(value)
	if !ok {
		return nil, apperr.Validation("delivery_spec 无效")
	}
	known := map[string]struct{}{
		"width": {}, "height": {}, "format": {}, "max_byte_size": {},
		"fit": {}, "background_color": {}, "crop_anchor": {},
	}
	for key := range raw {
		if _, ok := known[key]; !ok {
			return nil, apperr.Validation("delivery_spec 无效")
		}
	}
	width, ok := asInt(raw["width"])
	if !ok || width < 1 || width > 16384 {
		return nil, apperr.Validation("delivery_spec 无效")
	}
	height, ok := asInt(raw["height"])
	if !ok || height < 1 || height > 16384 {
		return nil, apperr.Validation("delivery_spec 无效")
	}
	format, _ := raw["format"].(string)
	if !oneOf(format, "png", "jpeg", "webp") {
		return nil, apperr.Validation("delivery_spec 无效")
	}
	fit, _ := raw["fit"].(string)
	if !oneOf(fit, "contain", "cover") {
		return nil, apperr.Validation("delivery_spec 无效")
	}
	if width*height > deliverySpecMaxTotalPixels {
		return nil, apperr.Validation(fmt.Sprintf("delivery_spec 无效: 交付规格总像素不能超过 %d", deliverySpecMaxTotalPixels))
	}
	var maxByte any
	if rawSize, exists := raw["max_byte_size"]; exists && rawSize != nil {
		n, ok := asInt(rawSize)
		if !ok || n < 1 {
			return nil, apperr.Validation("delivery_spec 无效")
		}
		maxByte = n
	}
	var background any
	if rawColor, exists := raw["background_color"]; exists && rawColor != nil {
		s, ok := rawColor.(string)
		if !ok || !hexColorPattern.MatchString(s) {
			return nil, apperr.Validation("delivery_spec 无效")
		}
		background = s
	}
	var crop any
	if rawCrop, exists := raw["crop_anchor"]; exists && rawCrop != nil {
		s, ok := rawCrop.(string)
		if !ok || !oneOf(s, "center", "top", "bottom", "left", "right") {
			return nil, apperr.Validation("delivery_spec 无效")
		}
		crop = s
	}
	if fit == "contain" && crop != nil {
		return nil, apperr.Validation("delivery_spec 无效: contain 交付规格不能指定 crop_anchor")
	}
	if fit == "cover" && background != nil {
		return nil, apperr.Validation("delivery_spec 无效: cover 交付规格不能指定 background_color")
	}
	return map[string]any{
		"width":            width,
		"height":           height,
		"format":           format,
		"max_byte_size":    maxByte,
		"fit":              fit,
		"background_color": background,
		"crop_anchor":      crop,
	}, nil
}

func strOr(raw map[string]any, key, fallback string) string {
	if value, ok := raw[key]; ok {
		if s, ok := value.(string); ok {
			return s
		}
	}
	return fallback
}

func oneOf(value string, options ...string) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}

// asInt 从 JSON number / 整数类型收 int。非有限或超范围返回 false。
func asInt(value any) (int, bool) {
	switch n := value.(type) {
	case int:
		return n, true
	case int8:
		return int(n), true
	case int16:
		return int(n), true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case uint:
		return int(n), true
	case uint64:
		return int(n), true
	case float32:
		f := float64(n)
		if f == math.Trunc(f) && !math.IsNaN(f) && !math.IsInf(f, 0) {
			return int(f), true
		}
		return 0, false
	case float64:
		if n == math.Trunc(n) && !math.IsNaN(n) && !math.IsInf(n, 0) {
			return int(n), true
		}
		return 0, false
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			f, ferr := n.Float64()
			if ferr != nil || f != math.Trunc(f) {
				return 0, false
			}
			return int(f), true
		}
		return int(i), true
	default:
		return 0, false
	}
}
