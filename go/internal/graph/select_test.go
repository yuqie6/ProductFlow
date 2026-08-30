package graph

import "testing"

func TestSelectRunNodeIDsEmptyGraph(t *testing.T) {
	_, err := SelectRunNodeIDs(EmptyGraph, RunScopeGraph, "", nil)
	if err == nil || err.Error() != "没有可运行的处理节点" {
		t.Fatalf("err %v", err)
	}
}

func TestSelectRunNodeIDsNodeScopeRequiresTarget(t *testing.T) {
	_, err := SelectRunNodeIDs(EmptyGraph, RunScopeNode, "", nil)
	if err == nil || err.Error() != "节点运行范围必须指定目标节点" {
		t.Fatalf("err %v", err)
	}
}

func cookSelectGraph() AppliedGraph {
	spec := map[string]any{
		"aspect_ratio": "1:1", "resolution_tier": "high", "quality_intent": "high",
		"reference_fidelity": "high", "background_intent": "auto", "text_policy": "none",
	}
	return AppliedGraph{
		Revision: 1,
		Nodes: []AppliedNode{
			{ID: "brief", NodeType: NodeCreativeBrief, Title: "要求", DocumentOrigin: OriginSeed, Config: map[string]any{}},
			{ID: "prompt", NodeType: NodePromptGeneration, Title: "提示词", DocumentOrigin: OriginAuthored, Config: map[string]any{
				"image_type_key": "hero",
				"prompt":         map[string]any{"design_goal": "手填", "composition": map[string]any{"layout": "左侧留白"}},
			}},
			{ID: "image", NodeType: NodeImageGeneration, Title: "生图", Config: map[string]any{
				"image_type_key":  "hero",
				"generation_spec": spec,
			}},
		},
		Edges: []AppliedEdge{
			{ID: "e-brief", SourceNodeID: "brief", TargetNodeID: "prompt", DataType: DataCreativeBrief, Role: RoleBrief, Order: 0},
			{ID: "e-prompt", SourceNodeID: "prompt", TargetNodeID: "image", DataType: DataPrompt, Role: RolePrompt, Order: 0},
		},
	}
}

func TestSelectGraphQueuesReadyNodesForSkipHistory(t *testing.T) {
	g := cookSelectGraph()
	ids, err := SelectRunNodeIDs(g, RunScopeGraph, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, id := range ids {
		got[id] = true
	}
	if !got["brief"] || !got["prompt"] || !got["image"] {
		t.Fatalf("selected %v", ids)
	}
}

func TestSelectNodeScopeDoesNotPullAncestors(t *testing.T) {
	g := cookSelectGraph()
	ids, err := SelectRunNodeIDs(g, RunScopeNode, "image", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "image" {
		t.Fatalf("selected %v", ids)
	}
}

func TestSelectToNodeSkipsAuthoredAncestors(t *testing.T) {
	g := cookSelectGraph()
	ids, err := SelectRunNodeIDs(g, RunScopeToNode, "image", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, id := range ids {
		got[id] = true
	}
	if !got["image"] || !got["brief"] || got["prompt"] {
		t.Fatalf("selected %v", ids)
	}
}

func TestSelectGraphKeepsAuthoredNodesForSkipHistory(t *testing.T) {
	g := cookSelectGraph()
	ids, err := SelectRunNodeIDsWithMode(g, RunScopeGraph, "", nil, nil, true, RegenerateReplace)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, id := range ids {
		got[id] = true
	}
	if !got["prompt"] {
		t.Fatalf("graph selection must retain authored nodes for skipped history: %v", ids)
	}
}

func TestSelectForceNodeCooksAuthoredTarget(t *testing.T) {
	g := cookSelectGraph()
	ids, err := SelectRunNodeIDsWithMode(g, RunScopeNode, "prompt", nil, nil, true, RegenerateReplace)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "prompt" {
		t.Fatalf("selected %v", ids)
	}
}

func TestSelectSelectionQueuesExplicitImages(t *testing.T) {
	g := cookSelectGraph()
	ids, err := SelectRunNodeIDsWithMode(g, RunScopeSelection, "", []string{"image"}, nil, false, RegenerateFill)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "image" {
		t.Fatalf("selected %v", ids)
	}
}

func TestPlanRunMarksAuthoredPromptFrozen(t *testing.T) {
	g := cookSelectGraph()
	nodes, err := PlanRun(g, RunScopeGraph, "", nil, nil, false, RegenerateFill)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]RunPreviewNode{}
	for _, node := range nodes {
		byID[node.NodeID] = node
	}
	if byID["prompt"].Action != PlannedFrozen {
		t.Fatalf("prompt %+v", byID["prompt"])
	}
	if byID["brief"].Action != PlannedGenerate {
		t.Fatalf("brief %+v", byID["brief"])
	}
	if byID["image"].Action != PlannedGenerate {
		t.Fatalf("image %+v", byID["image"])
	}
}
