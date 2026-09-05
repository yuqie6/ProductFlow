package graph

import (
	"strings"
	"testing"
)

func TestReferenceMeaningChangesOnlyConnectedInputDigests(t *testing.T) {
	asset := "reference-asset"
	g := AppliedGraph{Nodes: []AppliedNode{
		{ID: "ref", NodeType: NodeImageAsset, BoundAssetID: &asset, Config: map[string]any{"role": "style", "label": "仅参考光线"}},
		{ID: "brief", NodeType: NodeCreativeBrief, Config: map[string]any{}},
		{ID: "visual", NodeType: NodeVisualSystem, Config: map[string]any{}},
		{ID: "prompt", NodeType: NodeImagePrompt, Config: map[string]any{"prompt": map[string]any{"design_goal": "展示商品"}}},
		{ID: "image", NodeType: NodeImageGeneration, Config: FillDefaultNodeConfig(NodeImageGeneration, nil)},
	}, Edges: []AppliedEdge{{ID: "prompt-image", SourceNodeID: "prompt", TargetNodeID: "image", Role: RolePrompt, DataType: DataPrompt}}}
	for _, target := range []string{"brief", "visual", "prompt", "image"} {
		g.Edges = append(g.Edges, AppliedEdge{ID: "ref-" + target, SourceNodeID: "ref", TargetNodeID: target, Role: RoleReference, DataType: DataImageAsset})
	}
	sources := map[string]SourceRecord{}
	digest := func(target string) string {
		t.Helper()
		var value string
		var err error
		switch target {
		case "image":
			value, err = compileImageRuntime(g, target, sources)
		case "prompt":
			value, err = compilePromptRuntime(g, target, sources)
		default:
			value, err = compileContextRuntime(g, target, sources)
		}
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	for _, target := range []string{"brief", "visual", "prompt", "image"} {
		before := digest(target)
		g.Nodes[0].Config["label"] = "仅参考背景"
		if digest(target) == before {
			t.Fatalf("%s ignored reference note", target)
		}
		before = digest(target)
		g.Nodes[0].Config["role"] = "environment"
		if digest(target) == before {
			t.Fatalf("%s ignored reference role", target)
		}
		g.Nodes[0].Config["label"] = "仅参考光线"
		g.Nodes[0].Config["role"] = "style"
	}
	g.Edges = g.Edges[:1]
	before := digest("image")
	g.Nodes[0].Config["label"] = "断开的参考"
	g.Nodes[0].Config["role"] = "evidence"
	if digest("image") != before {
		t.Fatal("disconnected reference changed image digest")
	}
	g.Nodes[4].Config["delivery_spec"] = cloneMap(defaultDeliverySpec)
	if digest("image") != before {
		t.Fatal("delivery settings changed image digest")
	}
}

func TestReferenceInstructionsReachImagePromptInImageOrder(t *testing.T) {
	got := CompileImageModelPrompt(ImageRequest{References: []ReferenceImage{
		{Role: "product_identity", Label: "保留杯盖形状"},
		{Role: "style", Label: "仅参考光线，不参考其中商品"},
	}})
	for _, want := range []string{"Reference image 1: role=product_identity", "保留杯盖形状", "Reference image 2: role=style", "仅参考光线，不参考其中商品"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
}
