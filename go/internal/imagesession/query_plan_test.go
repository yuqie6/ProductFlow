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

const (
	queryPlanHotSessionID = "plan-img-00000"
	queryPlanHistoryLimit = 21
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
	testdb.AssertIndexUsed(t, "image session list", listPlan, "ix_image_sessions_updated")

	historyPlan := testdb.ExplainAnalyze(t, ctx, pool, `
		SELECT id, session_id, created_at
		FROM image_session_rounds
		WHERE session_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2
	`, queryPlanHotSessionID, queryPlanHistoryLimit)
	testdb.AssertNoSeqScan(t, "history first page", historyPlan, float64(queryPlanHistoryLimit))
	testdb.AssertIndexUsed(t, "history first page", historyPlan, "ix_image_session_rounds_session_created")

	var cursorAt time.Time
	var cursorID string
	if err := pool.QueryRow(ctx, `
		SELECT created_at, id
		FROM image_session_rounds
		WHERE session_id = $1
		ORDER BY created_at DESC, id DESC
		OFFSET $2
		LIMIT 1
	`, queryPlanHotSessionID, queryPlanHistoryLimit-1).Scan(&cursorAt, &cursorID); err != nil {
		t.Fatal(err)
	}
	keysetPlan := testdb.ExplainAnalyze(t, ctx, pool, `
		SELECT id, session_id, created_at
		FROM image_session_rounds
		WHERE session_id = $1
		  AND (created_at < $2 OR (created_at = $2 AND id < $3))
		ORDER BY created_at DESC, id DESC
		LIMIT $4
	`, queryPlanHotSessionID, cursorAt, cursorID, queryPlanHistoryLimit)
	testdb.AssertNoSeqScan(t, "history keyset page", keysetPlan, float64(queryPlanHistoryLimit))
	testdb.AssertIndexUsed(t, "history keyset page", keysetPlan, "ix_image_session_rounds_session_created")

	taskPagePlan := testdb.ExplainAnalyze(t, ctx, pool, `
		SELECT id
		FROM image_session_generation_tasks
		WHERE session_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2
	`, queryPlanHotSessionID, queryPlanHistoryLimit)
	testdb.AssertNoSeqScan(t, "task first page", taskPagePlan, float64(queryPlanHistoryLimit))
	testdb.AssertIndexUsed(t, "task first page", taskPagePlan, "ix_image_session_generation_tasks_session_created")

	// 热会话独占全部 rounds/tasks 时，无 LIMIT 的 COUNT/详情扫描会读完整张表；planner 选 Seq Scan。
	// 这些计划只记录，不作为无 Seq Scan 闸门。GET 详情仍会装入全部匹配任务，payload 仍是缺口。
	_ = testdb.ExplainAnalyze(t, ctx, pool, `
		SELECT COUNT(*)
		FROM image_session_rounds
		WHERE session_id = $1
	`, queryPlanHotSessionID)
	_ = testdb.ExplainAnalyze(t, ctx, pool, `
		SELECT id
		FROM image_session_generation_tasks
		WHERE session_id = $1
		  AND (
		        status IN ('queued', 'running', 'failed', 'unknown', 'cancelled')
		     OR (status = 'succeeded' AND result_generation_group_id IS NULL)
		  )
		ORDER BY created_at DESC, id DESC
	`, queryPlanHotSessionID)
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
	if _, err := pool.Exec(ctx, `
		INSERT INTO media_objects (id, storage_path, mime_type, verification_status, created_at)
		SELECT 'plan-imedia-' || lpad(g::text, 5, '0'),
		       '/tmp/plan-imedia-' || g,
		       'image/png',
		       'legacy_pending',
		       NOW()
		FROM generate_series(0, 9999) AS g
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO image_session_assets (
			id, session_id, kind, original_filename, mime_type, storage_path, created_at, media_object_id
		)
		SELECT 'plan-iasset-' || lpad(g::text, 5, '0'),
		       $1,
		       'generated_image',
		       'g.png',
		       'image/png',
		       '/tmp/plan-imedia-' || g,
		       NOW() - (g * INTERVAL '1 second'),
		       'plan-imedia-' || lpad(g::text, 5, '0')
		FROM generate_series(0, 9999) AS g
	`, queryPlanHotSessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO image_session_rounds (
			id, session_id, prompt, assistant_message, size, model_name, provider_name, prompt_version,
			generated_asset_id, created_at, candidate_index, candidate_count
		)
		SELECT 'plan-iround-' || lpad(g::text, 5, '0'),
		       $1,
		       'p',
		       'a',
		       '1024x1024',
		       'model',
		       'provider',
		       'v1',
		       'plan-iasset-' || lpad(g::text, 5, '0'),
		       NOW() - (g * INTERVAL '1 second'),
		       0,
		       1
		FROM generate_series(0, 9999) AS g
	`, queryPlanHotSessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO image_session_generation_tasks (
			id, session_id, status, prompt, size, generation_count, created_at,
			attempts, is_retryable, completed_candidates
		)
		SELECT 'plan-itask-' || lpad(g::text, 4, '0'),
		       $1,
		       'queued'::jobstatus,
		       'p',
		       '1024x1024',
		       1,
		       NOW() - (g * INTERVAL '1 second'),
		       0,
		       TRUE,
		       1
		FROM generate_series(0, 9) AS g
	`, queryPlanHotSessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO image_session_generation_tasks (
			id, session_id, status, prompt, size, generation_count, created_at,
			attempts, is_retryable, completed_candidates
		)
		SELECT 'plan-itask-' || lpad(g::text, 4, '0'),
		       $1,
		       'succeeded'::jobstatus,
		       'p',
		       '1024x1024',
		       1,
		       NOW() - (g * INTERVAL '1 second'),
		       0,
		       TRUE,
		       1
		FROM generate_series(10, 999) AS g
	`, queryPlanHotSessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		ANALYZE image_sessions;
		ANALYZE image_session_rounds;
		ANALYZE image_session_assets;
		ANALYZE image_session_generation_tasks;
		ANALYZE media_objects
	`); err != nil {
		t.Fatal(err)
	}
}
