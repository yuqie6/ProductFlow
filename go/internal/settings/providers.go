package settings

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

var (
	providerTypes = map[string]struct{}{"openai_compatible": {}, "google_gemini": {}}
	capabilities  = map[string]struct{}{
		"text_responses": {}, "image_responses": {}, "image_images": {},
		"image_google_gemini": {}, "image_mask_edit": {},
	}
	kindsByPurpose = map[string]map[string]struct{}{
		"prompt": {"mock": {}, "openai": {}},
		"agent":  {"mock": {}, "openai": {}},
		"image":  {"mock": {}, "openai_responses": {}, "openai_images": {}, "google_gemini_image": {}},
	}
	capabilityByKind = map[string]string{
		"openai": "text_responses", "openai_responses": "image_responses",
		"openai_images": "image_images", "google_gemini_image": "image_google_gemini",
	}
)

type ProviderProfile struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	ProviderType  string         `json:"provider_type"`
	BaseURL       *string        `json:"base_url"`
	Capabilities  []string       `json:"capabilities"`
	DefaultModels map[string]any `json:"default_models"`
	Config        map[string]any `json:"config"`
	Enabled       bool           `json:"enabled"`
	ArchivedAt    *string        `json:"archived_at"`
	HasAPIKey     bool           `json:"has_api_key"`
	CreatedAt     string         `json:"created_at"`
	UpdatedAt     string         `json:"updated_at"`
}

type ProviderBindingView struct {
	ID                string         `json:"id"`
	Purpose           string         `json:"purpose"`
	ProviderKind      string         `json:"provider_kind"`
	ProviderProfileID *string        `json:"provider_profile_id"`
	ModelSettings     map[string]any `json:"model_settings"`
	Config            map[string]any `json:"config"`
	CreatedAt         string         `json:"created_at"`
	UpdatedAt         string         `json:"updated_at"`
}

type ProviderConfigResponse struct {
	Profiles []ProviderProfile     `json:"profiles"`
	Bindings []ProviderBindingView `json:"bindings"`
}

func (s *Store) ProviderConfig(ctx context.Context) (ProviderConfigResponse, error) {
	if err := s.ensureBindings(ctx); err != nil {
		return ProviderConfigResponse{}, err
	}
	profiles, err := s.listProfiles(ctx)
	if err != nil {
		return ProviderConfigResponse{}, err
	}
	bindings, err := s.listBindings(ctx)
	if err != nil {
		return ProviderConfigResponse{}, err
	}
	return ProviderConfigResponse{Profiles: profiles, Bindings: bindings}, nil
}

