package graph_test

import (
	"context"
	"testing"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/quota"
)

func TestImageSettlementFailureDoesNotPublishSuccess(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	ctx := context.Background()
	merchantID := auth.MustDevMerchantID(t, gs.db)
	productID, graphID := gs.createDirectGraph(t)
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "graph"})
	gs.mustStatus(t, resp, 201)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)
	const constraint = "test_graph_settle_failure"
	if _, err := gs.pool.Exec(ctx, "ALTER TABLE merchant_quota_holds ADD CONSTRAINT "+constraint+" CHECK (merchant_id <> '"+merchantID+"' OR status <> 'settled') NOT VALID"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := gs.pool.Exec(context.Background(), "ALTER TABLE merchant_quota_holds DROP CONSTRAINT IF EXISTS "+constraint); err != nil {
			t.Error(err)
		}
	})
	image := &countingImage{}
	executor := graph.Executor{DB: gs.db, Deps: graph.Dependencies{Prompt: graph.MockPromptProvider{}, Image: image, Assets: product.Service{DB: gs.db, Media: gs.media}}}
	// Either a propagated persistence error or committed unknown is recoverable; success is not.
	_ = gs.tryExecuteLocally(t, run.ID, executor)
	artifacts, assets := countGraphImageArtifacts(t, gs, run.ID, productID)
	if artifacts != 0 || assets != 0 {
		t.Fatalf("settlement failure published artifacts=%d assets=%d", artifacts, assets)
	}
	var status string
	if err := gs.pool.QueryRow(ctx, "SELECT status FROM workflow_graph_runs WHERE id=$1", run.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "unknown" {
		t.Fatalf("expected recoverable unknown, got %s", status)
	}
	key := findAnyImageHoldKey(t, gs.db, merchantID)
	if hold := loadQuotaHold(t, gs.db, merchantID, key); hold.Status != quota.StatusPendingReconciliation {
		t.Fatalf("hold=%s", hold.Status)
	}
	var effectResult, failureReason string
	if err := gs.pool.QueryRow(ctx, `SELECT e.effect_result,n.failure_reason FROM workflow_graph_provider_effects e
        JOIN workflow_graph_node_runs n ON n.id=e.node_run_id
        JOIN workflow_graph_nodes w ON w.id=n.node_id
        WHERE n.graph_run_id=$1 AND w.node_type='image_generation'`, run.ID).Scan(&effectResult, &failureReason); err != nil {
		t.Fatal(err)
	}
	if effectResult != "applied" || failureReason != "更新预留失败" {
		t.Fatalf("lost persistence failure evidence: effect=%s reason=%s", effectResult, failureReason)
	}
	before := image.imageCalls()
	if before != 1 {
		t.Fatalf("image calls=%d", before)
	}
	if _, err := gs.pool.Exec(ctx, "ALTER TABLE merchant_quota_holds DROP CONSTRAINT "+constraint); err != nil {
		t.Fatal(err)
	}
	executor.Products = product.GraphGuard{}
	if err := executor.ExecuteRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if image.imageCalls() != before {
		t.Fatal("redelivery repeated the paid provider call")
	}
}
