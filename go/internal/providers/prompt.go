package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/settings"
	"github.com/yuqie6/productflow/prompts"
)

// 指令正文在 go/prompts/providers；缩短或改口径会直接改 brief/prompt 质量。

type jsonRoundTrip func(ctx context.Context, method, url, apiKey string, body []byte) (int, []byte, error)

// OpenAIPrompt 走 Responses structured outputs，字段对齐旧 Python client.responses.parse。
type OpenAIPrompt struct {
	APIKey    string        // 明文，只给进程内调用
	BaseURL   string        // 空则用 OpenAI 默认
	Model     string        // Responses 模型 id
	Transport jsonRoundTrip // 可注入 HTTP；测试用
}

// LivePrompt 每次调用按当前 PostgreSQL 绑定解析提示词供应商。
type LivePrompt struct {
	// Store 每次调用重新 Resolve，不要缓存过期 Key；nil 时回落 MockPromptProvider，不报错。
	Store *settings.Store
}

func (l LivePrompt) resolve(ctx context.Context) (graph.PromptProvider, error) {
	return Prompt(ctx, l.Store)
}

// Name 实现 graph.PromptProvider。每次从 settings 解析当前绑定；无法解析时返回 "unconfigured"。
func (l LivePrompt) Name() string {
	p, err := l.resolve(context.Background())
	if err != nil || p == nil {
		return "unconfigured"
	}
	return p.Name()
}

// GenerateCreativeBrief 实现 graph.PromptProvider，按当前 settings 绑定调用底层供应商。
func (l LivePrompt) GenerateCreativeBrief(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	p, err := l.resolve(ctx)
	if err != nil {
		return graph.PromptResult{}, err
	}
	return p.GenerateCreativeBrief(ctx, req)
}

// GenerateVisualOverlay 实现 graph.PromptProvider，按当前 settings 绑定调用底层供应商。
func (l LivePrompt) GenerateVisualOverlay(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	p, err := l.resolve(ctx)
	if err != nil {
		return graph.PromptResult{}, err
	}
	return p.GenerateVisualOverlay(ctx, req)
}

// GeneratePrompt 实现 graph.PromptProvider，按当前 settings 绑定调用底层供应商。
func (l LivePrompt) GeneratePrompt(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	p, err := l.resolve(ctx)
	if err != nil {
		return graph.PromptResult{}, err
	}
	return p.GeneratePrompt(ctx, req)
}

// GenerateSourceNote 实现 graph.PromptProvider。底层没有该方法时回退 MockSourceNotePayload，不打网。
func (l LivePrompt) GenerateSourceNote(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	p, err := l.resolve(ctx)
	if err != nil {
		return graph.PromptResult{}, err
	}
	if g, ok := p.(interface {
		GenerateSourceNote(context.Context, graph.PromptRequest) (graph.PromptResult, error)
	}); ok {
		return g.GenerateSourceNote(ctx, req)
	}
	return graph.PromptResult{Payload: MockSourceNotePayload(), Model: "mock-source-note"}, nil
}

// Prompt 按当前 prompt 用途绑定构造图运行提示词供应商。
// store 为 nil 或 Kind=mock 返回 MockPromptProvider（不打网）。
func Prompt(ctx context.Context, store *settings.Store) (graph.PromptProvider, error) {
	if store == nil {
		return graph.MockPromptProvider{}, nil
	}
	binding, err := store.ResolvePrompt(ctx)
	if err != nil {
		return nil, err
	}
	if binding.Kind == "" || binding.Kind == "mock" {
		return graph.MockPromptProvider{}, nil
	}
	if binding.Kind != "openai" {
		return nil, apperr.Unavailable(fmt.Sprintf("暂不支持的 prompt provider: %s", binding.Kind))
	}
	return OpenAIPrompt{APIKey: binding.APIKey, BaseURL: binding.BaseURL, Model: binding.Model}, nil
}

// Name 实现 graph.PromptProvider，返回 "openai"。
func (p OpenAIPrompt) Name() string { return "openai" }

// GenerateCreativeBrief 实现 graph.PromptProvider，走 Responses structured outputs。
// 已证明 4xx 可为失败；超时/断流/对不上 schema 走 unknown，调用方不得当失败自动重试。
func (p OpenAIPrompt) GenerateCreativeBrief(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	payload, model, id, err := p.parseStructured(ctx, prompts.BriefInstructions(), "generated_creative_brief", briefJSONSchema, req, "brief")
	if err != nil {
		return graph.PromptResult{}, err
	}
	return graph.PromptResult{Payload: payload, Model: model, ResponseID: id}, nil
}

