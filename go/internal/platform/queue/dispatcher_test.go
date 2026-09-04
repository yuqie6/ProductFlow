package queue_test

import (
	"context"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/testdb"
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
