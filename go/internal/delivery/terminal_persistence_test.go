package delivery

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/queue"
)

func TestFailurePersistenceDoesNotConsumeDelivery(t *testing.T) {
	ctx := context.Background()
	ds := newDeliveryServer(t)
	created := ds.createProduct(t)
	jobID := insertQueuedDeliveryJob(t, ds, created.Product.ID, created.CreatedAssets[0].ID, strings.Repeat("e", 64), time.Now().UTC())
	if err := os.Remove(resolveDeliveryMedia(t, ds, created.CreatedAssets[0].MediaObjectID)); err != nil {
		t.Fatal(err)
	}
	const constraint = "test_delivery_failure_write"
	if _, err := ds.pool.Exec(ctx, "ALTER TABLE delivery_rendition_jobs ADD CONSTRAINT "+constraint+" CHECK (id <> '"+jobID+"' OR status <> 'failed')"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := ds.pool.Exec(context.Background(), "ALTER TABLE delivery_rendition_jobs DROP CONSTRAINT "+constraint); err != nil {
			t.Error(err)
		}
	})
	if _, err := restageDeliveryJob(ctx, ds.db, jobID); err != nil {
		t.Fatal(err)
	}
	var dispatchID string
	if err := ds.pool.QueryRow(ctx, "UPDATE async_dispatches SET status='sent',attempts=1 WHERE aggregate_id=$1 RETURNING id", jobID).Scan(&dispatchID); err != nil {
		t.Fatal(err)
	}
	executor := Executor{DB: ds.db, Media: ds.media}
	err := queue.Consume(ctx, ds.pool, dispatchID, jobID, map[string]queue.ActorFunc{queue.ActorDelivery: executor.Execute})
	if err == nil || !strings.Contains(err.Error(), constraint) {
		t.Fatalf("want write error, got %v", err)
	}
	var status, dispatchStatus string
	if err := ds.pool.QueryRow(ctx, "SELECT j.status,d.status FROM delivery_rendition_jobs j JOIN async_dispatches d ON d.aggregate_id=j.id WHERE j.id=$1", jobID).Scan(&status, &dispatchStatus); err != nil {
		t.Fatal(err)
	}
	if status != "running" || dispatchStatus != "pending" {
		t.Fatalf("job=%s dispatch=%s", status, dispatchStatus)
	}
}
