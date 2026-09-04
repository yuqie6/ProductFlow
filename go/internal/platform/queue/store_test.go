package queue_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/metrics"
	"github.com/yuqie6/productflow/internal/platform/notify"
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

func TestStagePublishesDispatchNotifyAfterCommit(t *testing.T) {
	pool, gdb := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	notes, err := notify.Listen(ctx, pool, notify.ChannelDispatch)
	if err != nil {
		t.Fatal(err)
	}
	agg := uniqueID(t)
	var staged queue.Dispatch
	err = tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		var err error
		staged, err = queue.Stage(ctx, pgxTx, "graph:"+agg, queue.ActorGraphRun, agg, nil, nil)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case n := <-notes:
		if n.Channel != notify.ChannelDispatch || n.Payload != staged.ID {
			t.Fatalf("note %+v, want payload %s", n, staged.ID)
		}
	case <-ctx.Done():
		t.Fatal("did not receive dispatch notify after commit")
	}

	err = tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		_, err := queue.Stage(ctx, pgxTx, "graph:"+agg, queue.ActorGraphRun, agg, nil, nil)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case n := <-notes:
		t.Fatalf("no-op pending stage notified %+v", n)
	case <-time.After(200 * time.Millisecond):
	}
}

func waitDispatchNotify(t *testing.T, ctx context.Context, notes <-chan notify.Notification, dispatchID string) {
	t.Helper()
	for {
		select {
		case n, ok := <-notes:
			if !ok {
				t.Fatal("dispatch notify channel closed")
			}
			if n.Channel == notify.ChannelDispatch && n.Payload == dispatchID {
				return
			}
		case <-ctx.Done():
			t.Fatal("did not receive dispatch notify")
		}
	}
}

