package settings

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

// SettingsExport 是可再导入的设置导出文档 HTTP 体。
// 仅站点 Operator 可导出。含实例级 provider API Key（Op 备份）；不含商家 BYOK/商家密钥字段（首版无商家密钥）。
// schema_version 必须对上才能导入。不要当 ConfigResponse 用。
type SettingsExport struct {
	// Metadata 含 schema_version；对不上不能导入。
	Metadata struct {
		SchemaVersion int       `json:"schema_version"`
		ExportedAt    time.Time `json:"exported_at"`
		App           string    `json:"app"`
		AppVersion    string    `json:"app_version"`
		Compatibility string    `json:"compatibility"`
	} `json:"metadata"`
	RuntimeConfig    map[string]any   `json:"runtime_config"`    // app_settings 键值，含可编辑运行时
	ProviderProfiles []map[string]any `json:"provider_profiles"` // 未归档档案，导出含实例 API Key；无商家密钥
	ProviderBindings []map[string]any `json:"provider_bindings"` // prompt/agent/image 用途行
}

// ImportPreview 是导入前的计数摘要 HTTP 体，还不写入。
// IncludesAPIKeys 只表示文档里有没有 Key，不是已经替换了现网档案。
type ImportPreview struct {
	SchemaVersion                   int      `json:"schema_version"`                       // 必须与导出文档一致才能导入
	RuntimeConfigCount              int      `json:"runtime_config_count"`                 // 将写入的 app_settings 条数
	ProviderProfileCount            int      `json:"provider_profile_count"`               // 文档中的档案数
	ProviderBindingCount            int      `json:"provider_binding_count"`               // 文档中的用途绑定数
	ProviderProfileNames            []string `json:"provider_profile_names"`               // 将导入的档案名
	ProviderBindingPurposes         []string `json:"provider_binding_purposes"`            // prompt | agent | image
	IncludesAPIKeys                 bool     `json:"includes_api_keys"`                    // 只表示文档里有 Key，不是已替换现网
	ProviderProfilesWithAPIKeyCount int      `json:"provider_profiles_with_api_key_count"` // 含 API Key 的档案数
}

// Export 导出运行时配置、未归档供应商档案与绑定（含 API Key）。
// 调用时机：GET /api/settings/export。已归档档案跳过。无写入。
// 读配置或档案失败、ctx 取消时返回 error。
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
		var row schema.ProviderProfiles
		if err := s.db.WithContext(ctx).Where("id = ?", profile.ID).Take(&row).Error; err == nil {
			apiKey = row.APIKey
		}
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

// PreviewImport 校验导入文档并返回预览，不写入。
// 调用时机：POST /import/preview。版本/键闭集不对返回 Validation。第二个返回值是规范化后的 wire 文档。
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

