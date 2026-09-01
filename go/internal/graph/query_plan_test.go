package graph_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/platform/testdb"
)

type explainPlanResult struct {
	Plan          explainPlanNode `json:"Plan"`
	ExecutionTime float64         `json:"Execution Time"`
	PlanningTime  float64         `json:"Planning Time"`
}

type explainPlanNode struct {
	NodeType   string            `json:"Node Type"`
	ActualRows float64           `json:"Actual Rows"`
	Plans      []explainPlanNode `json:"Plans"`
}

// TestGraphSummaryQueryPlanTargetScale is opt-in because it creates an isolated database
// and inserts 25k runs plus 100k node-run rows. It protects the list indexes at the acceptance scale.
func TestGraphSummaryQueryPlanTargetScale(t *testing.T) {
	if os.Getenv("PRODUCTFLOW_RUN_GRAPH_QUERY_PLAN") != "1" {
		t.Skip("set PRODUCTFLOW_RUN_GRAPH_QUERY_PLAN=1 to run the target-scale PostgreSQL plan gate")
	}
	rawURL := os.Getenv("DATABASE_URL")
	if rawURL == "" {
		t.Skip("DATABASE_URL not set")
	}

	name := fmt.Sprintf("pf_gplan_%d", time.Now().UnixNano()%1_000_000_000)
	_, pool, _ := isolatedMigratedDB(t, testdb.Pool(t), rawURL, name)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	seedTargetScaleGraphData(t, ctx, pool)

	runIDs := make([]string, 0, 20)
	rows, err := pool.Query(ctx, `
		SELECT id
		FROM workflow_graph_runs
		WHERE graph_id = $1
		ORDER BY started_at DESC, id DESC
		LIMIT 20
	`, "plan-graph-0")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		runIDs = append(runIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	if len(runIDs) != 20 {
		t.Fatalf("target graph returned %d runs", len(runIDs))
	}

	runPlan := explainQuery(t, ctx, pool, `
		SELECT id, graph_id, status, run_scope, requested_node_id, graph_revision,
		       failure_reason, is_retryable, progress_metadata, started_at, finished_at
		FROM workflow_graph_runs
		WHERE graph_id = $1
		ORDER BY started_at DESC, id DESC
		LIMIT 20
	`, "plan-graph-0")
	assertPlanShape(t, "run summary", runPlan, 20)

	placeholders := make([]string, len(runIDs))
	args := make([]any, len(runIDs))
	for i, id := range runIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	nodePlan := explainQuery(t, ctx, pool, fmt.Sprintf(`
		SELECT id, graph_run_id, node_id, status, sort_order, failure_reason,
		       attempt_count, progress_phase, planned_action, started_at, finished_at
		FROM workflow_graph_node_runs
		WHERE graph_run_id IN (%s)
		ORDER BY graph_run_id, sort_order, id
	`, strings.Join(placeholders, ", ")), args...)
	assertPlanShape(t, "node summary", nodePlan, 400)

	detailRunPlan := explainQuery(t, ctx, pool, `
		SELECT *
		FROM workflow_graph_runs
		WHERE id = $1 AND graph_id = $2
	`, runIDs[0], "plan-graph-0")
	assertPlanShape(t, "run detail", detailRunPlan, 1)
	detailNodePlan := explainQuery(t, ctx, pool, `
		SELECT *
		FROM workflow_graph_node_runs
		WHERE graph_run_id = $1
		ORDER BY sort_order, id
	`, runIDs[0])
	assertPlanShape(t, "node detail", detailNodePlan, 20)
}

func seedTargetScaleGraphData(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO products (id, name, created_at, updated_at)
		SELECT 'plan-product-' || g, 'query-plan-' || g, NOW(), NOW()
		FROM generate_series(0, 4) AS g
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO workflow_graphs
			(id, product_id, title, active, schema_version, revision, created_at, updated_at)
		SELECT 'plan-graph-' || g, 'plan-product-' || g, 'query-plan-' || g,
		       TRUE, 3, 1, NOW(), NOW()
		FROM generate_series(0, 4) AS g
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO workflow_graph_runs
			(id, graph_id, status, run_scope, graph_revision, snapshot_json, is_retryable, started_at)
		SELECT 'plan-run-' || g || '-' || lpad(r::text, 4, '0'),
		       'plan-graph-' || g, 'succeeded', 'graph', 1, '{}', FALSE,
		       NOW() - ((g * 5000 + r) * INTERVAL '1 second')
		FROM generate_series(0, 4) AS g
		CROSS JOIN generate_series(0, 4999) AS r
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO workflow_graph_node_runs
			(id, graph_run_id, status, sort_order, attempt_count, started_at)
		SELECT 'plan-node-run-' || g || '-' || lpad(r::text, 4, '0') || '-' || lpad(n::text, 2, '0'),
		       'plan-run-' || g || '-' || lpad(r::text, 4, '0'),
		       'succeeded', n, 1,
		       NOW() - ((g * 5000 + r) * INTERVAL '1 second')
		FROM generate_series(0, 0) AS g
		CROSS JOIN generate_series(0, 4999) AS r
		CROSS JOIN generate_series(0, 19) AS n
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ANALYZE workflow_graph_runs; ANALYZE workflow_graph_node_runs`); err != nil {
		t.Fatal(err)
	}
}

func explainQuery(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, args ...any) explainPlanResult {
	t.Helper()
	var raw []byte
	if err := pool.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var result []explainPlanResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode explain: %v: %s", err, raw)
	}
	if len(result) != 1 {
		t.Fatalf("unexpected explain result: %s", raw)
	}
	t.Logf("explain planning=%.3fms execution=%.3fms plan=%s", result[0].PlanningTime, result[0].ExecutionTime, raw)
	return result[0]
}

func assertPlanShape(t *testing.T, name string, result explainPlanResult, wantRows float64) {
	t.Helper()
	if result.Plan.ActualRows != wantRows {
		t.Fatalf("%s returned %.0f rows, want %.0f", name, result.Plan.ActualRows, wantRows)
	}
	nodes := planNodeTypes(result.Plan)
	for _, node := range nodes {
		if node == "Seq Scan" {
			t.Fatalf("%s used Seq Scan at target scale: %v", name, nodes)
		}
	}
}

func planNodeTypes(node explainPlanNode) []string {
	out := []string{node.NodeType}
	for _, child := range node.Plans {
		out = append(out, planNodeTypes(child)...)
	}
	return out
}
