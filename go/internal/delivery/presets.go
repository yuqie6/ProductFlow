package delivery

import (
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/product"
)

const (
	presetReviewedAt = "2026-08-24"
	presetSource     = "docs/ARCHITECTURE.md §7"
	presetDisclaimer = "内置模板仅提供便捷默认值，不构成平台审核或合规保证；平台规则可能变化，请在使用前自行确认。"
)

func init() {
	product.BindDeliveryPresetSpec(func(key string) (map[string]any, error) {
		preset, err := GetPreset(key)
		if err != nil {
			return nil, err
		}
		return SpecAsMap(preset.DeliverySpec), nil
	})
}

// Preset 是内置交付尺寸模板的 HTTP 项，不构成平台审核或合规保证。
// DeliverySpec 是确定性渲染合同，改它不会调用图像模型。
type Preset struct {
	Key                 string `json:"key"` // 进程内常量键，如 taobao_tmall_hero
	Title               string `json:"title"`
	AspectRatio         string `json:"aspect_ratio"`          // 如 3:4，与 DeliverySpec 宽高对应
	ApplicableImageType string `json:"applicable_image_type"` // hero | detail | scene
	ReviewedAt          string `json:"reviewed_at"`           // 模板审阅日期，不是任务时间
	Source              string `json:"source"`                // 文档出处，非供应商
	Disclaimer          string `json:"disclaimer"`            // 不构成平台审核或合规保证
	DeliverySpec        Spec   `json:"delivery_spec"`         // 确定性渲染合同，改它不调图像模型
}

// PresetCatalog 列出内置模板，并声明是否允许自定义 DeliverySpec。
type PresetCatalog struct {
	SupportsCustom bool     `json:"supports_custom"` // true 表示也允许自定义 DeliverySpec
	Items          []Preset `json:"items"`           // 进程内常量目录，无 DB
}

// ListPresets 返回内置交付模板目录（进程内常量，无 DB）。
// 调用时机：HTTP GET /api/v3/delivery-presets。SupportsCustom=true 表示也允许自定义 Spec。
func ListPresets() PresetCatalog {
	return PresetCatalog{
		SupportsCustom: true,
		Items: []Preset{
			makePreset("taobao_tmall_hero", "淘宝/天猫首屏", "3:4", "hero", 1200, 1600),
			makePreset("jd_hero", "京东主图", "1:1", "hero", 1200, 1200),
			makePreset("amazon_hero", "Amazon 主图", "1:1", "hero", 1200, 1200),
			makePreset("detail_portrait", "详情竖图", "3:4", "detail", 1200, 1600),
			makePreset("scene_landscape", "场景横图", "4:3", "scene", 1600, 1200),
		},
	}
}

// GetPreset 按 key 读取内置模板；不存在时返回 NotFound。
func GetPreset(key string) (Preset, error) {
	for _, item := range ListPresets().Items {
		if item.Key == key {
			return item, nil
		}
	}
	return Preset{}, apperr.NotFound("交付预设不存在")
}

// SpecAsMap 把 Spec 编成可写入节点 config 的 map。
func SpecAsMap(spec Spec) map[string]any {
	var maxByte any
	if spec.MaxByteSize != nil {
		maxByte = *spec.MaxByteSize
	}
	var background any
	if spec.BackgroundColor != nil {
		background = *spec.BackgroundColor
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
		"background_color": background,
		"crop_anchor":      crop,
	}
}

func makePreset(key, title, aspect, imageType string, width, height int) Preset {
	spec := Spec{Width: width, Height: height, Format: "png", Fit: "contain"}
	return Preset{
		Key: key, Title: title, AspectRatio: aspect, ApplicableImageType: imageType,
		ReviewedAt: presetReviewedAt, Source: presetSource, Disclaimer: presetDisclaimer,
		DeliverySpec: spec,
	}
}
