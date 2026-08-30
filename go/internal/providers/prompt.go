package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yuqie6/productflow/internal/graph"
)

// 指令必须与 Python openai_provider.py 的长 listing 文本一致，缩短会直接改 brief/prompt 质量。
const (
	briefInstructions = "You write one ecommerce listing brief from the attached product photos, confirmed facts, and image_types. " +
		"Follow listing_look: a shopper would click, the product is the hero, hierarchy is clear. " +
		"goal is the listing job, not a restatement of studio notes. " +
		"current_brief.goal and source notes are product facts (what it is, who it is for). " +
		"Ignore leftover art-direction words such as 极简, 浅灰, 静物, 干净, 留白. " +
		"Do not invert them into sticker-bomb or oversaturated layouts either. " +
		"design_goals are concrete layout and photography aims for the planned image_types. " +
		"If image_types include infographic keys such as selling_point, require cutout, recomposed layout, " +
		"and 2-4 short aligned benefits with restrained color. If they include photography keys such as hero or scene, " +
		"require a large product and commercial lighting, not empty-canvas still life and not badge spam. " +
		"prohibitions must include both extremes: 极简大留白/浅灰空棚/杂志静物, and 爆炸贴/满屏色块/牛皮癣标签. " +
		"Also block invented logos, certificates, prices, and product structures. " +
		"Do not prohibit new composition, lighting, scene, or type layout. " +
		"Obey text_policy: none means required_copy must be empty; allow or required may include " +
		"short on-image benefit copy in text_language. " +
		"Do not invent facts that are not in the photos or confirmed facts."

	overlayInstructions = "You write a compact visual overlay for a commercial listing set. " +
		"style is 2 to 6 keywords for a balanced sellable look " +
		"(product hero, clear hierarchy, category-appropriate color), " +
		"not 极简静物, not 干净商业摄影, not 花里胡哨. " +
		"Do not copy the reference photo's empty background or crop as the brand system; " +
		"only lock product material colors. " +
		"colors must include a background with presence (warm off-white or a category color, never empty zinc-gray studio) " +
		"plus one muted accent for headlines or modules. Hex values like #F3EFE8. Never neon carnival palettes. " +
		"prohibitions block product-identity changes and both listing extremes " +
		"(empty gray still-life, sticker-bomb layouts). Do not block layout changes. " +
		"Do not invent a brand system that is not visible in the photos or facts."

	promptInstructions = "You write one ListingPromptPayload for a clickable commercial listing image. " +
		"Attached photos lock product identity only: shape, materials, color, structure, visible parts. " +
		"The photo's crop, empty background, camera distance, and layout are not the finished frame. " +
		"Follow listing_look: product is the hero, hierarchy is clear, benefits are readable. " +
		"Forbidden both extremes: empty gray still-life / huge whitespace; and sticker-bomb / neon carnival layouts. " +
		"Treat leftover brief text such as 极简, 浅灰, 静物, or 干净 as product-photo notes, not the listing look. " +
		"Do not invert those notes into busy sticker spam. " +
		"Use image_type_key, image_type_family, image_type_job, image_type_title, " +
		"and image_type_description as the job. " +
		"photography: commercial product photography; product occupies about 55-75% of the frame; real lighting; " +
		"category-appropriate background; never shrink the product into a corner of empty canvas; " +
		"never cover it with badges. " +
		"infographic: cut the product out and redesign the layout. One clear headline plus 2-4 short aligned benefits. " +
		"Restrained color blocks, readable type. The product stays the visual hero. " +
		"Never paste a caption onto the original photo. " +
		"evidence: only user-supplied certificates or factory photos; if missing, leave the gap; " +
		"never invent seals or plants. " +
		"Do not invent logos, certifications, prices, spec numbers, or structures absent from facts and photos. " +
		"When generate_from_context is true, current_prompt is a schema seed. Observe the photos and write composition, " +
		"background, lighting, focus, and selling points for that image type. " +
		"Do not keep placeholder phrases such as 干净背景, 正面, 均匀照明, or 根据参考图、商品资料与图片类型生成. " +
		"When generate_from_context is false, refine current_prompt and keep user-authored fields. " +
		"Obey text_policy. none means no on-image letters, digits, prices, logos, or watermarks; keep text.headline, " +
		"subtitle, and body null and copy_regions empty. " +
		"required means short benefit copy in text_language, not a spec sheet. " +
		"Do not emit images, image_plan_key, fact_keys, or evidence_asset_ids."
)

