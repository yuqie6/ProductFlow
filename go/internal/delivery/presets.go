package delivery

const (
	presetReviewedAt = "2026-08-24"
	presetSource     = "docs/ARCHITECTURE.md §7"
	presetDisclaimer = "内置模板仅提供便捷默认值，不构成平台审核或合规保证；平台规则可能变化，请在使用前自行确认。"
)

type Preset struct {
	Key                 string `json:"key"`
	Title               string `json:"title"`
	AspectRatio         string `json:"aspect_ratio"`
	ApplicableImageType string `json:"applicable_image_type"`
	ReviewedAt          string `json:"reviewed_at"`
	Source              string `json:"source"`
	Disclaimer          string `json:"disclaimer"`
	DeliverySpec        Spec   `json:"delivery_spec"`
}

type PresetCatalog struct {
	SupportsCustom bool     `json:"supports_custom"`
	Items          []Preset `json:"items"`
}

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

func makePreset(key, title, aspect, imageType string, width, height int) Preset {
	spec := Spec{Width: width, Height: height, Format: "png", Fit: "contain"}
	return Preset{
		Key: key, Title: title, AspectRatio: aspect, ApplicableImageType: imageType,
		ReviewedAt: presetReviewedAt, Source: presetSource, Disclaimer: presetDisclaimer,
		DeliverySpec: spec,
	}
}
