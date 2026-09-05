package delivery

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/platform/clockid"
)

func TestRecoverUnfinishedLimitHasMoreAndIsolation(t *testing.T) {
	ds := newDeliveryServer(t)
	drainDeliveryRecovery(t, ds)
	created := ds.createProduct(t)
	assetID := created.CreatedAssets[0].ID

	older := insertQueuedDeliveryJob(t, ds, created.Product.ID, assetID, strings.Repeat("a", 64), time.Unix(1, 0).UTC())
	newer := insertQueuedDeliveryJob(t, ds, created.Product.ID, assetID, strings.Repeat("b", 64), time.Unix(2, 0).UTC())

	first, err := recoverUnfinished(context.Background(), ds.pool, time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.QueuedJobs != 1 || first.EnqueuedJobs != 1 || !first.HasMore {
		t.Fatalf("first round %+v", first)
	}
	if pendingDispatchCount(t, ds.pool, older) != 1 {
		t.Fatal("older job must commit before the rest of the batch")
	}
	if pendingDispatchCount(t, ds.pool, newer) != 0 {
		t.Fatal("newer job must wait for the next round")
	}

	second, err := recoverUnfinished(context.Background(), ds.pool, time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	if second.QueuedJobs != 1 || second.EnqueuedJobs != 1 || second.HasMore {
		t.Fatalf("second round %+v", second)
	}
	if pendingDispatchCount(t, ds.pool, newer) != 1 {
		t.Fatal("newer job must restage on the second round")
	}
}

func TestRecoverUnfinishedRequeuesStaleRunning(t *testing.T) {
	ds := newDeliveryServer(t)
	drainDeliveryRecovery(t, ds)
	created := ds.createProduct(t)
	assetID := created.CreatedAssets[0].ID
	jobID := insertQueuedDeliveryJob(t, ds, created.Product.ID, assetID, strings.Repeat("c", 64), time.Now().UTC())
	attempt := clockid.New()
	if _, err := ds.pool.Exec(context.Background(), `
		UPDATE delivery_rendition_jobs SET
			status = 'running',
			active_attempt_id = $2,
			started_at = NOW() - INTERVAL '2 hours',
			finished_at = NULL,
			updated_at = NOW() - INTERVAL '2 hours'
		WHERE id = $1
	`, jobID, attempt); err != nil {
		t.Fatal(err)
	}

	summary, err := RecoverUnfinished(context.Background(), ds.pool, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if summary.StaleRunningJobs < 1 || summary.EnqueuedJobs < 1 {
		t.Fatalf("summary %+v", summary)
	}
	var status string
	if err := ds.pool.QueryRow(context.Background(), `
		SELECT status FROM delivery_rendition_jobs WHERE id = $1
	`, jobID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "queued" {
		t.Fatalf("status %s", status)
	}
	if pendingDispatchCount(t, ds.pool, jobID) != 1 {
		t.Fatal("stale running job must restage")
	}
}

func drainDeliveryRecovery(t *testing.T, ds *deliveryServer) {
	t.Helper()
	for i := 0; i < 20; i++ {
		summary, err := recoverUnfinished(context.Background(), ds.pool, time.Minute, 25)
		if err != nil {
			t.Fatal(err)
		}
		if !summary.HasMore {
			return
		}
	}
	t.Fatal("delivery recovery drain did not empty")
}

func insertQueuedDeliveryJob(t *testing.T, ds *deliveryServer, productID, assetID, specHash string, updatedAt time.Time) string {
	t.Helper()
	jobID := clockid.New()
	specJSON := `{"width":64,"height":64,"format":"png","fit":"contain"}`
	if _, err := ds.pool.Exec(context.Background(), `
		INSERT INTO delivery_rendition_jobs (
			id, product_id, source_asset_id, spec_schema_version, spec_json, spec_hash,
			status, attempts, is_retryable, created_at, updated_at
		) VALUES ($1, $2, $3, 1, $4::json, $5, 'queued', 0, TRUE, $6, $6)
	`, jobID, productID, assetID, specJSON, specHash, updatedAt); err != nil {
		t.Fatal(err)
	}
	return jobID
}

func pendingDispatchCount(t *testing.T, pool *pgxpool.Pool, aggregateID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM async_dispatches WHERE aggregate_id = $1 AND status = 'pending'
	`, aggregateID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRecoverUnfinishedSkipsLockedCandidatePrefix(t *testing.T) {
	ds := newDeliveryServer(t)
	drainDeliveryRecovery(t, ds)
	created := ds.createProduct(t)
	var ids []string
	for i := 0; i <= recoveryBatchLimit; i++ {
		ids = append(ids, insertQueuedDeliveryJob(t, ds, created.Product.ID, created.CreatedAssets[0].ID,
			fmt.Sprintf("%064x", i), time.Unix(int64(i+1), 0).UTC()))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	lock, err := ds.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(context.Background())
	if _, err := lock.Exec(ctx, `SELECT id FROM delivery_rendition_jobs WHERE id=ANY($1) FOR UPDATE`, ids[:recoveryBatchLimit]); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		summary, err := RecoverUnfinished(ctx, ds.pool, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		if i == 0 {
			want = 1
		}
		if summary.EnqueuedJobs != want {
			t.Fatalf("cycle %d enqueued %d jobs, want %d", i, summary.EnqueuedJobs, want)
		}
	}
	if got := pendingDispatchCount(t, ds.pool, ids[recoveryBatchLimit]); got != 1 {
		t.Fatalf("unlocked job after %d locked candidates has %d dispatches after 3 cycles, want 1", recoveryBatchLimit, got)
	}
	for _, id := range ids[:recoveryBatchLimit] {
		if pendingDispatchCount(t, ds.pool, id) != 0 {
			t.Fatal("locked job was restaged")
		}
	}
	if err := lock.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if summary, err := RecoverUnfinished(ctx, ds.pool, time.Minute); err != nil || summary.EnqueuedJobs != recoveryBatchLimit {
		t.Fatalf("released jobs not recovered: %+v err=%v", summary, err)
	}
}
