package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

type SettingsExport struct {
	Metadata struct {
		SchemaVersion int       `json:"schema_version"`
		ExportedAt    time.Time `json:"exported_at"`
		App           string    `json:"app"`
		AppVersion    string    `json:"app_version"`
		Compatibility string    `json:"compatibility"`
	} `json:"metadata"`
	RuntimeConfig    map[string]any   `json:"runtime_config"`
	ProviderProfiles []map[string]any `json:"provider_profiles"`
	ProviderBindings []map[string]any `json:"provider_bindings"`
}

type ImportPreview struct {
	SchemaVersion                   int      `json:"schema_version"`
	RuntimeConfigCount              int      `json:"runtime_config_count"`
	ProviderProfileCount            int      `json:"provider_profile_count"`
	ProviderBindingCount            int      `json:"provider_binding_count"`
	ProviderProfileNames            []string `json:"provider_profile_names"`
	ProviderBindingPurposes         []string `json:"provider_binding_purposes"`
	IncludesAPIKeys                 bool     `json:"includes_api_keys"`
	ProviderProfilesWithAPIKeyCount int      `json:"provider_profiles_with_api_key_count"`
}

func (s *Store) Export(ctx context.Context) (SettingsExport, error) {
	view, err := s.ConfigView(ctx)
	if err != nil {
		return SettingsExport{}, err
	}
	runtime := map[string]any{}
	for _, item := range view.Items {
		runtime[item.Key] = item.Value
	}
	profiles, err := s.listProfiles(ctx)
	if err != nil {
		return SettingsExport{}, err
	}
	bindings, err := s.listBindings(ctx)
	if err != nil {
		return SettingsExport{}, err
	}
	out := SettingsExport{}
	out.Metadata.SchemaVersion = exportSchemaVersion
	out.Metadata.ExportedAt = time.Now().UTC()
	out.Metadata.App = "ProductFlow"
	out.Metadata.AppVersion = exportAppVersion
	out.Metadata.Compatibility = exportCompatibility
	out.RuntimeConfig = runtime
	out.ProviderProfiles = []map[string]any{}
	for _, profile := range profiles {
		if profile.ArchivedAt != nil {
			continue
		}
		var apiKey *string
		_ = pfdb.QueryRow(ctx, s.db, `SELECT api_key FROM provider_profiles WHERE id = $1`, profile.ID).Scan(&apiKey)
		out.ProviderProfiles = append(out.ProviderProfiles, map[string]any{
			"id": profile.ID, "name": profile.Name, "provider_type": profile.ProviderType,
			"base_url": profile.BaseURL, "api_key": trimPtr(apiKey),
			"capabilities": profile.Capabilities, "default_models": profile.DefaultModels,
			"config": profile.Config, "enabled": profile.Enabled,
		})
	}
	out.ProviderBindings = []map[string]any{}
	for _, binding := range bindings {
		out.ProviderBindings = append(out.ProviderBindings, map[string]any{
			"purpose": binding.Purpose, "provider_kind": binding.ProviderKind,
			"provider_profile_id": binding.ProviderProfileID,
			"model_settings":      binding.ModelSettings, "config": binding.Config,
		})
	}
	return out, nil
}

type importProfile struct {
	ID            string
	Name          string
	ProviderType  string
	BaseURL       *string
	APIKey        *string
	Capabilities  []string
	DefaultModels map[string]any
	Config        map[string]any
	Enabled       bool
}

type importBinding struct {
	Purpose           string
	ProviderKind      string
	ProviderProfileID *string
	ModelSettings     map[string]any
	Config            map[string]any
}

type importBundle struct {
	version  int
	runtime  map[string]string
	profiles []importProfile
	bindings []importBinding
}

