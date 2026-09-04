package graph

import (
	"context"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"gorm.io/gorm"
)

func TestWriteTxRevisionConflict(t *testing.T) {
	ctx, tx := beginCommandTx(t)
	productID := insertCommandProduct(t, ctx, tx, "修订冲突")
	created := writeProductSource(t, ctx, tx, productID, "修订冲突")
	graphID := created.GraphID
	_, err := WriteTx(ctx, tx, Command{
		ProductID: productID,
		GraphID:   &graphID,
		ChangeSet: ChangeSet{
			BaseGraphRevision: created.Revision + 1,
			Summary:           "过期修订",
			Operations: []Operation{
				RenameNodeOp{NodeRef: created.Applied.Nodes[0].ID, Title: "新标题"},
			},
		},
	})
	assertAppErr(t, err, 409, "图 revision 已变化，请刷新后重试")
}

func TestWriteTxRequireActiveMissing(t *testing.T) {
	ctx, tx := beginCommandTx(t)
	productID := insertCommandProduct(t, ctx, tx, "无图商品")
	fakeID := clockid.New()
	_, err := WriteTx(ctx, tx, Command{
		ProductID:     productID,
		GraphID:       &fakeID,
		RequireActive: true,
		ChangeSet: ChangeSet{
			BaseGraphRevision: 1,
			Summary:           "无图写入",
			Operations: []Operation{
				CreateNodeOp{ClientRef: "n1", NodeType: NodeProductSource, Title: "商品"},
			},
		},
	})
	assertAppErr(t, err, 409, "商品没有可写入的 schema-v3 工作流")
}

func TestWriteTxRequireActiveMismatch(t *testing.T) {
	ctx, tx := beginCommandTx(t)
	productID := insertCommandProduct(t, ctx, tx, "图身份变化")
	created := writeProductSource(t, ctx, tx, productID, "图身份变化")
	staleID := clockid.New()
	_, err := WriteTx(ctx, tx, Command{
		ProductID:     productID,
		GraphID:       &staleID,
		RequireActive: true,
		ChangeSet: ChangeSet{
			BaseGraphRevision: created.Revision,
			Summary:           "过期图",
			Operations: []Operation{
				RenameNodeOp{NodeRef: created.Applied.Nodes[0].ID, Title: "新标题"},
			},
		},
	})
	assertAppErr(t, err, 409, "工作流已变化，请重新预览后重试")
}

func TestWriteTxDoesNotCommit(t *testing.T) {
	_, gdb := testdb.Open(t)
	ctx := WithProductGuard(context.Background(), cmdTestProducts{})
	tx := gdb.WithContext(ctx).Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	productID := insertCommandProduct(t, ctx, tx, "只 flush")
	result := writeProductSource(t, ctx, tx, productID, "只 flush")
	var count int
	if err := pfdb.QueryRow(ctx, tx, `SELECT COUNT(*) FROM workflow_graphs WHERE id = $1`, result.GraphID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("same tx count %d", count)
	}
	if err := tx.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	if err := pfdb.QueryRow(ctx, gdb, `SELECT COUNT(*) FROM workflow_graphs WHERE id = $1`, result.GraphID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("caller rollback left graph row: %d", count)
	}
}

func TestTryLiveMissing(t *testing.T) {
	ctx, tx := beginCommandTx(t)
	productID := insertCommandProduct(t, ctx, tx, "尚无图")
	live, err := TryLive(ctx, tx, productID)
	if err != nil {
		t.Fatal(err)
	}
	if live != nil {
		t.Fatalf("got %+v", live)
	}
}

func TestLoadLiveForUpdateRevisionConflict(t *testing.T) {
	ctx, tx := beginCommandTx(t)
	productID := insertCommandProduct(t, ctx, tx, "提取过期")
	created := writeProductSource(t, ctx, tx, productID, "提取过期")
	_, err := LoadLiveForUpdate(ctx, tx, productID, created.GraphID, created.Revision+1)
	assertAppErr(t, err, 409, "工作流已变化，请刷新后重试")
}

