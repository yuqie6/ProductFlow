package graph

import (
	"strings"
	"testing"
)

func TestAssemblePromptRequestBuildsListingSeed(t *testing.T) {
	node := AppliedNode{
		ID:       "prompt-1",
		NodeType: NodePromptGeneration,
		Title:    "核心卖点图提示词",
		Config: map[string]any{
			"image_type_key": "selling_point",
			"prompt":         map[string]any{},
		},
	}
	facts := []map[string]any{{"key": "product_name", "value": "云白瓷杯"}}
	req, err := AssemblePromptRequest(node, facts, nil, nil, nil, "digest")
	if err != nil {
		t.Fatal(err)
	}
	if !req.GenerateFromContext {
		t.Fatal("empty prompt config must be treated as generation seed")
	}
	if req.ImageTypeKey != "selling_point" || req.ImageTypeFamily != "infographic" {
		t.Fatalf("type %+v family %q", req.ImageTypeKey, req.ImageTypeFamily)
	}
	if req.ImageTypeTitle != "核心卖点图" {
		t.Fatalf("title %q", req.ImageTypeTitle)
	}
	if !strings.Contains(req.ImageTypeJob, "详情卖点图") {
		t.Fatalf("job %q", req.ImageTypeJob)
	}
	if _, ok := req.CurrentPrompt["visual_variant_key"]; !ok {
		t.Fatal("seed must include visual_variant_key")
	}
	if req.CurrentPrompt["visual_variant_key"] != nil {
		t.Fatalf("visual_variant_key %+v", req.CurrentPrompt["visual_variant_key"])
	}
	if req.CurrentPrompt["design_goal"] == nil {
		t.Fatal("seed must have design_goal")
	}
	shared, _ := req.CurrentPrompt["shared_rules"].([]any)
	if len(shared) == 0 {
		t.Fatal("seed must include identity shared rules")
	}
}

func TestApplyTextPolicyClearsOnImageCopy(t *testing.T) {
	payload := map[string]any{
		"text":        map[string]any{"headline": "买它", "subtitle": nil, "body": nil},
		"composition": map[string]any{"copy_regions": []any{"顶栏"}},
		"design_goal": "卖点",
	}
	got := ApplyTextPolicyToPrompt(payload, "none", false)
	text, _ := got["text"].(map[string]any)
	if text["headline"] != nil {
		t.Fatalf("headline %+v", text["headline"])
	}
	composition, _ := got["composition"].(map[string]any)
	regions, _ := composition["copy_regions"].([]any)
	if len(regions) != 0 {
		t.Fatalf("copy_regions %+v", regions)
	}
}

func TestStripV3PromptPayloadDropsPlanKeys(t *testing.T) {
	got := stripV3PromptPayload(map[string]any{
		"design_goal":    "目标",
		"images":         []any{map[string]any{"image_plan_key": "a"}},
		"image_plan_key": "x",
		"fact_keys":      []any{"k"},
	})
	if _, ok := got["images"]; ok {
		t.Fatal("images must be stripped")
	}
	if _, ok := got["image_plan_key"]; ok {
		t.Fatal("image_plan_key must be stripped")
	}
	if got["design_goal"] != "目标" {
		t.Fatalf("kept %+v", got)
	}
}
