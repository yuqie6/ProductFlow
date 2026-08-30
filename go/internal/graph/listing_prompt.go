package graph

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	noOnImageTextRule               = "画面中不得出现文字、数字、价格、Logo 或水印"
	noCaptionOnReferenceRule        = "禁止把参考图原样放大缩小后只加一行字交差"
	promptContextDerivedPlaceholder = "根据参考图、商品资料与图片类型生成"
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
	if typeTitle == imageTypeKey && strings.TrimSpace(req.NodeTitle) != "" {
		typeTitle = req.NodeTitle
	}
	job := imageTypeGenerationJobs[imageTypeKey]
	briefLines := []string{
		fmt.Sprintf("生成一张能上淘宝/天猫详情的%s，不是参考图修图交差。", typeTitle),
		"商品外形、结构、颜色、材质和可见零件以参考图为准。",
		"参考图只提供商品本体，构图、布光、场景和排版必须按图种重做。",
		listingLookRule,
		noCaptionOnReferenceRule + "。",
		"不要编造 Logo、认证、规格数字、价格或参考图与商品资料中未出现的结构。",
	}
	if job != "" {
		briefLines = append(briefLines, "图种任务："+job)
	}
	switch family {
	case "infographic":
		briefLines = append(briefLines, "这是详情卖点图：抠出商品重新排版。一个主标题加 2 到 4 条对齐的短利益点，色块克制。商品仍是主角。")
	case "evidence":
		briefLines = append(briefLines, "只能使用用户提供的资质或工厂画面，没有素材就不要生成假文件或假车间。")
	default:
		briefLines = append(briefLines, "这是可上架的商品摄影：主体约占画面 55%–75%，有光影质感。不要大面积空洞把商品挤到一角，也不要贴满标签。")
	}
	policy, _ := spec["text_policy"].(string)
	if policy == "" {
		policy = "none"
	}
	if policy == "none" {
		briefLines = append(briefLines, noOnImageTextRule+"。")
	} else if policy == "required" {
		if language, ok := spec["text_language"].(string); ok && strings.TrimSpace(language) != "" {
			briefLines = append(briefLines, "画面必须包含图片内文字，语种为"+strings.TrimSpace(language)+"，写短利益点，不要说明书。")
		} else {
			briefLines = append(briefLines, "画面必须包含图片内文字，写短利益点，不要说明书。")
		}
	}
	if designGoal := usablePromptText(payload["design_goal"]); designGoal != "" {
		briefLines = append(briefLines, "图目标："+designGoal)
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
	}
	if shared := usablePromptTexts(payload["shared_rules"]); len(shared) > 0 {
		briefLines = append(briefLines, "规则："+strings.Join(shared, "；"))
	}
	refAssets := make([]map[string]any, 0, len(req.References))
	for _, item := range req.References {
		refAssets = append(refAssets, map[string]any{
			"asset_id": item.AssetID,
			"label":    item.Label,
			"role":     item.Role,
		})
	}
	contract, _ := json.MarshalIndent(map[string]any{
		"contract_version":  3,
		"task":              "generate_one_ecommerce_listing_image",
		"image_type_key":    emptyToNil(imageTypeKey),
		"image_type_family": family,
		"prompt_artifact":   payload,
		"generation_spec":   spec,
		"reference_assets":  refAssets,
	}, "", "  ")
	return strings.Join(briefLines, "\n") + "\n\n" + string(contract)
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
	if text == "" || text == promptContextDerivedPlaceholder {
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