func (s *Store) PreviewImport(doc map[string]any) (ImportPreview, map[string]any, error) {
	bundle, err := normalizeImportDocument(doc)
	if err != nil {
		return ImportPreview{}, nil, err
	}
	names := make([]string, 0, len(bundle.profiles))
	keyCount := 0
	for _, profile := range bundle.profiles {
		names = append(names, profile.Name)
		if profile.APIKey != nil && *profile.APIKey != "" {
			keyCount++
		}
	}
	purposes := make([]string, 0, len(bundle.bindings))
	for _, binding := range bundle.bindings {
		purposes = append(purposes, binding.Purpose)
	}
	preview := ImportPreview{
		SchemaVersion: bundle.version, RuntimeConfigCount: len(bundle.runtime),
		ProviderProfileCount: len(bundle.profiles), ProviderBindingCount: len(bundle.bindings),
		ProviderProfileNames: names, ProviderBindingPurposes: sortedUnique(purposes),
		IncludesAPIKeys: keyCount > 0, ProviderProfilesWithAPIKeyCount: keyCount,
	}
	return preview, bundle.wire(doc["metadata"]), nil
}

func (s *Store) ApplyImport(ctx context.Context, doc map[string]any) error {
	bundle, err := normalizeImportDocument(doc)
	if err != nil {
		return err
	}
	return tx.WithGorm(ctx, s.db, func(dbTx *gorm.DB) error {
		for key, value := range bundle.runtime {
			if _, err := pfdb.Exec(ctx, dbTx, `
				INSERT INTO app_settings (key, value, created_at, updated_at)
				VALUES ($1, $2, NOW(), NOW())
				ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
			`, key, value); err != nil {
				return err
			}
		}
		if _, err := pfdb.Exec(ctx, dbTx, `DELETE FROM provider_bindings`); err != nil {
			return err
		}
		if _, err := pfdb.Exec(ctx, dbTx, `DELETE FROM provider_profiles`); err != nil {
			return err
		}
		for _, profile := range bundle.profiles {
			caps, _ := json.Marshal(profile.Capabilities)
			models, _ := json.Marshal(orEmptyMap(profile.DefaultModels))
			cfg, _ := json.Marshal(orEmptyMap(profile.Config))
			if _, err := pfdb.Exec(ctx, dbTx, `
				INSERT INTO provider_profiles (
					id, name, provider_type, base_url, api_key, capabilities_json, default_models_json, config_json, enabled, created_at, updated_at
				) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW(),NOW())
			`, profile.ID, profile.Name, profile.ProviderType, profile.BaseURL, profile.APIKey, caps, models, cfg, profile.Enabled); err != nil {
				return err
			}
		}
		for _, binding := range bundle.bindings {
			modelJSON, _ := json.Marshal(orEmptyMap(binding.ModelSettings))
			cfgJSON, _ := json.Marshal(orEmptyMap(binding.Config))
			id := clockid.New()
			if _, err := pfdb.Exec(ctx, dbTx, `
				INSERT INTO provider_bindings (id, purpose, provider_kind, provider_profile_id, model_settings_json, config_json, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
			`, id, binding.Purpose, binding.ProviderKind, binding.ProviderProfileID, modelJSON, cfgJSON); err != nil {
				return err
			}
		}
		return nil
	})
}

func normalizeImportDocument(doc map[string]any) (importBundle, error) {
	if doc == nil {
		return importBundle{}, apperr.Validation("配置文件格式不正确")
	}
	meta, _ := doc["metadata"].(map[string]any)
	if meta == nil {
		return importBundle{}, apperr.Validation("配置文件格式不正确")
	}
	version := toIntOr(meta["schema_version"], 0)
	compat, _ := meta["compatibility"].(string)
	if version != exportSchemaVersion {
		return importBundle{}, apperr.Validation("配置文件版本不支持")
	}
	if compat != exportCompatibility {
		return importBundle{}, apperr.Validation("配置文件兼容标识不支持")
	}
	runtime, err := normalizeImportRuntime(doc["runtime_config"])
	if err != nil {
		return importBundle{}, err
	}
	profiles, err := normalizeImportProfiles(doc["provider_profiles"])
	if err != nil {
		return importBundle{}, err
	}
	bindings, err := normalizeImportBindings(doc["provider_bindings"], profiles)
	if err != nil {
		return importBundle{}, err
	}
	return importBundle{version: version, runtime: runtime, profiles: profiles, bindings: bindings}, nil
}

