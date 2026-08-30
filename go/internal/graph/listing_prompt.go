package graph

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yuqie6/productflow/prompts"
)

// CompileImageModelPrompt 把 listing prompt 与 generation_spec 编成发给生图模型的自然语言，而不是 JSON dump。
func CompileImageModelPrompt(req ImageRequest) string {
	payload := promptPayloadForImageModel(req)
	spec := req.GenerationSpec
	if spec == nil {
		spec = map[string]any{}
	}
	composition, _ := payload["composition"].(map[string]any)
	if composition == nil {
		composition = map[string]any{}
	}
	content, _ := payload["content"].(map[string]any)
	if content == nil {
		content = map[string]any{}
	}
	atmosphere, _ := payload["atmosphere"].(map[string]any)
	if atmosphere == nil {
		atmosphere = map[string]any{}
	}
	fidelity, _ := payload["product_fidelity"].(map[string]any)
	if fidelity == nil {
		fidelity = map[string]any{}
	}
	text, _ := payload["text"].(map[string]any)
	if text == nil {
		text = map[string]any{}
	}
	imageTypeKey := strings.TrimSpace(req.ImageTypeKey)
	family := imageTypeFamily(imageTypeKey)
	typeTitle := imageTypeTitle(imageTypeKey)
	job := imageTypeJob(imageTypeKey)
	compile := prompts.CompileImageTemplates()
	identity := prompts.IdentityRules()
	briefLines := []string{
		compile.LeadFor(typeTitle),
		compile.Identity,
		compile.Recompose,
		prompts.ListingLook().Rule,
		identity.NoCaptionOnReference + "。",
		compile.Invent,
	}
	if job != "" {
		briefLines = append(briefLines, "图种任务："+job)
	}
	briefLines = append(briefLines, compile.FamilyLine(family))
	policy, _ := spec["text_policy"].(string)
	if policy == "" {
		policy = "none"
	}
	language, _ := spec["text_language"].(string)
	if line := compile.TextPolicyLine(policy, language); line != "" {
		briefLines = append(briefLines, line)
	}
	if designGoal := usablePromptText(payload["design_goal"]); designGoal != "" {
		briefLines = append(briefLines, "图目标："+designGoal)
	}
	for _, line := range overlayBriefLines(mergeImageVisual(req.VisualSystem, req.VisualOverlay)) {
		briefLines = append(briefLines, line)
	}
	viewpoint := usablePromptText(composition["viewpoint"])
	layout := usablePromptText(composition["layout"])
	if viewpoint != "" || layout != "" {
		parts := make([]string, 0, 2)
		if viewpoint != "" {
			parts = append(parts, viewpoint)
		}
		if layout != "" {
			parts = append(parts, layout)
		}
		briefLines = append(briefLines, "构图："+strings.Join(parts, "，"))
	}
	if share, ok := asFiniteNumber(composition["product_share_percent"]); ok {
		briefLines = append(briefLines, fmt.Sprintf("商品占比约 %g%%", share))
	}
	if focus := usablePromptTexts(content["focus"]); len(focus) > 0 {
		briefLines = append(briefLines, "主体："+strings.Join(focus, "、"))
	}
	if selling := usablePromptTexts(content["selling_points"]); len(selling) > 0 {
		briefLines = append(briefLines, "卖点："+strings.Join(selling, "、"))
	}
	if background := usablePromptText(content["background"]); background != "" {
		briefLines = append(briefLines, "背景："+background)
	}
	if decorations := usablePromptTexts(content["decorations"]); len(decorations) > 0 {
		briefLines = append(briefLines, "点缀："+strings.Join(decorations, "、"))
	}
	lighting := usablePromptText(atmosphere["lighting"])
	mood := strings.Join(usablePromptTexts(atmosphere["keywords"]), "、")
	if lighting != "" || mood != "" {
		parts := make([]string, 0, 2)
		if lighting != "" {
			parts = append(parts, lighting)
		}
		if mood != "" {
			parts = append(parts, mood)
		}
		briefLines = append(briefLines, "氛围："+strings.Join(parts, "，"))
	}
	if requirements := usablePromptTexts(fidelity["requirements"]); len(requirements) > 0 {
		briefLines = append(briefLines, "保真："+strings.Join(requirements, "、"))
	}
	if boundary := usablePromptTexts(payload["creative_boundary"]); len(boundary) > 0 {
		briefLines = append(briefLines, "禁令："+strings.Join(boundary, "、"))
	}
	if policy != "none" {
		copyBits := []string{}
		for _, key := range []string{"headline", "subtitle", "body"} {
			if bit := usablePromptText(text[key]); bit != "" {
				copyBits = append(copyBits, bit)
			}
		}
		if len(copyBits) > 0 {
			briefLines = append(briefLines, "图片内文字："+strings.Join(copyBits, "；"))
		}
		if regions := usablePromptTexts(composition["copy_regions"]); len(regions) > 0 {
			briefLines = append(briefLines, "文案区域："+strings.Join(regions, "、"))
		}
	}
	if shared := usablePromptTexts(payload["shared_rules"]); len(shared) > 0 {
		briefLines = append(briefLines, "规则："+strings.Join(shared, "；"))
	}
	if strings.TrimSpace(req.VariationInstruction) != "" {
		briefLines = append(briefLines, "变化："+strings.TrimSpace(req.VariationInstruction))
	}
	refAssets := make([]map[string]any, 0, len(req.References))
	for _, item := range req.References {
		refAssets = append(refAssets, map[string]any{
			"asset_id": item.AssetID,
			"label":    item.Label,
			"edge_id":  emptyToNil(item.EdgeID),
		})
	}
	incoming := req.IncomingEdgeIDs
	if incoming == nil {
		incoming = []string{}
	}
	contract := marshalListingContract(map[string]any{
		"contract_version":      3,
		"task":                  "generate_one_ecommerce_listing_image",
		"image_type_key":        emptyToNil(imageTypeKey),
		"image_type_family":     family,
		"prompt_artifact":       payload,
		"visual_system":         req.VisualSystem,
		"visual_overlay":        req.VisualOverlay,
		"variation_instruction": emptyToNil(strings.TrimSpace(req.VariationInstruction)),
		"generation_spec":       spec,
		"reference_assets":      refAssets,
		"incoming_edge_ids":     incoming,
	})
	return strings.Join(briefLines, "\n") + "\n\n" + contract
}

