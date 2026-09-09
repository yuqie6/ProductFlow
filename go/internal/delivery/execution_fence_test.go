package delivery

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/queue"
)

func TestOldDeliveryExecutionCannotClaimExplicitRetry(t *testing.T) {
	ctx := context.Background()
	ds := newDeliveryServer(t)
	created := ds.createProduct(t)
	jobID := insertQueuedDeliveryJob(t, ds, created.Product.ID, created.CreatedAssets[0].ID, strings.Repeat("f", 64), time.Now().UTC())
	if changed, err := restageDeliveryJob(ctx, ds.db, jobID); err != nil || !changed {
		t.Fatalf("stage initial delivery job changed=%v err=%v", changed, err)
	}
	oldArgs, err := deliveryRiverArgs(ctx, ds.pool, jobID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := ds.pool.Exec(ctx, `
		UPDATE delivery_rendition_jobs
		SET status = 'failed', is_retryable = TRUE, failure_reason = 'test retry',
		    finished_at = NOW(), updated_at = NOW()
		WHERE id = $1
	`, jobID); err != nil {
		t.Fatal(err)
	}
	merchantCtx := auth.WithMerchantID(ctx, auth.MustDevMerchantID(t, ds.db))
	if _, err := (Service{DB: ds.db}).Retry(merchantCtx, jobID); err != nil {
		t.Fatal(err)
	}
	newArgs, err := deliveryRiverArgs(ctx, ds.pool, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if oldArgs.ExecutionID == newArgs.ExecutionID {
		t.Fatalf("explicit retry reused execution id %q", oldArgs.ExecutionID)
	}

	executor := Executor{DB: ds.db, Media: ds.media}
	if err := runDeliveryRiverWorkerArgs(ctx, oldArgs, executor); err != nil {
		t.Fatalf("superseded old execution should be consumed: %v", err)
	}
	var status string
	var attempts int
	if err := ds.pool.QueryRow(ctx, `
		SELECT status, attempts FROM delivery_rendition_jobs WHERE id = $1
	`, jobID).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "queued" || attempts != 0 {
		t.Fatalf("old execution changed current round: status=%s attempts=%d", status, attempts)
	}
	var assets int
	if err := ds.pool.QueryRow(ctx, `
		SELECT count(*) FROM product_image_assets WHERE parent_asset_id = $1
	`, created.CreatedAssets[0].ID).Scan(&assets); err != nil {
		t.Fatal(err)
	}
	if assets != 0 {
		t.Fatalf("old execution created %d result assets", assets)
	}

	if err := runDeliveryRiverWorkerArgs(ctx, newArgs, executor); err != nil {
		t.Fatalf("new execution failed: %v", err)
	}
	if err := ds.pool.QueryRow(ctx, `SELECT status FROM delivery_rendition_jobs WHERE id = $1`, jobID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "succeeded" {
		t.Fatalf("new execution status=%s", status)
	}
	if got := riverTaskCount(t, ds.pool, queue.ActorDelivery, jobID); got != 2 {
		t.Fatalf("execution rounds=%d want 2", got)
	}
}
