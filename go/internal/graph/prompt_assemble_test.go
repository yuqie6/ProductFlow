package graph

import (
	"strings"
	"testing"
)

func TestAssemblePromptRequestBuildsListingSeed(t *testing.T) {
	node := AppliedNode{
		ID:             "prompt-1",
		NodeType:       NodeImagePrompt,
		Title:          "核心卖点图提示词",
		DocumentOrigin: OriginSeed,
		Config: map[string]any{
			"image_type_key": "selling_point",
			"prompt":         map[string]any{},
		},
	}
	facts := []map[string]any{{"key": "product_name", "value": "云白瓷杯"}}
	req, err := AssemblePromptRequest(node, facts, nil, nil, nil, "digest", AppliedGraph{})
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

func TestAssemblePromptRequestAuthoredDoesNotGenerateFromContext(t *testing.T) {
	node := AppliedNode{
		ID:             "prompt-1",
		NodeType:       NodeImagePrompt,
		Title:          "核心卖点图提示词",
		DocumentOrigin: OriginAuthored,
		Config: map[string]any{
			"image_type_key": "selling_point",
			"prompt":         map[string]any{"design_goal": imageTypePromptGoal("selling_point")},
		},
	}
	req, err := AssemblePromptRequest(node, nil, nil, nil, nil, "digest", AppliedGraph{})
	if err != nil {
		t.Fatal(err)
	}
	if req.GenerateFromContext {
		t.Fatal("authored prompt must not be treated as generation seed")
	}
}

func TestAssemblePromptRequestIncludesBriefsImageTypesAndDownstreamTextPolicy(t *testing.T) {
	prompt := AppliedNode{
		ID:       "prompt-1",
		NodeType: NodeImagePrompt,
		Title:    "信息图提示词",
		Config: map[string]any{
			"image_type_key": "faq",
			"prompt":         map[string]any{"design_goal": imageTypePromptGoal("faq")},
		},
	}
	image := AppliedNode{
		ID:       "image-1",
		NodeType: NodeImageGeneration,
		Title:    "信息图",
		Config: map[string]any{
			"image_type_key": "faq",
			"generation_spec": map[string]any{
				"text_policy":   "required",
				"text_language": "zh-CN",
			},
		},
	}
	g := AppliedGraph{
		Nodes: []AppliedNode{prompt, image},
		Edges: []AppliedEdge{
			{ID: "e-prompt", SourceNodeID: prompt.ID, TargetNodeID: image.ID, DataType: DataPrompt, Role: RolePrompt, Order: 0},
		},
	}
	briefs := []map[string]any{
		{"goal": "卖点一", "design_goals": []any{"镜头 A"}},
		{"goal": "卖点二", "required_copy": []any{"限时"}},
	}
	req, err := AssemblePromptRequest(prompt, nil, briefs, nil, nil, "digest", g)
	if err != nil {
		t.Fatal(err)
	}
	if req.TextPolicy != "required" || req.TextLanguage != "zh-CN" {
		t.Fatalf("text policy %q %q", req.TextPolicy, req.TextLanguage)
	}
	if len(req.Briefs) != 2 || req.Briefs[0]["goal"] != "卖点一" {
		t.Fatalf("briefs %+v", req.Briefs)
	}
	if len(req.ImageTypes) != 1 {
		t.Fatalf("image types %+v", req.ImageTypes)
	}
	if req.ImageTypes[0]["key"] != "faq" {
		t.Fatalf("image type %+v", req.ImageTypes[0])
	}
}

func TestAssembleCreativeBriefCollectsGraphImageTypes(t *testing.T) {
	brief := AppliedNode{ID: "brief", NodeType: NodeCreativeBrief, Title: "要求", Config: map[string]any{"goal": "卖"}}
	prompt := AppliedNode{
		ID: "prompt", NodeType: NodeImagePrompt, Title: "提示词",
		Config: map[string]any{"image_type_key": "hero"},
	}
	g := AppliedGraph{Nodes: []AppliedNode{brief, prompt}}
	req, err := AssemblePromptRequest(brief, nil, nil, nil, nil, "digest", g)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.ImageTypes) != 1 || req.ImageTypes[0]["key"] != "hero" {
		t.Fatalf("image types %+v", req.ImageTypes)
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
