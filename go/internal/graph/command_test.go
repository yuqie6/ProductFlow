package graph

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	sqldb "database/sql"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"gorm.io/gorm"
)

type cmdTestProducts struct{}

func (cmdTestProducts) Lock(ctx context.Context, tx *gorm.DB, productID string) error {
	var id string
	err := pfdb.QueryRow(ctx, tx, `SELECT id FROM products WHERE id = $1 FOR UPDATE`, productID).Scan(&id)
	if errors.Is(err, sqldb.ErrNoRows) {
		return apperr.NotFound("商品不存在")
	}
	return err
}

func (cmdTestProducts) HasAssets(context.Context, *gorm.DB, string, []string) error {
	return nil
}

func (cmdTestProducts) LoadSource(ctx context.Context, tx *gorm.DB, productID string) (*SourceProduct, error) {
	var out SourceProduct
	err := pfdb.QueryRow(ctx, tx, `
		SELECT id, name, category, price::text, source_note, current_fact_set_version_id
		FROM products WHERE id = $1
	`, productID).Scan(&out.ID, &out.Name, &out.Category, &out.Price, &out.SourceNote, &out.CurrentFactSetID)
	if errors.Is(err, sqldb.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (cmdTestProducts) LoadFactSet(ctx context.Context, tx *gorm.DB, factSetID, productID string) (*FactSet, error) {
	q := `SELECT id, product_id, version, payload_json FROM product_fact_set_versions WHERE id = $1`
	args := []any{factSetID}
	if productID != "" {
		q += ` AND product_id = $2`
		args = append(args, productID)
	}
	var out FactSet
	var payloadJSON []byte
	err := pfdb.QueryRow(ctx, tx, q, args...).Scan(&out.ID, &out.ProductID, &out.Version, &payloadJSON)
	if errors.Is(err, sqldb.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out.Facts = []map[string]any{}
	return &out, nil
}

func (cmdTestProducts) LoadSources(ctx context.Context, tx *gorm.DB, productIDs []string) (map[string]*SourceProduct, error) {
	out := map[string]*SourceProduct{}
	for _, productID := range productIDs {
		source, err := (cmdTestProducts{}).LoadSource(ctx, tx, productID)
		if err != nil {
			return nil, err
		}
		if source != nil {
			out[productID] = source
		}
	}
	return out, nil
}

func (cmdTestProducts) LoadFactSets(ctx context.Context, tx *gorm.DB, factSetIDs []string) (map[string]*FactSet, error) {
	out := map[string]*FactSet{}
	for _, factSetID := range factSetIDs {
		set, err := (cmdTestProducts{}).LoadFactSet(ctx, tx, factSetID, "")
		if err != nil {
			return nil, err
		}
		if set != nil {
			out[factSetID] = set
		}
	}
	return out, nil
}

func (cmdTestProducts) BoundAssetMetas(context.Context, *gorm.DB, string, []string) (map[string]BoundAssetMetadata, error) {
	return map[string]BoundAssetMetadata{}, nil
}

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
	_, gdb := testdb.Open(t)
	ctx := WithProductGuard(context.Background(), cmdTestProducts{})
	tx := gdb.WithContext(ctx).Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer func() { _ = tx.Rollback() }()

	productID := clockid.New()
	_, err := pfdb.Exec(ctx, tx, `
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
	err = pfdb.QueryRow(ctx, tx, `
		SELECT schema_version, revision, active, title FROM workflow_graphs WHERE id = $1
	`, result.GraphID).Scan(&schemaVersion, &revision, &active, &title)
	if err != nil {
		t.Fatal(err)
	}
	if schemaVersion != 3 || revision != 1 || !active || title != "名称出生商品" {
		t.Fatalf("graph %d %d %v %q", schemaVersion, revision, active, title)
	}

	var nodeCount int
	if err := pfdb.QueryRow(ctx, tx, `SELECT COUNT(*) FROM workflow_graph_nodes WHERE graph_id = $1`, result.GraphID).Scan(&nodeCount); err != nil {
		t.Fatal(err)
	}
	if nodeCount != 1 {
		t.Fatalf("node count %d", nodeCount)
	}

	var actor, historyKind, summary string
	var opsRaw, inverseRaw []byte
	err = pfdb.QueryRow(ctx, tx, `
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
