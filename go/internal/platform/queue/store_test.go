package queue_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

func uniqueID(t *testing.T) string {
	t.Helper()
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(buf[:])
}

func TestStageAsyncDispatchIsIdempotentByDeliveryKey(t *testing.T) {
	_, gdb := testdb.Open(t)
	ctx := context.Background()
	key := "workflow_run:" + uniqueID(t)
	agg := uniqueID(t)
	var first, second queue.Dispatch
	err := tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		var err error
		first, err = queue.Stage(ctx, pgxTx, key, queue.ActorGraphRun, agg, map[string]any{"scope": "workflow"}, nil)
		if err != nil {
			return err
		}
		second, err = queue.Stage(ctx, pgxTx, key, queue.ActorGraphRun, agg, nil, nil)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("id %s vs %s", first.ID, second.ID)
	}
	if first.Status != queue.StatusPending {
		t.Fatalf("status %s", first.Status)
	}
}

func TestDispatcherMarksSentBeforeEnqueue(t *testing.T) {
	pool, gdb := testdb.Open(t)
	ctx := context.Background()
	agg := uniqueID(t)
	var dispatchID string
	err := tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		row, err := queue.Stage(ctx, pgxTx, "delivery:"+agg, queue.ActorDelivery, agg, nil, nil)
		dispatchID = row.ID
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	enqueue := func(id, aggregateID string) error {
		var status string
		if err := pool.QueryRow(ctx, `SELECT status FROM async_dispatches WHERE id = $1`, id).Scan(&status); err != nil {
			return err
		}
		seen[id] = status
		return nil
	}
	_, err = queue.RunDispatcherOnce(ctx, pool, enqueue, 100)
	if err != nil {
		t.Fatal(err)
	}
	if seen[dispatchID] != queue.StatusSent {
		t.Fatalf("enqueue saw %s, want sent", seen[dispatchID])
	}
}

func TestEnqueueFailureKeepsSent(t *testing.T) {
	pool, gdb := testdb.Open(t)
	ctx := context.Background()
	agg := uniqueID(t)
	var dispatchID string
	err := tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		row, err := queue.Stage(ctx, pgxTx, "graph:"+agg, queue.ActorGraphRun, agg, nil, nil)
		dispatchID = row.ID
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = queue.RunDispatcherOnce(ctx, pool, func(id, aggregateID string) error {
		if id == dispatchID {
			return errors.New("broker down")
		}
		return nil
	}, 100)
	if err != nil {
		t.Fatal(err)
	}
	var status string
	var lastError *string
	if err := pool.QueryRow(ctx, `SELECT status, last_error FROM async_dispatches WHERE id = $1`, dispatchID).Scan(&status, &lastError); err != nil {
		t.Fatal(err)
	}
	if status != queue.StatusSent {
		t.Fatalf("status %s", status)
	}
	if lastError == nil || *lastError == "" {
		t.Fatal("expected last_error")
	}
}

func TestConsumeClaimsSentAndMarksConsumed(t *testing.T) {
	pool, gdb := testdb.Open(t)
	ctx := context.Background()
	agg := uniqueID(t)
	var dispatch queue.Dispatch
	err := tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		var err error
		dispatch, err = queue.StageForActor(ctx, pgxTx, queue.ActorGraphRun, agg, 0)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	ran := false
	_, err = queue.RunDispatcherOnce(ctx, pool, func(id, aggregateID string) error { return nil }, 100)
	if err != nil {
		t.Fatal(err)
	}
	err = queue.Consume(ctx, pool, dispatch.ID, agg, map[string]queue.ActorFunc{
		queue.ActorGraphRun: func(ctx context.Context, aggregateID string) error {
			if aggregateID != agg {
				return nil
			}
			ran = true
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("actor not called")
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM async_dispatches WHERE id = $1`, dispatch.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != queue.StatusConsumed {
		t.Fatalf("status %s", status)
	}
}

func TestConsumeBusyReleasesToPending(t *testing.T) {
	pool, gdb := testdb.Open(t)
	ctx := context.Background()
	agg := uniqueID(t)
	var dispatch queue.Dispatch
	err := tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		var err error
		dispatch, err = queue.StageForActor(ctx, pgxTx, queue.ActorGraphRun, agg, 0)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := queue.RunDispatcherOnce(ctx, pool, func(id, aggregateID string) error { return nil }, 100); err != nil {
		t.Fatal(err)
	}
	err = queue.Consume(ctx, pool, dispatch.ID, agg, map[string]queue.ActorFunc{
		queue.ActorGraphRun: func(ctx context.Context, aggregateID string) error {
			return queue.ErrBusy
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM async_dispatches WHERE id = $1`, dispatch.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != queue.StatusPending {
		t.Fatalf("status %s, want pending", status)
	}
}

func TestConsumeLaterReleasesToPending(t *testing.T) {
	pool, gdb := testdb.Open(t)
	ctx := context.Background()
	agg := uniqueID(t)
	var dispatch queue.Dispatch
	err := tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		var err error
		dispatch, err = queue.StageForActor(ctx, pgxTx, queue.ActorGraphRun, agg, 0)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := queue.RunDispatcherOnce(ctx, pool, func(id, aggregateID string) error { return nil }, 100); err != nil {
		t.Fatal(err)
	}
	err = queue.Consume(ctx, pool, dispatch.ID, agg, map[string]queue.ActorFunc{
		queue.ActorGraphRun: func(ctx context.Context, aggregateID string) error {
			return queue.ErrLater
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM async_dispatches WHERE id = $1`, dispatch.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != queue.StatusPending {
		t.Fatalf("status %s, want pending", status)
	}
}

func TestConsumerLeaseExceedsTaskTimeout(t *testing.T) {
	if queue.DefaultConsumerLeaseSeconds <= int(queue.TaskTimeout/time.Second) {
		t.Fatalf("lease %d must exceed task timeout %s", queue.DefaultConsumerLeaseSeconds, queue.TaskTimeout)
	}
}

func TestRestageIfIdleSkipsPendingAndRestagesConsumed(t *testing.T) {
	pool, gdb := testdb.Open(t)
	ctx := context.Background()
	agg := uniqueID(t)
	err := tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		_, err := queue.StageForActor(ctx, pgxTx, queue.ActorGraphRun, agg, 0)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	err = tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		changed, err := queue.RestageIfIdle(ctx, pgxTx, queue.ActorGraphRun, agg, nil)
		if err != nil {
			return err
		}
		if changed {
			t.Fatal("pending dispatch must not restage")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE async_dispatches SET status = $1, consumed_at = NOW(), updated_at = NOW()
		WHERE aggregate_id = $2
	`, queue.StatusConsumed, agg); err != nil {
		t.Fatal(err)
	}
	err = tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		changed, err := queue.RestageIfIdle(ctx, pgxTx, queue.ActorGraphRun, agg, nil)
		if err != nil {
			return err
		}
		if !changed {
			t.Fatal("consumed unfinished dispatch must restage")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE async_dispatches SET status = $1, updated_at = NOW() WHERE aggregate_id = $2
	`, queue.StatusDead, agg); err != nil {
		t.Fatal(err)
	}
	err = tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		changed, err := queue.RestageIfIdle(ctx, pgxTx, queue.ActorGraphRun, agg, nil)
		if err != nil {
			return err
		}
		if !changed {
			t.Fatal("dead unfinished dispatch must restage")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM async_dispatches WHERE aggregate_id = $1`, agg).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != queue.StatusPending {
		t.Fatalf("status %s", status)
	}
}