func (s *Store) ensureBindings(ctx context.Context) error {
	defaults := []struct{ purpose, model string }{
		{"prompt", "mock-prompt-v2"},
		{"agent", "gpt-5.4"},
		{"image", "mock-image-v2"},
	}
	return tx.WithGorm(ctx, s.db, func(dbTx *gorm.DB) error {
		if err := migrateLegacyTextBinding(ctx, dbTx); err != nil {
			return err
		}
		for _, item := range defaults {
			var n int64
			if err := dbTx.Model(&schema.ProviderBindings{}).Where("purpose = ?", item.purpose).Count(&n).Error; err != nil {
				return err
			}
			if n > 0 {
				continue
			}
			settingsJSON, _ := json.Marshal(map[string]any{"model": item.model})
			now := time.Now().UTC()
			row := schema.ProviderBindings{
				ID: clockid.New(), Purpose: item.purpose, ProviderKind: "mock",
				ModelSettingsJSON: string(settingsJSON), ConfigJSON: "{}",
				CreatedAt: now, UpdatedAt: now,
			}
			if err := dbTx.Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

type legacyBinding struct {
	id            string
	kind          string
	profileID     *string
	modelSettings []byte
	configJSON    []byte
}

func loadBindingByPurpose(ctx context.Context, dbTx *gorm.DB, purpose string) (*legacyBinding, error) {
	var row schema.ProviderBindings
	err := dbTx.WithContext(ctx).Where("purpose = ?", purpose).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &legacyBinding{
		id:            row.ID,
		kind:          row.ProviderKind,
		profileID:     row.ProviderProfileID,
		modelSettings: []byte(row.ModelSettingsJSON),
		configJSON:    []byte(row.ConfigJSON),
	}, nil
}

// migrateLegacyTextBinding 把 cutover 前残留的 purpose=text 拆成 prompt 与 agent，避免已部署库上的真实 Key 落到 mock。
func migrateLegacyTextBinding(ctx context.Context, dbTx *gorm.DB) error {
	text, err := loadBindingByPurpose(ctx, dbTx, "text")
	if err != nil || text == nil {
		return err
	}
	promptModel := firstNonEmpty(
		lookupJSONString(text.modelSettings, "model"),
		lookupJSONString(text.modelSettings, "copy_model"),
		lookupJSONString(text.modelSettings, "brief_model"),
	)
	if promptModel == "" {
		promptModel = "gpt-4.1"
	}
	agentModel := firstNonEmpty(lookupJSONString(text.modelSettings, "agent_model"), promptModel)
	promptSettings, _ := json.Marshal(map[string]any{"model": promptModel})
	agentSettings, _ := json.Marshal(map[string]any{"model": agentModel})
	agentConfig, _ := json.Marshal(legacyAgentConfig(text.modelSettings))

	prompt, err := loadBindingByPurpose(ctx, dbTx, "prompt")
	if err != nil {
		return err
	}
	if prompt == nil {
		if err := dbTx.Model(&schema.ProviderBindings{}).Where("id = ?", text.id).Updates(map[string]any{
			"purpose": "prompt", "model_settings_json": string(promptSettings), "updated_at": time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
	} else if prompt.kind == "mock" && text.kind != "mock" {
		if err := dbTx.Model(&schema.ProviderBindings{}).Where("id = ?", prompt.id).Updates(map[string]any{
			"provider_kind":       text.kind,
			"provider_profile_id": text.profileID,
			"model_settings_json": string(promptSettings),
			"config_json":         string(text.configJSON),
			"updated_at":          time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
		if err := dbTx.Where("id = ?", text.id).Delete(&schema.ProviderBindings{}).Error; err != nil {
			return err
		}
	} else if err := dbTx.Where("id = ?", text.id).Delete(&schema.ProviderBindings{}).Error; err != nil {
		return err
	}

	agent, err := loadBindingByPurpose(ctx, dbTx, "agent")
	if err != nil {
		return err
	}
	if agent != nil {
		return nil
	}
	source, err := loadBindingByPurpose(ctx, dbTx, "prompt")
	if err != nil || source == nil {
		return err
	}
	empty := "{}"
	agentConfigStr := string(agentConfig)
	if len(agentConfig) == 0 {
		agentConfigStr = empty
	}
	now := time.Now().UTC()
	row := schema.ProviderBindings{
		ID: clockid.New(), Purpose: "agent", ProviderKind: source.kind,
		ProviderProfileID: source.profileID,
		ModelSettingsJSON: string(agentSettings), ConfigJSON: agentConfigStr,
		CreatedAt: now, UpdatedAt: now,
	}
	return dbTx.Create(&row).Error
}

func legacyAgentConfig(raw []byte) map[string]any {
	out := map[string]any{}
	pairs := [][2]string{
		{"agent_reasoning_effort", "reasoning_effort"},
		{"agent_reasoning_summary", "reasoning_summary"},
		{"agent_text_verbosity", "text_verbosity"},
		{"reasoning_effort", "reasoning_effort"},
		{"reasoning_summary", "reasoning_summary"},
		{"text_verbosity", "text_verbosity"},
		{"service_tier", "service_tier"},
	}
	for _, pair := range pairs {
		if v := lookupJSONString(raw, pair[0]); v != "" {
			out[pair[1]] = v
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (s *Store) listProfiles(ctx context.Context) ([]ProviderProfile, error) {
	var rows []schema.ProviderProfiles
	if err := s.db.WithContext(ctx).Order("created_at, name").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ProviderProfile, 0, len(rows))
	for _, row := range rows {
		out = append(out, profileFromModel(row))
	}
	return out, nil
}

func (s *Store) listBindings(ctx context.Context) ([]ProviderBindingView, error) {
	var rows []schema.ProviderBindings
	if err := s.db.WithContext(ctx).Order("purpose").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ProviderBindingView, 0, len(rows))
	for _, row := range rows {
		out = append(out, bindingFromModel(row))
	}
	return out, nil
}

func profileFromModel(p schema.ProviderProfiles) ProviderProfile {
	out := ProviderProfile{
		ID: p.ID, Name: p.Name, ProviderType: p.ProviderType, Enabled: p.Enabled,
		BaseURL:       emptyToNil(p.BaseURL),
		Capabilities:  decodeStringSlice([]byte(p.CapabilitiesJSON)),
		DefaultModels: decodeMap([]byte(p.DefaultModelsJSON)),
		Config:        decodeMap([]byte(p.ConfigJSON)),
		HasAPIKey:     p.APIKey != nil && strings.TrimSpace(*p.APIKey) != "",
		CreatedAt:     p.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:     p.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if p.ArchivedAt != nil {
		stamp := p.ArchivedAt.UTC().Format(time.RFC3339Nano)
		out.ArchivedAt = &stamp
	}
	return out
}

func bindingFromModel(row schema.ProviderBindings) ProviderBindingView {
	return ProviderBindingView{
		ID: row.ID, Purpose: row.Purpose, ProviderKind: row.ProviderKind,
		ProviderProfileID: row.ProviderProfileID,
		ModelSettings:     decodeMap([]byte(row.ModelSettingsJSON)),
		Config:            decodeMap([]byte(row.ConfigJSON)),
		CreatedAt:         row.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:         row.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func (s *Store) CreateProfile(ctx context.Context, name, providerType string, baseURL, apiKey *string, caps []string, defaults, cfg map[string]any, enabled bool) (ProviderProfile, error) {
	providerType, err := normalizeProviderType(providerType)
	if err != nil {
		return ProviderProfile{}, err
	}
	caps, err = normalizeCapabilities(caps, providerType)
	if err != nil {
		return ProviderProfile{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return ProviderProfile{}, apperr.Validation("供应商名称不能为空")
	}
	normalizedURL := trimPtr(baseURL)
	if err := validateConnection(providerType, normalizedURL); err != nil {
		return ProviderProfile{}, err
	}
	id := clockid.New()
	capsJSON, _ := json.Marshal(caps)
	modelsJSON, _ := json.Marshal(orEmptyMap(defaults))
	cfgJSON, _ := json.Marshal(orEmptyMap(cfg))
	now := time.Now().UTC()
	row := schema.ProviderProfiles{
		ID: id, Name: name, ProviderType: providerType,
		BaseURL: normalizedURL, APIKey: trimPtr(apiKey),
		CapabilitiesJSON: string(capsJSON), DefaultModelsJSON: string(modelsJSON), ConfigJSON: string(cfgJSON),
		Enabled: enabled, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return ProviderProfile{}, err
	}
	return s.getProfile(ctx, id)
}

func (s *Store) UpdateProfile(ctx context.Context, id string, fields map[string]json.RawMessage) (ProviderProfile, error) {
	var out ProviderProfile
	err := tx.WithGorm(ctx, s.db, func(dbTx *gorm.DB) error {
		current, err := s.getProfileRow(ctx, dbTx, id)
		if err != nil {
			return err
		}
		if current.ArchivedAt != nil {
			return apperr.Validation("供应商不存在")
		}
		if _, ok := fields["name"]; ok {
			var name string
			if err := json.Unmarshal(fields["name"], &name); err != nil {
				return apperr.Validation("请求体无效")
			}
			current.Name = strings.TrimSpace(name)
			if current.Name == "" {
				return apperr.Validation("供应商名称不能为空")
			}
		}
		if _, ok := fields["provider_type"]; ok {
			var raw string
			if err := json.Unmarshal(fields["provider_type"], &raw); err != nil {
				return apperr.Validation("请求体无效")
			}
			current.ProviderType, err = normalizeProviderType(raw)
			if err != nil {
				return err
			}
		}
		if _, ok := fields["base_url"]; ok {
			var raw *string
			if err := json.Unmarshal(fields["base_url"], &raw); err != nil {
				return apperr.Validation("请求体无效")
			}
			current.BaseURL = trimPtr(raw)
		}
		if _, ok := fields["api_key"]; ok {
			var raw *string
			if err := json.Unmarshal(fields["api_key"], &raw); err != nil {
				return apperr.Validation("请求体无效")
			}
			if trimmed := trimPtr(raw); trimmed != nil {
				current.apiKey = trimmed
			}
		}
		if _, ok := fields["capabilities"]; ok {
			var caps []string
			if err := json.Unmarshal(fields["capabilities"], &caps); err != nil {
				return apperr.Validation("请求体无效")
			}
			current.Capabilities, err = normalizeCapabilities(caps, current.ProviderType)
			if err != nil {
				return err
			}
		} else if err := validateCapsForType(current.Capabilities, current.ProviderType); err != nil {
			return err
		}
		if err := validateConnection(current.ProviderType, current.BaseURL); err != nil {
			return err
		}
		if _, ok := fields["default_models"]; ok {
			var models map[string]any
			if err := json.Unmarshal(fields["default_models"], &models); err != nil {
				return apperr.Validation("请求体无效")
			}
			current.DefaultModels = orEmptyMap(models)
		}
		if _, ok := fields["config"]; ok {
			var cfg map[string]any
			if err := json.Unmarshal(fields["config"], &cfg); err != nil {
				return apperr.Validation("请求体无效")
			}
			current.Config = orEmptyMap(cfg)
		}
		if _, ok := fields["enabled"]; ok {
			var enabled bool
			if err := json.Unmarshal(fields["enabled"], &enabled); err != nil {
				return apperr.Validation("请求体无效")
			}
			current.Enabled = enabled
		}
		if err := s.validateProfileKeepsBindings(ctx, dbTx, current); err != nil {
			return err
		}
		capsJSON, _ := json.Marshal(current.Capabilities)
		modelsJSON, _ := json.Marshal(current.DefaultModels)
		cfgJSON, _ := json.Marshal(current.Config)
		updates := map[string]any{
			"name":                current.Name,
			"provider_type":       current.ProviderType,
			"base_url":            current.BaseURL,
			"capabilities_json":   string(capsJSON),
			"default_models_json": string(modelsJSON),
			"config_json":         string(cfgJSON),
			"enabled":             current.Enabled,
			"updated_at":          time.Now().UTC(),
		}
		if current.apiKey != nil {
			updates["api_key"] = current.apiKey
		}
		if err := dbTx.Model(&schema.ProviderProfiles{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return err
		}
		updated, err := s.getProfileRow(ctx, dbTx, id)
		if err != nil {
			return err
		}
		out = updated.ProviderProfile
		return nil
	})
	if err != nil {
		return ProviderProfile{}, err
	}
	return out, nil
}

func (s *Store) ArchiveProfile(ctx context.Context, id string) (ProviderProfile, error) {
	var n int64
	if err := s.db.WithContext(ctx).Model(&schema.ProviderBindings{}).Where("provider_profile_id = ?", id).Count(&n).Error; err != nil {
		return ProviderProfile{}, err
	}
	if n > 0 {
		return ProviderProfile{}, apperr.Validation("供应商仍被提示词、图片或 Agent 配置使用，不能归档")
	}
	now := time.Now().UTC()
	res := s.db.WithContext(ctx).Model(&schema.ProviderProfiles{}).
		Where("id = ? AND archived_at IS NULL", id).
		Updates(map[string]any{"archived_at": now, "enabled": false, "updated_at": now})
	if res.Error != nil {
		return ProviderProfile{}, res.Error
	}
	if res.RowsAffected == 0 {
		return ProviderProfile{}, apperr.Validation("供应商不存在")
	}
	return s.getProfile(ctx, id)
}

func (s *Store) UpdateBinding(ctx context.Context, purpose, kind string, profileID *string, modelSettings, cfg map[string]any) (ProviderBindingView, error) {
	if err := s.ensureBindings(ctx); err != nil {
		return ProviderBindingView{}, err
	}
	allowed, ok := kindsByPurpose[purpose]
	if !ok {
		return ProviderBindingView{}, apperr.Validation("用途必须是 prompt、agent 或 image")
	}
	if _, ok := allowed[kind]; !ok {
		return ProviderBindingView{}, apperr.Validation("供应商接口类型不支持当前用途")
	}
	if err := validateBindingRuntime(purpose, kind, modelSettings, cfg); err != nil {
		return ProviderBindingView{}, err
	}
	if kind == "mock" {
		profileID = nil
	} else {
		if profileID == nil || strings.TrimSpace(*profileID) == "" {
			return ProviderBindingView{}, apperr.Validation("真实供应商必须选择供应商档案")
		}
		profile, err := s.getProfileRow(ctx, s.db, *profileID)
		if err != nil {
			return ProviderBindingView{}, err
		}
		if !profile.Enabled {
			return ProviderBindingView{}, apperr.Validation("供应商已停用")
		}
		need := capabilityByKind[kind]
		if !contains(profile.Capabilities, need) {
			return ProviderBindingView{}, apperr.Validation("供应商档案不支持当前接口能力")
		}
		if err := validateTypeCapability(profile.ProviderType, need); err != nil {
			return ProviderBindingView{}, err
		}
	}
	modelJSON, _ := json.Marshal(normalizeModelSettings(purpose, modelSettings))
	cfgJSON, _ := json.Marshal(normalizeBindingConfig(purpose, kind, cfg))
	if err := s.db.WithContext(ctx).Model(&schema.ProviderBindings{}).Where("purpose = ?", purpose).Updates(map[string]any{
		"provider_kind":       kind,
		"provider_profile_id": profileID,
		"model_settings_json": string(modelJSON),
		"config_json":         string(cfgJSON),
		"updated_at":          time.Now().UTC(),
	}).Error; err != nil {
		return ProviderBindingView{}, err
	}
	return s.getBinding(ctx, purpose)
}

type profileRow struct {
	ProviderProfile
	apiKey *string
}

func (s *Store) getProfile(ctx context.Context, id string) (ProviderProfile, error) {
	row, err := s.getProfileRow(ctx, s.db, id)
	if err != nil {
		return ProviderProfile{}, err
	}
	return row.ProviderProfile, nil
}

func (s *Store) getProfileRow(ctx context.Context, dbTx *gorm.DB, id string) (profileRow, error) {
	var row schema.ProviderProfiles
	err := dbTx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Where("id = ?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return profileRow{}, apperr.Validation("供应商不存在")
	}
	if err != nil {
		return profileRow{}, err
	}
	return profileRow{ProviderProfile: profileFromModel(row), apiKey: row.APIKey}, nil
}

func (s *Store) getBinding(ctx context.Context, purpose string) (ProviderBindingView, error) {
	var row schema.ProviderBindings
	if err := s.db.WithContext(ctx).Where("purpose = ?", purpose).Take(&row).Error; err != nil {
		return ProviderBindingView{}, err
	}
	return bindingFromModel(row), nil
}

func (s *Store) validateProfileKeepsBindings(ctx context.Context, dbTx *gorm.DB, profile profileRow) error {
	var bindings []schema.ProviderBindings
	if err := dbTx.WithContext(ctx).Where("provider_profile_id = ?", profile.ID).Find(&bindings).Error; err != nil {
		return err
	}
	if len(bindings) == 0 {
		return nil
	}
	if !profile.Enabled {
		return apperr.Validation("供应商仍被提示词、图片或 Agent 配置使用，不能停用")
	}
	for _, binding := range bindings {
		if binding.ProviderKind == "mock" {
			continue
		}
		need := capabilityByKind[binding.ProviderKind]
		if !contains(profile.Capabilities, need) {
			return apperr.Validation("供应商仍被提示词、图片或 Agent 配置使用，不能移除当前接口能力")
		}
	}
	return nil
}

func normalizeProviderType(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		value = "openai_compatible"
	}
	if _, ok := providerTypes[value]; !ok {
		return "", apperr.Validation("供应商类型不支持")
	}
	return value, nil
}

func normalizeCapabilities(caps []string, providerType string) ([]string, error) {
	if len(caps) == 0 {
		return nil, apperr.Validation("供应商能力不能为空")
	}
	out := []string{}
	seen := map[string]struct{}{}
	for _, cap := range caps {
		if _, ok := capabilities[cap]; !ok {
			return nil, apperr.Validation("供应商能力不支持: " + cap)
		}
		if _, dup := seen[cap]; dup {
			continue
		}
		seen[cap] = struct{}{}
		out = append(out, cap)
	}
	return out, validateCapsForType(out, providerType)
}

func validateCapsForType(caps []string, providerType string) error {
	for _, cap := range caps {
		if err := validateTypeCapability(providerType, cap); err != nil {
			return err
		}
	}
	return nil
}

func validateTypeCapability(providerType, capability string) error {
	if providerType == "google_gemini" && capability != "image_google_gemini" {
		return apperr.Validation("Google Gemini 供应商档案只支持 Gemini 图片能力")
	}
	if providerType == "openai_compatible" && capability == "image_google_gemini" {
		return apperr.Validation("OpenAI 兼容供应商档案不支持 Gemini 图片能力")
	}
	return nil
}

func validateConnection(providerType string, baseURL *string) error {
	if providerType == "google_gemini" && baseURL != nil && *baseURL != "" {
		return apperr.Validation("Google Gemini 供应商暂不支持自定义 Base URL")
	}
	return nil
}

func validateBindingRuntime(purpose, kind string, model, cfg map[string]any) error {
	modelText := lookupMapString(model, "model")
	if purpose == "prompt" && modelText == "" {
		return apperr.Validation("提示词模型未配置")
	}
	if purpose == "agent" && modelText == "" {
		return apperr.Validation("工作流 Agent 模型未配置")
	}
	if purpose == "image" && modelText == "" {
		return apperr.Validation("图片模型未配置")
	}
	if kind == "openai_responses" {
		if _, ok := cfg["responses_background_enabled"]; !ok {
			return apperr.Validation("图片 Responses 后台响应模式未配置")
		}
	}
	if kind == "google_gemini_image" {
		ver := lookupMapString(cfg, "gemini_api_version")
		if ver == "" {
			ver = "v1beta"
		}
		if ver != "v1" && ver != "v1beta" {
			return apperr.Validation("Gemini API 版本必须是 v1 或 v1beta")
		}
	}
	return nil
}

func normalizeModelSettings(purpose string, model map[string]any) map[string]any {
	text := lookupMapString(model, "model")
	if purpose == "prompt" || purpose == "agent" {
		if text == "" {
			return map[string]any{}
		}
		return map[string]any{"model": text}
	}
	out := map[string]any{}
	for key, value := range model {
		if value != nil {
			out[key] = value
		}
	}
	return out
}

func normalizeBindingConfig(purpose, kind string, cfg map[string]any) map[string]any {
	if purpose == "agent" {
		out := map[string]any{}
		for _, key := range []string{"reasoning_effort", "reasoning_summary", "text_verbosity", "service_tier"} {
			if v := lookupMapString(cfg, key); v != "" {
				out[key] = v
			}
		}
		return out
	}
	if purpose != "image" {
		return map[string]any{}
	}
	switch kind {
	case "openai_responses":
		enabled, _ := cfg["responses_background_enabled"].(bool)
		return map[string]any{"responses_background_enabled": enabled}
	case "openai_images":
		out := map[string]any{}
		if v := lookupMapString(cfg, "images_quality"); v != "" {
			out["images_quality"] = v
		}
		if v := lookupMapString(cfg, "images_style"); v != "" {
			out["images_style"] = v
		}
		return out
	case "google_gemini_image":
		out := map[string]any{"gemini_api_version": "v1beta"}
		if v := lookupMapString(cfg, "gemini_api_version"); v != "" {
			out["gemini_api_version"] = v
		}
		if v := lookupMapString(cfg, "gemini_output_mime_type"); v != "" {
			out["gemini_output_mime_type"] = v
		}
		return out
	}
	return map[string]any{}
}

func decodeMap(raw []byte) map[string]any {
	out := map[string]any{}
	if len(raw) == 0 {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		return map[string]any{}
	}
	return out
}

func decodeStringSlice(raw []byte) []string {
	out := []string{}
	if len(raw) == 0 {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		return []string{}
	}
	return out
}

func orEmptyMap(v map[string]any) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	return v
}

func trimPtr(v *string) *string {
	if v == nil {
		return nil
	}
	s := strings.TrimSpace(*v)
	if s == "" {
		return nil
	}
	return &s
}

func lookupMapString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	raw, _ := values[key].(string)
	return strings.TrimSpace(raw)
}
