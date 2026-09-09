package graph

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/testdb"
)

func TestRecoverUnfinishedGraphRunsLimitHasMoreAndIsolation(t *testing.T) {
	pool, _ := testdb.Open(t)
	drainGraphRecovery(t, pool)

	older := insertRunningGraphRunWithQueuedNode(t, pool, time.Unix(1, 0).UTC())
	newer := insertRunningGraphRunWithQueuedNode(t, pool, time.Unix(2, 0).UTC())

	first, err := recoverUnfinishedGraphRuns(context.Background(), pool, time.Hour, cmdTestProducts{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.QueuedRuns != 1 || first.EnqueuedRuns != 1 || !first.HasMore {
		t.Fatalf("first round %+v", first)
	}
	if pendingDispatchCount(t, pool, older) != 1 {
		t.Fatal("older run must commit before the rest of the batch")
	}
	if pendingDispatchCount(t, pool, newer) != 0 {
		t.Fatal("newer run must wait for the next round")
	}

	second, err := recoverUnfinishedGraphRuns(context.Background(), pool, time.Hour, cmdTestProducts{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if second.QueuedRuns != 1 || second.EnqueuedRuns != 1 || second.HasMore {
		t.Fatalf("second round %+v", second)
	}
	if pendingDispatchCount(t, pool, newer) != 1 {
		t.Fatal("newer run must restage on the second round")
	}
}

func TestRecoverUnfinishedGraphRunsRequeuesClaimedAndMarksUnknown(t *testing.T) {
	pool, db := testdb.Open(t)
	drainGraphRecovery(t, pool)

	requeueID := insertStaleRunningGraphRun(t, pool, "claimed", nil, time.Unix(1, 0).UTC())
	attempt := clockid.New()
	unknownID := insertStaleRunningGraphRun(t, pool, "provider_call", &attempt, time.Unix(2, 0).UTC())
	var nodeID string
	if err := pool.QueryRow(context.Background(), "SELECT id FROM workflow_graph_node_runs WHERE graph_run_id=$1", unknownID).Scan(&nodeID); err != nil {
		t.Fatal(err)
	}
	recordGraphEffectFixture(t, db, nodeID, attempt, nil)

	summary, err := recoverUnfinishedGraphRuns(context.Background(), pool, time.Minute, cmdTestProducts{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if summary.StaleRunningRuns < 1 || summary.UnknownRuns < 1 {
		t.Fatalf("summary %+v", summary)
	}

	var requeueStatus, requeuePhase string
	if err := pool.QueryRow(context.Background(), `
		SELECT status, progress_phase FROM workflow_graph_node_runs WHERE graph_run_id = $1
	`, requeueID).Scan(&requeueStatus, &requeuePhase); err != nil {
		t.Fatal(err)
	}
	if requeueStatus != "queued" || requeuePhase != "requeued_after_idle" {
		t.Fatalf("requeue status=%s phase=%s", requeueStatus, requeuePhase)
	}
	if pendingDispatchCount(t, pool, requeueID) != 1 {
		t.Fatal("requeued run must restage")
	}

	var nodeStatus, runStatus string
	var retryable bool
	if err := pool.QueryRow(context.Background(), `
		SELECT n.status, r.status, r.is_retryable
		FROM workflow_graph_node_runs n
		JOIN workflow_graph_runs r ON r.id = n.graph_run_id
		WHERE n.graph_run_id = $1
	`, unknownID).Scan(&nodeStatus, &runStatus, &retryable); err != nil {
		t.Fatal(err)
	}
	if nodeStatus != "unknown" || runStatus != "unknown" {
		t.Fatalf("unknown node=%s run=%s", nodeStatus, runStatus)
	}
	if retryable {
		t.Fatal("unknown run must not be retryable")
	}
	if pendingDispatchCount(t, pool, unknownID) != 0 {
		t.Fatal("terminal unknown run must not restage")
	}
}

func drainGraphRecovery(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		UPDATE workflow_graph_node_runs
		SET status = 'cancelled', finished_at = NOW()
		WHERE status IN ('queued', 'running')
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `
		UPDATE workflow_graph_runs
		SET status = 'cancelled', finished_at = NOW()
		WHERE status IN ('queued', 'running')
	`); err != nil {
		t.Fatal(err)
	}
}

func insertRunningGraphRunWithQueuedNode(t *testing.T, pool *pgxpool.Pool, startedAt time.Time) string {
	t.Helper()
	return insertGraphRun(t, pool, startedAt, "queued", nil, nil)
}

func insertStaleRunningGraphRun(t *testing.T, pool *pgxpool.Pool, phase string, attemptID *string, startedAt time.Time) string {
	t.Helper()
	return insertGraphRun(t, pool, startedAt, "running", &phase, attemptID)
}

func insertGraphRun(t *testing.T, pool *pgxpool.Pool, startedAt time.Time, nodeStatus string, phase *string, attemptID *string) string {
	t.Helper()
	return insertGraphRunForMerchant(t, pool, auth.MustDevMerchantID(t, testdb.Gorm(t)), startedAt, nodeStatus, phase, attemptID)
}

func insertGraphRunForMerchant(t *testing.T, pool *pgxpool.Pool, merchantID string, startedAt time.Time, nodeStatus string, phase *string, attemptID *string) string {
	t.Helper()
	productID := clockid.New()
	graphID := clockid.New()
	runID := clockid.New()
	nodeRunID := clockid.New()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO products (id, name, created_at, updated_at, merchant_id) VALUES ($1, 'recovery', NOW(), NOW(), $2)
	`, productID, merchantID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO workflow_graphs (id, product_id, title, active, schema_version, revision, created_at, updated_at)
		VALUES ($1, $2, 'recovery', TRUE, 3, 1, NOW(), NOW())
	`, graphID, productID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO workflow_graph_runs (
			id, graph_id, status, run_scope, graph_revision, snapshot_json, is_retryable, started_at
		) VALUES ($1, $2, 'running', 'graph', 1, '{}', TRUE, $3)
	`, runID, graphID, startedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO workflow_graph_node_runs (
			id, graph_run_id, status, sort_order, started_at, progress_phase, progress_updated_at, active_attempt_id, attempt_count
		) VALUES ($1, $2, $3, 0, $4, $5, $4, $6, 0)
	`, nodeRunID, runID, nodeStatus, startedAt, phase, attemptID); err != nil {
		t.Fatal(err)
	}
	return runID
}

func pendingDispatchCount(t *testing.T, pool *pgxpool.Pool, aggregateID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM river_job WHERE args ->> 'aggregate_id' = $1 AND state = 'available'
	`, aggregateID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
