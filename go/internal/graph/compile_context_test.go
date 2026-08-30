package graph

import "testing"

func TestCompiledContextTraceCountsOnlyIncomingEdges(t *testing.T) {
	otherFacts := []map[string]any{{"key": "other", "value": "x"}}
	ownFacts := []map[string]any{{"key": "product_name", "value": "杯"}}
	g := AppliedGraph{
		Revision: 1,
		Nodes: []AppliedNode{
			{ID: "src", NodeType: NodeProductSource, Title: "资料", Config: map[string]any{}},
			{ID: "brief", NodeType: NodeCreativeBrief, Title: "要求", Config: map[string]any{"goal": "卖"}},
			{ID: "prompt", NodeType: NodePromptGeneration, Title: "提示词", Config: map[string]any{"image_type_key": "hero"}},
			{ID: "other", NodeType: NodePromptGeneration, Title: "其他", Config: map[string]any{"image_type_key": "scene"}},
		},
		Edges: []AppliedEdge{
			{ID: "e-facts", SourceNodeID: "src", TargetNodeID: "prompt", DataType: DataProductFacts, Role: RoleFacts, Order: 0},
			{ID: "e-brief", SourceNodeID: "brief", TargetNodeID: "prompt", DataType: DataCreativeBrief, Role: RoleBrief, Order: 1},
			{ID: "e-other", SourceNodeID: "src", TargetNodeID: "other", DataType: DataProductFacts, Role: RoleFacts, Order: 0},
		},
	}
	sources := map[string]SourceRecord{
		"src":   {Facts: ownFacts},
		"brief": {Brief: map[string]any{"goal": "卖"}},
		"other": {Facts: otherFacts},
	}
	trace := compiledContextTrace(g, g.Nodes[2], sources, "digest-1")
	ids, _ := trace["incoming_edge_ids"].([]string)
	got := map[string]bool{}
	for _, id := range ids {
		got[id] = true
	}
	if len(ids) != 2 || !got["e-facts"] || !got["e-brief"] {
		t.Fatalf("incoming_edge_ids %+v", trace["incoming_edge_ids"])
	}
	if trace["input_digest"] != "digest-1" {
		t.Fatalf("digest %+v", trace["input_digest"])
	}
	if trace["fact_count"] != 1 {
		t.Fatalf("fact_count %+v (must not scan the other prompt node)", trace["fact_count"])
	}
	if trace["brief_count"] != 1 {
		t.Fatalf("brief_count %+v", trace["brief_count"])
	}
}

func TestCompiledContextTraceImageIncludesPromptArtifact(t *testing.T) {
	promptID := "art-prompt"
	g := AppliedGraph{
		Revision: 1,
		Nodes: []AppliedNode{
			{ID: "prompt", NodeType: NodePromptGeneration, Title: "提示词", Config: map[string]any{"prompt": map[string]any{"design_goal": "主图"}}},
			{ID: "image", NodeType: NodeImageGeneration, Title: "生图", Config: map[string]any{"image_type_key": "hero"}},
		},
		Edges: []AppliedEdge{
			{ID: "e-prompt", SourceNodeID: "prompt", TargetNodeID: "image", DataType: DataPrompt, Role: RolePrompt, Order: 0},
		},
	}
	sources := map[string]SourceRecord{
		"prompt": {
			CurrentArtifactID: &promptID,
			CurrentArtifactPayload: map[string]any{
				"schema_version": 1,
				"design_goal":    "主图",
				"shared_rules":   []any{"锁商品"},
			},
		},
	}
	trace := compiledContextTrace(g, g.Nodes[1], sources, "img-digest")
	if trace["prompt_artifact_id"] != promptID {
		t.Fatalf("prompt_artifact_id %+v", trace["prompt_artifact_id"])
	}
	if trace["prompt_edge_id"] != "e-prompt" {
		t.Fatalf("prompt_edge_id %+v", trace["prompt_edge_id"])
	}
	if _, ok := trace["fact_count"]; ok {
		t.Fatalf("image trace must not include fact_count: %+v", trace)
	}
}

func TestCollectPromptInputsPrefersMergedVisualPayload(t *testing.T) {
	g := AppliedGraph{
		Revision: 1,
		Nodes: []AppliedNode{
			{
				ID: "visual", NodeType: NodeVisualSystem, Title: "视觉",
				Config: map[string]any{
					"visual_system_version_id": "ver-1",
					"visual_overlay":           map[string]any{"style": []any{"overlay-style"}},
				},
			},
			{ID: "prompt", NodeType: NodePromptGeneration, Title: "提示词", Config: map[string]any{"image_type_key": "hero"}},
		},
		Edges: []AppliedEdge{
			{ID: "e-vis", SourceNodeID: "visual", TargetNodeID: "prompt", DataType: DataVisualSystem, Role: RoleVisualGuidance, Order: 0},
		},
	}
	sources := map[string]SourceRecord{
		"visual": {
			VisualPayload:         map[string]any{"style": []any{"draft-style"}, "photography": map[string]any{"lighting": "柔光"}},
			VisualSystemVersionID: strPtr("ver-1"),
		},
	}
	_, _, visual, _, err := collectPromptInputs(g, "prompt", sources)
	if err != nil {
		t.Fatal(err)
	}
	if visual["photography"] == nil {
		t.Fatalf("merged visual must keep version draft fields, got %+v", visual)
	}
	style, _ := visual["style"].([]any)
	if len(style) != 1 || style[0] != "overlay-style" {
		t.Fatalf("overlay must merge onto draft style, got %+v", visual["style"])
	}
}

func TestAssemblePromptRequestKeepsFullVisualDraft(t *testing.T) {
	node := AppliedNode{
		ID: "prompt-1", NodeType: NodePromptGeneration, Title: "提示词",
		Config: map[string]any{"image_type_key": "hero", "prompt": map[string]any{}},
	}
	visual := map[string]any{
		"style":       []any{"商业套图"},
		"photography": map[string]any{"lighting": "柔光"},
		"colors":      []any{map[string]any{"role": "background", "value": "#F3EFE8", "label": "暖白"}},
	}
	req, err := AssemblePromptRequest(node, nil, nil, visual, nil, "d")
	if err != nil {
		t.Fatal(err)
	}
	if req.Visual == nil || req.Visual["photography"] == nil {
		t.Fatalf("full visual draft must stay on visual_system, got %+v exceptions %+v", req.Visual, req.VisualExceptions)
	}
	if len(req.VisualExceptions) != 0 {
		t.Fatalf("full draft must not be coerced to inline overlay exceptions: %+v", req.VisualExceptions)
	}
}
