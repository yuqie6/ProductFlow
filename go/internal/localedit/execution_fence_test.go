package localedit

import (
	"context"
	"testing"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/queue"
)

func TestOldLocalEditExecutionCannotClaimExplicitRetry(t *testing.T) {
	ctx := context.Background()
	provider := &countingResultProvider{MockProvider: MockProvider{Cap: SupportedCapability("mock-local")}}
	es := newEditServer(t, provider)
	created := es.createProduct(t)
	taskID := createQueuedLocalEdit(t, es, created, "old-execution-fence")
	if changed, err := restageLocalEditTask(ctx, es.db, taskID); err != nil || !changed {
		t.Fatalf("stage initial local edit changed=%v err=%v", changed, err)
	}
	oldArgs, err := localEditRiverArgs(ctx, es.pool, taskID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := es.pool.Exec(ctx, `
		UPDATE local_image_edit_tasks
		SET status = 'failed', is_retryable = TRUE, failure_reason = 'test retry',
		    progress_phase = NULL, finished_at = NOW(), updated_at = NOW()
		WHERE id = $1
	`, taskID); err != nil {
		t.Fatal(err)
	}
	merchantCtx := auth.WithMerchantID(ctx, auth.MustDevMerchantID(t, es.db))
	if _, err := (Service{DB: es.db, Media: es.media, Provider: provider}).Retry(merchantCtx, created.Product.ID, taskID, nil); err != nil {
		t.Fatal(err)
	}
	newArgs, err := localEditRiverArgs(ctx, es.pool, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if oldArgs.ExecutionID == newArgs.ExecutionID {
		t.Fatalf("explicit retry reused execution id %q", oldArgs.ExecutionID)
	}

	executor := Executor{DB: es.db, Media: es.media, Provider: provider}
	if err := runLocalEditRiverWorkerArgs(ctx, oldArgs, executor); err != nil {
		t.Fatalf("superseded old execution should be consumed: %v", err)
	}
	var status string
	var attempts int
	if err := es.pool.QueryRow(ctx, `
		SELECT status, attempts FROM local_image_edit_tasks WHERE id = $1
	`, taskID).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "queued" || attempts != 0 || provider.calls != 0 {
		t.Fatalf("old execution changed current round: status=%s attempts=%d provider_calls=%d", status, attempts, provider.calls)
	}

	if err := runLocalEditRiverWorkerArgs(ctx, newArgs, executor); err != nil {
		t.Fatalf("new execution failed: %v", err)
	}
	if err := es.pool.QueryRow(ctx, `SELECT status FROM local_image_edit_tasks WHERE id = $1`, taskID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "succeeded" || provider.calls != 1 {
		t.Fatalf("new execution status=%s provider_calls=%d", status, provider.calls)
	}
	if got := riverTaskCount(t, es.pool, queue.ActorLocalEdit, taskID); got != 2 {
		t.Fatalf("execution rounds=%d want 2", got)
	}
}
