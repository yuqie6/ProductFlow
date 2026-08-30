package graph

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseChangeSetForbidsExtraFields(t *testing.T) {
	_, err := ParseChangeSet([]byte(`{
		"base_graph_revision": 1,
		"summary": "改名",
		"operations": [{"op": "rename_node", "node_ref": "n1", "title": "新标题"}],
		"extra": true
	}`))
	assertAppErr(t, err, 400, "请求体无效")
}

func TestParseChangeSetRoundTripMoveNodes(t *testing.T) {
	raw := []byte(`{
		"base_graph_revision": 2,
		"summary": "移动节点",
		"actor_type": "user",
		"operations": [{"op": "move_nodes", "nodes": [["n1", 10, 20]]}]
	}`)
	cs, err := ParseChangeSet(raw)
	if err != nil {
		t.Fatal(err)
	}
	op, ok := cs.Operations[0].(MoveNodesOp)
	if !ok || len(op.Nodes) != 1 || op.Nodes[0].Ref != "n1" || op.Nodes[0].X != 10 {
		t.Fatalf("%+v", cs.Operations[0])
	}
	encoded, err := marshalOperations(cs.Operations)
	if err != nil {
		t.Fatal(err)
	}
	again, err := UnmarshalOperations(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := again[0].(MoveNodesOp); !ok {
		t.Fatalf("%s", encoded)
	}
}

func TestParseChangeSetRejectsEmptyOperations(t *testing.T) {
	_, err := ParseChangeSet([]byte(`{"base_graph_revision": 1, "summary": "空", "operations": []}`))
	assertAppErr(t, err, 400, "不支持的 Graph 操作")
}

func TestParseChangeSetAcceptsJSONNumberConfig(t *testing.T) {
	raw := []byte(`{
		"base_graph_revision": 0,
		"summary": "数字配置",
		"operations": [{
			"op": "create_node",
			"client_ref": "prompt-1",
			"node_type": "prompt_generation",
			"title": "提示词",
			"config": {"prompt": {"composition": {"product_share_percent": 70}}}
		}]
	}`)
	if _, err := ParseChangeSet(raw); err != nil {
		t.Fatal(err)
	}
	if _, err := NormalizeNodeConfig(NodePromptGeneration, map[string]any{
		"prompt": map[string]any{"composition": map[string]any{"product_share_percent": json.Number("70")}},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestParseChangeSetRejectsNonIntPosition(t *testing.T) {
	_, err := ParseChangeSet([]byte(`{
		"base_graph_revision": 0,
		"summary": "非法坐标",
		"operations": [{
			"op": "create_node",
			"client_ref": "n1",
			"node_type": "product_source",
			"title": "资料",
			"position_x": 1.5
		}]
	}`))
	assertAppErr(t, err, 400, "不支持的 Graph 操作")
}

func TestParseChangeSetRejectsNonStringBoundAsset(t *testing.T) {
	_, err := ParseChangeSet([]byte(`{
		"base_graph_revision": 0,
		"summary": "错误绑定",
		"operations": [{
			"op": "create_node",
			"client_ref": "asset-1",
			"node_type": "image_asset",
			"title": "参考图",
			"bound_asset_id": 12
		}]
	}`))
	assertAppErr(t, err, 400, "不支持的 Graph 操作")
}

func TestGraphRefAllowsUnicodeLength(t *testing.T) {
	ref := strings.Repeat("中", 80)
	raw, _ := json.Marshal(map[string]any{
		"base_graph_revision": 0,
		"summary":             "中文引用",
		"operations": []map[string]any{{
			"op": "create_node", "client_ref": ref, "node_type": "product_source", "title": "资料",
		}},
	})
	if _, err := ParseChangeSet(raw); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogJSONStableNodeOrderAndLabels(t *testing.T) {
	payload := CatalogJSON()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"label_key":"agentWorkbench.nodeEditor.aspectRatio"`) {
		t.Fatalf("%s", raw)
	}
	nodes := payload["nodes"].([]map[string]any)
	if nodes[0]["node_type"] != NodeProductSource || nodes[5]["node_type"] != NodeImageGeneration {
		t.Fatalf("%+v", nodes)
	}
	image := nodes[5]
	accepts := image["accepts"].([]map[string]any)
	if accepts[len(accepts)-1]["role"] != RolePrompt || accepts[len(accepts)-1]["required_to_run"] != true {
		t.Fatalf("accepts %+v", accepts)
	}
}
