package queue_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/platform/queue"
)

// Delay only real recovery writes in the disposable database, never production.
func stageSlowRecoveryLoad(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO image_sessions (id, title, created_at, updated_at)
		VALUES ('recovery-latency', 'fixture', NOW(), NOW());
		INSERT INTO image_session_generation_tasks
		(id, session_id, status, prompt, size, generation_count, created_at, started_at,
		 attempts, is_retryable, completed_candidates, active_attempt_id, active_candidate_index,
		 progress_phase, progress_updated_at)
		SELECT 'recovery-task-' || g, 'recovery-latency', 'running', 'fixture', '1024x1024', 1,
		 NOW() - INTERVAL '2 hours', NOW() - INTERVAL '2 hours', 1, TRUE, 0,
		 'recovery-attempt-' || g, 1, 'provider_running', NOW() - INTERVAL '2 hours'
		FROM generate_series(1,1000) g;
		CREATE TABLE recovery_latency_probe (
		 task_id text PRIMARY KEY, started_at timestamptz NOT NULL, finished_at timestamptz NOT NULL
		);
		CREATE FUNCTION delay_recovery_write() RETURNS trigger LANGUAGE plpgsql AS $$
		DECLARE started timestamptz;
		BEGIN
		 IF OLD.status = 'running' AND NEW.status = 'unknown' THEN
		  started := clock_timestamp();
		  PERFORM pg_sleep(0.1);
		  INSERT INTO recovery_latency_probe VALUES (NEW.id, started, clock_timestamp());
		 END IF;
		 RETURN NEW;
		END $$;
		CREATE TRIGGER recovery_latency_observer BEFORE UPDATE ON image_session_generation_tasks
		FOR EACH ROW EXECUTE FUNCTION delay_recovery_write();
		ANALYZE image_session_generation_tasks;
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func waitSlowRecoveryStarted(t *testing.T, ctx context.Context, pool *pgxpool.Pool, appName string) {
	t.Helper()
	for {
		var active bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM recovery_latency_probe)
		 AND EXISTS (SELECT 1 FROM pg_stat_activity
		 WHERE datname=current_database() AND application_name=$1 AND state='active'
		 AND wait_event='PgSleep' AND query LIKE '%image_session_generation_tasks%')`, appName).Scan(&active); err != nil {
			t.Fatal(err)
		}
		if active {
			return
		}
		if ctx.Err() != nil {
			t.Fatalf("dispatcher %s did not enter slow recovery: %v", appName, ctx.Err())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func assertRecoveryOverlapsDispatch(t *testing.T, ctx context.Context, pool *pgxpool.Pool, released time.Time, samples []dispatchLatencySample) {
	t.Helper()
	var count int
	for {
		if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM recovery_latency_probe").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count >= 25 {
			break
		}
		if ctx.Err() != nil {
			t.Fatalf("recovery made insufficient progress: %d, %v", count, ctx.Err())
		}
		time.Sleep(10 * time.Millisecond)
	}
	var start, finish time.Time
	var invalid, remaining, dispatches int
	if err := pool.QueryRow(ctx, `SELECT MIN(started_at), MAX(finished_at) FROM recovery_latency_probe`).Scan(&start, &finish); err != nil {
		t.Fatal(err)
	}
	if start.After(released) {
		t.Fatalf("normal load released %s before recovery began %s (delta=%s, recovered=%d)", released.Format(time.RFC3339Nano), start.Format(time.RFC3339Nano), start.Sub(released), count)
	}
	for _, sample := range samples {
		if sample.SentAt.After(finish) {
			t.Fatalf("dispatch %s was sent after the measured recovery window", sample.ID)
		}
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM recovery_latency_probe p
	 JOIN image_session_generation_tasks t ON t.id=p.task_id
	 WHERE t.status<>'unknown' OR t.is_retryable OR t.active_attempt_id IS NOT NULL
	 OR t.active_candidate_index IS NOT NULL`).Scan(&invalid); err != nil || invalid != 0 {
		t.Fatalf("invalid recovered rows=%d err=%v", invalid, err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM image_session_generation_tasks WHERE status='running'`).Scan(&remaining); err != nil || remaining == 0 {
		t.Fatalf("expected remaining recovery backlog, got %d err=%v", remaining, err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM async_dispatches WHERE actor_name=$1`, queue.ActorImageSession).Scan(&dispatches); err != nil || dispatches != 0 {
		t.Fatalf("unknown recovery must not enqueue: %d err=%v", dispatches, err)
	}
	t.Logf("RECOVERY_OVERLAP seeded=1000 observed_recovered=%d remaining_running=%d injected_delay_per_write=100ms recovery_span=%s all_normal_sent_inside_span=true", count, remaining, finish.Sub(start))
}