// GenerateVisualOverlay 实现 graph.PromptProvider，走 Responses structured outputs。
// 已证明 4xx 可为失败；超时/断流/对不上 schema 走 unknown，调用方不得当失败自动重试。
func (p OpenAIPrompt) GenerateVisualOverlay(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	payload, model, id, err := p.parseStructured(ctx, prompts.OverlayInstructions(), "generated_visual_overlay", overlayJSONSchema, req, "overlay")
	if err != nil {
		return graph.PromptResult{}, err
	}
	return graph.PromptResult{Payload: payload, Model: model, ResponseID: id}, nil
}

// GeneratePrompt 实现 graph.PromptProvider，走 Responses structured outputs。
// 已证明 4xx 可为失败；超时/断流/对不上 schema 走 unknown，调用方不得当失败自动重试。
func (p OpenAIPrompt) GeneratePrompt(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	payload, model, id, err := p.parseStructured(ctx, prompts.PromptInstructions(), "listing_prompt_payload", listingPromptJSONSchema, req, "prompt")
	if err != nil {
		return graph.PromptResult{}, err
	}
	return graph.PromptResult{Payload: payload, Model: model, ResponseID: id}, nil
}

// GenerateSourceNote 实现 graph.PromptProvider，走 Responses structured outputs。
// 已证明 4xx 可为失败；超时/断流/对不上 schema 走 unknown，调用方不得当失败自动重试。
func (p OpenAIPrompt) GenerateSourceNote(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	payload, model, id, err := p.parseStructured(ctx, prompts.SourceNoteInstructions(), "generated_source_note", sourceNoteJSONSchema, req, "source_note")
	if err != nil {
		return graph.PromptResult{}, err
	}
	return graph.PromptResult{Payload: payload, Model: model, ResponseID: id}, nil
}

// parseStructured 走 Responses structured outputs。schema 必须是 strict；解析失败或对不上 schema 返回 unknown。
func (p OpenAIPrompt) parseStructured(
	ctx context.Context,
	instructions, schemaName string,
	schema map[string]any,
	req graph.PromptRequest,
	kind string,
) (map[string]any, string, string, error) {
	bodyMap, err := BuildPromptResponsesBody(p.Model, instructions, schemaName, schema, req, kind)
	if err != nil {
		return nil, "", "", err
	}
	body, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, "", "", err
	}
	status, raw, err := p.post(ctx, endpoint(p.BaseURL, "/v1/responses"), body)
	if err != nil {
		return nil, "", "", err
	}
	if err := mapWorkflowStatus(status, raw); err != nil {
		return nil, "", "", err
	}
	payload, model, id, err := parseResponsesStructured(raw, p.Model, schema)
	if err != nil {
		return nil, "", "", err
	}
	return payload, model, id, nil
}

func (p OpenAIPrompt) post(ctx context.Context, url string, body []byte) (int, []byte, error) {
	if p.Transport != nil {
		return p.Transport(ctx, "POST", url, p.APIKey, body)
	}
	return doJSON(ctx, newHTTPClient(), "POST", url, p.APIKey, bytes.NewReader(body), "application/json")
}

// BuildPromptResponsesBody 是 prompt 出站请求的纯函数，测试直接断言 path/字段而不打真实模型。
// 上下文 JSON 无法编码时失败并返回 error。
func BuildPromptResponsesBody(model, instructions, schemaName string, schema map[string]any, req graph.PromptRequest, kind string) (map[string]any, error) {
	content, err := promptRequestContent(req, kind)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"model":        model,
		"instructions": instructions,
		"input":        []map[string]any{{"role": "user", "content": content}},
		"text": map[string]any{
			"format": map[string]any{
				"type":   "json_schema",
				"name":   schemaName,
				"strict": true,
				"schema": schema,
			},
		},
	}, nil
}

