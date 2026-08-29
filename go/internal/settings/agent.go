package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/yuqie6/productflow/internal/platform/apperr"
)

type AgentProviderConfig struct {
	SchemaVersion    int     `json:"schema_version"`
	ProviderKind     string  `json:"provider_kind"`
	APIKey           string  `json:"api_key"`
	BaseURL          *string `json:"base_url"`
	Model            string  `json:"model"`
	ReasoningEffort  *string `json:"reasoning_effort"`
	ReasoningSummary *string `json:"reasoning_summary"`
	TextVerbosity    *string `json:"text_verbosity"`
	ServiceTier      *string `json:"service_tier"`
}

// ResolveAgentProvider 给 Pi 内部服务解析当前工作流 Agent 绑定。
func (s *Store) ResolveAgentProvider(ctx context.Context) (AgentProviderConfig, error) {
	var kind string
	var profileID *string
	var modelSettings, bindingConfig []byte
	err := s.pool.QueryRow(ctx, `
		SELECT provider_kind, provider_profile_id, model_settings_json, config_json
		FROM provider_bindings WHERE purpose = 'agent'
	`).Scan(&kind, &profileID, &modelSettings, &bindingConfig)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AgentProviderConfig{}, apperr.Unavailable("工作流 Agent 供应商尚未配置")
		}
		return AgentProviderConfig{}, err
	}
	if kind == "mock" {
		return AgentProviderConfig{}, apperr.Unavailable("工作流 Agent 供应商尚未配置")
	}
	if kind != "openai" {
		return AgentProviderConfig{}, apperr.Unavailable(fmt.Sprintf("暂不支持的工作流 Agent provider: %s", kind))
	}
	if profileID == nil {
		return AgentProviderConfig{}, apperr.Unavailable("工作流 Agent 供应商尚未配置")
	}
	var enabled bool
	var archived any
	var apiKey, baseURL *string
	var capabilities, defaultModels []byte
	err = s.pool.QueryRow(ctx, `
		SELECT enabled, archived_at, api_key, base_url, capabilities_json, default_models_json
		FROM provider_profiles WHERE id = $1
	`, *profileID).Scan(&enabled, &archived, &apiKey, &baseURL, &capabilities, &defaultModels)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AgentProviderConfig{}, apperr.Unavailable("工作流 Agent 供应商尚未配置")
		}
		return AgentProviderConfig{}, err
	}
	if !enabled || archived != nil {
		return AgentProviderConfig{}, apperr.Unavailable("工作流 Agent 供应商尚未配置")
	}
	if apiKey == nil || *apiKey == "" {
		return AgentProviderConfig{}, apperr.Unavailable("工作流 Agent 供应商 API Key 未配置")
	}
	caps := []string{}
	_ = json.Unmarshal(capabilities, &caps)
	hasText := false
	for _, cap := range caps {
		if cap == "text_responses" {
			hasText = true
			break
		}
	}
	if !hasText {
		return AgentProviderConfig{}, apperr.Unavailable("工作流 Agent 供应商缺少 text_responses 能力")
	}
	model := lookupJSONString(modelSettings, "model")
	if model == "" {
		model = lookupJSONString(defaultModels, "agent_model")
	}
	if model == "" {
		return AgentProviderConfig{}, apperr.Unavailable("工作流 Agent 模型未配置")
	}
	cfg := AgentProviderConfig{
		SchemaVersion:    1,
		ProviderKind:     "openai",
		APIKey:           *apiKey,
		BaseURL:          emptyToNil(baseURL),
		Model:            model,
		ReasoningEffort:  lookupJSONStringPtr(bindingConfig, "reasoning_effort"),
		ReasoningSummary: lookupJSONStringPtr(bindingConfig, "reasoning_summary"),
		TextVerbosity:    lookupJSONStringPtr(bindingConfig, "text_verbosity"),
		ServiceTier:      lookupJSONStringPtr(bindingConfig, "service_tier"),
	}
	return cfg, nil
}

func lookupJSONString(raw []byte, key string) string {
	if len(raw) == 0 {
		return ""
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	value, _ := obj[key].(string)
	return value
}

func lookupJSONStringPtr(raw []byte, key string) *string {
	value := lookupJSONString(raw, key)
	if value == "" {
		return nil
	}
	return &value
}

func emptyToNil(value *string) *string {
	if value == nil || *value == "" {
		return nil
	}
	return value
}
