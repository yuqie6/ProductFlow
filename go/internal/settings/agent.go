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

// AgentProviderConfig 是解析给 Pi 内部服务的工作流 Agent 绑定，不是设置页 HTTP 投影。
// 含 API Key 明文，只给 agent-service 进程内用。ProviderKind 当前只支持 openai。
// BackgroundResumable 是档案声明 AND 适配器常数（现为 false），不要只看档案字段对外说可恢复。
type AgentProviderConfig struct {
	SchemaVersion       int     `json:"schema_version"`       // 当前为 1
	ProviderKind        string  `json:"provider_kind"`        // 当前只支持 openai；mock 会 Unavailable
	APIKey              string  `json:"api_key"`              // 明文，只给 agent-service 进程内用
	BaseURL             *string `json:"base_url"`             // nil 表示用供应商默认
	Model               string  `json:"model"`                // 绑定解析出的 Agent 模型
	ReasoningEffort     *string `json:"reasoning_effort"`     // nil 表示不发送该 OpenAI 参数
	ReasoningSummary    *string `json:"reasoning_summary"`    // nil 表示不发送
	TextVerbosity       *string `json:"text_verbosity"`       // nil 表示不发送
	ServiceTier         *string `json:"service_tier"`         // nil 表示不发送
	BackgroundResumable bool    `json:"background_resumable"` // profile 与 adapter 合取；当前 adapter 为 false
}

// agentAdapterBackgroundResumable 是 Pi 生产适配器是否支持 background 恢复。当前固定 false；
// 有效能力是档案声明 AND 本常数，不要只看档案字段就对外说可恢复。
const agentAdapterBackgroundResumable = false

// ResolveAgentProvider 给 Pi 内部服务解析当前工作流 Agent 绑定。
// 无绑定、mock、档案禁用/没 Key 或缺 text_responses 返回 Unavailable。
func (s *Store) ResolveAgentProvider(ctx context.Context) (AgentProviderConfig, error) {
	var binding schema.ProviderBindings
	err := s.db.WithContext(ctx).Where("purpose = ?", "agent").Take(&binding).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return AgentProviderConfig{}, apperr.Unavailable("工作流 Agent 供应商尚未配置")
		}
		return AgentProviderConfig{}, err
	}
	if binding.ProviderKind == "mock" {
		return AgentProviderConfig{}, apperr.Unavailable("工作流 Agent 供应商尚未配置")
	}
	if binding.ProviderKind != "openai" {
		return AgentProviderConfig{}, apperr.Unavailable(fmt.Sprintf("暂不支持的工作流 Agent provider: %s", binding.ProviderKind))
	}
	if binding.ProviderProfileID == nil {
		return AgentProviderConfig{}, apperr.Unavailable("工作流 Agent 供应商尚未配置")
	}
	var profile schema.ProviderProfiles
	err = s.db.WithContext(ctx).Where("id = ?", *binding.ProviderProfileID).Take(&profile).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return AgentProviderConfig{}, apperr.Unavailable("工作流 Agent 供应商尚未配置")
		}
		return AgentProviderConfig{}, err
	}
	if !profile.Enabled || profile.ArchivedAt != nil {
		return AgentProviderConfig{}, apperr.Unavailable("工作流 Agent 供应商尚未配置")
	}
	if profile.APIKey == nil || *profile.APIKey == "" {
		return AgentProviderConfig{}, apperr.Unavailable("工作流 Agent 供应商 API Key 未配置")
	}
	caps := []string{}
	_ = json.Unmarshal([]byte(profile.CapabilitiesJSON), &caps)
	hasText := false
	profileBackground := false
	for _, cap := range caps {
		if cap == "text_responses" {
			hasText = true
		}
		if cap == "background_responses" || cap == "background_resumable" {
			profileBackground = true
		}
	}
	if !hasText {
		return AgentProviderConfig{}, apperr.Unavailable("工作流 Agent 供应商缺少 text_responses 能力")
	}
	model := lookupJSONString([]byte(binding.ModelSettingsJSON), "model")
	if model == "" {
		model = lookupJSONString([]byte(profile.DefaultModelsJSON), "agent_model")
	}
	if model == "" {
		return AgentProviderConfig{}, apperr.Unavailable("工作流 Agent 模型未配置")
	}
	cfg := AgentProviderConfig{
		SchemaVersion:       1,
		ProviderKind:        "openai",
		APIKey:              *profile.APIKey,
		BaseURL:             emptyToNil(profile.BaseURL),
		Model:               model,
		ReasoningEffort:     lookupJSONStringPtr([]byte(binding.ConfigJSON), "reasoning_effort"),
		ReasoningSummary:    lookupJSONStringPtr([]byte(binding.ConfigJSON), "reasoning_summary"),
		TextVerbosity:       lookupJSONStringPtr([]byte(binding.ConfigJSON), "text_verbosity"),
		ServiceTier:         lookupJSONStringPtr([]byte(binding.ConfigJSON), "service_tier"),
		BackgroundResumable: profileBackground && agentAdapterBackgroundResumable,
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
