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
