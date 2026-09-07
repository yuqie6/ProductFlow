package visualsystem_test

import (
	"testing"

	"github.com/yuqie6/productflow/internal/visualsystem"
)

// IQ-CF-07 / CF-B5：商品 overlay 色 > 方案色 >（品牌占位跳过）> 默认色。
func TestResolveInheritanceColorPriorityFourLayers(t *testing.T) {
	selectedID := "ver-scheme"
	systemID := "sys-cup"
	systemName := "杯系列"
	got := visualsystem.ResolveInheritance(visualsystem.ResolveInput{
		ProductID: "cup-new",
		ProductOverride: map[string]any{
			"colors": []any{map[string]any{"role": "accent", "value": "#FF6600"}},
		},
		SelectedVersionID:  &selectedID,
		SelectedSystemID:   &systemID,
		SelectedSystemName: &systemName,
		SelectedPayload: map[string]any{
			"style":  []any{"方案冷色棚拍"},
			"colors": []any{map[string]any{"role": "accent", "value": "#111111"}},
		},
		// 误传品牌色：未选定品牌时不得进入 EffectivePayload。
		BrandPayload: map[string]any{
			"colors": []any{map[string]any{"role": "accent", "value": "#00AA00"}},
		},
		ProductDefault: map[string]any{
			"style":  []any{"默认灰"},
			"colors": []any{map[string]any{"role": "accent", "value": "#CCCCCC"}},
		},
	})

	if got.BrandPlaceholder.Status != visualsystem.BrandStatusUnavailable {
		t.Fatalf("brand placeholder status=%s", got.BrandPlaceholder.Status)
	}
	if got.BrandPlaceholder.Reason != visualsystem.BrandReasonNotSelected {
		t.Fatalf("brand reason=%s", got.BrandPlaceholder.Reason)
	}
	if got.Layers[2].Layer != visualsystem.LayerBrandVersion || got.Layers[2].Active {
		t.Fatalf("brand layer must stay inactive placeholder %+v", got.Layers[2])
	}

	colors, _ := got.EffectivePayload["colors"].([]any)
	if len(colors) != 1 {
		t.Fatalf("colors %+v", got.EffectivePayload["colors"])
	}
	color, _ := colors[0].(map[string]any)
	if color["value"] != "#FF6600" {
		t.Fatalf("overlay color must win over scheme/default; got %+v", color)
	}
	style, _ := got.EffectivePayload["style"].([]any)
	if len(style) != 1 || style[0] != "方案冷色棚拍" {
		t.Fatalf("scheme style should remain when override omits style: %+v", got.EffectivePayload["style"])
	}
	if got.SelectedVersionID == nil || *got.SelectedVersionID != selectedID {
		t.Fatalf("instance must retain selected version id %+v", got.SelectedVersionID)
	}
}

func TestResolveInheritanceSchemeBeatsDefaultWithoutOverride(t *testing.T) {
	selectedID := "ver-2"
	got := visualsystem.ResolveInheritance(visualsystem.ResolveInput{
		ProductID:         "prod-1",
		SelectedVersionID: &selectedID,
		SelectedPayload: map[string]any{
			"colors": []any{map[string]any{"role": "background", "value": "#111111"}},
		},
		ProductDefault: map[string]any{
			"colors": []any{map[string]any{"role": "background", "value": "#EEEEEE"}},
			"style":  []any{"默认"},
		},
	})
	colors, _ := got.EffectivePayload["colors"].([]any)
	color, _ := colors[0].(map[string]any)
	if color["value"] != "#111111" {
		t.Fatalf("scheme must beat default: %+v", got.EffectivePayload)
	}
	style, _ := got.EffectivePayload["style"].([]any)
	if len(style) != 1 || style[0] != "默认" {
		t.Fatalf("default style retained: %+v", got.EffectivePayload["style"])
	}
}

// 反例：事实/参考不得进入风格链 EffectivePayload。
func TestResolveInheritanceRejectsFactsAndIdentityFromStyleChain(t *testing.T) {
	selectedID := "ver-leak"
	got := visualsystem.ResolveInheritance(visualsystem.ResolveInput{
		ProductID:         "cup-b",
		SelectedVersionID: &selectedID,
		SelectedPayload: map[string]any{
			"style":                    []any{"冷色"},
			"colors":                   []any{map[string]any{"value": "#222222"}},
			"capacity":                 "600ml",
			"fact_keys":                []any{"capacity"},
			"source_product_id":        "cup-a",
			"fact_set_version_id":      "fact-old",
			"visual_system_version_id": "ver-leak",
			"product_identity":         "asset-old",
			"reference_asset_ids":      []any{"ref-1"},
			"evidence_asset_ids":       []any{"ev-1"},
			"images":                   []any{"img"},
			"unknown_noise":            "drop-me",
		},
		ProductOverride: map[string]any{
			"style":     []any{"本商品暖色"},
			"fact_keys": []any{"should-not-merge"},
			"capacity":  "750ml",
		},
		ProductDefault: map[string]any{
			"style":    []any{"默认"},
			"capacity": "500ml",
		},
	})
	if _, ok := got.EffectivePayload["capacity"]; ok {
		t.Fatalf("capacity must not enter style chain: %+v", got.EffectivePayload)
	}
	if _, ok := got.EffectivePayload["fact_keys"]; ok {
		t.Fatalf("fact_keys must not enter style chain: %+v", got.EffectivePayload)
	}
	if _, ok := got.EffectivePayload["source_product_id"]; ok {
		t.Fatalf("source_product_id must not enter style chain")
	}
	if _, ok := got.EffectivePayload["product_identity"]; ok {
		t.Fatalf("product_identity must not enter style chain")
	}
	if _, ok := got.EffectivePayload["unknown_noise"]; ok {
		t.Fatalf("non-style keys must be dropped")
	}
	style, _ := got.EffectivePayload["style"].([]any)
	if len(style) != 1 || style[0] != "本商品暖色" {
		t.Fatalf("override style %+v", got.EffectivePayload["style"])
	}
	if len(got.Layers) != 4 {
		t.Fatalf("layers=%d", len(got.Layers))
	}
}

