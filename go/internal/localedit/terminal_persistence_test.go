package localedit

import (
	"context"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/queue"
)

// Real PostgreSQL constraints fail writes after the worker has claimed the task.
// This exercises transaction rollback and the queue consumer, not a mocked DB.
func TestTerminalPersistenceFailureDoesNotConsumeDispatch(t *testing.T) {
	for _, table := range []string{"local_image_edit_tasks", "local_image_edit_provider_attempts"} {
		t.Run(table, func(t *testing.T) {
			ctx := context.Background()
			es := newEditServer(t, MockProvider{Cap: SupportedCapability("submitted-provider")})
			taskID := createQueuedLocalEdit(t, es, es.createProduct(t), "terminal-write-failure")
			column, key := "status", "id"
			if table == "local_image_edit_provider_attempts" {
				column, key = "phase", "task_id"
			}
			constraint := "test_terminal_write_failure"
			if _, err := es.pool.Exec(ctx, "ALTER TABLE "+table+" ADD CONSTRAINT "+constraint+" CHECK ("+key+" <> '"+taskID+"' OR "+column+" <> 'failed')"); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := es.pool.Exec(context.Background(), "ALTER TABLE "+table+" DROP CONSTRAINT "+constraint); err != nil {
					t.Error(err)
				}
			})
			if _, err := restageLocalEditTask(ctx, es.db, taskID); err != nil {
				t.Fatal(err)
			}
			var dispatchID string
			if err := es.pool.QueryRow(ctx, "UPDATE async_dispatches SET status='sent', attempts=1 WHERE aggregate_id=$1 RETURNING id", taskID).Scan(&dispatchID); err != nil {
				t.Fatal(err)
			}
			executor := Executor{DB: es.db, Media: es.media, Provider: MockProvider{Cap: SupportedCapability("different-provider")}}
			err := queue.Consume(ctx, es.pool, dispatchID, taskID, map[string]queue.ActorFunc{queue.ActorLocalEdit: executor.Execute})
			if err == nil || !strings.Contains(err.Error(), constraint) {
				t.Fatalf("want PostgreSQL write error, got %v", err)
			}
			var status, phase, dispatchStatus string
			if err := es.pool.QueryRow(ctx, "SELECT t.status, a.phase, d.status FROM local_image_edit_tasks t JOIN local_image_edit_provider_attempts a ON a.task_id=t.id JOIN async_dispatches d ON d.aggregate_id=t.id WHERE t.id=$1", taskID).Scan(&status, &phase, &dispatchStatus); err != nil {
				t.Fatal(err)
			}
			if status != "running" || phase != "claimed" || dispatchStatus != "pending" {
				t.Fatalf("partial commit or consumed failure: task=%s attempt=%s dispatch=%s", status, phase, dispatchStatus)
			}
		})
	}
}

func TestClaimCommitsStaleProviderUnknown(t *testing.T) {
	es := newEditServer(t, MockProvider{Cap: SupportedCapability("mock-local")})
	taskID := createQueuedLocalEdit(t, es, es.createProduct(t), "stale-provider-claim")
	markStaleLocalEdit(t, es, taskID, "provider_call")
	executor := Executor{DB: es.db, Media: es.media}
	for i := 0; i < 2; i++ {
		if err := executor.Execute(context.Background(), taskID); err != nil {
			t.Fatal(err)
		}
	}
	var status, effect string
	var retryable bool
	if err := es.pool.QueryRow(context.Background(), "SELECT t.status, t.is_retryable, a.effect_result FROM local_image_edit_tasks t JOIN local_image_edit_provider_attempts a ON a.task_id=t.id WHERE t.id=$1", taskID).Scan(&status, &retryable, &effect); err != nil {
		t.Fatal(err)
	}
	if status != "unknown" || effect != "unknown" || retryable {
		t.Fatalf("task=%s effect=%s retryable=%v", status, effect, retryable)
	}
}

func TestFinishStaleAttemptDoesNotOverwrite(t *testing.T) {
	es := newEditServer(t, MockProvider{Cap: SupportedCapability("mock-local")})
	taskID := createQueuedLocalEdit(t, es, es.createProduct(t), "stale-finish")
	executor := Executor{DB: es.db}
	_, attemptID, err := executor.claim(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.finish(context.Background(), taskID, "old-attempt", "failed", "failed", "failed", "late result", false, "", ""); err != nil {
		t.Fatal(err)
	}
	var active, status string
	if err := es.pool.QueryRow(context.Background(), "SELECT active_attempt_id, status FROM local_image_edit_tasks WHERE id=$1", taskID).Scan(&active, &status); err != nil {
		t.Fatal(err)
	}
	if active != attemptID || status != "running" {
		t.Fatalf("active=%s status=%s", active, status)
	}
}
