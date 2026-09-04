package localedit

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/product"
)

func TestRecoverUnfinishedLimitHasMoreAndIsolation(t *testing.T) {
	es := newEditServer(t, MockProvider{Cap: SupportedCapability("mock-local")})
	drainLocalEditRecovery(t, es)
	created := es.createProduct(t)

	older := createQueuedLocalEdit(t, es, created, "k-older")
	newer := createQueuedLocalEdit(t, es, created, "k-newer")
	stampUpdatedAt(t, es, older, time.Unix(1, 0).UTC())
	stampUpdatedAt(t, es, newer, time.Unix(2, 0).UTC())

	first, err := recoverUnfinished(context.Background(), es.pool, time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.QueuedTasks != 1 || first.EnqueuedTasks != 1 || !first.HasMore {
		t.Fatalf("first round %+v", first)
	}
	if pendingDispatchCount(t, es.pool, older) != 1 {
		t.Fatal("older task must commit before the rest of the batch")
	}
	if pendingDispatchCount(t, es.pool, newer) != 0 {
		t.Fatal("newer task must wait for the next round")
	}

	second, err := recoverUnfinished(context.Background(), es.pool, time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	if second.QueuedTasks != 1 || second.EnqueuedTasks != 1 || second.HasMore {
		t.Fatalf("second round %+v", second)
	}
	if pendingDispatchCount(t, es.pool, newer) != 1 {
		t.Fatal("newer task must restage on the second round")
	}
}

func TestRecoverUnfinishedRequeuesClaimedAndMarksUnknown(t *testing.T) {
	es := newEditServer(t, MockProvider{Cap: SupportedCapability("mock-local")})
	drainLocalEditRecovery(t, es)
	created := es.createProduct(t)

	claimedID := createQueuedLocalEdit(t, es, created, "k-claimed")
	unknownID := createQueuedLocalEdit(t, es, created, "k-unknown")
	markStaleLocalEdit(t, es, claimedID, "claimed")
	markStaleLocalEdit(t, es, unknownID, "provider_call")

	summary, err := RecoverUnfinished(context.Background(), es.pool, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if summary.StaleRunningTasks < 1 || summary.UnknownTasks < 1 {
		t.Fatalf("summary %+v", summary)
	}

	var claimedStatus string
	if err := es.pool.QueryRow(context.Background(), `
		SELECT status FROM local_image_edit_tasks WHERE id = $1
	`, claimedID).Scan(&claimedStatus); err != nil {
		t.Fatal(err)
	}
	if claimedStatus != "queued" {
		t.Fatalf("claimed status %s", claimedStatus)
	}
	if pendingDispatchCount(t, es.pool, claimedID) != 1 {
		t.Fatal("claimed task must restage")
	}

	var unknownStatus string
	var retryable bool
	if err := es.pool.QueryRow(context.Background(), `
		SELECT status, is_retryable FROM local_image_edit_tasks WHERE id = $1
	`, unknownID).Scan(&unknownStatus, &retryable); err != nil {
		t.Fatal(err)
	}
	if unknownStatus != "unknown" || retryable {
		t.Fatalf("unknown status=%s retryable=%v", unknownStatus, retryable)
	}
	if pendingDispatchCount(t, es.pool, unknownID) != 0 {
		t.Fatal("unknown task must not restage")
	}
}

func drainLocalEditRecovery(t *testing.T, es *editServer) {
	t.Helper()
	for i := 0; i < 20; i++ {
		summary, err := recoverUnfinished(context.Background(), es.pool, time.Minute, 25)
		if err != nil {
			t.Fatal(err)
		}
		if !summary.HasMore {
			return
		}
	}
	t.Fatal("local edit recovery drain did not empty")
}

func createQueuedLocalEdit(t *testing.T, es *editServer, created product.CreateResponse, key string) string {
	t.Helper()
	form := createForm(t, created.CreatedAssets[0].ID, false)
	draft := es.do(t, http.MethodPost, "/api/v3/products/"+created.Product.ID+"/image-edits", form.body, form.contentType)
	es.mustStatus(t, draft, http.StatusCreated)
	var task TaskResponse
	es.decode(t, draft, &task)
	submitted := es.doJSON(t, http.MethodPost, "/api/v3/products/"+created.Product.ID+"/image-edits/"+task.ID+"/submit", map[string]any{
		"idempotency_key": key,
	})
	es.mustStatus(t, submitted, http.StatusAccepted)
	es.dropDispatch(t, task.ID)
	return task.ID
}

func stampUpdatedAt(t *testing.T, es *editServer, taskID string, updatedAt time.Time) {
	t.Helper()
	if _, err := es.pool.Exec(context.Background(), `
		UPDATE local_image_edit_tasks SET updated_at = $2 WHERE id = $1
	`, taskID, updatedAt); err != nil {
		t.Fatal(err)
	}
}

func markStaleLocalEdit(t *testing.T, es *editServer, taskID, phase string) {
	t.Helper()
	attempt := clockid.New()
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := es.pool.Exec(context.Background(), `
		INSERT INTO local_image_edit_provider_attempts (
			id, task_id, attempt_id, attempt_number, operation_key, request_hash,
			phase, effect_result, provider_name, created_at, updated_at
		) VALUES ($1, $2, $3, 1, $4, $5, $6, 'pending', 'mock', NOW(), NOW())
	`, clockid.New(), taskID, attempt, "local-edit:"+taskID, hash, phase); err != nil {
		t.Fatal(err)
	}
	if _, err := es.pool.Exec(context.Background(), `
		UPDATE local_image_edit_tasks SET
			status = 'running',
			active_attempt_id = $2,
			progress_phase = $3,
			started_at = NOW() - INTERVAL '2 hours',
			finished_at = NULL,
			updated_at = NOW() - INTERVAL '2 hours',
			is_retryable = TRUE
		WHERE id = $1
	`, taskID, attempt, phase); err != nil {
		t.Fatal(err)
	}
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
