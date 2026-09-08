package queue_test

import (
	"context"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

func TestReconcileStaleSentIsBounded(t *testing.T) {
	pool, gdb := testdb.Open(t)
	ctx := context.Background()
	now := time.Now().UTC()
	before := countStaleSent(t, gdb, ctx, now)

	sentAt := now.Add(-24 * time.Hour)
	extra := 8
	total := queue.DefaultStaleSentReconcileLimit + extra
	ids := make([]string, total)
	rows := make([]schema.AsyncDispatches, total)
	for i := range rows {
		id := uniqueID(t)
		ids[i] = id
		rows[i] = schema.AsyncDispatches{
			ID:          id,
			DeliveryKey: "stale-sent:" + id,
			ActorName:   queue.ActorGraphRun,
			AggregateID: uniqueID(t),
			Status:      queue.StatusSent,
			AvailableAt: now,
			Attempts:    1,
			SentAt:      &sentAt,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
	}
	if err := gdb.WithContext(ctx).Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = gdb.Unscoped().Where("id IN ?", ids).Delete(&schema.AsyncDispatches{}).Error
	})

	afterInsert := countStaleSent(t, gdb, ctx, time.Now().UTC())
	if afterInsert != before+int64(total) {
		t.Fatalf("stale SENT after insert %d, want %d", afterInsert, before+int64(total))
	}

	if _, err := queue.RunDispatcherOnce(ctx, pool, nil, queue.DefaultClaimLimit); err != nil {
		t.Fatal(err)
	}

	after := countStaleSent(t, gdb, ctx, time.Now().UTC())
	dropped := afterInsert - after
	if dropped > int64(queue.DefaultStaleSentReconcileLimit) {
		t.Fatalf("reconciled %d stale SENT rows, want at most %d", dropped, queue.DefaultStaleSentReconcileLimit)
	}
	if after == 0 {
		t.Fatal("expected stale SENT rows beyond DefaultStaleSentReconcileLimit to remain")
	}
}

func TestRunDispatcherOnceHasMoreWhenClaimFillsLimit(t *testing.T) {
	pool, gdb := testdb.Open(t)
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		drained, err := queue.RunDispatcherOnce(ctx, pool, nil, queue.DefaultClaimLimit)
		if err != nil {
			t.Fatal(err)
		}
		if drained.Pending == 0 {
			break
		}
	}

	ids := make([]string, 3)
	err := tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		for i := range ids {
			agg := uniqueID(t)
			d, err := queue.Stage(ctx, pgxTx, "has-more:"+agg, queue.ActorDelivery, agg, nil, nil)
			if err != nil {
				return err
			}
			ids[i] = d.ID
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = gdb.Unscoped().Where("id IN ?", ids).Delete(&schema.AsyncDispatches{}).Error
	})

	first, err := queue.RunDispatcherOnce(ctx, pool, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if first.Pending != 2 || first.Sent != 2 || !first.HasMore {
		t.Fatalf("first cycle %+v, want pending=2 sent=2 has_more=true", first)
	}

	second, err := queue.RunDispatcherOnce(ctx, pool, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if second.Pending != 1 || second.Sent != 1 || second.HasMore {
		t.Fatalf("second cycle %+v, want pending=1 sent=1 has_more=false", second)
	}
}

func countStaleSent(t *testing.T, gdb *gorm.DB, ctx context.Context, now time.Time) int64 {
	t.Helper()
	cutoff := now.Add(-time.Duration(queue.DefaultSentReconcileAfter) * time.Second)
	var n int64
	if err := gdb.WithContext(ctx).Model(&schema.AsyncDispatches{}).
		Where("status = ? AND sent_at IS NOT NULL AND sent_at <= ?", queue.StatusSent, cutoff).
		Where("lease_token IS NULL OR (lease_expires_at IS NOT NULL AND lease_expires_at <= ?)", now).
		Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}
