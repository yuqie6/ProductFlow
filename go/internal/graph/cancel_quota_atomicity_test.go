package graph

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"github.com/yuqie6/productflow/internal/quota"
	"gorm.io/gorm"
)

func TestCancelGraphQuotaIsAtomicForBothEntryPoints(t *testing.T) {
	for _, entry := range []string{"service", "caller_transaction"} {
		for _, started := range []bool{false, true} {
			name := entry + "/before_provider"
			if started {
				name = entry + "/after_provider"
			}
			t.Run(name, func(t *testing.T) {
				pool, db := testdb.Open(t)
				ctx := context.Background()
				attempt := clockid.New()
				runID := insertStaleRunningGraphRun(t, pool, "claimed", &attempt, time.Now().UTC())
				var productID, graphID, nodeID string
				if err := pool.QueryRow(ctx, "SELECT g.product_id,r.graph_id,n.id FROM workflow_graph_runs r JOIN workflow_graphs g ON g.id=r.graph_id JOIN workflow_graph_node_runs n ON n.graph_run_id=r.id WHERE r.id=$1", runID).Scan(&productID, &graphID, &nodeID); err != nil {
					t.Fatal(err)
				}
				merchantID := auth.MustDevMerchantID(t, db)
				q := &quota.Service{DB: db}
				if _, err := q.Adjust(ctx, merchantID, clockid.New(), 10, "cancel fixture", ""); err != nil {
					t.Fatal(err)
				}
				key := imageNodeQuotaKey(nodeID, attempt)
				if _, _, err := q.Reserve(ctx, merchantID, key, 1, quota.DefaultPriceVersionID); err != nil {
					t.Fatal(err)
				}
				if started {
					now := time.Now().UTC()
					if err := db.Create(&schema.WorkflowGraphProviderEffects{ID: clockid.New(), NodeRunID: nodeID, OperationKey: clockid.New(), EffectKind: "image", QuotaKey: &key, RequestHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ProviderName: "fixture", AttemptID: attempt, EffectResult: "pending", ReconciliationState: "not_requested", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
						t.Fatal(err)
					}
				}
				before, err := q.GetAccount(ctx, merchantID)
				if err != nil {
					t.Fatal(err)
				}
				const constraint = "test_graph_cancel_quota_atomic"
				if _, err := pool.Exec(ctx, "ALTER TABLE merchant_quota_holds ADD CONSTRAINT "+constraint+" CHECK (idempotency_key <> '"+key+"' OR status='reserved')"); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if _, err := pool.Exec(context.Background(), "ALTER TABLE merchant_quota_holds DROP CONSTRAINT IF EXISTS "+constraint); err != nil {
						t.Error(err)
					}
				})
				callbackCount := 0
				svc := Service{DB: db, Products: cmdTestProducts{}, AfterRunStatus: func(context.Context, *gorm.DB, string) error { callbackCount++; return nil }}
				cancel := func() error {
					if entry == "caller_transaction" {
						return tx.WithGorm(ctx, db, func(dbtx *gorm.DB) error { _, err := svc.CancelRunTx(ctx, dbtx, productID, graphID, runID); return err })
					}
					_, err := svc.CancelRun(ctx, productID, graphID, runID)
					return err
				}
				err = cancel()
				var appErr apperr.Error
				var pgErr *pgconn.PgError
				if !errors.As(err, &appErr) || appErr.Status != 500 || appErr.Detail != "更新预留失败" {
					t.Fatalf("quota application error lost: %v", err)
				}
				if !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != constraint {
					t.Fatalf("quota database cause lost: %v", err)
				}
				var runStatus, nodeStatus, active string
				if err := pool.QueryRow(ctx, "SELECT r.status,n.status,n.active_attempt_id FROM workflow_graph_runs r JOIN workflow_graph_node_runs n ON n.graph_run_id=r.id WHERE r.id=$1", runID).Scan(&runStatus, &nodeStatus, &active); err != nil {
					t.Fatal(err)
				}
				if runStatus != "running" || nodeStatus != "running" || active != attempt {
					t.Fatalf("partial cancel run=%s node=%s attempt=%s", runStatus, nodeStatus, active)
				}
				var events int
				if err := pool.QueryRow(ctx, "SELECT count(*) FROM workflow_graph_run_events WHERE graph_run_id=$1", runID).Scan(&events); err != nil {
					t.Fatal(err)
				}
				if events != 0 || callbackCount != 0 {
					t.Fatalf("failed cancellation projected events=%d callbacks=%d", events, callbackCount)
				}
				after, err := q.GetAccount(ctx, merchantID)
				if err != nil {
					t.Fatal(err)
				}
				if before.AvailableUnits != after.AvailableUnits || before.ReservedUnits != after.ReservedUnits {
					t.Fatal("quota balance partially committed")
				}
				if _, err := pool.Exec(ctx, "ALTER TABLE merchant_quota_holds DROP CONSTRAINT "+constraint); err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 2; i++ {
					if err := cancel(); err != nil {
						t.Fatal(err)
					}
				}
				var hold schema.MerchantQuotaHolds
				if err := db.Where("merchant_id=? AND idempotency_key=?", merchantID, key).Take(&hold).Error; err != nil {
					t.Fatal(err)
				}
				want := quota.StatusReleased
				if started {
					want = quota.StatusPendingReconciliation
				}
				if hold.Status != want {
					t.Fatalf("hold=%s want=%s", hold.Status, want)
				}
				if err := pool.QueryRow(ctx, "SELECT count(*) FROM workflow_graph_run_events WHERE graph_run_id=$1 AND kind='run.cancelled'", runID).Scan(&events); err != nil {
					t.Fatal(err)
				}
				if events != 1 {
					t.Fatalf("cancel events=%d", events)
				}
			})
		}
	}
}