func TestResolveInheritancePriority(t *testing.T) {
	selectedID := "ver-2"
	systemID := "sys-1"
	systemName := "杯系列"
	got := visualsystem.ResolveInheritance(visualsystem.ResolveInput{
		ProductID: "prod-1",
		ProductOverride: map[string]any{
			"style": []any{"本商品暖色"},
		},
		SelectedVersionID:  &selectedID,
		SelectedSystemID:   &systemID,
		SelectedSystemName: &systemName,
		SelectedPayload: map[string]any{
			"style":  []any{"方案冷色"},
			"colors": []any{map[string]any{"value": "#111111"}},
		},
		ProductDefault: map[string]any{
			"style": []any{"默认灰"},
		},
	})
	if got.BrandPlaceholder.Status != visualsystem.BrandStatusUnavailable {
		t.Fatalf("brand placeholder status=%s", got.BrandPlaceholder.Status)
	}
	if got.BrandPlaceholder.Reason != visualsystem.BrandReasonNotSelected {
		t.Fatalf("brand reason=%s", got.BrandPlaceholder.Reason)
	}
	style, _ := got.EffectivePayload["style"].([]any)
	if len(style) != 1 || style[0] != "本商品暖色" {
		t.Fatalf("override should win: %+v", got.EffectivePayload["style"])
	}
	if _, ok := got.EffectivePayload["colors"]; !ok {
		t.Fatal("selected colors should remain when override omits them")
	}
	if len(got.Layers) != 4 {
		t.Fatalf("layers=%d", len(got.Layers))
	}
	if got.Layers[0].Layer != visualsystem.LayerProductOverride || !got.Layers[0].Active {
		t.Fatalf("override layer %+v", got.Layers[0])
	}
	if got.Layers[1].Layer != visualsystem.LayerSelectedVisual || !got.Layers[1].Active {
		t.Fatalf("selected layer %+v", got.Layers[1])
	}
	if got.Layers[2].Layer != visualsystem.LayerBrandVersion || got.Layers[2].Active {
		t.Fatalf("brand layer must be inactive placeholder %+v", got.Layers[2])
	}
	if got.Layers[3].Layer != visualsystem.LayerProductDefault || !got.Layers[3].Active {
		t.Fatalf("default layer %+v", got.Layers[3])
	}
}

func TestResolveInheritanceWithoutSelectionUsesDefault(t *testing.T) {
	got := visualsystem.ResolveInheritance(visualsystem.ResolveInput{
		ProductID:      "prod-2",
		ProductDefault: map[string]any{"style": []any{"默认"}},
	})
	if got.SelectedVersionID != nil {
		t.Fatal("expected no selection")
	}
	if got.Layers[1].Active {
		t.Fatal("selected layer should be inactive without selection")
	}
	style, _ := got.EffectivePayload["style"].([]any)
	if len(style) != 1 || style[0] != "默认" {
		t.Fatalf("default payload %+v", got.EffectivePayload)
	}
}

func TestResolveInheritanceBrandExistsNoMerge(t *testing.T) {
	got := visualsystem.ResolveInheritance(visualsystem.ResolveInput{
		ProductID: "prod-branded",
		BrandID:   "brand-1",
		// 已选定品牌但无可解析风格载荷 → 诚实占位，不合并。
		BrandPayload:   nil,
		ProductDefault: map[string]any{"style": []any{"默认"}},
	})
	if got.BrandPlaceholder.Reason != visualsystem.BrandReasonExistsNoMerge {
		t.Fatalf("reason=%s", got.BrandPlaceholder.Reason)
	}
	if got.Layers[2].Active {
		t.Fatal("brand layer must stay inactive without resolvable style")
	}
	if _, ok := got.EffectivePayload["colors"]; ok {
		t.Fatalf("brand colors must not merge without payload: %+v", got.EffectivePayload)
	}
}

