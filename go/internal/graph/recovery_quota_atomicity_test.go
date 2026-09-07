package graph

import (
	"context"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/quota"
)

func TestGraphRecoveryQuotaFailurePreservesRecoverableAttempt(t *testing.T) {
	for _, phase := range []string{"claimed", "provider_call"} {
		t.Run(phase, func(t *testing.T) {
			pool, db := testdb.Open(t)
			ctx := WithProductGuard(context.Background(), cmdTestProducts{})
			attempt := clockid.New()
			runID := insertStaleRunningGraphRun(t, pool, phase, &attempt, time.Now().UTC().Add(-time.Hour))
			var nodeID string
			if err := pool.QueryRow(ctx, "SELECT id FROM workflow_graph_node_runs WHERE graph_run_id=$1", runID).Scan(&nodeID); err != nil {
				t.Fatal(err)
			}
			merchantID := auth.MustDevMerchantID(t, db)
			q := &quota.Service{DB: db}
			if _, err := q.Adjust(ctx, merchantID, clockid.New(), 10, "recovery fixture", ""); err != nil {
				t.Fatal(err)
			}
			key := imageNodeQuotaKey(nodeID, attempt)
			if _, _, err := q.Reserve(ctx, merchantID, key, 1, quota.DefaultPriceVersionID); err != nil {
				t.Fatal(err)
			}
			before, err := q.GetAccount(ctx, merchantID)
			if err != nil {
				t.Fatal(err)
			}
			const constraint = "test_graph_recovery_quota_atomic"
			if _, err := pool.Exec(ctx, "ALTER TABLE merchant_quota_holds ADD CONSTRAINT "+constraint+" CHECK (idempotency_key <> '"+key+"' OR status='reserved')"); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := pool.Exec(context.Background(), "ALTER TABLE merchant_quota_holds DROP CONSTRAINT IF EXISTS "+constraint); err != nil {
					t.Error(err)
				}
			})
			cutoff := time.Now().UTC().Add(-time.Minute)
			if _, err := recoverGraphRunState(ctx, db, runID, cutoff); err == nil {
				t.Fatal("recovery swallowed quota write failure")
			}
			var nodeStatus, runStatus, active string
			if err := pool.QueryRow(ctx, "SELECT r.status,n.status,n.active_attempt_id FROM workflow_graph_runs r JOIN workflow_graph_node_runs n ON n.graph_run_id=r.id WHERE r.id=$1", runID).Scan(&runStatus, &nodeStatus, &active); err != nil {
				t.Fatal(err)
			}
			if runStatus != "running" || nodeStatus != "running" || active != attempt {
				t.Fatalf("partial recovery run=%s node=%s active=%s", runStatus, nodeStatus, active)
			}
			after, err := q.GetAccount(ctx, merchantID)
			if err != nil {
				t.Fatal(err)
			}
			if before.AvailableUnits != after.AvailableUnits || before.ReservedUnits != after.ReservedUnits {
				t.Fatal("quota balances partially committed")
			}
			var events int
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM workflow_graph_run_events WHERE graph_run_id=$1", runID).Scan(&events); err != nil {
				t.Fatal(err)
			}
			if events != 0 {
				t.Fatalf("failed recovery projected %d events", events)
			}
			if _, err := pool.Exec(ctx, "ALTER TABLE merchant_quota_holds DROP CONSTRAINT "+constraint); err != nil {
				t.Fatal(err)
			}
			result, err := recoverGraphRunState(ctx, db, runID, cutoff)
			if err != nil {
				t.Fatal(err)
			}
			wantHold, eventType := quota.StatusReleased, quota.EventRelease
			if phase == "claimed" {
				if !result.restage || !result.stale {
					t.Fatalf("requeue result=%+v", result)
				}
			} else {
				wantHold, eventType = quota.StatusPendingReconciliation, quota.EventMarkUnknown
				if !result.unknown || result.restage {
					t.Fatalf("unknown result=%+v", result)
				}
			}
			if err := pool.QueryRow(ctx, "SELECT r.status,n.status FROM workflow_graph_runs r JOIN workflow_graph_node_runs n ON n.graph_run_id=r.id WHERE r.id=$1", runID).Scan(&runStatus, &nodeStatus); err != nil {
				t.Fatal(err)
			}
			if phase == "claimed" {
				if runStatus != "running" || nodeStatus != "queued" {
					t.Fatalf("requeue run=%s node=%s", runStatus, nodeStatus)
				}
			} else if runStatus != "unknown" || nodeStatus != "unknown" {
				t.Fatalf("unknown run=%s node=%s", runStatus, nodeStatus)
			}
			if _, err := recoverGraphRunState(ctx, db, runID, cutoff); err != nil {
				t.Fatal(err)
			}
			var hold schema.MerchantQuotaHolds
			if err := db.Where("merchant_id=? AND idempotency_key=?", merchantID, key).Take(&hold).Error; err != nil {
				t.Fatal(err)
			}
			if hold.Status != wantHold {
				t.Fatalf("hold=%s want=%s", hold.Status, wantHold)
			}
			var quotaEvents int64
			if err := db.Model(&schema.MerchantQuotaEvents{}).Where("merchant_id=? AND idempotency_key=? AND event_type=?", merchantID, key, eventType).Count(&quotaEvents).Error; err != nil {
				t.Fatal(err)
			}
			if quotaEvents != 1 {
				t.Fatalf("replayed quota events=%d", quotaEvents)
			}
		})
	}
}