// promptRequestContent 把 PromptRequest 编成 Responses input。参考图以 input_image 附上；缺字节返回 error。
func promptRequestContent(req graph.PromptRequest, kind string) ([]map[string]any, error) {
	refMeta := make([]map[string]any, 0, len(req.References))
	for _, ref := range req.References {
		refMeta = append(refMeta, map[string]any{
			"asset_id": ref.AssetID, "role": ref.Role, "label": ref.Label,
			"filename": ref.Filename, "mime_type": ref.MIME,
		})
	}
	var context map[string]any
	switch kind {
	case "prompt":
		current := req.CurrentPrompt
		if current == nil {
			current = asPromptMap(req.Config["prompt"])
		}
		context = map[string]any{
			"task":                   "generate_ecommerce_image_prompt_artifact",
			"document_action":        req.DocumentAction,
			"current_document":       req.CurrentDocument,
			"generate_from_context":  req.GenerateFromContext,
			"image_type_key":         emptyToUnspecified(req.ImageTypeKey),
			"image_type_title":       nilIfEmpty(req.ImageTypeTitle),
			"image_type_description": nilIfEmpty(req.ImageTypeDescription),
			"image_type_family":      req.ImageTypeFamily,
			"image_type_job":         nilIfEmpty(req.ImageTypeJob),
			"confirmed_facts":        factsOrEmpty(req.Facts),
			"visual_system":          req.Visual,
			"visual_exceptions":      exceptionsOrEmpty(req.VisualExceptions),
			"current_prompt":         current,
			"briefs":                 briefsOrEmpty(req.Briefs),
			"text_policy":            textPolicyOrNone(req.TextPolicy),
			"text_language":          nilIfEmpty(req.TextLanguage),
			"reference_images":       refMeta,
			"listing_look":           graph.ListingLookContext(),
			"listing_look_rule":      graph.ListingLookRule(),
		}
	case "source_note":
		current := req.CurrentDocument
		if current == nil {
			current = map[string]any{}
		}
		context = map[string]any{
			"task":             "draft_product_source_note",
			"product_name":     req.NodeTitle,
			"current_document": current,
			"reference_images": refMeta,
		}
	default:
		imageTypes := req.ImageTypes
		if imageTypes == nil {
			imageTypes = []map[string]any{}
		}
		context = map[string]any{
			"task":              "generate_ecommerce_context_node",
			"document_action":   req.DocumentAction,
			"current_document":  req.CurrentDocument,
			"node_title":        req.NodeTitle,
			"confirmed_facts":   factsOrEmpty(req.Facts),
			"current_brief":     req.Brief,
			"current_overlay":   req.Visual,
			"reference_images":  refMeta,
			"image_types":       imageTypes,
			"listing_look":      graph.ListingLookContext(),
			"listing_look_rule": graph.ListingLookRule(),
		}
	}
	text, err := compactJSON(context)
	if err != nil {
		return nil, err
	}
	content := []map[string]any{{"type": "input_text", "text": text}}
	for _, ref := range req.References {
		roleText, err := compactJSON(map[string]any{
			"asset_id": ref.AssetID, "role": ref.Role, "label": ref.Label,
		})
		if err != nil {
			return nil, err
		}
		content = append(content, map[string]any{"type": "input_text", "text": roleText})
		content = append(content, map[string]any{
			"type":      "input_image",
			"image_url": dataURL(ref.MIME, ref.Bytes),
		})
	}
	return content, nil
}

// parseResponsesStructured 从 Responses JSON 抽 output_parsed 并 matchJSONSchema。对不上返回 error，不要把原文当成功。
func parseResponsesStructured(raw []byte, fallbackModel string, schema map[string]any) (map[string]any, string, string, error) {
	var envelope struct {
		ID           string          `json:"id"`
		Model        string          `json:"model"`
		OutputParsed json.RawMessage `json:"output_parsed"`
		Output       []struct {
			Type    string `json:"type"`
			Content []struct {
				Type   string          `json:"type"`
				Text   string          `json:"text"`
				Parsed json.RawMessage `json:"parsed"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, "", "", graph.ErrProviderUnknown()
	}
	candidates := [][]byte{envelope.OutputParsed}
	for _, item := range envelope.Output {
		for _, part := range item.Content {
			if len(part.Parsed) > 0 {
				candidates = append(candidates, part.Parsed)
			}
			if strings.TrimSpace(part.Text) != "" {
				candidates = append(candidates, []byte(stripFence(part.Text)))
			}
		}
	}
	var payload map[string]any
	for _, item := range candidates {
		if len(bytes.TrimSpace(item)) == 0 {
			continue
		}
		if err := json.Unmarshal(item, &payload); err == nil && payload != nil {
			break
		}
	}
	if payload == nil {
		return nil, "", "", fmt.Errorf("提示词 provider 未返回结构化输出")
	}
	if err := matchJSONSchema(schema, payload); err != nil {
		return nil, "", "", fmt.Errorf("提示词 provider 未返回结构化输出")
	}
	model := envelope.Model
	if model == "" {
		model = fallbackModel
	}
	return payload, model, envelope.ID, nil
}

func compactJSON(v any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

func stripFence(text string) string {
	content := strings.TrimSpace(text)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	return strings.TrimSpace(content)
}

func factsOrEmpty(facts []map[string]any) []map[string]any {
	if facts == nil {
		return []map[string]any{}
	}
	return facts
}

func briefsOrEmpty(briefs []map[string]any) []map[string]any {
	if briefs == nil {
		return []map[string]any{}
	}
	return briefs
}

func exceptionsOrEmpty(items []map[string]any) []map[string]any {
	if items == nil {
		return []map[string]any{}
	}
	return items
}

func asPromptMap(value any) map[string]any {
	m, _ := value.(map[string]any)
	return m
}

func emptyToUnspecified(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unspecified"
	}
	return value
}

func nilIfEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func textPolicyOrNone(value string) string {
	if strings.TrimSpace(value) == "" {
		return "none"
	}
	return value
}
