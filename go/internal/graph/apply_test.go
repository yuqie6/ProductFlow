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
}

func TestApplyReorderEdgesUpdatesOrder(t *testing.T) {
	g := AppliedGraph{
		Revision: 1,
		Nodes: []AppliedNode{
			{ID: "img1", NodeType: NodeImageAsset, Title: "a"},
			{ID: "img2", NodeType: NodeImageAsset, Title: "b"},
			{ID: "prompt", NodeType: NodeImagePrompt, Title: "p", DocumentOrigin: OriginSeed},
		},
		Edges: []AppliedEdge{
			{ID: "e1", SourceNodeID: "img1", TargetNodeID: "prompt", DataType: DataImageAsset, Role: RoleReference, Order: 0},
			{ID: "e2", SourceNodeID: "img2", TargetNodeID: "prompt", DataType: DataImageAsset, Role: RoleReference, Order: 1},
		},
	}
	got, err := Apply(g, ChangeSet{
		BaseGraphRevision: 1,
		Summary:           "重排参考图",
		Operations: []Operation{ReorderEdgesOp{
			NodeRef:  "prompt",
			Role:     RoleReference,
			EdgeRefs: []string{"e2", "e1"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]AppliedEdge{}
	for _, edge := range got.Edges {
		byID[edge.ID] = edge
	}
	if byID["e2"].Order != 0 || byID["e1"].Order != 1 {
		t.Fatalf("order %+v %+v", byID["e1"], byID["e2"])
	}
}

func TestInvertReorderEdges(t *testing.T) {
	before := AppliedGraph{
		Nodes: []AppliedNode{
			{ID: "img1", NodeType: NodeImageAsset},
			{ID: "img2", NodeType: NodeImageAsset},
			{ID: "prompt", NodeType: NodeImagePrompt, DocumentOrigin: OriginSeed},
		},
		Edges: []AppliedEdge{
			{ID: "e1", SourceNodeID: "img1", TargetNodeID: "prompt", DataType: DataImageAsset, Role: RoleReference, Order: 0},
			{ID: "e2", SourceNodeID: "img2", TargetNodeID: "prompt", DataType: DataImageAsset, Role: RoleReference, Order: 1},
		},
	}
	after := before
	after.Revision = 1
	after.Edges = []AppliedEdge{
		{ID: "e1", SourceNodeID: "img1", TargetNodeID: "prompt", DataType: DataImageAsset, Role: RoleReference, Order: 1},
		{ID: "e2", SourceNodeID: "img2", TargetNodeID: "prompt", DataType: DataImageAsset, Role: RoleReference, Order: 0},
	}
	inverse := Invert(before, after)
	if len(inverse) != 1 {
		t.Fatalf("inverse %+v", inverse)
	}
	reorder, ok := inverse[0].(ReorderEdgesOp)
	if !ok || reorder.NodeRef != "prompt" || reorder.Role != RoleReference || !sameStringSlice(reorder.EdgeRefs, []string{"e1", "e2"}) {
		t.Fatalf("inverse %+v", inverse)
	}
}

func TestFillDefaultNodeConfigAddsGenerationSpec(t *testing.T) {
	got := FillDefaultNodeConfig(NodeImageGeneration, map[string]any{})
	spec, _ := got["generation_spec"].(map[string]any)
	if spec["aspect_ratio"] != "1:1" || spec["text_policy"] != nil {
		t.Fatalf("spec %+v", spec)
	}
}

func TestNormalizeRejectsUnknownConfigField(t *testing.T) {
	if _, err := NormalizeNodeConfig(NodeProductSource, map[string]any{
		"source_product_id": "prod-1",
		"unknown_field":     true,
	}); err == nil || !strings.Contains(err.Error(), "未登记字段") {
		t.Fatalf("unknown field: %v", err)
	}
}

func TestBuildDirectCreateTemplateSeedsPerShotVariation(t *testing.T) {
	cs, err := BuildDirectCreateTemplate(DirectCreateInput{
		ImageTypes: []DirectCreateImageType{
			{Key: "hero", Quantity: 2, Order: 0},
			{Key: "selling_point", Quantity: 4, Order: 1},
		},
		ReferenceAssetIDs: []string{"asset-a"},
		ProductTitle:      "套图商品",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Apply(EmptyGraph, cs)
	if err != nil {
		t.Fatal(err)
	}
	hero := mustNodeByID(t, got, "image-hero-2")
	if v, _ := hero.Config["variation_instruction"].(string); !strings.Contains(v, "另一张封面") {
		t.Fatalf("hero variation %+v", hero.Config["variation_instruction"])
	}
	selling := mustNodeByID(t, got, "image-selling_point-3")
	if v, _ := selling.Config["variation_instruction"].(string); !strings.Contains(v, "第 3 张") || !strings.Contains(v, "钩子") {
		t.Fatalf("selling_point variation %+v", selling.Config["variation_instruction"])
	}
	single, err := BuildDirectCreateTemplate(DirectCreateInput{
		ImageTypes:        []DirectCreateImageType{{Key: "hero", Quantity: 1, Order: 0}},
		ReferenceAssetIDs: []string{"asset-a"},
		ProductTitle:      "单封面",
	})
	if err != nil {
		t.Fatal(err)
	}
	one, err := Apply(EmptyGraph, single)
	if err != nil {
		t.Fatal(err)
	}
	only := mustNodeByID(t, one, "image-hero-1")
	if _, ok := only.Config["variation_instruction"]; ok {
		t.Fatalf("single hero must not seed variation: %+v", only.Config)
	}
}

func mustNodeByID(t *testing.T, g AppliedGraph, id string) AppliedNode {
	t.Helper()
	node, err := g.Node(id)
	if err != nil {
		t.Fatal(err)
	}
	return node
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
	if types[NodeImageAsset] != 1 || types[NodeImagePrompt] != 1 || types[NodeImageGeneration] != 1 {
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

func TestTemplateForExistingProductSourceReusesBirthNode(t *testing.T) {
	birthCS, err := BuildProductSourceCreateGraph("待确认商品", "prod-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	birth, err := Apply(EmptyGraph, birthCS)
	if err != nil {
		t.Fatal(err)
	}
	changeSet, err := TemplateForExistingProductSource("product-source", birth.Revision, DirectCreateInput{
		ImageTypes:        []DirectCreateImageType{{Key: "hero", Quantity: 1, Order: 0, Title: "首屏海报图"}},
		ReferenceAssetIDs: []string{"asset-a"},
		ProductTitle:      "待确认商品",
		SourceProductID:   stringPtr("prod-1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range changeSet.Operations {
		if op, ok := operation.(CreateNodeOp); ok && op.ClientRef == "product-source" {
			t.Fatal("must reuse existing product-source")
		}
	}
	if changeSet.BaseGraphRevision != birth.Revision {
		t.Fatalf("base %d", changeSet.BaseGraphRevision)
	}
	expanded, err := Apply(birth, changeSet)
	if err != nil {
		t.Fatal(err)
	}
	source, err := expanded.Node("product-source")
	if err != nil || source.NodeType != NodeProductSource {
		t.Fatalf("source %+v %v", source, err)
	}
	types := map[NodeType]struct{}{}
	for _, node := range expanded.Nodes {
		types[node.NodeType] = struct{}{}
	}
	for _, want := range []NodeType{NodeVisualSystem, NodeCreativeBrief, NodeImageAsset, NodeImagePrompt, NodeImageGeneration} {
		if _, ok := types[want]; !ok {
			t.Fatalf("missing %s in %+v", want, types)
		}
	}
	found := false
	for _, edge := range expanded.Edges {
		if edge.SourceNodeID == "product-source" && edge.TargetNodeID == "visual-system" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("missing product-source -> visual-system")
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
