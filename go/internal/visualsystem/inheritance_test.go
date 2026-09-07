package visualsystem_test

import (
	"testing"

	"github.com/yuqie6/productflow/internal/visualsystem"
)

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
	if got.BrandPlaceholder.Reason != visualsystem.BrandReasonNotReady {
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
}
