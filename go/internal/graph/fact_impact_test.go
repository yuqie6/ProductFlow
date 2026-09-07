package graph

import "testing"

func TestDiffFactKeysCapacityChange(t *testing.T) {
	current := []map[string]any{{"key": "capacity", "value": "500ml"}, {"key": "material", "value": "钢"}}
	proposed := []map[string]any{{"key": "capacity", "value": "600ml"}, {"key": "material", "value": "钢"}}
	got := DiffFactKeys(current, proposed)
	if len(got) != 1 || got[0] != "capacity" {
		t.Fatalf("got %+v", got)
	}
}

func TestPreviewFactImpactListsSpecAndSellingNotSceneByDefault(t *testing.T) {
	factV1 := "fact-v1"
	artifactSpec := "art-spec"
	artifactScene := "art-scene"
	artifactSell := "art-sell"
	g := AppliedGraph{
		Revision: 1,
		Nodes: []AppliedNode{
			{ID: "src", NodeType: NodeProductSource, Title: "资料", Config: map[string]any{"source_product_id": "prod-1"}},
			{ID: "spec-prompt", NodeType: NodeImagePrompt, Title: "规格提示词", Config: map[string]any{
				"image_type_key": "specifications",
				"prompt":         map[string]any{"text": map[string]any{"body": "容量 600ml"}},
			}},
			{ID: "sell-prompt", NodeType: NodeImagePrompt, Title: "卖点提示词", Config: map[string]any{
				"image_type_key": "selling_point",
				"prompt":         map[string]any{"content": map[string]any{"selling_points": []any{"轻量杯身 600ml 便携"}}},
			}},
			{ID: "scene-prompt", NodeType: NodeImagePrompt, Title: "场景提示词", Config: map[string]any{
				"image_type_key": "lifestyle",
				"prompt":         map[string]any{"text": map[string]any{"body": "户外露营氛围"}},
			}},
			{ID: "orphan-prompt", NodeType: NodeImagePrompt, Title: "未连边", Config: map[string]any{
				"image_type_key": "specifications",
				"prompt":         map[string]any{"text": map[string]any{"body": "容量 600ml"}},
			}},
			{ID: "spec-image", NodeType: NodeImageGeneration, Title: "规格图", Config: map[string]any{"image_type_key": "specifications"}},
			{ID: "scene-image", NodeType: NodeImageGeneration, Title: "场景图", Config: map[string]any{"image_type_key": "lifestyle"}},
		},
		Edges: []AppliedEdge{
			{ID: "e1", SourceNodeID: "src", TargetNodeID: "spec-prompt", DataType: DataProductFacts, Role: RoleFacts, Order: 0},
			{ID: "e2", SourceNodeID: "src", TargetNodeID: "sell-prompt", DataType: DataProductFacts, Role: RoleFacts, Order: 0},
			{ID: "e3", SourceNodeID: "src", TargetNodeID: "scene-prompt", DataType: DataProductFacts, Role: RoleFacts, Order: 0},
			{ID: "e4", SourceNodeID: "spec-prompt", TargetNodeID: "spec-image", DataType: DataPrompt, Role: RolePrompt, Order: 0},
			{ID: "e5", SourceNodeID: "scene-prompt", TargetNodeID: "scene-image", DataType: DataPrompt, Role: RolePrompt, Order: 0},
		},
	}
	facts := []map[string]any{
		{"key": "capacity", "value": "600ml", "status": "confirmed"},
		{"key": "selling_point", "value": "轻量杯身", "status": "confirmed", "layer": "marketing"},
	}
	pid := "prod-1"
	sources := map[string]SourceRecord{
		"src": {
			Facts: facts,
			ProductSource: &productSourceSnapshot{
				SourceProductID:  &pid,
				FactSetVersionID: &factV1,
				Facts:            facts,
			},
		},
		"spec-prompt": {
			CurrentArtifactID: &artifactSpec,
			CurrentArtifactPayload: map[string]any{
				"text_trace": map[string]any{"fact_keys": []any{"capacity"}},
			},
			PromptDocument: map[string]any{"text": map[string]any{"body": "容量 600ml"}},
		},
		"sell-prompt": {
			CurrentArtifactID: &artifactSell,
			CurrentArtifactPayload: map[string]any{
				"text_trace": map[string]any{"fact_keys": []any{"capacity", "selling_point"}},
			},
		},
		"scene-prompt": {
			CurrentArtifactID: &artifactScene,
			CurrentArtifactPayload: map[string]any{
				"text_trace": map[string]any{"fact_keys": []any{}},
			},
			PromptDocument: map[string]any{"text": map[string]any{"body": "户外露营氛围"}},
		},
		"orphan-prompt": {
			PromptDocument: map[string]any{"text": map[string]any{"body": "容量 600ml"}},
		},
		"spec-image":  {CurrentArtifactID: strPtr("art-spec-img")},
		"scene-image": {CurrentArtifactID: strPtr("art-scene-img")},
	}
	sources = MergeRoleFactsIntoSources(g, sources)
	preview := PreviewFactImpact(g, sources, "prod-1", []string{"capacity"})

	byID := map[string]FactImpactNode{}
	for _, node := range preview.Nodes {
		byID[node.NodeID] = node
	}
	if _, ok := byID["orphan-prompt"]; ok {
		t.Fatal("must not inject disconnected orphan prompt")
	}
	if !byID["spec-prompt"].DefaultSelected || !byID["spec-image"].DefaultSelected {
		t.Fatalf("spec should default selected: %+v", byID["spec-prompt"])
	}
	if !byID["sell-prompt"].DefaultSelected {
		t.Fatalf("selling point should default selected: %+v", byID["sell-prompt"])
	}
	if byID["scene-prompt"].DefaultSelected || byID["scene-image"].DefaultSelected {
		t.Fatalf("scene without capacity must not default select: scene=%+v image=%+v", byID["scene-prompt"], byID["scene-image"])
	}
	if byID["scene-prompt"].BoundFactSetVersionID == nil || *byID["scene-prompt"].BoundFactSetVersionID != factV1 {
		t.Fatalf("must explain bound fact_set_version_id: %+v", byID["scene-prompt"])
	}
	if len(preview.DefaultUpdateNodeIDs) < 2 {
		t.Fatalf("default update set %+v", preview.DefaultUpdateNodeIDs)
	}
}

