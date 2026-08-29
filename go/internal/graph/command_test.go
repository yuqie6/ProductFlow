package graph

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/testdb"
)

func TestStageNewRequiresZeroBaseRevision(t *testing.T) {
	_, err := StageNew(context.Background(), nil, "prod", "标题", ChangeSet{
		BaseGraphRevision: 1,
		Summary:           "非法出生",
		Operations: []Operation{
			CreateNodeOp{ClientRef: "product-source", NodeType: NodeProductSource, Title: "商品"},
		},
	})
	assertAppErr(t, err, 409, "新建图的 base_graph_revision 必须为 0")
}

func TestStageNewProductSourceTemplate(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	productID := clockid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO products (id, name, created_at, updated_at)
		VALUES ($1, $2, NOW(), NOW())
	`, productID, "名称出生商品")
	if err != nil {
		t.Fatal(err)
	}

	cs, err := BuildProductSourceCreateGraph("名称出生商品", productID, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := StageNew(ctx, tx, productID, "名称出生商品", cs)
	if err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != 3 || result.Revision != 1 || !result.Active {
		t.Fatalf("%+v", result)
	}
	if len(result.Applied.Nodes) != 1 || result.Applied.Nodes[0].NodeType != NodeProductSource {
		t.Fatalf("nodes %+v", result.Applied.Nodes)
	}
	if len(result.Applied.Nodes[0].ID) != 36 {
		t.Fatalf("persistent id %q", result.Applied.Nodes[0].ID)
	}

	var schemaVersion, revision int
	var active bool
	var title string
	err = tx.QueryRow(ctx, `
		SELECT schema_version, revision, active, title FROM workflow_graphs WHERE id = $1
	`, result.GraphID).Scan(&schemaVersion, &revision, &active, &title)
	if err != nil {
		t.Fatal(err)
	}
	if schemaVersion != 3 || revision != 1 || !active || title != "名称出生商品" {
		t.Fatalf("graph %d %d %v %q", schemaVersion, revision, active, title)
	}

	var nodeCount int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM workflow_graph_nodes WHERE graph_id = $1`, result.GraphID).Scan(&nodeCount); err != nil {
		t.Fatal(err)
	}
	if nodeCount != 1 {
		t.Fatalf("node count %d", nodeCount)
	}

	var actor, historyKind, summary string
	var opsRaw, inverseRaw []byte
	err = tx.QueryRow(ctx, `
		SELECT actor_type, history_kind, summary, operations_json, inverse_operations_json
		FROM workflow_operation_groups WHERE id = $1
	`, result.OperationGroupID).Scan(&actor, &historyKind, &summary, &opsRaw, &inverseRaw)
	if err != nil {
		t.Fatal(err)
	}
	if actor != string(ActorUser) || historyKind != string(HistoryEdit) || summary != "商品资料" {
		t.Fatalf("op group %s %s %s", actor, historyKind, summary)
	}
	var ops []map[string]any
	if err := json.Unmarshal(opsRaw, &ops); err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 || ops[0]["op"] != "create_node" {
		t.Fatalf("operations %s", opsRaw)
	}
	var inverse []map[string]any
	if err := json.Unmarshal(inverseRaw, &inverse); err != nil {
		t.Fatal(err)
	}
	if len(inverse) != 1 || inverse[0]["op"] != "delete_node" {
		t.Fatalf("inverse %s", inverseRaw)
	}

	_, err = StageNew(ctx, tx, productID, "第二次", cs)
	assertAppErr(t, err, 409, "商品已有 active schema-v3 工作流")
}
