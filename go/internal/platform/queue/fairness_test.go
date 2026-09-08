package queue_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

func TestGenerationFairnessMaxOneServesNewMerchantAfterCompletion(t *testing.T) {
	pool, db := fairnessDB(t)
	setGenerationMax(t, db, 1)
	ctx := context.Background()

	oldA := stageFairDispatch(t, db, queue.ActorImageSession, "merchant-a", time.Now().UTC().Add(-2*time.Second))
	backlogA := stageFairDispatch(t, db, queue.ActorImageSession, "merchant-a", time.Now().UTC().Add(-time.Second))
	if _, err := queue.RunDispatcherOnce(ctx, pool, nil, 1); err != nil {
		t.Fatal(err)
	}
	consumeFairDispatch(t, pool, oldA, queue.ActorImageSession, nil)

	newB := stageFairDispatch(t, db, queue.ActorImageSession, "merchant-b", time.Now().UTC())
	if _, err := queue.RunDispatcherOnce(ctx, pool, nil, 1); err != nil {
		t.Fatal(err)
	}
	assertDispatchStatus(t, db, newB.ID, queue.StatusSent)
	assertDispatchStatus(t, db, backlogA.ID, queue.StatusPending)
}

func TestGenerationFairnessUnservedMerchantsArriveInOrder(t *testing.T) {
	pool, db := fairnessDB(t)
	setGenerationMax(t, db, 1)
	ctx := context.Background()
	base := time.Now().UTC().Add(-3 * time.Second)
	dispatches := []queue.Dispatch{
		stageFairDispatch(t, db, queue.ActorImageSession, "merchant-a", base),
		stageFairDispatch(t, db, queue.ActorImageSession, "merchant-b", base.Add(time.Millisecond)),
		stageFairDispatch(t, db, queue.ActorImageSession, "merchant-c", base.Add(2*time.Millisecond)),
	}

	var got []string
	for _, want := range dispatches {
		var sentID string
		_, err := queue.RunDispatcherOnce(ctx, pool, func(id, _ string) error {
			sentID = id
			return nil
		}, 1)
		if err != nil {
			t.Fatal(err)
		}
		if sentID != want.ID {
			t.Fatalf("sent %s, want %s", sentID, want.ID)
		}
		got = append(got, sentID)
		consumeFairDispatch(t, pool, want, queue.ActorImageSession, nil)
	}
	if len(got) != 3 {
		t.Fatalf("unexpected dispatch order %v", got)
	}
}

func TestGenerationFairnessRetryDoesNotEraseServiceHistory(t *testing.T) {
	pool, db := fairnessDB(t)
	setGenerationMax(t, db, 1)
	ctx := context.Background()

	retryA := stageFairDispatch(t, db, queue.ActorImageSession, "merchant-a", time.Now().UTC().Add(-2*time.Second))
	if _, err := queue.RunDispatcherOnce(ctx, pool, nil, 1); err != nil {
		t.Fatal(err)
	}
	consumeFairDispatch(t, pool, retryA, queue.ActorImageSession, queue.ErrLater)
	var retryRow schema.AsyncDispatches
	if err := db.WithContext(ctx).Select("sent_at", "attempts").Where("id = ?", retryA.ID).Take(&retryRow).Error; err != nil {
		t.Fatal(err)
	}
	if retryRow.SentAt != nil || retryRow.Attempts != 1 {
		t.Fatalf("retry state sent_at=%v attempts=%d", retryRow.SentAt, retryRow.Attempts)
	}

	newB := stageFairDispatch(t, db, queue.ActorImageSession, "merchant-b", time.Now().UTC())
	var sentID string
	if _, err := queue.RunDispatcherOnce(ctx, pool, func(id, _ string) error {
		sentID = id
		return nil
	}, 1); err != nil {
		t.Fatal(err)
	}
	if sentID != newB.ID {
		t.Fatalf("sent retry %s before new merchant %s", sentID, newB.ID)
	}
}

