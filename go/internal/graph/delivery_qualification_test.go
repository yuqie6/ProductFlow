package graph

import "testing"

func TestParseArtifactDeliveryQualificationAndGates(t *testing.T) {
	empty := ParseArtifactDeliveryQualification(nil)
	if empty.HasTextTrace || empty.HasProduceRoute || ArtifactAllowsQualifiedAdoption(empty) {
		t.Fatalf("%+v", empty)
	}

	ok := ParseArtifactDeliveryQualification(map[string]any{
		"text_trace":    map[string]any{"text_qualified": true},
		"produce_route": map[string]any{"route": ProduceRouteSubjectPreserve, "route_qualified": true},
	})
	if !TextAllowsDeliveryPass(ok) || !RouteAllowsDeliveryPass(ok.Route) || !ArtifactAllowsQualifiedAdoption(ok) {
		t.Fatalf("%+v", ok)
	}

	textFail := ParseArtifactDeliveryQualification(map[string]any{
		"text_trace":    map[string]any{"text_qualified": false},
		"produce_route": map[string]any{"route": ProduceRouteGenerative, "route_qualified": true},
	})
	if TextAllowsDeliveryPass(textFail) || ArtifactAllowsQualifiedAdoption(textFail) {
		t.Fatalf("%+v", textFail)
	}

	routeFail := ParseArtifactDeliveryQualification(map[string]any{
		"text_trace": map[string]any{"text_qualified": true},
		"produce_route": map[string]any{
			"route":             ProduceRouteSubjectPreserve,
			"route_qualified":   false,
			"unresolved_items":  []any{"保留主体路线质检失败"},
		},
	})
	if RouteAllowsDeliveryPass(routeFail.Route) || ArtifactAllowsQualifiedAdoption(routeFail) {
		t.Fatalf("%+v", routeFail)
	}

	missingRoute := ParseArtifactDeliveryQualification(map[string]any{
		"text_trace": map[string]any{"text_qualified": true},
	})
	if !TextAllowsDeliveryPass(missingRoute) || ArtifactAllowsQualifiedAdoption(missingRoute) {
		t.Fatalf("%+v", missingRoute)
	}
}
