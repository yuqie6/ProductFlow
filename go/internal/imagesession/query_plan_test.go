package imagesession

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/platform/testdb"
)

func TestImageSessionQueryPlanTargetScale(t *testing.T) {
	if os.Getenv("PRODUCTFLOW_RUN_IMAGE_SESSION_QUERY_PLAN") != "1" {
		t.Skip("set PRODUCTFLOW_RUN_IMAGE_SESSION_QUERY_PLAN=1 to run the target-scale PostgreSQL plan gate")
	}
	name := fmt.Sprintf("pf_iplan_%d", time.Now().UnixNano()%1_000_000_000)
	pool, _ := testdb.IsolatedMigrated(t, name)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	seedTargetScaleImageSessions(t, ctx, pool)

	listPlan := testdb.ExplainAnalyze(t, ctx, pool, `
		SELECT id, title, created_at, updated_at
		FROM image_sessions
		ORDER BY updated_at DESC, id DESC
		LIMIT 21
	`)
	testdb.AssertNoSeqScan(t, "image session list", listPlan, 21)
}

func seedTargetScaleImageSessions(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO image_sessions (id, title, created_at, updated_at)
		SELECT 'plan-img-' || lpad(g::text, 5, '0'),
		       'query-plan',
		       NOW() - (g * INTERVAL '1 second'),
		       NOW() - (g * INTERVAL '1 second')
		FROM generate_series(0, 24999) AS g
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ANALYZE image_sessions`); err != nil {
		t.Fatal(err)
	}
}