func TestExpandBirthCreatesWhenMissing(t *testing.T) {
	ctx, tx := beginCommandTx(t)
	productID := insertCommandProduct(t, ctx, tx, "无图展开")
	assetID := insertCommandAsset(t, ctx, tx, productID)
	expanded, result, err := ExpandBirth(ctx, tx, productID, "无图展开", birthInput(productID, assetID))
	if err != nil {
		t.Fatal(err)
	}
	if !expanded || result.Revision != 1 {
		t.Fatalf("expanded=%v result=%+v", expanded, result)
	}
	if productSourceCount(result.Applied) != 1 {
		t.Fatalf("nodes %+v", result.Applied.Nodes)
	}
	if len(result.Applied.Nodes) < 2 {
		t.Fatalf("template nodes %d", len(result.Applied.Nodes))
	}
}

func TestExpandBirthMutatesNameOnly(t *testing.T) {
	ctx, tx := beginCommandTx(t)
	productID := insertCommandProduct(t, ctx, tx, "名称展开")
	assetID := insertCommandAsset(t, ctx, tx, productID)
	created := writeProductSource(t, ctx, tx, productID, "名称展开")
	expanded, result, err := ExpandBirth(ctx, tx, productID, "名称展开", birthInput(productID, assetID))
	if err != nil {
		t.Fatal(err)
	}
	if !expanded || result.GraphID != created.GraphID || result.Revision <= created.Revision {
		t.Fatalf("expanded=%v result=%+v created=%+v", expanded, result, created)
	}
	if productSourceCount(result.Applied) != 1 {
		t.Fatalf("nodes %+v", result.Applied.Nodes)
	}
	if len(result.Applied.Nodes) <= 1 {
		t.Fatalf("did not expand: %+v", result.Applied.Nodes)
	}
}

func TestExpandBirthSkipsExpandedGraph(t *testing.T) {
	ctx, tx := beginCommandTx(t)
	productID := insertCommandProduct(t, ctx, tx, "已展开")
	assetID := insertCommandAsset(t, ctx, tx, productID)
	if _, _, err := ExpandBirth(ctx, tx, productID, "已展开", birthInput(productID, assetID)); err != nil {
		t.Fatal(err)
	}
	live, err := TryLive(ctx, tx, productID)
	if err != nil || live == nil {
		t.Fatalf("live %+v %v", live, err)
	}
	expanded, result, err := ExpandBirth(ctx, tx, productID, "已展开", birthInput(productID, assetID))
	if err != nil {
		t.Fatal(err)
	}
	if expanded || result.GraphID != "" {
		t.Fatalf("second expand %+v %+v", expanded, result)
	}
	again, err := TryLive(ctx, tx, productID)
	if err != nil || again == nil {
		t.Fatal(err)
	}
	if again.Applied.Revision != live.Applied.Revision || len(again.Applied.Nodes) != len(live.Applied.Nodes) {
		t.Fatalf("rewrote expanded graph %+v -> %+v", live.Applied, again.Applied)
	}
}

func birthInput(productID, assetID string) DirectCreateInput {
	source := productID
	return DirectCreateInput{
		ImageTypes: []DirectCreateImageType{
			{Key: "hero", Quantity: 1, Order: 0, Title: "封面"},
		},
		ReferenceAssetIDs: []string{assetID},
		ProductTitle:      "展开商品",
		SourceProductID:   &source,
	}
}

func insertCommandAsset(t *testing.T, ctx context.Context, tx *gorm.DB, productID string) string {
	t.Helper()
	mediaID := clockid.New()
	assetID := clockid.New()
	_, err := pfdb.Exec(ctx, tx, `
		INSERT INTO media_objects (
			id, storage_path, mime_type, byte_size, width, height, sha256,
			verification_status, created_at, verified_at
		) VALUES ($1, $2, 'image/png', 1024, 100, 100, $3, 'verified', NOW(), NOW())
	`, mediaID, "cmd-test/"+mediaID+".png", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	_, err = pfdb.Exec(ctx, tx, `
		INSERT INTO product_image_assets (
			id, product_id, media_object_id, origin_type, display_name, original_filename, created_at, updated_at
		) VALUES ($1, $2, $3, 'upload', 'ref.png', 'ref.png', NOW(), NOW())
	`, assetID, productID, mediaID)
	if err != nil {
		t.Fatal(err)
	}
	return assetID
}

func productSourceCount(applied AppliedGraph) int {
	n := 0
	for _, node := range applied.Nodes {
		if node.NodeType == NodeProductSource {
			n++
		}
	}
	return n
}