type jsonRoundTrip func(ctx context.Context, method, url, apiKey string, body []byte) (int, []byte, error)

// OpenAIPrompt 走 Responses structured outputs，对齐 Python client.responses.parse。
type OpenAIPrompt struct {
	APIKey    string
	BaseURL   string
	Model     string
	Transport jsonRoundTrip
}

func (p OpenAIPrompt) Name() string { return "openai" }

func (p OpenAIPrompt) GenerateCreativeBrief(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	payload, model, id, err := p.parseStructured(ctx, briefInstructions, "generated_creative_brief", briefJSONSchema, req, "brief")
	if err != nil {
		return graph.PromptResult{}, err
	}
	if req.TextPolicy == "none" {
		payload["required_copy"] = []any{}
	}
	return graph.PromptResult{Payload: payload, Model: model, ResponseID: id}, nil
}

func (p OpenAIPrompt) GenerateVisualOverlay(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	payload, model, id, err := p.parseStructured(ctx, overlayInstructions, "generated_visual_overlay", overlayJSONSchema, req, "overlay")
	if err != nil {
		return graph.PromptResult{}, err
	}
	return graph.PromptResult{Payload: payload, Model: model, ResponseID: id}, nil
}

func (p OpenAIPrompt) GeneratePrompt(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	payload, model, id, err := p.parseStructured(ctx, promptInstructions, "listing_prompt_payload", listingPromptJSONSchema, req, "prompt")
	if err != nil {
		return graph.PromptResult{}, err
	}
	return graph.PromptResult{Payload: payload, Model: model, ResponseID: id}, nil
}

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
	if err := mapGraphStatus(status, raw); err != nil {
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

func promptRequestContent(req graph.PromptRequest, kind string) ([]map[string]any, error) {
	refMeta := make([]map[string]any, 0, len(req.References))
	for _, ref := range req.References {
		refMeta = append(refMeta, map[string]any{
			"asset_id": ref.AssetID, "role": ref.Role, "label": ref.Label,
			"filename": ref.Filename, "mime_type": ref.MIME,
		})
	}
	var context map[string]any
	if kind == "prompt" {
		current := req.CurrentPrompt
		if current == nil {
			current = asPromptMap(req.Config["prompt"])
		}
		textLanguages := []any{}
		if strings.TrimSpace(req.TextLanguage) != "" {
			textLanguages = []any{req.TextLanguage}
		}
		context = map[string]any{
			"task":                   "generate_ecommerce_image_prompt_artifact",
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
			"text_policy":            textPolicyOrNone(req.TextPolicy),
			"text_languages":         textLanguages,
			"reference_images":       refMeta,
			"listing_look":           graph.ListingLookContext(),
			"listing_look_rule":      graph.ListingLookRule(),
		}
	} else {
		imageTypes := req.ImageTypes
		if imageTypes == nil {
			imageTypes = []map[string]any{}
		}
		context = map[string]any{
			"task":              "generate_ecommerce_context_node",
			"node_title":        req.NodeTitle,
			"confirmed_facts":   factsOrEmpty(req.Facts),
			"current_brief":     req.Brief,
			"current_overlay":   req.Visual,
			"text_policy":       textPolicyOrNone(req.TextPolicy),
			"text_language":     nilIfEmpty(req.TextLanguage),
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
