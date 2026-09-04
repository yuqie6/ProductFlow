package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/platform/testdb"
)

func TestAgentSessionQueryPlanTargetScale(t *testing.T) {
	if os.Getenv("PRODUCTFLOW_RUN_AGENT_QUERY_PLAN") != "1" {
		t.Skip("set PRODUCTFLOW_RUN_AGENT_QUERY_PLAN=1 to run the target-scale PostgreSQL plan gate")
	}
	name := fmt.Sprintf("pf_aplan_%d", time.Now().UnixNano()%1_000_000_000)
	pool, _ := testdb.IsolatedMigrated(t, name)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	seedTargetScaleAgentSessions(t, ctx, pool)

	dockPlan := testdb.ExplainAnalyze(t, ctx, pool, `
		SELECT id
		FROM agent_sessions
		WHERE product_id IS NULL AND status = 'active'
		ORDER BY activity_at DESC, id DESC
		LIMIT 21
	`)
	testdb.AssertNoSeqScan(t, "dock session list", dockPlan, 21)

	productPlan := testdb.ExplainAnalyze(t, ctx, pool, `
		SELECT id
		FROM agent_sessions
		WHERE product_id = $1 AND status = 'active'
		ORDER BY activity_at DESC, id DESC
		LIMIT 21
	`, "plan-product-0")
	testdb.AssertNoSeqScan(t, "product session list", productPlan, 21)

	ids := make([]string, 0, 20)
	rows, err := pool.Query(ctx, `
		SELECT id
		FROM agent_sessions
		WHERE product_id IS NULL AND status = 'active'
		ORDER BY activity_at DESC, id DESC
		LIMIT 20
	`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	if len(ids) != 20 {
		t.Fatalf("dock list returned %d sessions", len(ids))
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	inList := strings.Join(placeholders, ", ")
	sessionPlan := testdb.ExplainAnalyze(t, ctx, pool, fmt.Sprintf(`
		SELECT id, product_id, title, summary, status, archived_at, created_at, updated_at
		FROM agent_sessions
		WHERE id IN (%s)
	`, inList), args...)
	testdb.AssertNoSeqScan(t, "session batch", sessionPlan, 20)

	countPlan := testdb.ExplainAnalyze(t, ctx, pool, fmt.Sprintf(`
		SELECT session_id, COUNT(*) AS count
		FROM agent_conversations
		WHERE session_id IN (%s)
		GROUP BY session_id
	`, inList), args...)
	testdb.AssertNoSeqScan(t, "conversation counts", countPlan, 20)
}

func seedTargetScaleAgentSessions(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO products (id, name, created_at, updated_at)
		VALUES ('plan-product-0', 'query-plan-product', NOW(), NOW())
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO agent_sessions (id, title, status, created_at, updated_at, activity_at, summary, product_id)
		SELECT 'plan-sess-' || lpad(g::text, 5, '0'),
		       'query-plan',
		       'active',
		       NOW() - (g * INTERVAL '1 second'),
		       NOW() - (g * INTERVAL '1 second'),
		       NOW() - (g * INTERVAL '1 second'),
		       'query-plan',
		       NULL
		FROM generate_series(0, 19999) AS g
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO agent_sessions (id, title, status, created_at, updated_at, activity_at, summary, product_id)
		SELECT 'plan-psess-' || lpad(g::text, 5, '0'),
		       'query-plan-product',
		       'active',
		       NOW() - (g * INTERVAL '1 second'),
		       NOW() - (g * INTERVAL '1 second'),
		       NOW() - (g * INTERVAL '1 second'),
		       'query-plan',
		       'plan-product-0'
		FROM generate_series(0, 4999) AS g
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO agent_conversations (
			id, harness_run_id, status, created_at, updated_at, session_id, scope_type
		)
		SELECT 'plan-conv-' || lpad(g::text, 5, '0'),
		       'plan-conv-' || lpad(g::text, 5, '0'),
		       'collecting',
		       NOW() - (g * INTERVAL '1 second'),
		       NOW() - (g * INTERVAL '1 second'),
		       'plan-sess-' || lpad(g::text, 5, '0'),
		       'global'
		FROM generate_series(0, 19999) AS g
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO agent_conversations (
			id, product_id, harness_run_id, status, created_at, updated_at, session_id, scope_type
		)
		SELECT 'plan-pconv-' || lpad(g::text, 5, '0'),
		       'plan-product-0',
		       'plan-pconv-' || lpad(g::text, 5, '0'),
		       'collecting',
		       NOW() - (g * INTERVAL '1 second'),
		       NOW() - (g * INTERVAL '1 second'),
		       'plan-psess-' || lpad(g::text, 5, '0'),
		       'product_workflow'
		FROM generate_series(0, 4999) AS g
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ANALYZE agent_sessions; ANALYZE agent_conversations`); err != nil {
		t.Fatal(err)
	}
}