func TestResolveInheritanceBrandMergesBetweenDefaultAndScheme(t *testing.T) {
	selectedID := "ver-scheme"
	brandVer := "ver-brand"
	brandSys := "sys-brand"
	brandName := "品牌方案"
	got := visualsystem.ResolveInheritance(visualsystem.ResolveInput{
		ProductID: "prod-branded",
		BrandID:   "brand-1",
		BrandPayload: map[string]any{
			"style":  []any{"品牌冷色"},
			"colors": []any{map[string]any{"role": "accent", "value": "#00AA00"}},
		},
		BrandVersionID:  &brandVer,
		BrandSystemID:   &brandSys,
		BrandSystemName: &brandName,
		SelectedVersionID: &selectedID,
		SelectedPayload: map[string]any{
			"colors": []any{map[string]any{"role": "accent", "value": "#111111"}},
		},
		ProductDefault: map[string]any{
			"style":  []any{"默认"},
			"colors": []any{map[string]any{"role": "accent", "value": "#CCCCCC"}},
		},
	})
	if got.BrandPlaceholder.Reason != visualsystem.BrandReasonMerged {
		t.Fatalf("reason=%s", got.BrandPlaceholder.Reason)
	}
	if !got.Layers[2].Active {
		t.Fatal("brand layer must be active when style resolvable")
	}
	colors, _ := got.EffectivePayload["colors"].([]any)
	color, _ := colors[0].(map[string]any)
	if color["value"] != "#111111" {
		t.Fatalf("selected scheme must beat brand: %+v", got.EffectivePayload)
	}
	style, _ := got.EffectivePayload["style"].([]any)
	if len(style) != 1 || style[0] != "品牌冷色" {
		t.Fatalf("brand style should fill when scheme omits style: %+v", got.EffectivePayload["style"])
	}
}

func TestResolveInheritanceBrandBeatsDefaultWithoutScheme(t *testing.T) {
	got := visualsystem.ResolveInheritance(visualsystem.ResolveInput{
		ProductID: "prod-brand-only",
		BrandID:   "brand-2",
		BrandPayload: map[string]any{
			"colors": []any{map[string]any{"value": "#00AA00"}},
			"facts":  map[string]any{"capacity": "不应进入"},
		},
		ProductDefault: map[string]any{
			"colors": []any{map[string]any{"value": "#EEEEEE"}},
		},
	})
	if !got.Layers[2].Active {
		t.Fatal("expected active brand layer")
	}
	colors, _ := got.EffectivePayload["colors"].([]any)
	color, _ := colors[0].(map[string]any)
	if color["value"] != "#00AA00" {
		t.Fatalf("brand must beat default: %+v", got.EffectivePayload)
	}
	if _, ok := got.EffectivePayload["facts"]; ok {
		t.Fatalf("facts must not enter style chain: %+v", got.EffectivePayload)
	}
}

func TestBuildReusePreviewListsInheritedAndPending(t *testing.T) {
	preferred := "ver-9"
	got := visualsystem.BuildReusePreview(&preferred, true, []string{"product_identity"})
	if len(got.Inherited) < 2 {
		t.Fatalf("inherited=%+v", got.Inherited)
	}
	if len(got.Pending) < 2 {
		t.Fatalf("pending=%+v", got.Pending)
	}
	if got.BrandPlaceholder.Status != visualsystem.BrandStatusUnavailable {
		t.Fatal("brand placeholder required")
	}
	if got.PreferredVisualSystemVersionID == nil || *got.PreferredVisualSystemVersionID != preferred {
		t.Fatal("preferred version missing")
	}
	foundBrandPending := false
	for _, item := range got.Pending {
		if item.Key == "brand_version" && item.Source == "placeholder" {
			foundBrandPending = true
		}
	}
	if !foundBrandPending {
		t.Fatalf("brand pending placeholder missing: %+v", got.Pending)
	}
	if len(got.InheritancePriority) != 4 || got.InheritancePriority[0] != visualsystem.LayerProductOverride {
		t.Fatalf("priority %+v", got.InheritancePriority)
	}
}

// 第二商品样例：复用方案风格，facts/参考为新商品待填。
func TestSecondProductReusePreviewKeepsStylePendingFacts(t *testing.T) {
	preferred := "ver-cup-series-v1"
	got := visualsystem.BuildReusePreview(&preferred, true, []string{"product_identity"})
	inheritedKeys := map[string]bool{}
	for _, item := range got.Inherited {
		inheritedKeys[item.Key] = true
	}
	if !inheritedKeys["preferred_visual_version"] {
		t.Fatalf("must inherit preferred visual version: %+v", got.Inherited)
	}
	if inheritedKeys["product_facts"] || inheritedKeys["product_identity"] {
		t.Fatalf("facts/identity must not be inherited: %+v", got.Inherited)
	}
	pendingKeys := map[string]bool{}
	for _, item := range got.Pending {
		pendingKeys[item.Key] = true
	}
	if !pendingKeys["product_facts"] || !pendingKeys["product_identity"] {
		t.Fatalf("new cup facts/refs must be pending: %+v", got.Pending)
	}
	if pendingKeys["preferred_visual_version"] {
		t.Fatal("preferred visual should not be pending when bound")
	}
}
