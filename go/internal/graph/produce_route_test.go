package graph

import (
	"strings"
	"testing"
)

func TestDefaultProduceRouteHeroAndScene(t *testing.T) {
	if got := DefaultProduceRoute("hero"); got != ProduceRouteSubjectPreserve {
		t.Fatalf("hero: %s", got)
	}
	if got := DefaultProduceRoute("scene"); got != ProduceRouteGenerative {
		t.Fatalf("scene: %s", got)
	}
	if got := DefaultProduceRoute("detail"); got != ProduceRouteSubjectPreserve {
		t.Fatalf("detail: %s", got)
	}
}

func TestResolveProduceRouteFallsBackAndNormalizes(t *testing.T) {
	if got := ResolveProduceRoute(map[string]any{"image_type_key": "hero"}, ""); got != ProduceRouteSubjectPreserve {
		t.Fatalf("fallback hero: %s", got)
	}
	if got := ResolveProduceRoute(map[string]any{
		"image_type_key": "hero",
		"produce_route":  "generative",
	}, "hero"); got != ProduceRouteGenerative {
		t.Fatalf("explicit override: %s", got)
	}
	if got := ResolveProduceRoute(map[string]any{"produce_route": "nope"}, "scene"); got != ProduceRouteGenerative {
		t.Fatalf("illegal falls back to scene default: %s", got)
	}
}

func TestBuildProduceRouteRecordGenerativeAppearanceAndBan(t *testing.T) {
	ok := BuildProduceRouteRecord(ProduceRouteInput{
		Route:        ProduceRouteGenerative,
		ImageTypeKey: "scene",
		PromptTexts:  []string{"真实使用场景", "自然光"},
	})
	if !ok.AppearanceMayChange || !ok.RouteQualified || ok.Route != ProduceRouteGenerative {
		t.Fatalf("clean generative: %+v", ok)
	}
	if !RouteAllowsDeliveryPass(ok) {
		t.Fatal("clean generative must allow delivery pass criterion")
	}

	bad := BuildProduceRouteRecord(ProduceRouteInput{
		Route:        ProduceRouteGenerative,
		ImageTypeKey: "scene",
		PromptTexts:  []string{"请保持像素级一致，不要改外观"},
	})
	if bad.RouteQualified || RouteAllowsDeliveryPass(bad) {
		t.Fatalf("generative with pixel claim must not qualify: %+v", bad)
	}
	if len(bad.ForbiddenPhrases) == 0 || !strings.Contains(strings.Join(bad.UnresolvedItems, ","), "像素级一致") {
		t.Fatalf("must record forbidden phrase: %+v", bad)
	}
}

func TestBuildProduceRouteRecordSubjectPreserveFailureUnresolved(t *testing.T) {
	pass := BuildProduceRouteRecord(ProduceRouteInput{
		Route:                ProduceRouteSubjectPreserve,
		ImageTypeKey:         "hero",
		HasIdentityReference: true,
		PromptTexts:          []string{"棚拍主图"},
	})
	if !pass.RouteQualified || pass.AppearanceMayChange {
		t.Fatalf("clean subject_preserve: %+v", pass)
	}

	fail := BuildProduceRouteRecord(ProduceRouteInput{
		Route:                 ProduceRouteSubjectPreserve,
		ImageTypeKey:          "hero",
		HasIdentityReference:  true,
		SubjectPreserveFailed: true,
		FailureReasons:        []string{"边缘透明质检未通过"},
	})
	if fail.RouteQualified || RouteAllowsDeliveryPass(fail) {
		t.Fatalf("subject_preserve failure must not mark delivery-qualified: %+v", fail)
	}
	joined := strings.Join(fail.UnresolvedItems, ",")
	if !strings.Contains(joined, "质检失败") || !strings.Contains(joined, "边缘透明") {
		t.Fatalf("unresolved items: %+v", fail.UnresolvedItems)
	}
}

func TestBuildProduceRouteRecordMissingIdentityUnresolved(t *testing.T) {
	rec := BuildProduceRouteRecord(ProduceRouteInput{
		Route:                ProduceRouteSubjectPreserve,
		ImageTypeKey:         "hero",
		HasIdentityReference: false,
	})
	if rec.RouteQualified {
		t.Fatalf("missing identity must be unresolved: %+v", rec)
	}
}

func TestCompileImageModelPromptGenerativeScrubsPixelClaims(t *testing.T) {
	got := CompileImageModelPrompt(ImageRequest{
		ImageTypeKey: "scene",
		ProduceRoute: ProduceRouteGenerative,
		Prompt: map[string]any{
			"design_goal":  "使用场景",
			"shared_rules": []any{"保持像素级一致"},
		},
		VariationInstruction: "像素级还原杯身",
	})
	if !strings.Contains(got, "可能改变外观") {
		t.Fatalf("generative compile must declare appearance may change\n%s", got)
	}
	// 禁令行可点名「像素级一致」；用户规则与变化不得再把它当正向要求。
	if strings.Contains(got, "必须遵守：保持像素级一致") {
		t.Fatalf("user shared_rules must not keep pixel claim\n%s", got)
	}
	if strings.Contains(got, "变化：像素级还原") {
		t.Fatalf("variation must not keep pixel claim\n%s", got)
	}
	if !strings.Contains(got, "变化：杯身") {
		t.Fatalf("variation should keep non-banned remainder\n%s", got)
	}
}

func TestCompileImageModelPromptHeroDefaultsSubjectPreserve(t *testing.T) {
	got := CompileImageModelPrompt(ImageRequest{
		ImageTypeKey: "hero",
		Prompt:       map[string]any{"design_goal": "封面"},
	})
	if strings.Contains(got, "可能改变外观") {
		t.Fatalf("hero default must not claim generative appearance change\n%s", got)
	}
	if strings.Contains(got, "像素级一致") {
		t.Fatalf("must not claim pixel fidelity\n%s", got)
	}
}

func TestFillDefaultNodeConfigSetsProduceRoute(t *testing.T) {
	hero := FillDefaultNodeConfig(NodeImageGeneration, map[string]any{"image_type_key": "hero"})
	if hero["produce_route"] != ProduceRouteSubjectPreserve {
		t.Fatalf("hero produce_route: %+v", hero["produce_route"])
	}
	scene := FillDefaultNodeConfig(NodeImagePrompt, map[string]any{"image_type_key": "scene"})
	if scene["produce_route"] != ProduceRouteGenerative {
		t.Fatalf("scene produce_route: %+v", scene["produce_route"])
	}
	kept := FillDefaultNodeConfig(NodeImageGeneration, map[string]any{
		"image_type_key": "hero",
		"produce_route":  ProduceRouteGenerative,
	})
	if kept["produce_route"] != ProduceRouteGenerative {
		t.Fatalf("must not overwrite explicit route: %+v", kept["produce_route"])
	}
}

func TestProduceRouteAsMapRoundTrip(t *testing.T) {
	rec := BuildProduceRouteRecord(ProduceRouteInput{
		Route:        ProduceRouteGenerative,
		ImageTypeKey: "scene",
		PromptTexts:  []string{"像素级一致"},
	})
	raw := ProduceRouteAsMap(rec)
	if raw["route"] != ProduceRouteGenerative || raw["route_qualified"] != false {
		t.Fatalf("map: %+v", raw)
	}
	items, _ := raw["unresolved_items"].([]any)
	if len(items) == 0 {
		t.Fatalf("expected unresolved_items: %+v", raw)
	}
}
