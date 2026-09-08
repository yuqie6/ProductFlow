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

func TestGraphUnknownTransitionsOwnQuota(t *testing.T) {
	for _, entry := range []string{"provider_error", "claimed_failure", "run_failure"} {
		t.Run(entry, func(t *testing.T) {
			pool, db := testdb.Open(t)
			ctx := WithProductGuard(context.Background(), cmdTestProducts{})
			attempt := clockid.New()
			runID := insertStaleRunningGraphRun(t, pool, "provider_call", &attempt, time.Now().UTC())
			var nodeID string
			if err := pool.QueryRow(ctx, "SELECT id FROM workflow_graph_node_runs WHERE graph_run_id=$1", runID).Scan(&nodeID); err != nil {
				t.Fatal(err)
			}
			merchantID := auth.MustDevMerchantID(t, db)
			q := &quota.Service{DB: db}
			if _, err := q.Adjust(ctx, merchantID, clockid.New(), 10, "unknown fixture", ""); err != nil {
				t.Fatal(err)
			}
			key := imageNodeQuotaKey(nodeID, attempt)
			if _, _, err := q.Reserve(ctx, merchantID, key, 1, quota.DefaultPriceVersionID); err != nil {
				t.Fatal(err)
			}
			recordGraphEffectFixture(t, db, nodeID, attempt, &key)
			const constraint = "test_graph_unknown_quota"
			if _, err := pool.Exec(ctx, "ALTER TABLE merchant_quota_holds ADD CONSTRAINT "+constraint+" CHECK (idempotency_key <> '"+key+"' OR status='reserved')"); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := pool.Exec(context.Background(), "ALTER TABLE merchant_quota_holds DROP CONSTRAINT IF EXISTS "+constraint); err != nil {
					t.Error(err)
				}
			})
			finish := func() error {
				switch entry {
				case "provider_error":
					return (Executor{DB: db}).markUnknownCommitted(ctx, runID, nodeID, &attempt, ProviderUnknownDetail)
				case "claimed_failure":
					return failClaimedNode(ctx, db, runID, nodeID, attempt, "write failure after provider")
				default:
					return failGraphRun(ctx, db, runID, "run failure after provider")
				}
			}
			if err := finish(); err == nil {
				t.Fatal("unknown transition ignored quota persistence failure")
			}
			var nodeStatus, runStatus, active string
			if err := pool.QueryRow(ctx, "SELECT r.status,n.status,n.active_attempt_id FROM workflow_graph_runs r JOIN workflow_graph_node_runs n ON n.graph_run_id=r.id WHERE r.id=$1", runID).Scan(&runStatus, &nodeStatus, &active); err != nil {
				t.Fatal(err)
			}
			if runStatus != "running" || nodeStatus != "running" || active != attempt {
				t.Fatalf("partial unknown run=%s node=%s attempt=%s", runStatus, nodeStatus, active)
			}
			if _, err := pool.Exec(ctx, "ALTER TABLE merchant_quota_holds DROP CONSTRAINT "+constraint); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if err := finish(); err != nil {
					t.Fatal(err)
				}
			}
			var hold schema.MerchantQuotaHolds
			if err := db.Where("merchant_id=? AND idempotency_key=?", merchantID, key).Take(&hold).Error; err != nil {
				t.Fatal(err)
			}
			if hold.Status != quota.StatusPendingReconciliation {
				t.Fatalf("unknown hold=%s", hold.Status)
			}
			var events int64
			if err := db.Model(&schema.MerchantQuotaEvents{}).Where("merchant_id=? AND idempotency_key=? AND event_type=?", merchantID, key, quota.EventMarkUnknown).Count(&events).Error; err != nil {
				t.Fatal(err)
			}
			if events != 1 {
				t.Fatalf("unknown events=%d", events)
			}
		})
	}
}
