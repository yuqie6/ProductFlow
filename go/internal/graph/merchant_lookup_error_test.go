package graph

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/testdb"
)

func TestQuotaMerchantLookupPreservesDatabaseCause(t *testing.T) {
	_, db := testdb.IsolatedMigrated(t, "pf_graph_merchant_"+strings.ReplaceAll(clockid.New(), "-", ""))
	ctx := context.Background()
	if err := db.Exec("ALTER TABLE products RENAME TO unavailable_merchant_source").Error; err != nil {
		t.Fatal(err)
	}
	_, err := merchantIDForGraphRun(ctx, db, clockid.New())
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42P01" {
		t.Errorf("database cause lost: %v", err)
	}
	var appErr apperr.Error
	if !errors.As(err, &appErr) || appErr.Detail != "读取运行商家失败" || appErr.Status != 500 {
		t.Errorf("public contract changed: %v", err)
	}
	if err := db.Exec("ALTER TABLE unavailable_merchant_source RENAME TO products").Error; err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = merchantIDForGraphRun(cancelled, db, clockid.New())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cancellation cause lost: %v", err)
	}
}

func TestClaimedFailureRollsBackOnMerchantReadFailure(t *testing.T) {
	pool, db := testdb.IsolatedMigrated(t, "pf_graph_owner_tx_"+strings.ReplaceAll(clockid.New(), "-", ""))
	ctx := context.Background()
	merchantID := auth.MustDevMerchantID(t, db)
	attempt, phase := clockid.New(), "claimed"
	runID := insertGraphRunForMerchant(t, pool, merchantID, time.Now().UTC(), "running", &phase, &attempt)
	var nodeID string
	if err := pool.QueryRow(ctx, "SELECT id FROM workflow_graph_node_runs WHERE graph_run_id=$1", runID).Scan(&nodeID); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE products RENAME TO unavailable_merchant_source").Error; err != nil {
		t.Fatal(err)
	}
	err := failClaimedNode(ctx, db, runID, nodeID, attempt, "input failure")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42P01" {
		t.Errorf("terminal transition lost original read failure: %v", err)
	}
	var runStatus, nodeStatus, active string
	if err := pool.QueryRow(ctx, "SELECT r.status,n.status,n.active_attempt_id FROM workflow_graph_runs r JOIN workflow_graph_node_runs n ON n.graph_run_id=r.id WHERE r.id=$1", runID).Scan(&runStatus, &nodeStatus, &active); err != nil {
		t.Fatal(err)
	}
	if runStatus != "running" || nodeStatus != "running" || active != attempt {
		t.Fatalf("partial terminal transition: %s %s %s", runStatus, nodeStatus, active)
	}
	if err := db.Exec("ALTER TABLE unavailable_merchant_source RENAME TO products").Error; err != nil {
		t.Fatal(err)
	}
	if err := failClaimedNode(ctx, db, runID, nodeID, attempt, "input failure"); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT status FROM workflow_graph_runs WHERE id=$1", runID).Scan(&runStatus); err != nil {
		t.Fatal(err)
	}
	if runStatus != "failed" {
		t.Fatalf("recovered run=%s", runStatus)
	}
}