func TestGenerationFairnessSingleMerchantUsesAllSlots(t *testing.T) {
	pool, db := fairnessDB(t)
	setGenerationMax(t, db, 2)
	ctx := context.Background()
	rows := []queue.Dispatch{
		stageFairDispatch(t, db, queue.ActorImageSession, "merchant-a", time.Now().UTC()),
		stageFairDispatch(t, db, queue.ActorImageSession, "merchant-a", time.Now().UTC()),
		stageFairDispatch(t, db, queue.ActorImageSession, "merchant-a", time.Now().UTC()),
	}
	var mu sync.Mutex
	var sent []string
	if _, err := queue.RunDispatcherOnce(ctx, pool, func(id, _ string) error {
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, id)
		return nil
	}, 10); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 {
		t.Fatalf("single merchant sent %d, want 2 (%v)", len(sent), sent)
	}
	for _, row := range rows[:2] {
		consumeFairDispatch(t, pool, row, queue.ActorImageSession, nil)
	}
	var thirdSent string
	if _, err := queue.RunDispatcherOnce(ctx, pool, func(id, _ string) error {
		thirdSent = id
		return nil
	}, 10); err != nil {
		t.Fatal(err)
	}
	if thirdSent != rows[2].ID {
		t.Fatalf("third dispatch %s, want %s", thirdSent, rows[2].ID)
	}
}

func TestGenerationFairnessMixedActorsKeepNonGenerationDispatchable(t *testing.T) {
	pool, db := fairnessDB(t)
	setGenerationMax(t, db, 1)
	ctx := context.Background()
	firstGeneration := stageFairDispatch(t, db, queue.ActorImageSession, "merchant-a", time.Now().UTC())
	secondGeneration := stageFairDispatch(t, db, queue.ActorImageSession, "merchant-b", time.Now().UTC())
	delivery := stageFairDispatch(t, db, queue.ActorDelivery, "merchant-a", time.Now().UTC())
	var mu sync.Mutex
	var sent []string
	if _, err := queue.RunDispatcherOnce(ctx, pool, func(id, _ string) error {
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, id)
		return nil
	}, 10); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 || !containsID(sent, firstGeneration.ID) || !containsID(sent, delivery.ID) {
		t.Fatalf("mixed dispatch sent=%v, want generation=%s and delivery=%s", sent, firstGeneration.ID, delivery.ID)
	}
	assertDispatchStatus(t, db, secondGeneration.ID, queue.StatusPending)
}

func TestGenerationFairnessLimitOneComparesWaitTimesAcrossRounds(t *testing.T) {
	pool, db := fairnessDB(t)
	setGenerationMax(t, db, 1)
	ctx := context.Background()
	base := time.Now().UTC().Add(-10 * time.Second)
	firstGeneration := stageFairDispatch(t, db, queue.ActorImageSession, "merchant-a", base)
	backlogGeneration := stageFairDispatch(t, db, queue.ActorImageSession, "merchant-a", base.Add(time.Millisecond))
	firstDelivery := stageFairDispatch(t, db, queue.ActorDelivery, "merchant-b", base.Add(time.Second))

	run := func(want queue.Dispatch) {
		t.Helper()
		var sentID string
		if _, err := queue.RunDispatcherOnce(ctx, pool, func(id, _ string) error {
			sentID = id
			return nil
		}, 1); err != nil {
			t.Fatal(err)
		}
		if sentID != want.ID {
			t.Fatalf("limit=1 sent %s, want %s", sentID, want.ID)
		}
	}

	run(firstGeneration)
	consumeFairDispatch(t, pool, firstGeneration, queue.ActorImageSession, nil)
	secondDelivery := stageFairDispatch(t, db, queue.ActorDelivery, "merchant-c", time.Now().UTC())

	// The first delivery has waited longer than generation's new service_at.
	run(firstDelivery)
	consumeFairDispatch(t, pool, firstDelivery, queue.ActorDelivery, nil)

	// The newly arrived delivery is newer than generation's persisted service time,
	// so the next generation head gets the slot instead of ordinary work winning forever.
	run(backlogGeneration)
	consumeFairDispatch(t, pool, backlogGeneration, queue.ActorImageSession, nil)
	run(secondDelivery)
}

func TestGenerationFairnessLimitOneDoesNotLetOrdinaryHeadStarveGeneration(t *testing.T) {
	pool, db := fairnessDB(t)
	setGenerationMax(t, db, 1)
	ctx := context.Background()
	base := time.Now().UTC().Add(-10 * time.Second)
	firstDelivery := stageFairDispatch(t, db, queue.ActorDelivery, "merchant-b", base)
	firstGeneration := stageFairDispatch(t, db, queue.ActorImageSession, "merchant-a", base.Add(time.Second))

	run := func(want queue.Dispatch) {
		t.Helper()
		var sentID string
		if _, err := queue.RunDispatcherOnce(ctx, pool, func(id, _ string) error {
			sentID = id
			return nil
		}, 1); err != nil {
			t.Fatal(err)
		}
		if sentID != want.ID {
			t.Fatalf("limit=1 sent %s, want %s", sentID, want.ID)
		}
	}

	// An older ordinary head wins the first comparison.
	run(firstDelivery)
	consumeFairDispatch(t, pool, firstDelivery, queue.ActorDelivery, nil)
	secondDelivery := stageFairDispatch(t, db, queue.ActorDelivery, "merchant-c", time.Now().UTC())

	// The new ordinary head is newer than generation's wait time, so generation wins.
	run(firstGeneration)
	consumeFairDispatch(t, pool, firstGeneration, queue.ActorImageSession, nil)
	secondGeneration := stageFairDispatch(t, db, queue.ActorImageSession, "merchant-a", time.Now().UTC())

	// The second ordinary head has now waited longer than generation's new service_at.
	run(secondDelivery)
	assertDispatchStatus(t, db, secondGeneration.ID, queue.StatusPending)
}

