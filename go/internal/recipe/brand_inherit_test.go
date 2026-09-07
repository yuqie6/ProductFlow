package recipe

import (
	"testing"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/visualsystem"
)

// CF-B5：配方 extract 清身份；不得带回 source_product_id / fact/visual 版本绑定。
func TestCFB5ExtractStripsSourceProductAndVisualBindings(t *testing.T) {
	applied := graph.AppliedGraph{
		Revision: 1,
		Nodes: []graph.AppliedNode{
			{
				ID:       "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
				NodeType: graph.NodeProductSource,
				Title:    "商品资料",
				Config: map[string]any{
					"source_product_id":   "cup-a",
					"fact_set_version_id": "fact-a-600ml",
				},
			},
			{
				ID:       "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
				NodeType: graph.NodeVisualSystem,
				Title:    "视觉规范",
				Config: map[string]any{
					"visual_system_version_id": "ver-cup-series",
					"visual_overlay": map[string]any{
						"style":  []any{"冷色棚拍"},
						"colors": []any{map[string]any{"value": "#111111"}},
					},
					"visual_overrides": map[string]any{"style": []any{"泄漏"}},
				},
			},
			{
				ID:       "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
				NodeType: graph.NodeImagePrompt,
				Title:    "规格提示词",
				Config: map[string]any{
					"image_type_key":     "spec",
					"fact_keys":          []any{"capacity"},
					"evidence_asset_ids": []any{"ev-a"},
					"images":             []any{"old-ref"},
				},
			},
		},
		Edges: []graph.AppliedEdge{
			{
				ID: "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
				SourceNodeID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
				TargetNodeID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
				DataType:     graph.DataProductFacts,
				Role:         graph.RoleFacts,
			},
			{
				ID: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee",
				SourceNodeID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
				TargetNodeID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
				DataType:     graph.DataVisualSystem,
				Role:         graph.RoleVisualGuidance,
			},
		},
	}
	payload, err := extractPayload(applied, ExtractInput{SourceType: SourceWorkflow})
	if err != nil {
		t.Fatal(err)
	}
	byType := map[graph.NodeType]PayloadNode{}
	for _, node := range payload.Nodes {
		byType[node.NodeType] = node
	}
	source := byType[graph.NodeProductSource]
	if source.Config["source_product_id"] != nil {
		t.Fatalf("source_product_id leaked: %+v", source.Config)
	}
	if source.Config["fact_set_version_id"] != nil {
		t.Fatalf("fact_set_version_id leaked: %+v", source.Config)
	}
	visual := byType[graph.NodeVisualSystem]
	if visual.Config["visual_system_version_id"] != nil {
		t.Fatalf("visual_system_version_id must be null after extract: %+v", visual.Config)
	}
	if _, ok := visual.Config["visual_overrides"]; ok {
		t.Fatalf("visual_overrides must be stripped: %+v", visual.Config)
	}
	overlay, _ := visual.Config["visual_overlay"].(map[string]any)
	if overlay == nil {
		t.Fatal("visual_overlay style template should remain")
	}
	prompt := byType[graph.NodeImagePrompt]
	if _, ok := prompt.Config["fact_keys"]; ok {
		t.Fatalf("fact_keys must not travel with recipe: %+v", prompt.Config)
	}
	if _, ok := prompt.Config["images"]; ok {
		t.Fatal("images identity refs must be stripped")
	}
}

// 第二商品样例：复用方案偏好，apply 绑新商品与新 fact；不带回旧 cup-a 身份。
func TestCFB5SecondProductApplyBindsTargetNotSource(t *testing.T) {
	payload := Payload{
		SchemaVersion: 3,
		Nodes: []PayloadNode{
			{
				Key:      "product",
				NodeType: graph.NodeProductSource,
				Title:    "商品资料",
				Config: map[string]any{
					"source_product_id":   nil,
					"fact_set_version_id": nil,
				},
			},
			{
				Key:      "visual",
				NodeType: graph.NodeVisualSystem,
				Title:    "视觉规范",
				Config: map[string]any{
					"visual_system_version_id": nil,
					"visual_overlay": map[string]any{
						"style": []any{"杯系列冷色"},
					},
				},
			},
			{
				Key:      "prompt",
				NodeType: graph.NodeImagePrompt,
				Title:    "规格提示词",
				Config:   map[string]any{"image_type_key": "spec"},
			},
		},
		Edges: []PayloadEdge{
			{
				Key: "e-facts", SourceNodeKey: "product", TargetNodeKey: "prompt",
				DataType: graph.DataProductFacts, Role: graph.RoleFacts,
			},
			{
				Key: "e-visual", SourceNodeKey: "visual", TargetNodeKey: "prompt",
				DataType: graph.DataVisualSystem, Role: graph.RoleVisualGuidance,
			},
		},
	}
	if err := validatePayload(payload); err != nil {
		t.Fatal(err)
	}

	cupB := "cup-b-new"
	factB := "fact-b-750ml"
	cs, err := buildChangeSet(payload, 0, "第二商品", cupB, &factB, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var sourceConfig map[string]any
	for _, op := range cs.Operations {
		create, ok := op.(graph.CreateNodeOp)
		if !ok || create.NodeType != graph.NodeProductSource {
			continue
		}
		sourceConfig = create.Config
	}
	if sourceConfig == nil {
		t.Fatal("missing product_source op")
	}
	if sourceConfig["source_product_id"] != cupB {
		t.Fatalf("apply must bind target product, got %+v", sourceConfig["source_product_id"])
	}
	if sourceConfig["fact_set_version_id"] != factB {
		t.Fatalf("apply must use new fact version, got %+v", sourceConfig["fact_set_version_id"])
	}

	preferred := "ver-cup-series-v1"
	preview := buildReusePreview(&preferred, true, []string{"product_identity"})
	if preview.BrandPlaceholder.Reason != visualsystem.BrandReasonNotReady {
		t.Fatalf("brand placeholder %+v", preview.BrandPlaceholder)
	}
	if preview.PreferredVisualSystemVersionID == nil || *preview.PreferredVisualSystemVersionID != preferred {
		t.Fatal("second product should reuse preferred scheme version")
	}
	foundFactsPending := false
	for _, item := range preview.Pending {
		if item.Key == "product_facts" {
			foundFactsPending = true
		}
	}
	if !foundFactsPending {
		t.Fatalf("new capacity/facts must be pending: %+v", preview.Pending)
	}
	for _, item := range preview.Inherited {
		if item.Key == "product_facts" || item.Key == "product_identity" {
			t.Fatalf("facts/identity must not be inherited: %+v", preview.Inherited)
		}
	}
}
