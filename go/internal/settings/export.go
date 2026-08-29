package settings

import (
	"context"
	"encoding/json"
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

func (s *Store) PreviewImport(doc map[string]any) (ImportPreview, map[string]any, error) {
	meta, _ := doc["metadata"].(map[string]any)
	if meta == nil {
		return ImportPreview{}, nil, apperr.Validation("配置文件格式不正确")
	}
	version := toIntOr(meta["schema_version"], 0)
	compat, _ := meta["compatibility"].(string)
	if version != exportSchemaVersion {
		return ImportPreview{}, nil, apperr.Validation("配置文件版本不支持")
	}
	if compat != exportCompatibility {
		return ImportPreview{}, nil, apperr.Validation("配置文件兼容标识不支持")
	}
	runtime, _ := doc["runtime_config"].(map[string]any)
	if runtime == nil {
		return ImportPreview{}, nil, apperr.Validation("配置文件格式不正确")
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
		return ImportPreview{}, nil, apperr.Validation("未知配置项: " + strings.Join(sortedUnique(unknown), ", "))
	}
	if len(missing) > 0 {
		return ImportPreview{}, nil, apperr.Validation("配置文件缺少配置项: " + strings.Join(sortedUnique(missing), ", "))
	}
	profiles, _ := doc["provider_profiles"].([]any)
	bindings, _ := doc["provider_bindings"].([]any)
	names := []string{}
	keyCount := 0
	for _, raw := range profiles {
		item, _ := raw.(map[string]any)
		name, _ := item["name"].(string)
		names = append(names, name)
		if lookupMapString(item, "api_key") != "" {
			keyCount++
		}
	}
	purposes := []string{}
	for _, raw := range bindings {
		item, _ := raw.(map[string]any)
		if p, _ := item["purpose"].(string); p != "" {
			purposes = append(purposes, p)
		}
	}
	preview := ImportPreview{
		SchemaVersion: version, RuntimeConfigCount: len(runtime),
		ProviderProfileCount: len(profiles), ProviderBindingCount: len(bindings),
		ProviderProfileNames: names, ProviderBindingPurposes: sortedUnique(purposes),
		IncludesAPIKeys: keyCount > 0, ProviderProfilesWithAPIKeyCount: keyCount,
	}
	return preview, doc, nil
}

func (s *Store) ApplyImport(ctx context.Context, doc map[string]any) error {
	runtime, _ := doc["runtime_config"].(map[string]any)
	normalized := map[string]string{}
	for key, value := range runtime {
		text, err := normalizeConfigValue(key, value)
		if err != nil {
			return err
		}
		normalized[key] = text
	}
	profiles, _ := doc["provider_profiles"].([]any)
	bindings, _ := doc["provider_bindings"].([]any)
	return tx.WithGorm(ctx, s.db, func(dbTx *gorm.DB) error {
		for key, value := range normalized {
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
		for _, raw := range profiles {
			item, _ := raw.(map[string]any)
			id, _ := item["id"].(string)
			name, _ := item["name"].(string)
			ptype, _ := item["provider_type"].(string)
			caps, _ := json.Marshal(item["capabilities"])
			models, _ := json.Marshal(orEmptyMap(asMap(item["default_models"])))
			cfg, _ := json.Marshal(orEmptyMap(asMap(item["config"])))
			enabled, _ := item["enabled"].(bool)
			var baseURL *string
			if v, ok := item["base_url"].(string); ok {
				baseURL = trimPtr(&v)
			}
			var apiKey *string
			if v, ok := item["api_key"].(string); ok {
				apiKey = trimPtr(&v)
			}
			if _, err := pfdb.Exec(ctx, dbTx, `
				INSERT INTO provider_profiles (
					id, name, provider_type, base_url, api_key, capabilities_json, default_models_json, config_json, enabled, created_at, updated_at
				) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW(),NOW())
			`, id, name, ptype, baseURL, apiKey, caps, models, cfg, enabled); err != nil {
				return err
			}
		}
		for _, raw := range bindings {
			item, _ := raw.(map[string]any)
			purpose, _ := item["purpose"].(string)
			kind, _ := item["provider_kind"].(string)
			var profileID *string
			if v, ok := item["provider_profile_id"].(string); ok {
				profileID = trimPtr(&v)
			}
			modelJSON, _ := json.Marshal(orEmptyMap(asMap(item["model_settings"])))
			cfgJSON, _ := json.Marshal(orEmptyMap(asMap(item["config"])))
			id := clockid.New()
			if _, err := pfdb.Exec(ctx, dbTx, `
				INSERT INTO provider_bindings (id, purpose, provider_kind, provider_profile_id, model_settings_json, config_json, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
			`, id, purpose, kind, profileID, modelJSON, cfgJSON); err != nil {
				return err
			}
		}
		return nil
	})
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
