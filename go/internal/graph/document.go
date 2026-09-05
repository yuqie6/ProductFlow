package graph

import (
	"strings"

	"github.com/yuqie6/productflow/prompts"
)

const (
	// OriginSeed 是模板创建的初始文稿。Fill cook 只对 seed 调 prompt provider。
	OriginSeed = "seed"
	// OriginGenerated 是成功 generate adopt 写入的文稿。
	OriginGenerated = "generated"
	// OriginAuthored 是检查器可见字段保存后的人工文稿。
	OriginAuthored = "authored"
	// OriginCollaborative 是对 generated 文稿再做部分人工编辑。
	OriginCollaborative = "collaborative"

	// DocumentActionComplete 补全空字段，不覆盖已有文稿。
	DocumentActionComplete = "complete"
	// DocumentActionRewrite 按模型输出重写整份可见文稿。
	DocumentActionRewrite = "rewrite"
	// DocumentActionReplace 用模型输出替换整份可见文稿。
	DocumentActionReplace = "replace"
)

func isContentNodeType(nodeType NodeType) bool {
	switch nodeType {
	case NodeCreativeBrief, NodeVisualSystem, NodeImagePrompt:
		return true
	default:
		return false
	}
}

func validDocumentOrigin(value string) (string, bool) {
	switch strings.TrimSpace(value) {
	case OriginSeed, OriginGenerated, OriginAuthored, OriginCollaborative:
		return strings.TrimSpace(value), true
	default:
		return "", false
	}
}

func validDocumentAction(value string) string {
	switch strings.TrimSpace(value) {
	case DocumentActionComplete, DocumentActionRewrite, DocumentActionReplace:
		return strings.TrimSpace(value)
	default:
		return ""
	}
}

// DocumentOrigin 返回内容节点文稿来源。持久化列是唯一来源；缺失或非法值不被解释为 seed。
func DocumentOrigin(node AppliedNode) string {
	if !isContentNodeType(node.NodeType) {
		return ""
	}
	if origin, ok := validDocumentOrigin(node.DocumentOrigin); ok {
		return origin
	}
	return ""
}

func documentOriginPtr(node AppliedNode) *string {
	if !isContentNodeType(node.NodeType) {
		return nil
	}
	origin := DocumentOrigin(node)
	if origin == "" {
		return nil
	}
	return &origin
}

func stampDocumentOriginOnCreate(nodeType NodeType, requested *string) string {
	if !isContentNodeType(nodeType) {
		return ""
	}
	if requested != nil {
		if origin, ok := validDocumentOrigin(*requested); ok {
			return origin
		}
	}
	return OriginSeed
}

func nextDocumentOrigin(nodeType NodeType, previous AppliedNode, normalized map[string]any, requested *string) string {
	if !isContentNodeType(nodeType) {
		return ""
	}
	if requested != nil {
		if origin, ok := validDocumentOrigin(*requested); ok {
			return origin
		}
	}
	if documentVisibleChanged(nodeType, previous.Config, normalized) {
		if DocumentOrigin(previous) == OriginGenerated || DocumentOrigin(previous) == OriginCollaborative {
			return OriginCollaborative
		}
		return OriginAuthored
	}
	return DocumentOrigin(previous)
}

func originConfigForInvert(node AppliedNode) map[string]any {
	config := cloneMap(node.Config)
	if config == nil {
		config = map[string]any{}
	}
	return config
}

func loadDocumentOrigin(nodeType NodeType, column *string) string {
	if !isContentNodeType(nodeType) {
		return ""
	}
	if column != nil {
		if origin, ok := validDocumentOrigin(*column); ok {
			return origin
		}
	}
	return ""
}

func documentVisibleChanged(nodeType NodeType, previous, next map[string]any) bool {
	keys := documentVisibleKeys(nodeType)
	for _, key := range keys {
		if !documentValueEqual(previous[key], next[key]) {
			return true
		}
	}
	return false
}

func liveDocumentDivergedFromSnapshot(live, snapshot AppliedNode) bool {
	if live.NodeType != snapshot.NodeType || DocumentOrigin(live) != DocumentOrigin(snapshot) {
		return true
	}
	return !documentValueEqual(live.Config, snapshot.Config)
}

func documentVisibleKeys(nodeType NodeType) []string {
	fields, ok := nodeConfigFields(nodeType)
	if !ok {
		return nil
	}
	var keys []string
	for _, field := range fields {
		if field.control == "hidden" || field.key == "text_settings" {
			continue
		}
		keys = append(keys, field.key)
	}
	return keys
}

