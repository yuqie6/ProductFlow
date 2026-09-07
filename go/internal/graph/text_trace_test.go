package graph

import (
	"reflect"
	"testing"
)

func TestBuildTextTraceCapacityFromFact(t *testing.T) {
	trace := BuildTextTrace(TextTraceInput{
		ImageTypeKey: "specifications",
		Prompt: map[string]any{
			"text": map[string]any{
				"headline": "容量 600ml",
				"subtitle": nil,
				"body":     nil,
			},
			"content": map[string]any{"selling_points": []any{}},
		},
		Facts: []map[string]any{
			{"key": "capacity", "value": "600ml", "status": "confirmed"},
		},
	})
	if !trace.TextQualified {
		t.Fatalf("expected qualified: %+v", trace)
	}
	if !reflect.DeepEqual(trace.FactKeys, []string{"capacity"}) {
		t.Fatalf("fact_keys %+v", trace.FactKeys)
	}
	if len(trace.Entries) != 1 || !trace.Entries[0].Traced || trace.Entries[0].Field != "text.headline" {
		t.Fatalf("entries %+v", trace.Entries)
	}
}

func TestBuildTextTraceRejectsUnbackedClaim(t *testing.T) {
	trace := BuildTextTrace(TextTraceInput{
		ImageTypeKey: "specifications",
		Prompt: map[string]any{
			"text": map[string]any{"headline": "24h 保温", "subtitle": nil, "body": nil},
		},
		Facts: []map[string]any{
			{"key": "capacity", "value": "600ml", "status": "confirmed"},
		},
	})
	if trace.TextQualified {
		t.Fatalf("unbacked claim must not qualify: %+v", trace)
	}
	if len(trace.FactKeys) != 0 {
		t.Fatalf("fact_keys %+v", trace.FactKeys)
	}
	if !reflect.DeepEqual(trace.UntracedFields, []string{"text.headline"}) {
		t.Fatalf("untraced %+v", trace.UntracedFields)
	}
}

func TestBuildTextTraceUserImageOverride(t *testing.T) {
	trace := BuildTextTrace(TextTraceInput{
		ImageTypeKey:      "specifications",
		UserImageOverride: true,
		Prompt: map[string]any{
			"text": map[string]any{"headline": "节日限定文案", "subtitle": nil, "body": nil},
		},
		Facts: nil,
	})
	if !trace.TextQualified || !trace.UserImageOverride {
		t.Fatalf("override must qualify: %+v", trace)
	}
	if len(trace.Entries) != 1 || !trace.Entries[0].UserImageOverride || !trace.Entries[0].Traced {
		t.Fatalf("entries %+v", trace.Entries)
	}
}

func TestSellingPointOneReasonPass(t *testing.T) {
	trace := BuildTextTrace(TextTraceInput{
		ImageTypeKey: "selling_point",
		Prompt: map[string]any{
			"content": map[string]any{
				"selling_points": []any{"轻量杯身"},
			},
			"text": map[string]any{"headline": nil, "subtitle": nil, "body": nil},
		},
		Facts: []map[string]any{
			{"key": "weight", "value": "轻量杯身", "status": "confirmed", "layer": "marketing"},
		},
	})
	if !trace.TextQualified {
		t.Fatalf("expected qualified: %+v", trace)
	}
	if trace.SellingPointCheck == nil || !trace.SellingPointCheck.Applicable || !trace.SellingPointCheck.Pass {
		t.Fatalf("selling check %+v", trace.SellingPointCheck)
	}
	if trace.SellingPointCheck.ReasonCount != 1 {
		t.Fatalf("reason_count %d", trace.SellingPointCheck.ReasonCount)
	}
}

func TestSellingPointOneReasonRejectsPileUp(t *testing.T) {
	trace := BuildTextTrace(TextTraceInput{
		ImageTypeKey: "selling_point",
		Prompt: map[string]any{
			"content": map[string]any{
				"selling_points": []any{"轻量杯身", "明星同款", "送礼首选"},
			},
		},
		Facts: []map[string]any{
			{"key": "weight", "value": "轻量杯身"},
			{"key": "collab", "value": "明星同款"},
			{"key": "gift", "value": "送礼首选"},
		},
	})
	if trace.TextQualified {
		t.Fatalf("pile-up must fail: %+v", trace)
	}
	if trace.SellingPointCheck == nil || trace.SellingPointCheck.Pass || trace.SellingPointCheck.ReasonCount != 3 {
		t.Fatalf("selling check %+v", trace.SellingPointCheck)
	}
}

func TestSellingPointOneReasonRejectsUnbackedSingle(t *testing.T) {
	check := CheckSellingPointOneReason("selling_point", map[string]any{
		"content": map[string]any{"selling_points": []any{"24h 保温口号"}},
	}, []TextTraceEntry{{
		Field: "content.selling_points[0]", Text: "24h 保温口号", Traced: false,
	}})
	if check.Pass || !check.Applicable || check.ReasonCount != 1 {
		t.Fatalf("%+v", check)
	}
}

func TestSellingPointCheckNotApplicableForHero(t *testing.T) {
	check := CheckSellingPointOneReason("hero", map[string]any{
		"content": map[string]any{"selling_points": []any{"a", "b"}},
	}, nil)
	if check.Applicable || !check.Pass {
		t.Fatalf("%+v", check)
	}
}

func TestTextTraceAsMapRoundTripKeys(t *testing.T) {
	trace := BuildTextTrace(TextTraceInput{
		ImageTypeKey: "specifications",
		Prompt:       map[string]any{"text": map[string]any{"headline": "600ml"}},
		Facts:        []map[string]any{{"key": "capacity", "value": "600ml"}},
	})
	raw := TextTraceAsMap(trace)
	keys, ok := raw["fact_keys"].([]any)
	if !ok || len(keys) != 1 || keys[0] != "capacity" {
		t.Fatalf("fact_keys %+v", raw["fact_keys"])
	}
	if raw["text_qualified"] != true {
		t.Fatalf("%+v", raw)
	}
}

func TestStripV3PromptPayloadDropsTextTrace(t *testing.T) {
	got := stripV3PromptPayload(map[string]any{
		"design_goal": "目标",
		"text_trace":  map[string]any{"fact_keys": []any{"capacity"}},
		"fact_keys":   []any{"legacy"},
	})
	if _, ok := got["text_trace"]; ok {
		t.Fatal("text_trace must be stripped from live prompt document")
	}
	if _, ok := got["fact_keys"]; ok {
		t.Fatal("fact_keys must stay stripped")
	}
	if got["design_goal"] != "目标" {
		t.Fatalf("%+v", got)
	}
}

func TestNodeHasTextOverride(t *testing.T) {
	if nodeHasTextOverride(nil) || nodeHasTextOverride(map[string]any{}) {
		t.Fatal("empty")
	}
	if nodeHasTextOverride(map[string]any{"text_override": nil}) {
		t.Fatal("nil override")
	}
	if !nodeHasTextOverride(map[string]any{"text_override": map[string]any{"policy": "required", "language": "zh-CN"}}) {
		t.Fatal("map override")
	}
}
