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
			{ID: "prompt", NodeType: NodeImagePrompt, Title: "提示词", Config: map[string]any{"image_type_key": "hero"}},
			{ID: "other", NodeType: NodeImagePrompt, Title: "其他", Config: map[string]any{"image_type_key": "scene"}},
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
			{ID: "prompt", NodeType: NodeImagePrompt, Title: "提示词", Config: map[string]any{"prompt": map[string]any{"design_goal": "主图"}}},
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

func TestGraphRuntimeInputTraceFillsArtifactIdentities(t *testing.T) {
	promptID := "art-prompt"
	factVersion := "fact-v1"
	bound := "asset-ref"
	g := AppliedGraph{
		Revision: 1,
		Nodes: []AppliedNode{
			{ID: "src", NodeType: NodeProductSource, Title: "资料", Config: map[string]any{}},
			{ID: "ref", NodeType: NodeImageAsset, Title: "参考", BoundAssetID: &bound, Config: map[string]any{}},
			{ID: "prompt", NodeType: NodeImagePrompt, Title: "提示词", Config: map[string]any{"image_type_key": "hero"}},
			{ID: "image", NodeType: NodeImageGeneration, Title: "生图", Config: map[string]any{"image_type_key": "hero"}},
		},
		Edges: []AppliedEdge{
			{ID: "e-prompt", SourceNodeID: "prompt", TargetNodeID: "image", DataType: DataPrompt, Role: RolePrompt, Order: 0},
			{ID: "e-ref", SourceNodeID: "ref", TargetNodeID: "image", DataType: DataImageAsset, Role: RoleReference, Order: 1},
			{ID: "e-facts", SourceNodeID: "src", TargetNodeID: "prompt", DataType: DataProductFacts, Role: RoleFacts, Order: 0},
		},
	}
	sources := map[string]SourceRecord{
		"src": {
			ProductSource: &productSourceSnapshot{FactSetVersionID: &factVersion},
		},
		"ref": {BoundAssetID: &bound},
		"prompt": {
			CurrentArtifactID:   &promptID,
			CurrentArtifactType: strPtr("prompt"),
		},
	}
	imageTrace := graphRuntimeInputTrace(g, "image", sources)
	if len(imageTrace) != 2 {
		t.Fatalf("image incoming %+v", imageTrace)
	}
	if imageTrace[0]["edge_id"] != "e-prompt" || imageTrace[0]["artifact_id"] != promptID {
		t.Fatalf("prompt identity %+v", imageTrace[0])
	}
	if imageTrace[0]["artifact_type"] != "prompt" {
		t.Fatalf("prompt type %+v", imageTrace[0])
	}
	if imageTrace[1]["edge_id"] != "e-ref" || imageTrace[1]["asset_id"] != bound {
		t.Fatalf("reference identity %+v", imageTrace[1])
	}
	promptTrace := graphRuntimeInputTrace(g, "prompt", sources)
	if len(promptTrace) != 1 || promptTrace[0]["version_id"] != factVersion {
		t.Fatalf("facts version %+v", promptTrace)
	}
	merged := compiledContextTrace(g, g.Nodes[3], sources, "digest-img")
	raw, ok := merged["input_trace"].([]map[string]any)
	if !ok || len(raw) != 2 {
		t.Fatalf("compiled_context must replace snapshot input_trace: %+v", merged["input_trace"])
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
			{ID: "prompt", NodeType: NodeImagePrompt, Title: "提示词", Config: map[string]any{"image_type_key": "hero"}},
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
		ID: "prompt-1", NodeType: NodeImagePrompt, Title: "提示词",
		Config: map[string]any{"image_type_key": "hero", "prompt": map[string]any{}},
	}
	visual := map[string]any{
		"style":       []any{"商业套图"},
		"photography": map[string]any{"lighting": "柔光"},
		"colors":      []any{map[string]any{"role": "background", "value": "#F3EFE8", "label": "暖白"}},
	}
	req, err := AssemblePromptRequest(node, nil, nil, visual, nil, "d", AppliedGraph{})
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

func TestCompileImageRuntimeHashesPromptDocumentNotArtifactID(t *testing.T) {
	g := AppliedGraph{
		Revision: 1,
		Nodes: []AppliedNode{
			{ID: "prompt", NodeType: NodeImagePrompt, Title: "提示词", Config: map[string]any{
				"prompt": map[string]any{"design_goal": "主图", "composition": map[string]any{"layout": "居中"}},
			}},
			{ID: "image", NodeType: NodeImageGeneration, Title: "生图", Config: map[string]any{
				"image_type_key": "hero",
				"generation_spec": map[string]any{
					"aspect_ratio": "1:1", "resolution_tier": "high", "quality_intent": "high",
					"reference_fidelity": "high", "background_intent": "auto", "text_policy": "none",
				},
			}},
		},
		Edges: []AppliedEdge{
			{ID: "e-prompt", SourceNodeID: "prompt", TargetNodeID: "image", DataType: DataPrompt, Role: RolePrompt, Order: 0},
		},
	}
	sources := map[string]SourceRecord{
		"prompt": {PromptDocument: map[string]any{"design_goal": "主图", "composition": map[string]any{"layout": "居中"}}},
	}
	first, err := compileImageRuntime(g, "image", sources)
	if err != nil {
		t.Fatal(err)
	}
	g.Nodes[0].Config["prompt"] = map[string]any{"design_goal": "主图", "composition": map[string]any{"layout": "左侧留白"}}
	sources["prompt"] = SourceRecord{PromptDocument: map[string]any{"design_goal": "主图", "composition": map[string]any{"layout": "左侧留白"}}}
	second, err := compileImageRuntime(g, "image", sources)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("changing prompt layout must change image digest")
	}
}

