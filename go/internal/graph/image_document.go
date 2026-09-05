package graph

import (
	"strings"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

func defaultTextSettings(imageType string) map[string]any {
	if imageTypeFamily(imageType) == "infographic" {
		return map[string]any{"policy": "required", "language": "zh-CN"}
	}
	return map[string]any{"policy": "none", "language": nil}
}

func textSettings(config map[string]any) (string, string) {
	settings := asMapOrNil(config["text_settings"])
	return strOr(settings, "policy", "none"), strings.TrimSpace(asString(settings["language"]))
}

func validateTextSettings(value any) error {
	if value == nil {
		return nil
	}
	settings, ok := asMap(value)
	if !ok {
		return apperr.Validation("文字设置无效")
	}
	policy := strOr(settings, "policy", "none")
	language := strings.TrimSpace(asString(settings["language"]))
	if !oneOf(policy, "none", "required") || (policy == "required" && language == "") || (policy == "none" && language != "") {
		return apperr.Validation("带文字时必须指定语言，无文字时语言必须为空")
	}
	return nil
}

// Overrides replace only present leaves. Empty strings/lists deliberately clear a value.
func mergeImagePrompt(base, overrides map[string]any) map[string]any {
	out := cloneMap(base)
	if out == nil {
		out = map[string]any{}
	}
	for key, value := range overrides {
		if child, ok := asMap(value); ok {
			out[key] = mergeImagePrompt(asMapOrNil(out[key]), child)
		} else {
			out[key] = cloneValue(value)
		}
	}
	return out
}

// resolveImageDocument is shared by hashing, execution and the inspector projection.
func resolveImageDocument(graph AppliedGraph, nodeID string, sources map[string]SourceRecord) (map[string]any, map[string]any, string, error) {
	node, err := graph.Node(nodeID)
	if err != nil {
		return nil, nil, "", err
	}
	base, artifactID, err := incomingPromptDocument(graph, nodeID, sources)
	if err != nil {
		return nil, nil, "", err
	}
	var source AppliedNode
	for _, edge := range incomingSorted(graph, nodeID) {
		if edge.Role == RolePrompt {
			source, err = graph.Node(edge.SourceNodeID)
			break
		}
	}
	if err != nil {
		return nil, nil, "", err
	}
	payload := mergeImagePrompt(base, asMapOrNil(node.Config["prompt_overrides"]))
	// A plan explicitly carries its connected brief's mandatory content to each image.
	for _, edge := range incomingSorted(graph, source.ID) {
		if edge.Role != RoleBrief {
			continue
		}
		briefNode, err := graph.Node(edge.SourceNodeID)
		if err != nil {
			return nil, nil, "", err
		}
		brief := visibleDocument(NodeCreativeBrief, briefNode.Config)
		payload["shared_rules"] = stringListToAny(uniqueStrings(append(stringList(payload["shared_rules"]), stringList(brief["required_elements"])...)))
		payload["creative_boundary"] = stringListToAny(uniqueStrings(append(stringList(payload["creative_boundary"]), stringList(brief["prohibitions"])...)))
	}
	policy, language := textSettings(source.Config)
	if local, ok := asMap(node.Config["text_override"]); ok {
		policy, language = strOr(local, "policy", "none"), asString(local["language"])
		payload["text"] = pickDocumentFields(local, []string{"headline", "subtitle", "body"})
		composition := asMapOrNil(payload["composition"])
		composition["copy_regions"] = cloneValue(local["copy_regions"])
		payload["composition"] = composition
	}
	// The provider receives derived text settings; persisted generation_spec has no text authority.
	spec := asMapOrNil(node.Config["generation_spec"])
	spec["text_policy"], spec["text_language"] = policy, nullableText(language)
	if policy == "none" {
		payload = ApplyTextPolicyToPrompt(payload, "none", false)
	}
	return payload, spec, artifactID, nil
}
