package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
)

// ModelBinding 是 prompt / image 用途在 PostgreSQL 里解析出的供应商绑定。
type ModelBinding struct {
	Kind                string
	APIKey              string
	BaseURL             string
	Model               string
	ImagesQuality       string
	ImagesStyle         string
	ResponsesBackground bool
	GeminiAPIVersion    string
	GeminiOutputMIME    string
	MaskEdit            bool
}

// ResolvePrompt 解析提示词用途绑定；mock 时 Kind=mock 且没有 API Key。
func (s *Store) ResolvePrompt(ctx context.Context) (ModelBinding, error) {
	return s.resolvePurpose(ctx, "prompt", "text_responses", "prompt_model", []string{"openai", "mock"})
}

// ResolveImage 解析图片用途绑定。
func (s *Store) ResolveImage(ctx context.Context) (ModelBinding, error) {
	binding, err := s.resolvePurpose(ctx, "image", "", "image_model", []string{"mock", "openai_responses", "openai_images", "google_gemini_image"})
	if err != nil || binding.Kind == "mock" {
		return binding, err
	}
	var profileID *string
	var configJSON, capabilities []byte
	err = pfdb.QueryRow(ctx, s.db, `
		SELECT b.provider_profile_id, COALESCE(b.config_json::text, '{}'), COALESCE(p.capabilities_json::text, '[]')
		FROM provider_bindings b
		LEFT JOIN provider_profiles p ON p.id = b.provider_profile_id
		WHERE b.purpose = 'image'
	`).Scan(&profileID, &configJSON, &capabilities)
	if err != nil {
		return binding, err
	}
	cfg := map[string]any{}
	_ = json.Unmarshal(configJSON, &cfg)
	caps := []string{}
	_ = json.Unmarshal(capabilities, &caps)
	if binding.Kind == "openai_images" {
		binding.ImagesQuality = lookupJSONString(configJSON, "images_quality")
		binding.ImagesStyle = lookupJSONString(configJSON, "images_style")
		binding.MaskEdit = contains(caps, "image_mask_edit")
	}
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

func (s *Store) resolvePurpose(ctx context.Context, purpose, capability, fallbackModelKey string, allowed []string) (ModelBinding, error) {
	var kind string
	var profileID *string
	var modelSettings []byte
	err := pfdb.QueryRow(ctx, s.db, `
		SELECT provider_kind, provider_profile_id, model_settings_json
		FROM provider_bindings WHERE purpose = $1
	`, purpose).Scan(&kind, &profileID, &modelSettings)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ModelBinding{Kind: "mock"}, nil
		}
		return ModelBinding{}, err
	}
	if !contains(allowed, kind) {
		return ModelBinding{}, apperr.Unavailable(fmt.Sprintf("暂不支持的 %s provider: %s", purpose, kind))
	}
	if kind == "mock" {
		model := lookupJSONString(modelSettings, "model")
		return ModelBinding{Kind: "mock", Model: model}, nil
	}
	if profileID == nil {
		return ModelBinding{}, apperr.Unavailable("供应商尚未配置")
	}
	var enabled bool
	var archived any
	var apiKey, baseURL *string
	var capabilities, defaultModels []byte
	err = pfdb.QueryRow(ctx, s.db, `
		SELECT enabled, archived_at, api_key, base_url, capabilities_json, default_models_json
		FROM provider_profiles WHERE id = $1
	`, *profileID).Scan(&enabled, &archived, &apiKey, &baseURL, &capabilities, &defaultModels)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ModelBinding{}, apperr.Unavailable("供应商尚未配置")
		}
		return ModelBinding{}, err
	}
	if !enabled || archived != nil {
		return ModelBinding{}, apperr.Unavailable("供应商尚未配置")
	}
	if apiKey == nil || *apiKey == "" {
		return ModelBinding{}, apperr.Unavailable("供应商 API Key 未配置")
	}
	if capability != "" {
		caps := []string{}
		_ = json.Unmarshal(capabilities, &caps)
		if !contains(caps, capability) {
			return ModelBinding{}, apperr.Unavailable("供应商缺少 " + capability + " 能力")
		}
	}
	model := lookupJSONString(modelSettings, "model")
	if model == "" {
		model = lookupJSONString(defaultModels, fallbackModelKey)
	}
	if model == "" {
		return ModelBinding{}, apperr.Unavailable("模型未配置")
	}
	out := ModelBinding{Kind: kind, APIKey: *apiKey, Model: model}
	if baseURL != nil {
		out.BaseURL = *baseURL
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