// inferDocumentOriginFromConfig 只在持久化列缺失时猜测 origin。内容空或像出生种子则为 seed，否则 authored。
// 不能猜 generated / collaborative。非内容节点返回空串。不要把它当唯一来源——DocumentOrigin() 优先读列。
func inferDocumentOriginFromConfig(nodeType NodeType, config map[string]any) string {
	if !isContentNodeType(nodeType) {
		return ""
	}
	if config == nil {
		config = map[string]any{}
	}
	switch nodeType {
	case NodeCreativeBrief:
		if contentFieldsEmpty(nodeType, config) || looksLikeSourceNoteSeedBrief(config) {
			return OriginSeed
		}
		return OriginAuthored
	case NodeVisualSystem:
		if documentFieldEmpty(config["visual_overlay"]) {
			return OriginSeed
		}
		return OriginAuthored
	case NodeImagePrompt:
		if looksLikeBirthPromptSeed(config) {
			return OriginSeed
		}
		return OriginAuthored
	default:
		return OriginSeed
	}
}

func contentFieldsEmpty(nodeType NodeType, config map[string]any) bool {
	for _, key := range documentVisibleKeys(nodeType) {
		if !documentFieldEmpty(config[key]) {
			return false
		}
	}
	return true
}

// looksLikeSourceNoteSeedBrief 识别创建页看图起草落成的 brief 种子：goal 等于 listing look rule，
// key_messages 带 source-note 前缀，prohibitions 与 look 一致。改 look.md 会让旧种子被当成 authored。
func looksLikeSourceNoteSeedBrief(config map[string]any) bool {
	look := prompts.ListingLook()
	goal, _ := config["goal"].(string)
	if strings.TrimSpace(goal) != look.Rule {
		return false
	}
	if !documentFieldEmpty(config["required_elements"]) {
		return false
	}
	goals := stringListValues(config["key_messages"])
	if len(goals) != 1 || !strings.HasPrefix(goals[0], sourceNoteDesignGoalPrefix) {
		return false
	}
	prohibitions := stringListValues(config["prohibitions"])
	if len(prohibitions) != len(look.BriefProhibitions) {
		return false
	}
	for i, item := range look.BriefProhibitions {
		if prohibitions[i] != item {
			return false
		}
	}
	return true
}

// looksLikeBirthPromptSeed 识别出生模板的 prompt 种子：只允许空字段或图种默认 design_goal。
// 用户改过可见字段后不再是 seed，Fill cook 不会再打 provider。
func looksLikeBirthPromptSeed(config map[string]any) bool {
	prompt, _ := config["prompt"].(map[string]any)
	if prompt == nil {
		return documentFieldEmpty(config["prompt"])
	}
	key, _ := config["image_type_key"].(string)
	expected := imageTypePromptGoal(strings.TrimSpace(key))
	goal, _ := prompt["design_goal"].(string)
	goal = strings.TrimSpace(goal)
	if goal != "" && goal != expected {
		return false
	}
	for childKey, value := range prompt {
		switch childKey {
		case "design_goal", "schema_version", "visual_variant_key":
			continue
		default:
			if !documentFieldEmpty(value) {
				return false
			}
		}
	}
	return goal == "" || goal == expected
}

func stringListValues(value any) []string {
	switch typed := value.(type) {
	case []string:
		out := make([]string, len(typed))
		copy(out, typed)
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text, _ := item.(string)
			out = append(out, text)
		}
		return out
	default:
		return nil
	}
}

func documentValueEqual(left, right any) bool {
	return pythonDumps(left) == pythonDumps(right)
}

// contentNodeShouldGenerate 为 true 才会打 prompt provider。非 force 时只有 document_origin=seed 为 true。
func contentNodeShouldGenerate(node AppliedNode, forceTarget bool, action string) bool {
	if !isContentNodeType(node.NodeType) {
		return false
	}
	origin := DocumentOrigin(node)
	if forceTarget && validDocumentAction(action) != "" {
		return true
	}
	return origin == OriginSeed
}

func publishedPromptDocument(node AppliedNode, record SourceRecord) map[string]any {
	if len(record.PromptDocument) > 0 {
		return cloneMap(record.PromptDocument)
	}
	if payload := stripV3Prompt(record.CurrentArtifactPayload); hasPromptPayload(payload) {
		return payload
	}
	stored, _ := node.Config["prompt"].(map[string]any)
	return cloneMap(stored)
}

