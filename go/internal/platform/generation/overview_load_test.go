package generation

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/testdb"
	"gorm.io/gorm"
)

func TestQueueOverviewActiveGraphScale(t *testing.T) {
	if os.Getenv("PRODUCTFLOW_RUN_QUEUE_OVERVIEW_LOAD") != "1" {
		t.Skip("set PRODUCTFLOW_RUN_QUEUE_OVERVIEW_LOAD=1 to run the active Graph overview gate")
	}
	if os.Getenv("DATABASE_URL") == "" {
		t.Fatal("queue overview gate requires DATABASE_URL")
	}
	pool, db := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_qoverview_%d", time.Now().UnixNano()))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	for _, query := range []string{
		`INSERT INTO products (id, name, created_at, updated_at) SELECT 'overview-product-' || g, 'fixture', NOW(), NOW() FROM generate_series(1,25000) g`,
		`INSERT INTO workflow_graphs (id, product_id, title, active, schema_version, revision, created_at, updated_at)
		 SELECT 'overview-graph-' || g, 'overview-product-' || g, 'fixture', TRUE, 3, 1, NOW(), NOW() FROM generate_series(1,25000) g`,
		`INSERT INTO workflow_graph_runs (id, graph_id, status, run_scope, graph_revision, snapshot_json, is_retryable, started_at)
		 SELECT 'overview-run-' || g, 'overview-graph-' || g, 'running', 'graph', 1, '{}', TRUE, NOW() FROM generate_series(1,25000) g`,
		`INSERT INTO workflow_graph_node_runs (id, graph_run_id, status, sort_order, started_at, attempt_count)
		 SELECT 'overview-node-' || g || '-' || n, 'overview-run-' || g,
		 (CASE WHEN g % 4 = 0 THEN 'succeeded' WHEN g % 4 = 1 THEN 'queued'
		 WHEN g % 4 = 2 THEN 'running' WHEN n = 1 THEN 'running' ELSE 'queued' END),
		 n, NOW(), 0 FROM generate_series(1,25000) g CROSS JOIN generate_series(1,4) n`,
		`ANALYZE workflow_graph_runs`,
		`ANALYZE workflow_graph_node_runs`,
	} {
		if _, err := pool.Exec(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	var sql string
	var args []any
	callback := "test:overview_scale_sql"
	if err := db.Callback().Query().After("gorm:query").Register(callback, func(query *gorm.DB) {
		if !query.DryRun && strings.Contains(query.Statement.SQL.String(), "AS session_counts") {
			sql, args = query.Statement.SQL.String(), append([]any(nil), query.Statement.Vars...)
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove(callback)
	for _, active := range []int{25000, 100, 0} {
		t.Run(fmt.Sprintf("active_%d", active), func(t *testing.T) {
			// Keep historical node states active to exercise the parent-run filter.
			if _, err := pool.Exec(ctx, `UPDATE workflow_graph_runs SET status = 'succeeded'
				WHERE split_part(id, '-', 3)::int > $1`, active); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `ANALYZE workflow_graph_runs`); err != nil {
				t.Fatal(err)
			}
			var durations []time.Duration
			for i := 0; i < 110; i++ {
				start := time.Now()
				snap, err := LoadQueueOverview(ctx, db)
				elapsed := time.Since(start)
				if err != nil {
					t.Fatal(err)
				}
				if snap.OverviewRunning != active/2 || snap.OverviewQueued != active/4 || snap.OverviewActive() != active*3/4 || snap.AdmissionRunning != 0 {
					t.Fatalf("incorrect mixed-node overview: %+v", snap)
				}
				if i >= 10 {
					durations = append(durations, elapsed)
				}
			}
			slices.Sort(durations)
			if sql == "" {
				t.Fatal("did not capture production aggregate SQL")
			}
			plan := testdb.ExplainAnalyze(t, ctx, pool, sql, args...)
			t.Logf("QUEUE_OVERVIEW_SCALE total_runs=25000 active_runs=%d nodes=100000 concurrency=1 warmup=10 samples=100 p50=%s p95=%s sql_execution_ms=%.3f", active, durations[49], durations[94], plan.ExecutionTime)
			// Local regression budget; this does not establish an HTTP or production SLO.
			if durations[94] >= 300*time.Millisecond {
				t.Errorf("overview p95 %s exceeds local 300ms budget", durations[94])
			}
		})
	}
}