func TestGenerationFairnessBatchLimitReservesOlderOrdinarySlot(t *testing.T) {
	pool, db := fairnessDB(t)
	setGenerationMax(t, db, 3)
	ctx := context.Background()
	base := time.Now().UTC().Add(-10 * time.Second)
	firstGeneration := stageFairDispatch(t, db, queue.ActorImageSession, "merchant-a", base)
	secondGeneration := stageFairDispatch(t, db, queue.ActorImageSession, "merchant-b", base.Add(time.Millisecond))
	olderDelivery := stageFairDispatch(t, db, queue.ActorDelivery, "merchant-c", base.Add(-time.Second))

	var mu sync.Mutex
	var sent []string
	if _, err := queue.RunDispatcherOnce(ctx, pool, func(id, _ string) error {
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, id)
		return nil
	}, 2); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 || !containsID(sent, olderDelivery.ID) {
		t.Fatalf("batch limit starved older ordinary dispatch: sent=%v", sent)
	}
	if containsID(sent, firstGeneration.ID) == containsID(sent, secondGeneration.ID) {
		t.Fatalf("want exactly one generation dispatch in batch: sent=%v", sent)
	}
	if containsID(sent, secondGeneration.ID) {
		assertDispatchStatus(t, db, firstGeneration.ID, queue.StatusPending)
	} else {
		assertDispatchStatus(t, db, secondGeneration.ID, queue.StatusPending)
	}
}

func TestGenerationFairnessSharesGraphAndImageBudget(t *testing.T) {
	pool, db := fairnessDB(t)
	setGenerationMax(t, db, 1)
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Second)
	graph := stageFairDispatch(t, db, queue.ActorGraphRun, "merchant-a", base)
	image := stageFairDispatch(t, db, queue.ActorImageSession, "merchant-b", base.Add(time.Millisecond))

	var sent []string
	if _, err := queue.RunDispatcherOnce(ctx, pool, func(id, _ string) error {
		sent = append(sent, id)
		return nil
	}, 10); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || sent[0] != graph.ID {
		t.Fatalf("shared budget sent=%v, want graph=%s only", sent, graph.ID)
	}
	assertDispatchStatus(t, db, image.ID, queue.StatusPending)
	consumeFairDispatch(t, pool, graph, queue.ActorGraphRun, nil)

	sent = nil
	if _, err := queue.RunDispatcherOnce(ctx, pool, func(id, _ string) error {
		sent = append(sent, id)
		return nil
	}, 10); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || sent[0] != image.ID {
		t.Fatalf("shared budget second cycle sent=%v, want image=%s", sent, image.ID)
	}
}

