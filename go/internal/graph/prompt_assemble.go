package graph

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/prompts"
)

var overlayColorRolePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
var overlayColorSlugClean = regexp.MustCompile(`[^a-z0-9_-]+`)

// AssemblePromptRequest 对齐 Python _to_prompt_request / _to_context_request。
// 出站 user JSON 要用图种 job、listing_look、seed prompt，不能把节点 config 原样 dump 给模型。
// 内联 visual overlay 抽不出有效字段时返回 Validation。
func AssemblePromptRequest(
	node AppliedNode,
	facts []map[string]any,
	briefs []map[string]any,
	visual map[string]any,
	refs []ReferenceImage,
	digest string,
	graph AppliedGraph,
) (PromptRequest, error) {
	req := PromptRequest{
		NodeType:    node.NodeType,
		NodeTitle:   node.Title,
		InputDigest: digest,
		Facts:       facts,
		Briefs:      briefs,
		Visual:      visual,
		Config:      cloneMap(node.Config),
		References:  refs,
		TextPolicy:  "none",
		ImageTypes:  collectGraphImageTypes(graph),
	}
	if len(briefs) > 0 {
		req.Brief = cloneMap(briefs[0])
	}
	switch node.NodeType {
	case NodeCreativeBrief:
		req.Brief = filteredBriefConfig(node.Config)
	case NodeVisualSystem:
		if overlay := CatalogVisualOverlay(asMapOrNil(node.Config["visual_overlay"])); overlay != nil {
			req.Visual = overlay
		}
	case NodeImagePrompt:
		key, _ := node.Config["image_type_key"].(string)
		key = strings.TrimSpace(key)
		if key == "" {
			key = "unspecified"
		}
		req.ImageTypeKey = key
		if option, ok := imageTypeByKey[key]; ok {
			req.ImageTypeTitle = option.Title
			req.ImageTypeDescription = option.Description
		}
		req.ImageTypeFamily = imageTypeFamily(key)
		req.ImageTypeJob = imageTypeJob(key)
		req.TextPolicy, req.TextLanguage = textSettings(node.Config)
		promptConfig := asMapOrNil(node.Config["prompt"])
		req.GenerateFromContext = DocumentOrigin(node) == OriginSeed
		seed, err := seedPromptFromRuntime(node.Title, key, facts, briefs, promptConfig, req.GenerateFromContext, req.TextPolicy)
		if err != nil {
			return PromptRequest{}, err
		}
		req.CurrentPrompt = stripV3PromptPayload(seed)
		if len(visual) > 0 && !isInlineVisualOverlay(visual) {
			req.Visual = visual
			req.VisualExceptions = nil
		} else {
			exceptions, err := visualExceptionsFromOverlay(visual)
			if err != nil {
				return PromptRequest{}, err
			}
			req.VisualExceptions = exceptions
			// 工作流内联 overlay 不是完整 VisualSystemDraft，Python 把它放进 visual_exceptions，visual_system 为 null。
			if len(exceptions) > 0 {
				req.Visual = nil
			}
		}
	}
	return req, nil
}