func TestCompileImageRuntimeHashesUpstreamVisualOverlay(t *testing.T) {
	spec := map[string]any{
		"aspect_ratio": "1:1", "resolution_tier": "high", "quality_intent": "high",
		"reference_fidelity": "high", "background_intent": "auto", "text_policy": "none",
	}
	g := AppliedGraph{
		Revision: 1,
		Nodes: []AppliedNode{
			{ID: "visual", NodeType: NodeVisualSystem, Title: "视觉", Config: map[string]any{
				"visual_overlay": map[string]any{"style": []any{"冷色"}},
			}},
			{ID: "prompt", NodeType: NodeImagePrompt, Title: "提示词", Config: map[string]any{
				"prompt": map[string]any{"design_goal": "主图"},
			}},
			{ID: "image", NodeType: NodeImageGeneration, Title: "生图", Config: map[string]any{
				"image_type_key": "hero", "generation_spec": spec,
			}},
		},
		Edges: []AppliedEdge{
			{ID: "e-prompt", SourceNodeID: "prompt", TargetNodeID: "image", DataType: DataPrompt, Role: RolePrompt, Order: 0},
			{ID: "e-vis", SourceNodeID: "visual", TargetNodeID: "image", DataType: DataVisualSystem, Role: RoleVisualGuidance, Order: 1},
		},
	}
	sources := map[string]SourceRecord{
		"prompt": {PromptDocument: map[string]any{"design_goal": "主图"}},
		"visual": {VisualPayload: map[string]any{"style": []any{"冷色"}}},
	}
	first, err := compileImageRuntime(g, "image", sources)
	if err != nil {
		t.Fatal(err)
	}
	g.Nodes[0].Config["visual_overlay"] = map[string]any{"style": []any{"暖色"}}
	sources["visual"] = SourceRecord{VisualPayload: map[string]any{"style": []any{"暖色"}}}
	second, err := compileImageRuntime(g, "image", sources)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("changing visual overlay must change image digest")
	}
}

func TestIncomingPromptDocumentUsesConfigWithoutArtifact(t *testing.T) {
	g := AppliedGraph{
		Revision: 1,
		Nodes: []AppliedNode{
			{ID: "prompt", NodeType: NodeImagePrompt, Title: "提示词", Config: map[string]any{
				"prompt": map[string]any{"design_goal": "主图"},
			}},
			{ID: "image", NodeType: NodeImageGeneration, Title: "生图", Config: map[string]any{}},
		},
		Edges: []AppliedEdge{
			{ID: "e-prompt", SourceNodeID: "prompt", TargetNodeID: "image", DataType: DataPrompt, Role: RolePrompt, Order: 0},
		},
	}
	payload, artifactID, err := incomingPromptDocument(g, "image", map[string]SourceRecord{})
	if err != nil {
		t.Fatal(err)
	}
	if artifactID != "" {
		t.Fatalf("artifact id %q", artifactID)
	}
	if payload["design_goal"] != "主图" {
		t.Fatalf("payload %+v", payload)
	}
}

func TestCatalogBriefFieldsAffectDigest(t *testing.T) {
	keys := catalogDigestKeys(NodeCreativeBrief)
	for _, key := range []string{"goal", "design_goals", "required_copy", "prohibitions"} {
		if _, ok := keys[key]; !ok {
			t.Fatalf("missing %s in %+v", key, keys)
		}
	}
}

func TestCompileContextRuntimeHashesBriefGoal(t *testing.T) {
	g := AppliedGraph{
		Revision: 1,
		Nodes: []AppliedNode{
			{ID: "brief", NodeType: NodeCreativeBrief, Title: "要求", Config: map[string]any{"goal": "卖"}},
		},
	}
	first, err := compileContextRuntime(g, "brief", map[string]SourceRecord{})
	if err != nil {
		t.Fatal(err)
	}
	g.Nodes[0].Config["goal"] = "改过"
	second, err := compileContextRuntime(g, "brief", map[string]SourceRecord{})
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("changing brief goal must change digest")
	}
}

func TestCompileContextRuntimeHashesIncomingFactSetVersion(t *testing.T) {
	v1 := "fact-v1"
	v2 := "fact-v2"
	g := AppliedGraph{
		Revision: 1,
		Nodes: []AppliedNode{
			{ID: "source", NodeType: NodeProductSource, Title: "资料"},
			{ID: "brief", NodeType: NodeCreativeBrief, Title: "要求", Config: map[string]any{"goal": "卖"}},
		},
		Edges: []AppliedEdge{{
			ID: "facts", SourceNodeID: "source", TargetNodeID: "brief",
			DataType: DataProductFacts, Role: RoleFacts, Order: 0,
		}},
	}
	sources := map[string]SourceRecord{
		"source": {ProductSource: &productSourceSnapshot{FactSetVersionID: &v1}},
	}
	first, err := compileContextRuntime(g, "brief", sources)
	if err != nil {
		t.Fatal(err)
	}
	sources["source"] = SourceRecord{ProductSource: &productSourceSnapshot{FactSetVersionID: &v2}}
	second, err := compileContextRuntime(g, "brief", sources)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("changing incoming fact-set version must change digest")
	}
}
