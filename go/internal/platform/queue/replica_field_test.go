package queue_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

// TestReplicaFieldTwoDispatchersClaimOnce 模拟两个 dispatcher 同时 RunDispatcherOnce：
// SKIP LOCKED 下每条 PENDING 只入队一次。
func TestReplicaFieldTwoDispatchersClaimOnce(t *testing.T) {
	name := fmt.Sprintf("pf_replq_%d", time.Now().UnixNano()%1_000_000_000)
	pool, gdb := testdb.IsolatedMigrated(t, name)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	const n = 8
	ids := make([]string, 0, n)
	err := tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		for i := 0; i < n; i++ {
			agg := uniqueID(t)
			d, err := queue.Stage(ctx, pgxTx, "replica-dispatch:"+agg, queue.ActorGraphRun, agg, nil, nil)
			if err != nil {
				return err
			}
			ids = append(ids, d.ID)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	enqueued := map[string]int{}
	enqueue := func(id, aggregateID string) error {
		mu.Lock()
		enqueued[id]++
		mu.Unlock()
		return nil
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := queue.RunDispatcherOnce(ctx, pool, enqueue, n)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(enqueued) != n {
		t.Fatalf("enqueued %d unique dispatches, want %d (%v)", len(enqueued), n, enqueued)
	}
	for _, id := range ids {
		if enqueued[id] != 1 {
			t.Fatalf("dispatch %s enqueued %d times", id, enqueued[id])
		}
	}
}
