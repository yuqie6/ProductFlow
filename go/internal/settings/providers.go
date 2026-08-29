package settings

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/tx"
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
	return tx.With(ctx, s.pool, func(pgxTx pgx.Tx) error {
		for _, item := range defaults {
			var exists int
			if err := pgxTx.QueryRow(ctx, `SELECT COUNT(1) FROM provider_bindings WHERE purpose = $1`, item.purpose).Scan(&exists); err != nil {
				return err
			}
			if exists > 0 {
				continue
			}
			settingsJSON, _ := json.Marshal(map[string]any{"model": item.model})
			empty := []byte("{}")
			if _, err := pgxTx.Exec(ctx, `
				INSERT INTO provider_bindings (id, purpose, provider_kind, model_settings_json, config_json, created_at, updated_at)
				VALUES ($1, $2, 'mock', $3, $4, NOW(), NOW())
			`, clockid.New(), item.purpose, settingsJSON, empty); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) listProfiles(ctx context.Context) ([]ProviderProfile, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, provider_type, base_url, capabilities_json, default_models_json, config_json,
		       enabled, archived_at, api_key, created_at, updated_at
		FROM provider_profiles ORDER BY created_at, name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProviderProfile{}
	for rows.Next() {
		profile, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, profile)
	}
	return out, rows.Err()
}

func (s *Store) listBindings(ctx context.Context) ([]ProviderBindingView, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, purpose, provider_kind, provider_profile_id, model_settings_json, config_json, created_at, updated_at
		FROM provider_bindings ORDER BY purpose
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProviderBindingView{}
	for rows.Next() {
		var row ProviderBindingView
		var modelRaw, configRaw []byte
		var created, updated time.Time
		if err := rows.Scan(&row.ID, &row.Purpose, &row.ProviderKind, &row.ProviderProfileID, &modelRaw, &configRaw, &created, &updated); err != nil {
			return nil, err
		}
		row.ModelSettings = decodeMap(modelRaw)
		row.Config = decodeMap(configRaw)
		row.CreatedAt = created.UTC().Format(time.RFC3339Nano)
		row.UpdatedAt = updated.UTC().Format(time.RFC3339Nano)
		out = append(out, row)
	}
	return out, rows.Err()
}

type profileScanner interface {
	Scan(dest ...any) error
}

func scanProfile(row profileScanner) (ProviderProfile, error) {
	var p ProviderProfile
	var baseURL, apiKey *string
	var archived *time.Time
	var caps, models, cfg []byte
	var created, updated time.Time
	if err := row.Scan(&p.ID, &p.Name, &p.ProviderType, &baseURL, &caps, &models, &cfg, &p.Enabled, &archived, &apiKey, &created, &updated); err != nil {
		return ProviderProfile{}, err
	}
	p.BaseURL = emptyToNil(baseURL)
	p.Capabilities = decodeStringSlice(caps)
	p.DefaultModels = decodeMap(models)
	p.Config = decodeMap(cfg)
	p.HasAPIKey = apiKey != nil && strings.TrimSpace(*apiKey) != ""
	if archived != nil {
		stamp := archived.UTC().Format(time.RFC3339Nano)
		p.ArchivedAt = &stamp
	}
	p.CreatedAt = created.UTC().Format(time.RFC3339Nano)
	p.UpdatedAt = updated.UTC().Format(time.RFC3339Nano)
	return p, nil
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
	_, err = s.pool.Exec(ctx, `
		INSERT INTO provider_profiles (
			id, name, provider_type, base_url, api_key, capabilities_json, default_models_json, config_json, enabled, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW(),NOW())
	`, id, name, providerType, normalizedURL, trimPtr(apiKey), capsJSON, modelsJSON, cfgJSON, enabled)
	if err != nil {
		return ProviderProfile{}, err
	}
	return s.getProfile(ctx, id)
}

func (s *Store) UpdateProfile(ctx context.Context, id string, fields map[string]json.RawMessage) (ProviderProfile, error) {
	current, err := s.getProfileRow(ctx, id)
	if err != nil {
		return ProviderProfile{}, err
	}
	if current.ArchivedAt != nil {
		return ProviderProfile{}, apperr.Validation("供应商不存在")
	}
	if _, ok := fields["name"]; ok {
		var name string
		if err := json.Unmarshal(fields["name"], &name); err != nil {
			return ProviderProfile{}, apperr.Validation("请求体无效")
		}
		current.Name = strings.TrimSpace(name)
		if current.Name == "" {
			return ProviderProfile{}, apperr.Validation("供应商名称不能为空")
		}
	}
	if _, ok := fields["provider_type"]; ok {
		var raw string
		if err := json.Unmarshal(fields["provider_type"], &raw); err != nil {
			return ProviderProfile{}, apperr.Validation("请求体无效")
		}
		current.ProviderType, err = normalizeProviderType(raw)
		if err != nil {
			return ProviderProfile{}, err
		}
	}
	if _, ok := fields["base_url"]; ok {
		var raw *string
		if err := json.Unmarshal(fields["base_url"], &raw); err != nil {
			return ProviderProfile{}, apperr.Validation("请求体无效")
		}
		current.BaseURL = trimPtr(raw)
	}
	if _, ok := fields["api_key"]; ok {
		var raw *string
		if err := json.Unmarshal(fields["api_key"], &raw); err != nil {
			return ProviderProfile{}, apperr.Validation("请求体无效")
		}
		if trimmed := trimPtr(raw); trimmed != nil {
			current.apiKey = trimmed
		}
	}
	if _, ok := fields["capabilities"]; ok {
		var caps []string
		if err := json.Unmarshal(fields["capabilities"], &caps); err != nil {
			return ProviderProfile{}, apperr.Validation("请求体无效")
		}
		current.Capabilities, err = normalizeCapabilities(caps, current.ProviderType)
		if err != nil {
			return ProviderProfile{}, err
		}
	} else if err := validateCapsForType(current.Capabilities, current.ProviderType); err != nil {
		return ProviderProfile{}, err
	}
	if err := validateConnection(current.ProviderType, current.BaseURL); err != nil {
		return ProviderProfile{}, err
	}
	if _, ok := fields["default_models"]; ok {
		var models map[string]any
		if err := json.Unmarshal(fields["default_models"], &models); err != nil {
			return ProviderProfile{}, apperr.Validation("请求体无效")
		}
		current.DefaultModels = orEmptyMap(models)
	}
	if _, ok := fields["config"]; ok {
		var cfg map[string]any
		if err := json.Unmarshal(fields["config"], &cfg); err != nil {
			return ProviderProfile{}, apperr.Validation("请求体无效")
		}
		current.Config = orEmptyMap(cfg)
	}
	if _, ok := fields["enabled"]; ok {
		var enabled bool
		if err := json.Unmarshal(fields["enabled"], &enabled); err != nil {
			return ProviderProfile{}, apperr.Validation("请求体无效")
		}
		current.Enabled = enabled
	}
	if err := s.validateProfileKeepsBindings(ctx, current); err != nil {
		return ProviderProfile{}, err
	}
	capsJSON, _ := json.Marshal(current.Capabilities)
	modelsJSON, _ := json.Marshal(current.DefaultModels)
	cfgJSON, _ := json.Marshal(current.Config)
	_, err = s.pool.Exec(ctx, `
		UPDATE provider_profiles SET
			name=$2, provider_type=$3, base_url=$4, api_key=COALESCE($5, api_key),
			capabilities_json=$6, default_models_json=$7, config_json=$8, enabled=$9, updated_at=NOW()
		WHERE id=$1
	`, id, current.Name, current.ProviderType, current.BaseURL, current.apiKey, capsJSON, modelsJSON, cfgJSON, current.Enabled)
	if err != nil {
		return ProviderProfile{}, err
	}
	return s.getProfile(ctx, id)
}

func (s *Store) ArchiveProfile(ctx context.Context, id string) (ProviderProfile, error) {
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(1) FROM provider_bindings WHERE provider_profile_id = $1`, id).Scan(&n); err != nil {
		return ProviderProfile{}, err
	}
	if n > 0 {
		return ProviderProfile{}, apperr.Validation("供应商仍被提示词、图片或 Agent 配置使用，不能归档")
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE provider_profiles SET archived_at = NOW(), enabled = FALSE, updated_at = NOW()
		WHERE id = $1 AND archived_at IS NULL
	`, id)
	if err != nil {
		return ProviderProfile{}, err
	}
	if tag.RowsAffected() == 0 {
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
		profile, err := s.getProfileRow(ctx, *profileID)
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
	_, err := s.pool.Exec(ctx, `
		UPDATE provider_bindings SET
			provider_kind=$2, provider_profile_id=$3, model_settings_json=$4, config_json=$5, updated_at=NOW()
		WHERE purpose=$1
	`, purpose, kind, profileID, modelJSON, cfgJSON)
	if err != nil {
		return ProviderBindingView{}, err
	}
	return s.getBinding(ctx, purpose)
}

type profileRow struct {
	ProviderProfile
	apiKey *string
}

func (s *Store) getProfile(ctx context.Context, id string) (ProviderProfile, error) {
	row, err := s.getProfileRow(ctx, id)
	if err != nil {
		return ProviderProfile{}, err
	}
	return row.ProviderProfile, nil
}

func (s *Store) getProfileRow(ctx context.Context, id string) (profileRow, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, name, provider_type, base_url, capabilities_json, default_models_json, config_json,
		       enabled, archived_at, api_key, created_at, updated_at
		FROM provider_profiles WHERE id = $1
	`, id)
	profile, err := scanProfile(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return profileRow{}, apperr.Validation("供应商不存在")
		}
		return profileRow{}, err
	}
	var apiKey *string
	_ = s.pool.QueryRow(ctx, `SELECT api_key FROM provider_profiles WHERE id = $1`, id).Scan(&apiKey)
	return profileRow{ProviderProfile: profile, apiKey: apiKey}, nil
}

func (s *Store) getBinding(ctx context.Context, purpose string) (ProviderBindingView, error) {
	var row ProviderBindingView
	var modelRaw, configRaw []byte
	var created, updated time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT id, purpose, provider_kind, provider_profile_id, model_settings_json, config_json, created_at, updated_at
		FROM provider_bindings WHERE purpose = $1
	`, purpose).Scan(&row.ID, &row.Purpose, &row.ProviderKind, &row.ProviderProfileID, &modelRaw, &configRaw, &created, &updated)
	if err != nil {
		return ProviderBindingView{}, err
	}
	row.ModelSettings = decodeMap(modelRaw)
	row.Config = decodeMap(configRaw)
	row.CreatedAt = created.UTC().Format(time.RFC3339Nano)
	row.UpdatedAt = updated.UTC().Format(time.RFC3339Nano)
	return row, nil
}

func (s *Store) validateProfileKeepsBindings(ctx context.Context, profile profileRow) error {
	rows, err := s.pool.Query(ctx, `SELECT provider_kind FROM provider_bindings WHERE provider_profile_id = $1`, profile.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	kinds := []string{}
	for rows.Next() {
		var kind string
		if err := rows.Scan(&kind); err != nil {
			return err
		}
		kinds = append(kinds, kind)
	}
	if len(kinds) == 0 {
		return nil
	}
	if !profile.Enabled {
		return apperr.Validation("供应商仍被提示词、图片或 Agent 配置使用，不能停用")
	}
	for _, kind := range kinds {
		if kind == "mock" {
			continue
		}
		need := capabilityByKind[kind]
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