// filteredBriefConfig 只保留 brief 可见字段给 prompt 组装。全空返回 nil，不要写成 {}。
func filteredBriefConfig(config map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"goal", "key_messages", "required_elements", "prohibitions", "fact_gaps"} {
		value, ok := config[key]
		if !ok || value == nil || value == "" {
			continue
		}
		if list, ok := value.([]any); ok && len(list) == 0 {
			continue
		}
		if list, ok := value.([]string); ok && len(list) == 0 {
			continue
		}
		out[key] = cloneValue(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func identityDefaultFidelity() map[string]any {
	return map[string]any{
		"complex_structure":  true,
		"product_present":    true,
		"picture_in_picture": "none",
		"requirements":       []any{"锁住参考图中的商品外形和材质", "构图和排版按图种重做"},
	}
}

func seedProductSharePercent(imageTypeKey string) int {
	if imageTypeFamily(imageTypeKey) == "infographic" {
		return 50
	}
	return 70
}

// seedPromptFromRuntime 在 seed cook 时从 facts/brief/图种拼一份初始 prompt 对象。
// 已有 stored 非空字段不覆盖。失败返回 Validation。不要拿它改 live config——那是 adopt 的事。
func seedPromptFromRuntime(
	title, imageTypeKey string,
	facts, briefs []map[string]any,
	stored map[string]any,
	seed bool,
	textPolicy string,
) (map[string]any, error) {
	stored = stripV3PromptPayload(stored)
	briefGoal, _, _ := briefFields(briefs)
	productName := factValue(facts, "product_name")
	designGoal := strings.TrimSpace(asString(stored["design_goal"]))
	if designGoal == "" {
		designGoal = briefGoal
	}
	if designGoal == "" {
		if option, ok := imageTypeByKey[imageTypeKey]; ok {
			designGoal = imageTypePromptGoal(option.Key)
			if productName != "" {
				designGoal = "为「" + productName + "」生成" + designGoal
			}
		} else if productName != "" {
			designGoal = "为「" + productName + "」生成" + title
		} else {
			designGoal = "生成" + title
		}
	}
	creativeBoundary := stringList(stored["creative_boundary"])
	text := asMapOrNil(stored["text"])
	if textPolicy == "none" && !promptConfigHasAuthoredText(stored) {
		text = map[string]any{}
	}
	sharedRules := stringList(stored["shared_rules"])
	if len(sharedRules) == 0 {
		sharedRules = []string{"参考图中的商品是身份基准；保留其外形、结构、材质、颜色和可见标识"}
	}
	derived := prompts.IdentityRules().ContextDerived
	composition := asMapOrNil(stored["composition"])
	if len(composition) == 0 {
		viewpoint, layout := "正面", "商品居中"
		if seed {
			viewpoint, layout = derived, derived
		}
		composition = map[string]any{
			"viewpoint": viewpoint, "product_share_percent": seedProductSharePercent(imageTypeKey), "layout": layout, "copy_regions": []any{},
		}
	}
	content := asMapOrNil(stored["content"])
	if len(content) == 0 {
		focus := title
		if option, ok := imageTypeByKey[imageTypeKey]; ok {
			focus = option.Title
		}
		if productName != "" {
			focus = productName
		}
		background := "干净背景"
		if seed {
			background = derived
		}
		content = map[string]any{
			"focus": []any{focus}, "selling_points": []any{}, "background": background, "decorations": []any{},
		}
	}
	atmosphere := asMapOrNil(stored["atmosphere"])
	if len(atmosphere) == 0 {
		keywords := []any{"清晰"}
		lighting := "均匀照明"
		if seed {
			keywords = []any{derived}
			lighting = derived
		}
		atmosphere = map[string]any{"keywords": keywords, "lighting": lighting}
	}
	fidelity := asMapOrNil(stored["product_fidelity"])
	if len(fidelity) == 0 {
		fidelity = identityDefaultFidelity()
	}
	return map[string]any{
		"schema_version":     1,
		"visual_variant_key": nil,
		"shared_rules":       stringListToAny(sharedRules),
		"design_goal":        designGoal,
		"product_fidelity":   fidelity,
		"creative_boundary":  stringListToAny(creativeBoundary),
		"composition":        composition,
		"content":            content,
		"text": map[string]any{
			"headline": nullableText(text["headline"]),
			"subtitle": nullableText(text["subtitle"]),
			"body":     nullableText(text["body"]),
		},
		"atmosphere": atmosphere,
	}, nil
}

// ApplyTextPolicyToPrompt 在 text_policy=none 且无人工文案时清空 text 与 copy_regions。
func ApplyTextPolicyToPrompt(payload map[string]any, textPolicy string, keepAuthored bool) map[string]any {
	out := cloneMap(payload)
	if textPolicy != "none" || keepAuthored {
		return out
	}
	out["text"] = map[string]any{"headline": nil, "subtitle": nil, "body": nil}
	if composition, ok := asMap(out["composition"]); ok && composition["copy_regions"] != nil {
		copied := cloneMap(composition)
		copied["copy_regions"] = []any{}
		out["composition"] = copied
	}
	return out
}

func stripV3PromptPayload(payload map[string]any) map[string]any {
	if payload == nil {
		return map[string]any{}
	}
	out := map[string]any{}
	for key, value := range payload {
		if _, skip := promptStrippedKeys[key]; skip {
			continue
		}
		out[key] = cloneValue(value)
	}
	return out
}

func promptConfigHasAuthoredText(promptConfig map[string]any) bool {
	if promptConfig == nil {
		return false
	}
	if text, ok := asMap(promptConfig["text"]); ok && mapHasNonEmptyString(text) {
		return true
	}
	if composition, ok := asMap(promptConfig["composition"]); ok {
		if listHasNonEmptyString(composition["copy_regions"]) {
			return true
		}
	}
	return false
}

func isInlineVisualOverlay(visual map[string]any) bool {
	if len(visual) == 0 {
		return true
	}
	for key := range visual {
		switch key {
		case "style", "colors":
		default:
			return false
		}
	}
	return true
}

// visualExceptionsFromOverlay 把工作流内联 overlay 编成 Python visual_exceptions。空 overlay 返回 nil；
// 有 overlay 但抽不出有效字段返回 Validation。不要把它写成完整 VisualSystemDraft。
func visualExceptionsFromOverlay(overlay map[string]any) ([]map[string]any, error) {
	if len(overlay) == 0 {
		return nil, nil
	}
	overrides := make([]any, 0, 3)
	if style := stringList(overlay["style"]); len(style) > 0 {
		overrides = append(overrides, map[string]any{"field": "style", "value": stringListToAny(style)})
	}
	if rawColors, ok := overlay["colors"].([]any); ok {
		used := map[string]struct{}{}
		coerced := make([]any, 0, len(rawColors))
		for i, item := range rawColors {
			color, ok := asMap(item)
			if !ok {
				continue
			}
			value := strings.TrimSpace(asString(color["value"]))
			if value == "" {
				continue
			}
			roleText := strings.TrimSpace(asString(color["role"]))
			role := overlayColorRole(roleText, i, used)
			label := strings.TrimSpace(asString(color["label"]))
			if label == "" {
				if roleText != "" {
					label = roleText
				} else {
					label = role
				}
			}
			coerced = append(coerced, map[string]any{"role": role, "value": value, "label": label})
		}
		if len(coerced) > 0 {
			overrides = append(overrides, map[string]any{"field": "colors", "value": coerced})
		}
	}
	if len(overrides) == 0 {
		return nil, apperr.Validation("视觉覆盖输入无效")
	}
	return []map[string]any{{
		"key":       "graph-inline-overlay",
		"scope":     map[string]any{"type": "workflow"},
		"overrides": overrides,
		"reason":    "工作流内联视觉覆盖",
	}}, nil
}

func overlayColorRole(raw string, index int, used map[string]struct{}) string {
	slug := overlayColorSlugClean.ReplaceAllString(strings.ToLower(strings.TrimSpace(raw)), "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 80 {
		slug = slug[:80]
	}
	candidate := slug
	if candidate == "" || !overlayColorRolePattern.MatchString(candidate) {
		candidate = "color-" + strconv.Itoa(index+1)
	}
	if _, exists := used[candidate]; exists {
		suffix := 2
		candidate = "color-" + strconv.Itoa(index+1)
		for {
			if _, exists := used[candidate]; !exists {
				break
			}
			candidate = "color-" + strconv.Itoa(index+1) + "-" + strconv.Itoa(suffix)
			suffix++
		}
	}
	used[candidate] = struct{}{}
	return candidate
}

// briefFields 从多份 brief 抽出 goal / key_messages / required_elements，供 seedPromptFromRuntime。
func briefFields(briefs []map[string]any) (string, []string, []string) {
	var goals, copyItems, prohibitions []string
	for _, brief := range briefs {
		if brief == nil {
			continue
		}
		goals = append(goals, stringList(brief["key_messages"])...)
		if goal := strings.TrimSpace(asString(brief["goal"])); goal != "" {
			goals = append(goals, goal)
		} else if title := strings.TrimSpace(asString(brief["title"])); title != "" {
			goals = append(goals, title)
		}
		copyItems = append(copyItems, stringList(brief["required_elements"])...)
		prohibitions = append(prohibitions, stringList(brief["prohibitions"])...)
	}
	goals = uniqueStrings(goals)
	if len(goals) == 0 {
		return "", uniqueStrings(copyItems), uniqueStrings(prohibitions)
	}
	return goals[0], uniqueStrings(copyItems), uniqueStrings(prohibitions)
}

func factValue(facts []map[string]any, key string) string {
	needle := strings.ToLower(key)
	for _, item := range facts {
		if strings.ToLower(strings.TrimSpace(asString(item["key"]))) != needle {
			continue
		}
		if value := strings.TrimSpace(asString(item["value"])); value != "" {
			return value
		}
	}
	return ""
}

func asMapOrNil(value any) map[string]any {
	m, ok := asMap(value)
	if !ok {
		return map[string]any{}
	}
	return cloneMap(m)
}

func asString(value any) string {
	s, _ := value.(string)
	return s
}

// stringList 把 []string / []any 收成去空白列表。其他类型当空，不要 panic。
func stringList(value any) []string {
	switch t := value.(type) {
	case []string:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := strings.TrimSpace(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := strings.TrimSpace(asString(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func stringListToAny(in []string) []any {
	out := make([]any, len(in))
	for i, item := range in {
		out[i] = item
	}
	return out
}

func uniqueStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, item := range in {
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func mapHasNonEmptyString(m map[string]any) bool {
	for _, value := range m {
		if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
			return true
		}
	}
	return false
}

func listHasNonEmptyString(value any) bool {
	return len(stringList(value)) > 0
}

func anyTextField(text map[string]any) bool {
	for _, key := range []string{"headline", "subtitle", "body"} {
		if s := strings.TrimSpace(asString(text[key])); s != "" {
			return true
		}
	}
	return false
}

func nullableText(value any) any {
	s := strings.TrimSpace(asString(value))
	if s == "" {
		return nil
	}
	return s
}
