package graph

import (
	"errors"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

func TestApplyCreateNodeProductSourceNormalizesConfig(t *testing.T) {
	cs, err := BuildProductSourceCreateGraph("测试商品", "prod-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Apply(EmptyGraph, cs)
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 1 || len(got.Nodes) != 1 {
		t.Fatalf("revision=%d nodes=%d", got.Revision, len(got.Nodes))
	}
	node := got.Nodes[0]
	if node.NodeType != NodeProductSource || node.ID != "product-source" {
		t.Fatalf("node %+v", node)
	}
	if node.Config["source_product_id"] != "prod-1" {
		t.Fatalf("config %+v", node.Config)
	}
	if node.Config["fact_set_version_id"] != nil {
		t.Fatalf("fact_set_version_id %+v", node.Config["fact_set_version_id"])
	}
	if _, err := NormalizeNodeConfig(NodeProductSource, map[string]any{
		"source_product_id": "prod-1",
		"unknown_field":     true,
	}); err == nil || !strings.Contains(err.Error(), "未登记字段") {
		t.Fatalf("unknown field: %v", err)
	}
}

func TestApplyDirectCreateTemplateHasNoCycle(t *testing.T) {
	cs, err := BuildDirectCreateTemplate(DirectCreateInput{
		ImageTypes:        []DirectCreateImageType{{Key: "hero", Quantity: 1, Order: 0}},
		ReferenceAssetIDs: []string{"asset-a"},
		ProductTitle:      "直接创建商品",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Apply(EmptyGraph, cs)
	if err != nil {
		t.Fatal(err)
	}
	types := map[NodeType]int{}
	for _, node := range got.Nodes {
		types[node.NodeType]++
	}
	if types[NodeProductSource] != 1 || types[NodeVisualSystem] != 1 || types[NodeCreativeBrief] != 1 {
		t.Fatalf("context nodes %+v", types)
	}
	if types[NodeImageAsset] != 1 || types[NodePromptGeneration] != 1 || types[NodeImageGeneration] != 1 {
		t.Fatalf("pipeline nodes %+v", types)
	}
	if len(got.Groups) != 1 || got.Groups[0].ID != "shot-hero" {
		t.Fatalf("groups %+v", got.Groups)
	}
	ruleNodes := make([]RuleNode, 0, len(got.Nodes))
	for _, node := range got.Nodes {
		ruleNodes = append(ruleNodes, RuleNode{node.ID, node.NodeType, node.Config, node.BoundAssetID})
	}
	ruleEdges := make([]RuleEdge, 0, len(got.Edges))
	for _, edge := range got.Edges {
		ruleEdges = append(ruleEdges, RuleEdge{edge.ID, edge.SourceNodeID, edge.TargetNodeID, edge.DataType, edge.Role, edge.Order})
	}
	ordered, err := TopologicalGraphNodeIDs(ruleNodes, ruleEdges)
	if err != nil {
		t.Fatal(err)
	}
	if len(ordered) != len(got.Nodes) {
		t.Fatalf("topo %d want %d", len(ordered), len(got.Nodes))
	}
}

func TestApplyConflictsOnBaseGraphRevisionMismatch(t *testing.T) {
	_, err := Apply(EmptyGraph, ChangeSet{
		BaseGraphRevision: 1,
		Summary:           "过期写入",
		Operations: []Operation{
			CreateNodeOp{ClientRef: "product-source", NodeType: NodeProductSource, Title: "商品"},
		},
	})
	assertAppErr(t, err, 409, "图 revision 已变化，请刷新后重试")
}

func TestForbiddenPlanKeysRejected(t *testing.T) {
	for _, key := range []string{"image_plan_key", "prompt_plan_key"} {
		_, err := NormalizeNodeConfig(NodeImageGeneration, map[string]any{key: "hero-1"})
		assertAppErr(t, err, 400, "拓扑字段")
		_, err = Apply(EmptyGraph, ChangeSet{
			BaseGraphRevision: 0,
			Summary:           "非法配置",
			Operations: []Operation{
				CreateNodeOp{
					ClientRef: "image",
					NodeType:  NodeImageGeneration,
					Title:     "图",
					Config:    map[string]any{key: "hero-1"},
				},
			},
		})
		assertAppErr(t, err, 400, "拓扑字段")
	}
}

func assertAppErr(t *testing.T, err error, status int, substr string) {
	t.Helper()
	var e apperr.Error
	if !errors.As(err, &e) {
		t.Fatalf("got %v", err)
	}
	if e.Status != status || !strings.Contains(e.Detail, substr) {
		t.Fatalf("status=%d detail=%q want %d %q", e.Status, e.Detail, status, substr)
	}
}