// mergeGeneratedBrief 按 document_action 合并 brief。rewrite/replace/seed 整段替换可见字段；complete 只填空。
// 不改未列出的键。adopt 写回用这份，不要把 payload 原样 dump 进 config。
func mergeGeneratedBrief(current, generated map[string]any, action, origin string) map[string]any {
	out := cloneMap(current)
	if out == nil {
		out = map[string]any{}
	}
	if action == DocumentActionRewrite || action == DocumentActionReplace || origin == OriginSeed {
		for _, key := range []string{"goal", "key_messages", "required_elements", "prohibitions", "fact_gaps"} {
			delete(out, key)
			if value, ok := generated[key]; ok {
				out[key] = cloneValue(value)
			}
		}
		return out
	}
	for _, key := range []string{"goal", "key_messages", "required_elements", "prohibitions", "fact_gaps"} {
		if documentFieldEmpty(out[key]) {
			if value, ok := generated[key]; ok {
				out[key] = cloneValue(value)
			}
		}
	}
	return out
}

// mergeGeneratedOverlay 合并 visual_overlay 的 style/colors。rewrite/replace/seed 整份替换 overlay。
func mergeGeneratedOverlay(current map[string]any, generated map[string]any, action, origin string) map[string]any {
	out := cloneMap(current)
	if out == nil {
		out = map[string]any{}
	}
	overlay, _ := out["visual_overlay"].(map[string]any)
	if overlay == nil {
		overlay = map[string]any{}
	}
	if action == DocumentActionRewrite || action == DocumentActionReplace || origin == OriginSeed {
		out["visual_overlay"] = cloneMap(generated)
		return out
	}
	for _, key := range []string{"style", "colors"} {
		if documentFieldEmpty(overlay[key]) {
			if value, ok := generated[key]; ok {
				overlay[key] = cloneValue(value)
			}
		}
	}
	out["visual_overlay"] = overlay
	return out
}

func mergeGeneratedPrompt(current map[string]any, generated map[string]any, action, origin string) map[string]any {
	out := cloneMap(current)
	if out == nil {
		out = map[string]any{}
	}
	stored, _ := out["prompt"].(map[string]any)
	payload := stripV3PromptPayload(generated)
	if action == DocumentActionRewrite || action == DocumentActionReplace || origin == OriginSeed {
		out["prompt"] = payload
		return out
	}
	out["prompt"] = mergePromptObjects(stored, payload)
	return out
}

// mergePromptObjects 做 complete：只填空子字段。product_share_percent 永不被模型覆盖。
func mergePromptObjects(current, generated map[string]any) map[string]any {
	out := cloneMap(current)
	if out == nil {
		out = map[string]any{}
	}
	for key, value := range generated {
		if key == "schema_version" || key == "visual_variant_key" {
			if documentFieldEmpty(out[key]) {
				out[key] = cloneValue(value)
			}
			continue
		}
		if documentFieldEmpty(out[key]) {
			out[key] = cloneValue(value)
			continue
		}
		currentMap, currentIsMap := asMap(out[key])
		generatedMap, generatedIsMap := asMap(value)
		if currentIsMap && generatedIsMap {
			merged := cloneMap(currentMap)
			for childKey, childVal := range generatedMap {
				if childKey == "product_share_percent" {
					continue
				}
				if documentFieldEmpty(merged[childKey]) {
					merged[childKey] = cloneValue(childVal)
				}
			}
			out[key] = merged
		}
	}
	return out
}

// documentFieldEmpty 把 nil、空白串、空切片、全空子对象视为空。complete 只填这些空位。
func documentFieldEmpty(value any) bool {
	if value == nil {
		return true
	}
	switch t := value.(type) {
	case string:
		return strings.TrimSpace(t) == ""
	case []any:
		return len(t) == 0
	case []string:
		return len(t) == 0
	case map[string]any:
		if len(t) == 0 {
			return true
		}
		for _, child := range t {
			if !documentFieldEmpty(child) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// collectGraphImageTypes 从 image_prompt 节点收集图种目录，供 AssemblePromptRequest。unspecified 跳过。
func collectGraphImageTypes(graph AppliedGraph) []map[string]any {
	seen := map[string]struct{}{}
	var out []map[string]any
	for _, node := range graph.Nodes {
		if node.NodeType != NodeImagePrompt {
			continue
		}
		key, _ := node.Config["image_type_key"].(string)
		key = strings.TrimSpace(key)
		if key == "" || key == "unspecified" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		item := map[string]any{"key": key, "family": imageTypeFamily(key), "job": imageTypeJob(key)}
		if option, ok := imageTypeByKey[key]; ok {
			item["title"] = option.Title
			item["description"] = option.Description
		}
		out = append(out, item)
	}
	return out
}

func mergeImageVisual(system, local map[string]any) map[string]any {
	out := cloneMap(system)
	if out == nil {
		out = map[string]any{}
	}
	overlay := CatalogVisualOverlay(local)
	for key, value := range overlay {
		out[key] = cloneValue(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