func TestResetPendingPublishesDispatchNotify(t *testing.T) {
	pool, gdb := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	agg := uniqueID(t)
	var staged queue.Dispatch
	err := tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		var err error
		staged, err = queue.Stage(ctx, pgxTx, "graph:"+agg, queue.ActorGraphRun, agg, nil, nil)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE async_dispatches SET status = $1, consumed_at = NOW(), updated_at = NOW()
		WHERE id = $2
	`, queue.StatusConsumed, staged.ID); err != nil {
		t.Fatal(err)
	}
	notes, err := notify.Listen(ctx, pool, notify.ChannelDispatch)
	if err != nil {
		t.Fatal(err)
	}
	var restaged queue.Dispatch
	err = tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		var err error
		restaged, err = queue.Stage(ctx, pgxTx, "graph:"+agg, queue.ActorGraphRun, agg, nil, nil)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if restaged.Status != queue.StatusPending {
		t.Fatalf("status %s, want pending", restaged.Status)
	}
	select {
	case n := <-notes:
		if n.Channel != notify.ChannelDispatch || n.Payload != staged.ID {
			t.Fatalf("note %+v, want payload %s", n, staged.ID)
		}
	case <-ctx.Done():
		t.Fatal("did not receive dispatch notify after resetPending commit")
	}
}

func TestConsumeBusyPublishesDispatchNotify(t *testing.T) {
	pool, gdb := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
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
	notes, err := notify.Listen(ctx, pool, notify.ChannelDispatch)
	if err != nil {
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
	waitDispatchNotify(t, ctx, notes, dispatch.ID)
}

func TestMarkFailedPublishesDispatchNotifyWhenPending(t *testing.T) {
	pool, gdb := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
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
	notes, err := notify.Listen(ctx, pool, notify.ChannelDispatch)
	if err != nil {
		t.Fatal(err)
	}
	err = queue.Consume(ctx, pool, dispatch.ID, agg, map[string]queue.ActorFunc{
		queue.ActorGraphRun: func(ctx context.Context, aggregateID string) error {
			return errors.New("provider boom")
		},
	})
	if err == nil {
		t.Fatal("expected consume error")
	}
	waitDispatchNotify(t, ctx, notes, dispatch.ID)
}

func TestMarkFailedDoesNotNotifyWhenDead(t *testing.T) {
	pool, gdb := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
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
	if _, err := pool.Exec(ctx, `UPDATE async_dispatches SET attempts = $1 WHERE id = $2`, queue.DefaultMaxAttempts, dispatch.ID); err != nil {
		t.Fatal(err)
	}
	notes, err := notify.Listen(ctx, pool, notify.ChannelDispatch)
	if err != nil {
		t.Fatal(err)
	}
	err = queue.Consume(ctx, pool, dispatch.ID, agg, map[string]queue.ActorFunc{
		queue.ActorGraphRun: func(ctx context.Context, aggregateID string) error {
			return errors.New("exhausted")
		},
	})
	if err == nil {
		t.Fatal("expected consume error")
	}
	select {
	case n := <-notes:
		if n.Channel == notify.ChannelDispatch && n.Payload == dispatch.ID {
			t.Fatalf("dead dispatch notified %+v", n)
		}
	case <-time.After(250 * time.Millisecond):
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM async_dispatches WHERE id = $1`, dispatch.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != queue.StatusDead {
		t.Fatalf("status %s, want dead", status)
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

func TestConsumeRecordsResultCounters(t *testing.T) {
	pool, gdb := testdb.Open(t)
	ctx := context.Background()
	stageSent := func(agg string) queue.Dispatch {
		t.Helper()
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
		return dispatch
	}

	t.Run("consumed", func(t *testing.T) {
		agg := uniqueID(t)
		dispatch := stageSent(agg)
		before := metrics.ConsumeResultCount("consumed")
		err := queue.Consume(ctx, pool, dispatch.ID, agg, map[string]queue.ActorFunc{
			queue.ActorGraphRun: func(context.Context, string) error { return nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		if metrics.ConsumeResultCount("consumed") != before+1 {
			t.Fatalf("consumed counter %d, want %d", metrics.ConsumeResultCount("consumed"), before+1)
		}
	})
	t.Run("busy", func(t *testing.T) {
		agg := uniqueID(t)
		dispatch := stageSent(agg)
		before := metrics.ConsumeResultCount("busy")
		err := queue.Consume(ctx, pool, dispatch.ID, agg, map[string]queue.ActorFunc{
			queue.ActorGraphRun: func(context.Context, string) error { return queue.ErrBusy },
		})
		if err != nil {
			t.Fatal(err)
		}
		if metrics.ConsumeResultCount("busy") != before+1 {
			t.Fatalf("busy counter %d, want %d", metrics.ConsumeResultCount("busy"), before+1)
		}
	})
	t.Run("later", func(t *testing.T) {
		agg := uniqueID(t)
		dispatch := stageSent(agg)
		before := metrics.ConsumeResultCount("later")
		err := queue.Consume(ctx, pool, dispatch.ID, agg, map[string]queue.ActorFunc{
			queue.ActorGraphRun: func(context.Context, string) error { return queue.ErrLater },
		})
		if err != nil {
			t.Fatal(err)
		}
		if metrics.ConsumeResultCount("later") != before+1 {
			t.Fatalf("later counter %d, want %d", metrics.ConsumeResultCount("later"), before+1)
		}
	})
	t.Run("failed", func(t *testing.T) {
		agg := uniqueID(t)
		dispatch := stageSent(agg)
		before := metrics.ConsumeResultCount("failed")
		err := queue.Consume(ctx, pool, dispatch.ID, agg, map[string]queue.ActorFunc{
			queue.ActorGraphRun: func(context.Context, string) error { return errors.New("actor failed") },
		})
		if err == nil {
			t.Fatal("expected actor error")
		}
		if metrics.ConsumeResultCount("failed") != before+1 {
			t.Fatalf("failed counter %d, want %d", metrics.ConsumeResultCount("failed"), before+1)
		}
	})
	t.Run("unknown", func(t *testing.T) {
		agg := uniqueID(t)
		dispatch := stageSent(agg)
		before := metrics.ConsumeResultCount("unknown")
		err := queue.Consume(ctx, pool, dispatch.ID, agg, map[string]queue.ActorFunc{})
		if err != nil {
			t.Fatal(err)
		}
		if metrics.ConsumeResultCount("unknown") != before+1 {
			t.Fatalf("unknown counter %d, want %d", metrics.ConsumeResultCount("unknown"), before+1)
		}
	})
}

func TestConsumerLeaseExceedsTaskTimeout(t *testing.T) {
	if queue.DefaultConsumerLeaseSeconds <= int(queue.TaskTimeout/time.Second) {
		t.Fatalf("lease %d must exceed task timeout %s", queue.DefaultConsumerLeaseSeconds, queue.TaskTimeout)
	}
}

func TestRestageIfIdleSkipsPendingSentDeadAndRestagesConsumed(t *testing.T) {
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
		UPDATE async_dispatches SET status = $1, sent_at = NOW(), updated_at = NOW()
		WHERE aggregate_id = $2
	`, queue.StatusSent, agg); err != nil {
		t.Fatal(err)
	}
	err = tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		changed, err := queue.RestageIfIdle(ctx, pgxTx, queue.ActorGraphRun, agg, nil)
		if err != nil {
			return err
		}
		if changed {
			t.Fatal("sent dispatch must not restage")
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
		UPDATE async_dispatches SET status = $1, attempts = 10, updated_at = NOW() WHERE aggregate_id = $2
	`, queue.StatusDead, agg); err != nil {
		t.Fatal(err)
	}
	err = tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		changed, err := queue.RestageIfIdle(ctx, pgxTx, queue.ActorGraphRun, agg, nil)
		if err != nil {
			return err
		}
		if changed {
			t.Fatal("dead dispatch must stay dead-lettered")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var status string
	var attempts int
	if err := pool.QueryRow(ctx, `SELECT status, attempts FROM async_dispatches WHERE aggregate_id = $1`, agg).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != queue.StatusDead {
		t.Fatalf("status %s", status)
	}
	if attempts != 10 {
		t.Fatalf("attempts %d", attempts)
	}
	missing := uniqueID(t)
	err = tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		changed, err := queue.RestageIfIdle(ctx, pgxTx, queue.ActorGraphRun, missing, nil)
		if err != nil {
			return err
		}
		if !changed {
			t.Fatal("missing envelope must be created")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM async_dispatches WHERE aggregate_id = $1`, missing).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != queue.StatusPending {
		t.Fatalf("missing restage status %s", status)
	}
}