func TestGenerationFairnessCountsValidPendingLease(t *testing.T) {
	pool, db := fairnessDB(t)
	setGenerationMax(t, db, 1)
	ctx := context.Background()
	reserved := stageFairDispatch(t, db, queue.ActorImageSession, "merchant-a", time.Now().UTC())
	leaseToken := uniqueID(t)
	leaseUntil := time.Now().UTC().Add(time.Hour)
	if err := db.WithContext(ctx).Model(&schema.AsyncDispatches{}).Where("id = ?", reserved.ID).Updates(map[string]any{
		"lease_token":      leaseToken,
		"lease_expires_at": leaseUntil,
		"attempts":         1,
		"updated_at":       time.Now().UTC(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	candidate := stageFairDispatch(t, db, queue.ActorImageSession, "merchant-b", time.Now().UTC())
	var sent []string
	summary, err := queue.RunDispatcherOnce(ctx, pool, func(id, _ string) error {
		sent = append(sent, id)
		return nil
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Pending != 0 || len(sent) != 0 {
		t.Fatalf("valid pending lease was ignored: summary=%+v sent=%v", summary, sent)
	}
	assertDispatchStatus(t, db, candidate.ID, queue.StatusPending)
}

func TestGenerationFairnessConcurrentDispatchersRespectBudget(t *testing.T) {
	pool, db := fairnessDB(t)
	setGenerationMax(t, db, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rows := []queue.Dispatch{
		stageFairDispatch(t, db, queue.ActorImageSession, "merchant-a", time.Now().UTC()),
		stageFairDispatch(t, db, queue.ActorImageSession, "merchant-a", time.Now().UTC()),
		stageFairDispatch(t, db, queue.ActorImageSession, "merchant-b", time.Now().UTC()),
		stageFairDispatch(t, db, queue.ActorImageSession, "merchant-b", time.Now().UTC()),
	}
	var mu sync.Mutex
	sent := map[string]int{}
	enqueue := func(id, _ string) error {
		mu.Lock()
		defer mu.Unlock()
		sent[id]++
		return nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := queue.RunDispatcherOnce(ctx, pool, enqueue, 2); err != nil {
				t.Errorf("dispatcher: %v", err)
			}
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 2 {
		t.Fatalf("unique sent=%d, want 2 (%v)", len(sent), sent)
	}
	for id, count := range sent {
		if count != 1 {
			t.Fatalf("dispatch %s enqueued %d times", id, count)
		}
	}
	for _, row := range rows {
		var status string
		if err := db.WithContext(ctx).Model(&schema.AsyncDispatches{}).Where("id = ?", row.ID).Pluck("status", &status).Error; err != nil {
			t.Fatal(err)
		}
		if status != queue.StatusSent && status != queue.StatusPending {
			t.Fatalf("dispatch %s status %s", row.ID, status)
		}
	}
}

func fairnessDB(t *testing.T) (*pgxpool.Pool, *gorm.DB) {
	t.Helper()
	name := fmt.Sprintf("pf_fair_0909_%s", uniqueID(t)[:12])
	pool, db := testdb.IsolatedMigrated(t, name)
	return pool, db
}

func setGenerationMax(t *testing.T, db *gorm.DB, max int) {
	t.Helper()
	now := time.Now().UTC()
	var previous schema.AppSettings
	hadPrevious := db.Where("key = ?", "generation_max_concurrent_tasks").Take(&previous).Error == nil
	if err := db.Where("key = ?", "generation_max_concurrent_tasks").Delete(&schema.AppSettings{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&schema.AppSettings{Key: "generation_max_concurrent_tasks", Value: fmt.Sprint(max), CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Where("key = ?", "generation_max_concurrent_tasks").Delete(&schema.AppSettings{}).Error
		if hadPrevious {
			_ = db.Create(&previous).Error
		}
	})
}

func stageFairDispatch(t *testing.T, db *gorm.DB, actor, merchant string, createdAt time.Time) queue.Dispatch {
	t.Helper()
	ctx := context.Background()
	aggregateID := uniqueID(t)
	var dispatch queue.Dispatch
	if err := tx.WithGorm(ctx, db, func(dbTx *gorm.DB) error {
		var err error
		dispatch, err = queue.Stage(ctx, dbTx, fmt.Sprintf("fair:%s:%s:%s", merchant, actor, aggregateID), actor, aggregateID, nil, nil)
		if err != nil {
			return err
		}
		return dbTx.Model(&schema.AsyncDispatches{}).Where("id = ?", dispatch.ID).Updates(map[string]any{
			"merchant_id":  merchant,
			"created_at":   createdAt,
			"available_at": createdAt,
			"updated_at":   createdAt,
		}).Error
	}); err != nil {
		t.Fatal(err)
	}
	return dispatch
}

func consumeFairDispatch(t *testing.T, pool *pgxpool.Pool, dispatch queue.Dispatch, actor string, actorErr error) {
	t.Helper()
	if err := queue.Consume(context.Background(), pool, dispatch.ID, dispatch.AggregateID, map[string]queue.ActorFunc{
		actor: func(context.Context, string) error { return actorErr },
	}); err != nil {
		t.Fatalf("consume %s: %v", dispatch.ID, err)
	}
}

func assertDispatchStatus(t *testing.T, db *gorm.DB, id, want string) {
	t.Helper()
	var got string
	if err := db.Model(&schema.AsyncDispatches{}).Where("id = ?", id).Pluck("status", &got).Error; err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("dispatch %s status=%s want=%s", id, got, want)
	}
}

func containsID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
