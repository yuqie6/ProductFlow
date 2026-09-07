package graph

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/quota"
)

func TestGraphProviderPreparationCannotReserveAfterCancellation(t *testing.T) {
	pool, db := testdb.Open(t)
	ctx := WithProductGuard(context.Background(), cmdTestProducts{})
	attempt := clockid.New()
	runID := insertStaleRunningGraphRun(t, pool, "claimed", &attempt, time.Now().UTC())
	var graphID, productID, nodeID string
	if err := pool.QueryRow(ctx, "SELECT r.graph_id,g.product_id,n.id FROM workflow_graph_runs r JOIN workflow_graphs g ON g.id=r.graph_id JOIN workflow_graph_node_runs n ON n.graph_run_id=r.id WHERE r.id=$1", runID).Scan(&graphID, &productID, &nodeID); err != nil {
		t.Fatal(err)
	}
	merchantID := auth.MustDevMerchantID(t, db)
	q := &quota.Service{DB: db}
	if _, err := q.Adjust(ctx, merchantID, clockid.New(), 10, "prepare fixture", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := (Service{DB: db, Products: cmdTestProducts{}}).CancelRun(ctx, productID, graphID, runID); err != nil {
		t.Fatal(err)
	}
	invoked := false
	_, _, err := (Executor{DB: db}).callImageProvider(ctx, runID, graphNodeRunRow{ID: nodeID, ActiveAttemptID: &attempt}, "fixture", "digest", NodeImageGeneration, func() (ImageResult, error) { invoked = true; return ImageResult{}, nil })
	if !errors.Is(err, errProviderFenced) {
		t.Fatalf("expected fence, got %v", err)
	}
	if invoked {
		t.Fatal("cancelled attempt called provider")
	}
	var holds int64
	if err := db.Model(&schema.MerchantQuotaHolds{}).Where("merchant_id=? AND idempotency_key=?", merchantID, imageNodeQuotaKey(nodeID, attempt)).Count(&holds).Error; err != nil {
		t.Fatal(err)
	}
	if holds != 0 {
		t.Fatalf("cancelled attempt created %d quota holds", holds)
	}
}

func TestGraphProviderPreparationQuotaFailureRollsBackIntent(t *testing.T) {
	pool, db := testdb.Open(t)
	ctx := WithProductGuard(context.Background(), cmdTestProducts{})
	attempt := clockid.New()
	runID := insertStaleRunningGraphRun(t, pool, "claimed", &attempt, time.Now().UTC())
	var nodeID string
	if err := pool.QueryRow(ctx, "SELECT id FROM workflow_graph_node_runs WHERE graph_run_id=$1", runID).Scan(&nodeID); err != nil {
		t.Fatal(err)
	}
	merchantID := auth.MustDevMerchantID(t, db)
	q := &quota.Service{DB: db}
	if _, err := q.Adjust(ctx, merchantID, clockid.New(), 10, "prepare fixture", ""); err != nil {
		t.Fatal(err)
	}
	key := imageNodeQuotaKey(nodeID, attempt)
	const constraint = "test_graph_provider_reserve"
	if _, err := pool.Exec(ctx, "ALTER TABLE merchant_quota_holds ADD CONSTRAINT "+constraint+" CHECK (idempotency_key <> '"+key+"')"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "ALTER TABLE merchant_quota_holds DROP CONSTRAINT IF EXISTS "+constraint); err != nil {
			t.Error(err)
		}
	})
	invoked := false
	invoke := func() (ImageResult, error) {
		invoked = true
		// A separate connection must be able to lock the run during the external call.
		if _, err := pool.Exec(ctx, "SELECT id FROM workflow_graph_runs WHERE id=$1 FOR UPDATE NOWAIT", runID); err != nil {
			return ImageResult{}, err
		}
		var hold schema.MerchantQuotaHolds
		if err := db.Where("merchant_id=? AND idempotency_key=?", merchantID, key).Take(&hold).Error; err != nil {
			return ImageResult{}, err
		}
		if hold.Status != quota.StatusReserved {
			t.Fatalf("provider entered with hold=%s", hold.Status)
		}
		return ImageResult{Model: "fixture"}, nil
	}
	e := Executor{DB: db}
	node := graphNodeRunRow{ID: nodeID, ActiveAttemptID: &attempt}
	if _, _, err := e.callImageProvider(ctx, runID, node, "fixture", "digest", NodeImageGeneration, invoke); err == nil || errors.Is(err, errProviderFenced) {
		t.Fatalf("expected quota persistence error, got %v", err)
	}
	if invoked {
		t.Fatal("provider called despite reserve failure")
	}
	var phase, active string
	if err := pool.QueryRow(ctx, "SELECT progress_phase,active_attempt_id FROM workflow_graph_node_runs WHERE id=$1", nodeID).Scan(&phase, &active); err != nil {
		t.Fatal(err)
	}
	if phase != "claimed" || active != attempt {
		t.Fatalf("partial preparation phase=%s active=%s", phase, active)
	}
	var effects int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM workflow_graph_provider_effects WHERE node_run_id=$1", nodeID).Scan(&effects); err != nil {
		t.Fatal(err)
	}
	if effects != 0 {
		t.Fatalf("failed reserve left %d effect intents", effects)
	}
	if _, err := pool.Exec(ctx, "ALTER TABLE merchant_quota_holds DROP CONSTRAINT "+constraint); err != nil {
		t.Fatal(err)
	}
	if _, promote, err := e.callImageProvider(ctx, runID, node, "fixture", "digest", NodeImageGeneration, invoke); err != nil || !promote {
		t.Fatalf("retry preparation promote=%v err=%v", promote, err)
	}
	if !invoked {
		t.Fatal("provider not called after fault removed")
	}
}
