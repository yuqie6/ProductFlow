package graph

import (
	"strings"
	"testing"
)

func TestCompileImageModelPromptIsListingText(t *testing.T) {
	got := CompileImageModelPrompt(ImageRequest{
		NodeTitle:    "核心卖点图",
		ImageTypeKey: "selling_point",
		GenerationSpec: map[string]any{
			"text_policy":    "none",
			"quality_intent": "high",
		},
		Prompt: map[string]any{
			"design_goal": "突出杯口密封",
			"composition": map[string]any{"layout": "商品居中", "product_share_percent": 70},
			"content":     map[string]any{"focus": []any{"保温杯"}},
		},
		References: []ReferenceImage{{AssetID: "a1", Label: "主体", Role: "product_identity"}},
	})
	if strings.HasPrefix(strings.TrimSpace(got), "{") {
		t.Fatalf("prompt should not be a JSON dump: %s", got[:80])
	}
	for _, needle := range []string{"核心卖点图", "商品外形", "突出杯口密封", "商品占比约 70%", "quality_intent"} {
		if !strings.Contains(got, needle) {
			t.Fatalf("missing %q in\n%s", needle, got)
		}
	}
}

func TestCompileImageModelPromptUnknownTypeKeepsKey(t *testing.T) {
	got := CompileImageModelPrompt(ImageRequest{
		NodeTitle:    "节点自定义名",
		ImageTypeKey: "custom_type_xyz",
		Prompt:       map[string]any{"design_goal": "自定义图种"},
	})
	first := strings.SplitN(strings.TrimSpace(got), "\n", 2)[0]
	if !strings.Contains(first, "custom_type_xyz") {
		t.Fatalf("unknown type must use key in title line: %s", first)
	}
	if strings.Contains(first, "节点自定义名") {
		t.Fatalf("unknown type must not use node title: %s", first)
	}
}

func TestCompileImageModelPromptIncludesVisualVariationAndEdges(t *testing.T) {
	got := CompileImageModelPrompt(ImageRequest{
		NodeTitle:            "核心卖点图",
		ImageTypeKey:         "selling_point",
		VariationInstruction: "换暖光",
		IncomingEdgeIDs:      []string{"e-prompt", "e-ref"},
		VisualSystem:         map[string]any{"style": []any{"商业套图"}},
		VisualOverlay:        map[string]any{"colors": []any{map[string]any{"value": "#F3EFE8"}}},
		GenerationSpec:       map[string]any{"text_policy": "required"},
		Prompt:               map[string]any{"design_goal": "卖点"},
		References:           []ReferenceImage{{AssetID: "a1", Label: "主体", EdgeID: "e-ref"}},
	})
	for _, needle := range []string{
		"变化：换暖光", `"visual_system"`, `"visual_overlay"`, `"incoming_edge_ids"`, `"edge_id"`, "e-ref",
	} {
		if !strings.Contains(got, needle) {
			t.Fatalf("missing %q in\n%s", needle, got)
		}
	}
}
