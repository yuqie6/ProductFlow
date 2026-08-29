package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yuqie6/productflow/internal/graph"
)

const (
	briefInstructions   = "You write one ecommerce listing brief as JSON with keys goal, design_goals, required_copy, prohibitions. Arrays of strings. Do not invent facts absent from the input."
	overlayInstructions = "You write a visual overlay as JSON with keys style (string array), colors (objects with role, value hex, label), prohibitions. Never use empty zinc-gray studio as the only background."
	promptInstructions  = "You write one ListingPromptPayload JSON. schema_version is 1. Required: shared_rules, design_goal, product_fidelity, composition, content, text, atmosphere. Do not emit images, image_plan_key, fact_keys, or evidence_asset_ids."
)

type jsonRoundTrip func(ctx context.Context, method, url, apiKey string, body []byte) (int, []byte, error)

// OpenAIPrompt 用 Chat Completions JSON 对象生成 brief / overlay / prompt。
type OpenAIPrompt struct {
	APIKey    string
	BaseURL   string
	Model     string
	Transport jsonRoundTrip
}

func (p OpenAIPrompt) Name() string { return "openai" }

func (p OpenAIPrompt) GenerateCreativeBrief(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	payload, model, id, err := p.parseJSON(ctx, briefInstructions, req)
	if err != nil {
		return graph.PromptResult{}, err
	}
	return graph.PromptResult{Payload: payload, Model: model, ResponseID: id}, nil
}

func (p OpenAIPrompt) GenerateVisualOverlay(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	payload, model, id, err := p.parseJSON(ctx, overlayInstructions, req)
	if err != nil {
		return graph.PromptResult{}, err
	}
	return graph.PromptResult{Payload: payload, Model: model, ResponseID: id}, nil
}

func (p OpenAIPrompt) GeneratePrompt(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	payload, model, id, err := p.parseJSON(ctx, promptInstructions, req)
	if err != nil {
		return graph.PromptResult{}, err
	}
	return graph.PromptResult{Payload: payload, Model: model, ResponseID: id}, nil
}

func (p OpenAIPrompt) parseJSON(ctx context.Context, instructions string, req graph.PromptRequest) (map[string]any, string, string, error) {
	user, err := json.Marshal(map[string]any{
		"node_type":    string(req.NodeType),
		"node_title":   req.NodeTitle,
		"input_digest": req.InputDigest,
		"facts":        req.Facts,
		"config":       req.Config,
	})
	if err != nil {
		return nil, "", "", err
	}
	body, _ := json.Marshal(map[string]any{
		"model":           p.Model,
		"response_format": map[string]any{"type": "json_object"},
		"messages": []map[string]any{
			{"role": "system", "content": instructions},
			{"role": "user", "content": string(user)},
		},
	})
	status, raw, err := p.post(ctx, endpoint(p.BaseURL, "/v1/chat/completions"), body)
	if err != nil {
		return nil, "", "", err
	}
	if err := mapGraphStatus(status, raw); err != nil {
		return nil, "", "", err
	}
	var parsed struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, "", "", graph.ErrProviderUnknown()
	}
	if len(parsed.Choices) == 0 || strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
		return nil, "", "", fmt.Errorf("提示词 provider 未返回结构化输出")
	}
	content := strings.TrimSpace(parsed.Choices[0].Message.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	var payload map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(content)), &payload); err != nil {
		return nil, "", "", fmt.Errorf("提示词 provider 未返回结构化输出")
	}
	model := parsed.Model
	if model == "" {
		model = p.Model
	}
	return payload, model, parsed.ID, nil
}

func (p OpenAIPrompt) post(ctx context.Context, url string, body []byte) (int, []byte, error) {
	if p.Transport != nil {
		return p.Transport(ctx, "POST", url, p.APIKey, body)
	}
	return doJSON(ctx, newHTTPClient(), "POST", url, p.APIKey, bytes.NewReader(body), "application/json")
}
