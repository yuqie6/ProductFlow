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