func overlayBriefLines(overlay map[string]any) []string {
	if len(overlay) == 0 {
		return nil
	}
	var lines []string
	if style := usablePromptTexts(overlay["style"]); len(style) > 0 {
		lines = append(lines, "风格："+strings.Join(style, "、"))
	}
	if colors := overlayColorTexts(overlay["colors"]); len(colors) > 0 {
		lines = append(lines, "色彩："+strings.Join(colors, "、"))
	}
	if prohibitions := usablePromptTexts(overlay["prohibitions"]); len(prohibitions) > 0 {
		lines = append(lines, "外观禁令："+strings.Join(prohibitions, "、"))
	}
	return lines
}

func overlayColorTexts(value any) []string {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if text := usablePromptText(item); text != "" {
			out = append(out, text)
			continue
		}
		entry, ok := asMap(item)
		if !ok {
			continue
		}
		hex := usablePromptText(entry["value"])
		if hex == "" {
			continue
		}
		if role := usablePromptText(entry["role"]); role != "" {
			out = append(out, role+" "+hex)
			continue
		}
		out = append(out, hex)
	}
	return out
}

func imageTypeTitle(key string) string {
	if key == "" {
		return "电商商品图"
	}
	if option, ok := imageTypeByKey[key]; ok {
		return option.Title
	}
	return key
}

func promptPayloadForImageModel(req ImageRequest) map[string]any {
	payload := cloneMap(req.Prompt)
	spec := req.GenerationSpec
	if spec == nil {
		return payload
	}
	policy, _ := spec["text_policy"].(string)
	if policy != "none" {
		return payload
	}
	payload["text"] = map[string]any{"headline": nil, "subtitle": nil, "body": nil}
	if composition, ok := payload["composition"].(map[string]any); ok && composition["copy_regions"] != nil {
		copied := cloneMap(composition)
		copied["copy_regions"] = []any{}
		payload["composition"] = copied
	}
	return payload
}

func usablePromptText(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	text = strings.TrimSpace(text)
	if text == "" || text == prompts.IdentityRules().ContextDerived {
		return ""
	}
	return text
}

func usablePromptTexts(value any) []string {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if text := usablePromptText(item); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func marshalListingContract(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return "{}"
	}
	return strings.TrimSpace(buf.String())
}