// ApplyImport 用已规范化文档整表替换运行时配置与供应商档案/绑定。
// 调用时机：POST /import 在 PreviewImport 成功之后。先删全部 bindings 和 profiles 再插入。
// 禁区：不要部分更新；失败应整事务回滚。env-only 密钥不会被本函数改写。
func (s *Store) ApplyImport(ctx context.Context, doc map[string]any) error {
	bundle, err := normalizeImportDocument(doc)
	if err != nil {
		return err
	}
	return tx.WithGorm(ctx, s.db, func(dbTx *gorm.DB) error {
		for key, value := range bundle.runtime {
			if err := upsertAppSetting(dbTx, key, value); err != nil {
				return err
			}
		}
		if err := dbTx.Where("id IS NOT NULL").Delete(&schema.ProviderBindings{}).Error; err != nil {
			return err
		}
		if err := dbTx.Where("id IS NOT NULL").Delete(&schema.ProviderProfiles{}).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		for _, profile := range bundle.profiles {
			caps, _ := json.Marshal(profile.Capabilities)
			models, _ := json.Marshal(orEmptyMap(profile.DefaultModels))
			cfg, _ := json.Marshal(orEmptyMap(profile.Config))
			row := schema.ProviderProfiles{
				ID: profile.ID, Name: profile.Name, ProviderType: profile.ProviderType,
				BaseURL: profile.BaseURL, APIKey: profile.APIKey,
				CapabilitiesJSON: string(caps), DefaultModelsJSON: string(models), ConfigJSON: string(cfg),
				Enabled: profile.Enabled, CreatedAt: now, UpdatedAt: now,
			}
			if err := dbTx.Create(&row).Error; err != nil {
				return err
			}
		}
		for _, binding := range bundle.bindings {
			modelJSON, _ := json.Marshal(orEmptyMap(binding.ModelSettings))
			cfgJSON, _ := json.Marshal(orEmptyMap(binding.Config))
			row := schema.ProviderBindings{
				ID: clockid.New(), Purpose: binding.Purpose, ProviderKind: binding.ProviderKind,
				ProviderProfileID: binding.ProviderProfileID,
				ModelSettingsJSON: string(modelJSON), ConfigJSON: string(cfgJSON),
				CreatedAt: now, UpdatedAt: now,
			}
			if err := dbTx.Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// normalizeImportDocument 校验导出文档：metadata 版本、runtime 键闭集、档案与绑定互相引用。
// 任一字段不对返回 400，不写入。nil doc 当格式错误。
func normalizeImportDocument(doc map[string]any) (importBundle, error) {
	if doc == nil {
		return importBundle{}, apperr.Validation("配置文件格式不正确")
	}
	version, err := validateImportMetadata(doc["metadata"])
	if err != nil {
		return importBundle{}, err
	}
	runtime, err := normalizeImportRuntime(doc["runtime_config"])
	if err != nil {
		return importBundle{}, err
	}
	profileItems, err := fieldObjectList(doc, "provider_profiles")
	if err != nil {
		return importBundle{}, err
	}
	profiles, err := normalizeImportProfiles(profileItems)
	if err != nil {
		return importBundle{}, err
	}
	bindingItems, err := fieldObjectList(doc, "provider_bindings")
	if err != nil {
		return importBundle{}, err
	}
	bindings, err := normalizeImportBindings(bindingItems, profiles)
	if err != nil {
		return importBundle{}, err
	}
	return importBundle{version: version, runtime: runtime, profiles: profiles, bindings: bindings}, nil
}

// normalizeImportRuntime 要求 runtime_config 正好覆盖 configDefinitions 全部键：多了未知项、少了必填都 400。
// 值再走 normalizeConfigValue + validateMerged，与设置页单条更新同一套闸门。
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

// normalizeImportProfiles 校验档案 id 不重复、类型在闭集、连接与能力合法。API Key 可空，导入后仍要用户补。
func normalizeImportProfiles(items []map[string]any) ([]importProfile, error) {
	seen := map[string]struct{}{}
	out := make([]importProfile, 0, len(items))
	for _, item := range items {
		id, err := requiredImportString(item, "id", 36)
		if err != nil {
			return nil, err
		}
		if id == "" {
			return nil, apperr.Validation("供应商档案 ID 不能为空")
		}
		if _, dup := seen[id]; dup {
			return nil, apperr.Validation("供应商档案不能重复")
		}
		seen[id] = struct{}{}
		providerTypeRaw, err := requiredImportString(item, "provider_type", 40)
		if err != nil {
			return nil, err
		}
		if providerTypeRaw == "" {
			return nil, apperr.Validation("供应商类型不支持")
		}
		providerType, err := normalizeProviderType(providerTypeRaw)
		if err != nil {
			return nil, err
		}
		capsRaw, err := optionalStringSlice(item, "capabilities")
		if err != nil {
			return nil, err
		}
		caps, err := normalizeCapabilities(capsRaw, providerType)
		if err != nil {
			return nil, err
		}
		name, err := requiredImportString(item, "name", 120)
		if err != nil {
			return nil, err
		}
		if name == "" {
			return nil, apperr.Validation("供应商名称不能为空")
		}
		baseURL, err := optionalImportStringPtr(item, "base_url", 0)
		if err != nil {
			return nil, err
		}
		if err := validateConnection(providerType, baseURL); err != nil {
			return nil, err
		}
		apiKey, err := optionalImportStringPtr(item, "api_key", 0)
		if err != nil {
			return nil, err
		}
		defaultModels, err := optionalObject(item, "default_models")
		if err != nil {
			return nil, err
		}
		cfg, err := optionalObject(item, "config")
		if err != nil {
			return nil, err
		}
		enabled, err := lookupBoolDefault(item, "enabled", true)
		if err != nil {
			return nil, err
		}
		out = append(out, importProfile{
			ID: id, Name: name, ProviderType: providerType, BaseURL: baseURL,
			APIKey: apiKey, Capabilities: caps,
			DefaultModels: orEmptyMap(defaultModels),
			Config:        orEmptyMap(cfg),
			Enabled:       enabled,
		})
	}
	return out, nil
}

// normalizeImportBindings 校验用途闭集、指向的档案存在且具备该能力。绑定到已归档/未启用档案也拒绝。
func normalizeImportBindings(items []map[string]any, profiles []importProfile) ([]importBinding, error) {
	profilesByID := map[string]importProfile{}
	for _, profile := range profiles {
		profilesByID[profile.ID] = profile
	}
	seen := map[string]struct{}{}
	out := make([]importBinding, 0, len(items))
	for _, item := range items {
		purpose, err := requiredImportString(item, "purpose", 40)
		if err != nil {
			return nil, err
		}
		if _, dup := seen[purpose]; dup {
			return nil, apperr.Validation("供应商用途绑定不能重复")
		}
		seen[purpose] = struct{}{}
		allowed, ok := kindsByPurpose[purpose]
		if !ok {
			return nil, apperr.Validation("用途必须是 prompt、agent 或 image")
		}
		kind, err := requiredImportString(item, "provider_kind", 40)
		if err != nil {
			return nil, err
		}
		if _, ok := allowed[kind]; !ok {
			return nil, apperr.Validation("供应商接口类型不支持当前用途")
		}
		modelSettings, err := optionalObject(item, "model_settings")
		if err != nil {
			return nil, err
		}
		cfg, err := optionalObject(item, "config")
		if err != nil {
			return nil, err
		}
		modelSettings = orEmptyMap(modelSettings)
		cfg = orEmptyMap(cfg)
		if err := validateBindingRuntime(purpose, kind, modelSettings, cfg); err != nil {
			return nil, err
		}
		var profileID *string
		if kind == "mock" {
			profileID = nil
		} else {
			profileID, err = optionalImportStringPtr(item, "provider_profile_id", 36)
			if err != nil {
				return nil, err
			}
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

// wire 把已规范化的 bundle 编回导出形状，给预览页原样展示。空指针写成 JSON null。
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

func fieldObjectList(doc map[string]any, key string) ([]map[string]any, error) {
	raw, ok := doc[key]
	if !ok {
		return []map[string]any{}, nil
	}
	if raw == nil {
		return nil, apperr.Validation("请求体无效")
	}
	return asObjectList(raw)
}

func asObjectList(v any) ([]map[string]any, error) {
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

// validateImportMetadata 只认 schema_version=3。缺 exported_at/app 等字段或版本不对返回 400，不尝试升级旧导出。
func validateImportMetadata(raw any) (int, error) {
	meta, ok := raw.(map[string]any)
	if !ok || meta == nil {
		return 0, apperr.Validation("配置文件格式不正确")
	}
	version, err := requiredImportInt(meta, "schema_version")
	if err != nil {
		return 0, err
	}
	if version != exportSchemaVersion {
		return 0, apperr.Validation("配置文件版本不支持")
	}
	for _, key := range []string{"exported_at", "app", "app_version", "compatibility"} {
		text, err := requiredImportString(meta, key, 0)
		if err != nil {
			return 0, err
		}
		if text == "" {
			return 0, apperr.Validation("请求体无效")
		}
		if key == "exported_at" {
			if err := parseImportDateTime(text); err != nil {
				return 0, err
			}
		}
	}
	compat, err := requiredImportString(meta, "compatibility", 0)
	if err != nil {
		return 0, err
	}
	if compat != exportCompatibility {
		return 0, apperr.Validation("配置文件兼容标识不支持")
	}
	return version, nil
}

func parseImportDateTime(value string) error {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.999999", "2006-01-02T15:04:05"} {
		if _, err := time.Parse(layout, value); err == nil {
			return nil
		}
	}
	return apperr.Validation("请求体无效")
}

func requiredImportInt(item map[string]any, key string) (int, error) {
	raw, ok := item[key]
	if !ok || raw == nil {
		return 0, apperr.Validation("请求体无效")
	}
	switch typed := raw.(type) {
	case int:
		return typed, nil
	case int64:
		return int(typed), nil
	case float64:
		if typed != float64(int(typed)) {
			return 0, apperr.Validation("请求体无效")
		}
		return int(typed), nil
	default:
		return 0, apperr.Validation("请求体无效")
	}
}

func requiredImportString(item map[string]any, key string, max int) (string, error) {
	raw, ok := item[key]
	if !ok {
		return "", apperr.Validation("请求体无效")
	}
	if raw == nil {
		return "", apperr.Validation("请求体无效")
	}
	s, ok := raw.(string)
	if !ok {
		return "", apperr.Validation("请求体无效")
	}
	s = strings.TrimSpace(s)
	if max > 0 && utf8.RuneCountInString(s) > max {
		return "", apperr.Validation("请求体无效")
	}
	return s, nil
}

func optionalImportStringPtr(item map[string]any, key string, max int) (*string, error) {
	raw, ok := item[key]
	if !ok || raw == nil {
		return nil, nil
	}
	s, ok := raw.(string)
	if !ok {
		return nil, apperr.Validation("请求体无效")
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	if max > 0 && utf8.RuneCountInString(s) > max {
		return nil, apperr.Validation("请求体无效")
	}
	return &s, nil
}

// optionalStringSlice 读可选字符串数组。缺键返回 nil；键在但值为 null 返回 400，避免把「明确清空」和「没写」搞混。
func optionalStringSlice(item map[string]any, key string) ([]string, error) {
	raw, ok := item[key]
	if !ok {
		return nil, nil
	}
	if raw == nil {
		return nil, apperr.Validation("请求体无效")
	}
	switch typed := raw.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, strings.TrimSpace(item))
		}
		return out, nil
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			s, ok := item.(string)
			if !ok {
				return nil, apperr.Validation("请求体无效")
			}
			out = append(out, strings.TrimSpace(s))
		}
		return out, nil
	default:
		return nil, apperr.Validation("请求体无效")
	}
}

func optionalObject(item map[string]any, key string) (map[string]any, error) {
	raw, ok := item[key]
	if !ok {
		return map[string]any{}, nil
	}
	if raw == nil {
		return nil, apperr.Validation("请求体无效")
	}
	out, ok := raw.(map[string]any)
	if !ok {
		return nil, apperr.Validation("请求体无效")
	}
	return out, nil
}

func anyOrNil(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

func lookupBoolDefault(item map[string]any, key string, fallback bool) (bool, error) {
	v, ok := item[key]
	if !ok {
		return fallback, nil
	}
	if v == nil {
		return false, apperr.Validation("请求体无效")
	}
	typed, ok := v.(bool)
	if !ok {
		return false, apperr.Validation("请求体无效")
	}
	return typed, nil
}
