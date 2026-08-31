package graph

import (
	"strings"

	"github.com/yuqie6/productflow/prompts"
)

const (
	OriginSeed          = "seed"
	OriginGenerated     = "generated"
	OriginAuthored      = "authored"
	OriginCollaborative = "collaborative"

	DocumentActionComplete = "complete"
	DocumentActionRewrite  = "rewrite"
	DocumentActionReplace  = "replace"
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
		if field.control == "hidden" {
			continue
		}
		keys = append(keys, field.key)
	}
	return keys
}

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

func looksLikeSourceNoteSeedBrief(config map[string]any) bool {
	look := prompts.ListingLook()
	goal, _ := config["goal"].(string)
	if strings.TrimSpace(goal) != look.Rule {
		return false
	}
	if !documentFieldEmpty(config["required_copy"]) {
		return false
	}
	goals := stringListValues(config["design_goals"])
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

func mergeGeneratedBrief(current, generated map[string]any, action, origin string) map[string]any {
	out := cloneMap(current)
	if out == nil {
		out = map[string]any{}
	}
	if action == DocumentActionRewrite || action == DocumentActionReplace || origin == OriginSeed {
		for _, key := range []string{"goal", "design_goals", "required_copy", "prohibitions"} {
			if value, ok := generated[key]; ok {
				out[key] = cloneValue(value)
			}
		}
		return out
	}
	for _, key := range []string{"goal", "design_goals", "required_copy", "prohibitions"} {
		if documentFieldEmpty(out[key]) {
			if value, ok := generated[key]; ok {
				out[key] = cloneValue(value)
			}
		}
	}
	return out
}

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
	for _, key := range []string{"style", "colors", "prohibitions"} {
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

func downstreamTextPolicy(graph AppliedGraph, promptNodeID string) (policy, language string) {
	policy = "none"
	for _, edge := range graph.Edges {
		if edge.SourceNodeID != promptNodeID || edge.Role != RolePrompt {
			continue
		}
		target, err := graph.Node(edge.TargetNodeID)
		if err != nil || target.NodeType != NodeImageGeneration {
			continue
		}
		spec, _ := target.Config["generation_spec"].(map[string]any)
		if spec == nil {
			continue
		}
		raw, _ := spec["text_policy"].(string)
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if raw != "none" {
			policy = raw
			if lang, _ := spec["text_language"].(string); strings.TrimSpace(lang) != "" {
				language = strings.TrimSpace(lang)
			}
			return policy, language
		}
	}
	return policy, language
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