func TestPreviewFactImpactEmptyWhenNoChangedKeys(t *testing.T) {
	preview := PreviewFactImpact(AppliedGraph{}, nil, "prod", nil)
	if len(preview.Nodes) != 0 {
		t.Fatalf("%+v", preview.Nodes)
	}
}

func TestIncomingFactSetVersionsOnlyRoleFacts(t *testing.T) {
	// 回归：未连边资料不得进入 fact_set_versions 签名。
	v1 := "fact-1"
	g := AppliedGraph{
		Nodes: []AppliedNode{
			{ID: "src", NodeType: NodeProductSource},
			{ID: "other", NodeType: NodeProductSource},
			{ID: "prompt", NodeType: NodeImagePrompt, Config: map[string]any{"image_type_key": "hero"}},
		},
		Edges: []AppliedEdge{
			{ID: "e-facts", SourceNodeID: "src", TargetNodeID: "prompt", DataType: DataProductFacts, Role: RoleFacts, Order: 0},
		},
	}
	sources := map[string]SourceRecord{
		"src":   {ProductSource: &productSourceSnapshot{FactSetVersionID: &v1}},
		"other": {ProductSource: &productSourceSnapshot{FactSetVersionID: strPtr("fact-other")}},
	}
	got := incomingFactSetVersions(g, "prompt", sources)
	if len(got) != 1 || got[0]["fact_set_version_id"] != v1 {
		t.Fatalf("got %+v", got)
	}
}

func TestSkipUnchangedRequiresMatchingDigest(t *testing.T) {
	digest := "abc"
	artifact := "art-1"
	e := Executor{}
	nodeID := "n1"
	req := nodeID
	run := graphRunRow{Force: true, RequestedNodeID: &req, RequestedNodeIDs: []string{nodeID}}
	nodeRun := graphNodeRunRow{NodeID: &nodeID}
	sources := map[string]SourceRecord{
		"n1": {CurrentArtifactID: &artifact, CurrentInputDigest: &digest},
	}
	skipped, err := e.skipUnchanged(nil, run, nodeRun, sources, digest)
	if err != nil {
		t.Fatal(err)
	}
	if skipped {
		t.Fatal("force target must not skip")
	}
	run.Force = false
	run.RequestedNodeID = nil
	run.RequestedNodeIDs = nil
	sources["n1"] = SourceRecord{CurrentArtifactID: &artifact, CurrentInputDigest: strPtr("other")}
	skipped, err = e.skipUnchanged(nil, run, nodeRun, sources, digest)
	if err != nil {
		t.Fatal(err)
	}
	if skipped {
		t.Fatal("mismatched digest must not skip")
	}
}