func normalizeImportRuntime(raw any) (map[string]string, error) {
	runtime, _ := raw.(map[string]any)
	if runtime == nil {
		return nil, apperr.Validation("配置文件格式不正确")
	}
	unknown := []string{}
	missing := []string{}
	defs := map[string]struct{}{}
	for _, def := range configDefinitions() {
		defs[def.Key] = struct{}{}
		if _, ok := runtime[def.Key]; !ok {
			missing = append(missing, def.Key)
		}
	}
	for key := range runtime {
		if _, ok := defs[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		return nil, apperr.Validation("未知配置项: " + strings.Join(sortedUnique(unknown), ", "))
	}
	if len(missing) > 0 {
		return nil, apperr.Validation("配置文件缺少配置项: " + strings.Join(sortedUnique(missing), ", "))
	}
	normalized := map[string]string{}
	for key, value := range runtime {
		text, err := normalizeConfigValue(key, value)
		if err != nil {
			return nil, err
		}
		normalized[key] = text
	}
	if err := validateMerged(normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}

func normalizeImportProfiles(raw any) ([]importProfile, error) {
	items, err := asObjectList(raw)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	out := make([]importProfile, 0, len(items))
	for _, item := range items {
		id := anyText(item["id"])
		if id == "" {
			return nil, apperr.Validation("供应商档案 ID 不能为空")
		}
		if _, dup := seen[id]; dup {
			return nil, apperr.Validation("供应商档案不能重复")
		}
		seen[id] = struct{}{}
		providerTypeRaw := anyText(item["provider_type"])
		if strings.TrimSpace(providerTypeRaw) == "" {
			return nil, apperr.Validation("供应商类型不支持")
		}
		providerType, err := normalizeProviderType(providerTypeRaw)
		if err != nil {
			return nil, err
		}
		caps, err := normalizeCapabilities(anyStringSlice(item["capabilities"]), providerType)
		if err != nil {
			return nil, err
		}
		name := anyText(item["name"])
		if name == "" {
			return nil, apperr.Validation("供应商名称不能为空")
		}
		baseURL := optionalTextPtr(item["base_url"])
		if err := validateConnection(providerType, baseURL); err != nil {
			return nil, err
		}
		out = append(out, importProfile{
			ID: id, Name: name, ProviderType: providerType, BaseURL: baseURL,
			APIKey: optionalTextPtr(item["api_key"]), Capabilities: caps,
			DefaultModels: orEmptyMap(asMap(item["default_models"])),
			Config:        orEmptyMap(asMap(item["config"])),
			Enabled:       lookupBoolDefault(item, "enabled", true),
		})
	}
	return out, nil
}

func normalizeImportBindings(raw any, profiles []importProfile) ([]importBinding, error) {
	items, err := asObjectList(raw)
	if err != nil {
		return nil, err
	}
	profilesByID := map[string]importProfile{}
	for _, profile := range profiles {
		profilesByID[profile.ID] = profile
	}
	seen := map[string]struct{}{}
	out := make([]importBinding, 0, len(items))
	for _, item := range items {
		purpose := anyText(item["purpose"])
		if _, dup := seen[purpose]; dup {
			return nil, apperr.Validation("供应商用途绑定不能重复")
		}
		seen[purpose] = struct{}{}
		allowed, ok := kindsByPurpose[purpose]
		if !ok {
			return nil, apperr.Validation("用途必须是 prompt、agent 或 image")
		}
		kind := anyText(item["provider_kind"])
		if _, ok := allowed[kind]; !ok {
			return nil, apperr.Validation("供应商接口类型不支持当前用途")
		}
		modelSettings := orEmptyMap(asMap(item["model_settings"]))
		cfg := orEmptyMap(asMap(item["config"]))
		if err := validateBindingRuntime(purpose, kind, modelSettings, cfg); err != nil {
			return nil, err
		}
		var profileID *string
		if kind == "mock" {
			profileID = nil
		} else {
			profileID = optionalTextPtr(item["provider_profile_id"])
			if profileID == nil {
				return nil, apperr.Validation("真实供应商必须选择供应商档案")
			}
			profile, exists := profilesByID[*profileID]
			if !exists {
				return nil, apperr.Validation("供应商不存在")
			}
			if !profile.Enabled {
				return nil, apperr.Validation("供应商已停用")
			}
			need := capabilityByKind[kind]
			if !contains(profile.Capabilities, need) {
				return nil, apperr.Validation("供应商档案不支持当前接口能力")
			}
			if err := validateTypeCapability(profile.ProviderType, need); err != nil {
				return nil, err
			}
		}
		out = append(out, importBinding{
			Purpose: purpose, ProviderKind: kind, ProviderProfileID: profileID,
			ModelSettings: normalizeModelSettings(purpose, modelSettings),
			Config:        normalizeBindingConfig(purpose, kind, cfg),
		})
	}
	missing := []string{}
	for purpose := range kindsByPurpose {
		if _, ok := seen[purpose]; !ok {
			missing = append(missing, purpose)
		}
	}
	if len(missing) > 0 {
		return nil, apperr.Validation("配置文件缺少供应商绑定: " + strings.Join(sortedUnique(missing), ", "))
	}
	return out, nil
}

func (b importBundle) wire(meta any) map[string]any {
	runtime := map[string]any{}
	for key, value := range b.runtime {
		runtime[key] = value
	}
	profiles := make([]any, 0, len(b.profiles))
	for _, profile := range b.profiles {
		item := map[string]any{
			"id": profile.ID, "name": profile.Name, "provider_type": profile.ProviderType,
			"base_url": anyOrNil(profile.BaseURL), "api_key": anyOrNil(profile.APIKey),
			"capabilities": profile.Capabilities, "default_models": orEmptyMap(profile.DefaultModels),
			"config": orEmptyMap(profile.Config), "enabled": profile.Enabled,
		}
		profiles = append(profiles, item)
	}
	bindings := make([]any, 0, len(b.bindings))
	for _, binding := range b.bindings {
		item := map[string]any{
			"purpose": binding.Purpose, "provider_kind": binding.ProviderKind,
			"provider_profile_id": anyOrNil(binding.ProviderProfileID),
			"model_settings":      orEmptyMap(binding.ModelSettings),
			"config":              orEmptyMap(binding.Config),
		}
		bindings = append(bindings, item)
	}
	return map[string]any{
		"metadata": meta, "runtime_config": runtime,
		"provider_profiles": profiles, "provider_bindings": bindings,
	}
}

func asObjectList(v any) ([]map[string]any, error) {
	if v == nil {
		return []map[string]any{}, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, apperr.Validation("配置文件格式不正确")
	}
	out := make([]map[string]any, 0, len(list))
	for _, raw := range list {
		item, ok := raw.(map[string]any)
		if !ok {
			return nil, apperr.Validation("配置文件格式不正确")
		}
		out = append(out, item)
	}
	return out, nil
}

func anyText(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func anyStringSlice(v any) []string {
	switch typed := v.(type) {
	case nil:
		return nil
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, strings.TrimSpace(item))
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, anyText(item))
		}
		return out
	default:
		return nil
	}
}

func optionalTextPtr(v any) *string {
	s := anyText(v)
	if s == "" {
		return nil
	}
	return &s
}

func anyOrNil(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

func lookupBoolDefault(item map[string]any, key string, fallback bool) bool {
	v, ok := item[key]
	if !ok || v == nil {
		return fallback
	}
	if typed, ok := v.(bool); ok {
		return typed
	}
	return fallback
}

func asMap(v any) map[string]any {
	out, _ := v.(map[string]any)
	return out
}

func toIntOr(v any, fallback int) int {
	n, err := toInt(v)
	if err != nil {
		return fallback
	}
	return n
}
