package graph

import (
	"strings"
	"testing"
)

func TestRunScopesRejectInvalidConfigAndPreviewNamesCause(t *testing.T) {
	for _, scope := range []string{RunScopeGraph, RunScopeNode, RunScopeToNode, RunScopeSelection} {
		t.Run(scope, func(t *testing.T) {
			g := cookSelectGraph()
			g.Nodes[2].Config["generation_spec"].(map[string]any)["text_policy"] = "none"
			if _, err := SelectRunNodeIDsWithMode(g, scope, "image", []string{"image"}, nil, false, DocumentActionComplete); err == nil || !strings.Contains(err.Error(), "text_policy") {
				t.Fatalf("invalid image must reject entire request: %v", err)
			}
			preview, err := PlanRun(g, scope, "image", []string{"image"}, nil, false, DocumentActionComplete)
			if err != nil {
				t.Fatal(err)
			}
			for _, node := range preview {
				if node.NodeID == "image" && (node.Action != PlannedBlocked || !strings.Contains(node.Reason, "text_policy")) {
					t.Fatalf("preview must expose config error: %+v", node)
				}
			}
		})
	}
}

func TestRunValidatesFrozenAncestorsOnlyWithinRequestedScope(t *testing.T) {
	g := cookSelectGraph()
	g.Nodes[0].DocumentOrigin = OriginAuthored
	g.Nodes[0].Config["design_goals"] = []any{"retired field"}
	for _, scope := range []string{RunScopeGraph, RunScopeToNode, RunScopeNode, RunScopeSelection} {
		if _, err := SelectRunNodeIDsWithMode(g, scope, "image", []string{"image"}, nil, false, DocumentActionComplete); err == nil || !strings.Contains(err.Error(), "design_goals") {
			t.Fatalf("%s must reject invalid frozen ancestor: %v", scope, err)
		}
		preview, err := PlanRun(g, scope, "image", []string{"image"}, nil, false, DocumentActionComplete)
		if err != nil {
			t.Fatal(err)
		}
		for _, node := range preview {
			if node.NodeID == "image" && (node.Action != PlannedBlocked || !strings.Contains(node.Reason, "design_goals")) {
				t.Fatalf("%s must show invalid ancestor: %+v", scope, node)
			}
		}
	}
	g.Edges = g.Edges[1:]
	if _, err := SelectRunNodeIDs(g, RunScopeNode, "image", nil); err != nil {
		t.Fatalf("unconnected invalid nodes must not block explicit target: %v", err)
	}
}

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
		"reference_fidelity": "high", "background_intent": "auto",
	}
	return AppliedGraph{
		Revision: 1,
		Nodes: []AppliedNode{
			{ID: "brief", NodeType: NodeCreativeBrief, Title: "要求", DocumentOrigin: OriginSeed, Config: map[string]any{}},
			{ID: "prompt", NodeType: NodeImagePrompt, Title: "提示词", DocumentOrigin: OriginAuthored, Config: map[string]any{
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

func TestSelectGraphOmitsNodesMissingRequiredEdges(t *testing.T) {
	g := cookSelectGraph()
	g.Edges = g.Edges[:1]
	ids, err := SelectRunNodeIDs(g, RunScopeGraph, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, id := range ids {
		got[id] = true
	}
	if !got["brief"] || !got["prompt"] {
		t.Fatalf("ready nodes %v", ids)
	}
	if got["image"] {
		t.Fatalf("incomplete image must not enqueue: %v", ids)
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
	ids, err := SelectRunNodeIDsWithMode(g, RunScopeGraph, "", nil, nil, true, DocumentActionReplace)
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
	ids, err := SelectRunNodeIDsWithMode(g, RunScopeNode, "prompt", nil, nil, true, DocumentActionReplace)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "prompt" {
		t.Fatalf("selected %v", ids)
	}
}

func TestSelectSelectionQueuesExplicitImages(t *testing.T) {
	g := cookSelectGraph()
	ids, err := SelectRunNodeIDsWithMode(g, RunScopeSelection, "", []string{"image"}, nil, false, DocumentActionComplete)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "image" {
		t.Fatalf("selected %v", ids)
	}
}

func TestPlanRunMarksAuthoredPromptFrozen(t *testing.T) {
	g := cookSelectGraph()
	nodes, err := PlanRun(g, RunScopeGraph, "", nil, nil, false, DocumentActionComplete)
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
