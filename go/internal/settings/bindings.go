package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

// ModelBinding 是 prompt / image 用途在 PostgreSQL 里解析出的供应商绑定。
type ModelBinding struct {
	Kind                string // mock | openai | openai_responses | openai_images | google_gemini_image
	APIKey              string // mock 时为空
	BaseURL             string // 空表示供应商默认
	Model               string // 绑定解析出的模型 id；mock 可空
	ImagesQuality       string // 仅 openai_images
	ImagesStyle         string // 仅 openai_images
	ResponsesBackground bool   // 仅 openai_responses
	GeminiAPIVersion    string // 仅 google_gemini_image；空则 v1beta
	GeminiOutputMIME    string // 仅 google_gemini_image
	MaskEdit            bool   // openai_images/responses 且档案有 image_mask_edit
}

// ResolvePrompt 解析提示词用途绑定；mock 时 Kind=mock 且没有 API Key。
// 档案禁用、没 Key、缺模型或能力返回 Unavailable；无绑定行时 Kind=mock，不是错误。
func (s *Store) ResolvePrompt(ctx context.Context) (ModelBinding, error) {
	return s.resolvePurpose(ctx, "prompt", "text_responses", "prompt_model", []string{"openai", "mock"})
}

// ResolveImage 解析图片用途绑定（内部 ModelBinding，含 API Key）。
// 调用时机：providers 工厂构造生图/局部编辑适配器。无绑定行时 Kind=mock，不是 404。
// 档案禁用/没 Key 返回 Unavailable(503)。Kind 闭集：mock / openai_responses / openai_images / google_gemini_image。
// 不要用本函数解析 prompt 或 agent 用途。
func (s *Store) ResolveImage(ctx context.Context) (ModelBinding, error) {
	binding, err := s.resolvePurpose(ctx, "image", "", "image_model", []string{"mock", "openai_responses", "openai_images", "google_gemini_image"})
	if err != nil || binding.Kind == "mock" {
		return binding, err
	}
	var row schema.ProviderBindings
	if err := s.db.WithContext(ctx).Where("purpose = ?", "image").Take(&row).Error; err != nil {
		return binding, err
	}
	configJSON := []byte(row.ConfigJSON)
	capabilities := []byte("[]")
	if row.ProviderProfileID != nil {
		var profile schema.ProviderProfiles
		if err := s.db.WithContext(ctx).Where("id = ?", *row.ProviderProfileID).Take(&profile).Error; err == nil {
			capabilities = []byte(profile.CapabilitiesJSON)
		}
	}
	cfg := map[string]any{}
	_ = json.Unmarshal(configJSON, &cfg)
	caps := []string{}
	_ = json.Unmarshal(capabilities, &caps)
	if binding.Kind == "openai_images" {
		binding.ImagesQuality = lookupJSONString(configJSON, "images_quality")
		binding.ImagesStyle = lookupJSONString(configJSON, "images_style")
	}
	binding.MaskEdit = imageMaskEditForKind(binding.Kind, caps)
	if binding.Kind == "openai_responses" {
		if v, ok := cfg["responses_background_enabled"].(bool); ok {
			binding.ResponsesBackground = v
		}
	}
	if binding.Kind == "google_gemini_image" {
		binding.GeminiAPIVersion = lookupJSONString(configJSON, "gemini_api_version")
		if binding.GeminiAPIVersion == "" {
			binding.GeminiAPIVersion = "v1beta"
		}
		binding.GeminiOutputMIME = lookupJSONString(configJSON, "gemini_output_mime_type")
	}
	if need := imageCapabilityForKind(binding.Kind); need != "" && !contains(caps, need) {
		return ModelBinding{}, apperr.Unavailable("供应商档案不支持当前接口能力")
	}
	return binding, nil
}

func imageMaskEditForKind(kind string, capabilities []string) bool {
	return (kind == "openai_images" || kind == "openai_responses") && contains(capabilities, "image_mask_edit")
}

func imageCapabilityForKind(kind string) string {
	switch kind {
	case "openai_responses":
		return "image_responses"
	case "openai_images":
		return "image_images"
	case "google_gemini_image":
		return "image_google_gemini"
	default:
		return ""
	}
}

// resolvePurpose 读某一用途绑定。没有行时回退 Kind=mock（开发默认可跑），不是 404。
// 档案禁用/归档/没 Key 返回 503「尚未配置」，前端应引导去设置页，不要当请求错误。
func (s *Store) resolvePurpose(ctx context.Context, purpose, capability, fallbackModelKey string, allowed []string) (ModelBinding, error) {
	var row schema.ProviderBindings
	err := s.db.WithContext(ctx).Where("purpose = ?", purpose).Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ModelBinding{Kind: "mock"}, nil
		}
		return ModelBinding{}, err
	}
	if !contains(allowed, row.ProviderKind) {
		return ModelBinding{}, apperr.Unavailable(fmt.Sprintf("暂不支持的 %s provider: %s", purpose, row.ProviderKind))
	}
	if row.ProviderKind == "mock" {
		model := lookupJSONString([]byte(row.ModelSettingsJSON), "model")
		return ModelBinding{Kind: "mock", Model: model}, nil
	}
	if row.ProviderProfileID == nil {
		return ModelBinding{}, apperr.Unavailable("供应商尚未配置")
	}
	var profile schema.ProviderProfiles
	err = s.db.WithContext(ctx).Where("id = ?", *row.ProviderProfileID).Take(&profile).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ModelBinding{}, apperr.Unavailable("供应商尚未配置")
		}
		return ModelBinding{}, err
	}
	if !profile.Enabled || profile.ArchivedAt != nil {
		return ModelBinding{}, apperr.Unavailable("供应商尚未配置")
	}
	if profile.APIKey == nil || *profile.APIKey == "" {
		return ModelBinding{}, apperr.Unavailable("供应商 API Key 未配置")
	}
	if capability != "" {
		caps := []string{}
		_ = json.Unmarshal([]byte(profile.CapabilitiesJSON), &caps)
		if !contains(caps, capability) {
			return ModelBinding{}, apperr.Unavailable("供应商缺少 " + capability + " 能力")
		}
	}
	model := lookupJSONString([]byte(row.ModelSettingsJSON), "model")
	if model == "" {
		model = lookupJSONString([]byte(profile.DefaultModelsJSON), fallbackModelKey)
	}
	if model == "" {
		return ModelBinding{}, apperr.Unavailable("模型未配置")
	}
	out := ModelBinding{Kind: row.ProviderKind, APIKey: *profile.APIKey, Model: model}
	if profile.BaseURL != nil {
		out.BaseURL = *profile.BaseURL
	}
	return out, nil
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
