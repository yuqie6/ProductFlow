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
	for _, needle := range []string{"核心卖点图", "商品外形", "突出杯口密封", "商品占比约 70%"} {
		if !strings.Contains(got, needle) {
			t.Fatalf("missing %q in\n%s", needle, got)
		}
	}
	for _, unwanted := range []string{`"prompt_artifact"`, `"quality_intent"`, "规则：", "禁令：", "外观禁令："} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("image prompt must not contain duplicated intermediate contract %q in\n%s", unwanted, got)
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

func TestImageInstructionsSeparateReferenceRolesAndPreservePrintedLabels(t *testing.T) {
	got := CompileImageModelPrompt(ImageRequest{ImageTypeKey: "hero", GenerationSpec: map[string]any{"text_policy": "none"}, Prompt: map[string]any{"design_goal": "展示商品"}})
	for _, needle := range []string{"product_identity", "environment 只借鉴", "style 只借鉴", "保留商品本体原有", "以本图已确认内容为准"} {
		if !strings.Contains(got, needle) {
			t.Fatalf("missing role/text priority instruction %q in %s", needle, got)
		}
	}
}

func TestCompileImageModelPromptIncludesVisualDirectionAndVariation(t *testing.T) {
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
	for _, needle := range []string{"变化：换暖光", "风格：商业套图", "色彩：", "#F3EFE8"} {
		if !strings.Contains(got, needle) {
			t.Fatalf("missing %q in\n%s", needle, got)
		}
	}
	for _, unwanted := range []string{`"visual_system"`, `"incoming_edge_ids"`, "e-ref"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("trace metadata must not leak into image prompt %q in\n%s", unwanted, got)
		}
	}
}

func TestCompileImageModelPromptPreservesExplicitConstraints(t *testing.T) {
	got := CompileImageModelPrompt(ImageRequest{
		ImageTypeKey:   "faq",
		GenerationSpec: map[string]any{"text_policy": "required", "text_language": "zh-CN"},
		Prompt: map[string]any{
			"design_goal":       "问答",
			"creative_boundary": []any{"不要变形"},
			"shared_rules":      []any{"保留杯盖形状"},
			"content":           map[string]any{"decorations": []any{"弱投影"}},
			"composition":       map[string]any{"copy_regions": []any{"顶栏"}},
			"text":              map[string]any{"headline": "三问三答"},
		},
	})
	for _, needle := range []string{"点缀：弱投影", "文案区域：顶栏", "图片内文字：三问三答", "不要变形", "保留杯盖形状"} {
		if !strings.Contains(got, needle) {
			t.Fatalf("missing %q in\n%s", needle, got)
		}
	}
}

func TestCompileImageModelPromptUsesPerTypeQualityLine(t *testing.T) {
	hero := CompileImageModelPrompt(ImageRequest{ImageTypeKey: "hero", Prompt: map[string]any{"design_goal": "认出来"}})
	if !strings.Contains(hero, "封面") || strings.Contains(hero, "双列") || strings.Contains(hero, "信息流") {
		t.Fatalf("hero compile\n%s", hero)
	}
	if strings.Contains(hero, "像标签不像说明书") {
		t.Fatalf("hero must not use the old infographic label line\n%s", hero)
	}
	selling := CompileImageModelPrompt(ImageRequest{
		ImageTypeKey:   "selling_point",
		GenerationSpec: map[string]any{"text_policy": "required", "text_language": "zh-CN"},
		Prompt:         map[string]any{"design_goal": "转化"},
	})
	for _, needle := range []string{"一页", "小图标", "详情模块"} {
		if !strings.Contains(selling, needle) {
			t.Fatalf("missing %q in selling_point compile\n%s", needle, selling)
		}
	}
	if strings.Contains(selling, "像搜索列表标签") {
		t.Fatalf("selling_point must not use photography label copy\n%s", selling)
	}
	scene := CompileImageModelPrompt(ImageRequest{ImageTypeKey: "scene", Prompt: map[string]any{"design_goal": "使用"}})
	if !strings.Contains(scene, "真实环境") || !strings.Contains(scene, "暖白台面") {
		t.Fatalf("scene compile\n%s", scene)
	}
	detail := CompileImageModelPrompt(ImageRequest{ImageTypeKey: "detail", Prompt: map[string]any{"design_goal": "工艺"}})
	if !strings.Contains(detail, "特写") || !strings.Contains(detail, "裁切") {
		t.Fatalf("detail compile\n%s", detail)
	}
}
