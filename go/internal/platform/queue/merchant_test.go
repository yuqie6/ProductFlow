package queue_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

func TestStageSnapshotsMerchantAndRejectsRewrite(t *testing.T) {
	_, gdb := testdb.Open(t)
	agg := uniqueID(t)
	merchantA := "merchant-a-" + uniqueID(t)[:8]
	merchantB := "merchant-b-" + uniqueID(t)[:8]
	ctxA := auth.WithMerchantID(context.Background(), merchantA)
	ctxB := auth.WithMerchantID(context.Background(), merchantB)

	var staged queue.Dispatch
	err := tx.WithGorm(ctxA, gdb, func(pgxTx *gorm.DB) error {
		var stageErr error
		staged, stageErr = queue.StageForActor(ctxA, pgxTx, queue.ActorGraphRun, agg, 0)
		return stageErr
	})
	if err != nil {
		t.Fatal(err)
	}
	if staged.MerchantID != merchantA {
		t.Fatalf("merchant_id=%q want %q", staged.MerchantID, merchantA)
	}

	err = tx.WithGorm(ctxB, gdb, func(pgxTx *gorm.DB) error {
		_, stageErr := queue.StageForActor(ctxB, pgxTx, queue.ActorGraphRun, agg, 0)
		return stageErr
	})
	assertConflict(t, err)

	var kept queue.Dispatch
	err = tx.WithGorm(context.Background(), gdb, func(pgxTx *gorm.DB) error {
		var stageErr error
		kept, stageErr = queue.StageForActor(context.Background(), pgxTx, queue.ActorGraphRun, agg, 0)
		return stageErr
	})
	if err != nil {
		t.Fatal(err)
	}
	if kept.MerchantID != merchantA {
		t.Fatalf("restage without ctx kept merchant_id=%q want %q", kept.MerchantID, merchantA)
	}
}

func TestRestageIfIdlePreservesMerchantMixedQueue(t *testing.T) {
	_, gdb := testdb.Open(t)
	merchantA := "merchant-mix-a-" + uniqueID(t)[:8]
	merchantB := "merchant-mix-b-" + uniqueID(t)[:8]
	aggA := uniqueID(t)
	aggB := uniqueID(t)
	ctxA := auth.WithMerchantID(context.Background(), merchantA)
	ctxB := auth.WithMerchantID(context.Background(), merchantB)

	err := tx.WithGorm(context.Background(), gdb, func(pgxTx *gorm.DB) error {
		if _, err := queue.StageForActor(ctxA, pgxTx, queue.ActorGraphRun, aggA, 0); err != nil {
			return err
		}
		if _, err := queue.StageForActor(ctxB, pgxTx, queue.ActorDelivery, aggB, 0); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	markConsumed := func(actor, agg string) {
		t.Helper()
		key := queue.DeliveryKey(actor, agg)
		if err := gdb.Exec(
			`UPDATE async_dispatches SET status = ?, consumed_at = NOW(), updated_at = NOW() WHERE delivery_key = ?`,
			queue.StatusConsumed, key,
		).Error; err != nil {
			t.Fatal(err)
		}
	}
	markConsumed(queue.ActorGraphRun, aggA)
	markConsumed(queue.ActorDelivery, aggB)

	// Restage without a working-merchant ctx keeps acceptance snapshots (recovery path).
	err = tx.WithGorm(context.Background(), gdb, func(pgxTx *gorm.DB) error {
		changed, restageErr := queue.RestageIfIdle(context.Background(), pgxTx, queue.ActorGraphRun, aggA, nil)
		if restageErr != nil {
			return restageErr
		}
		if !changed {
			t.Fatal("expected graph restage")
		}
		changed, restageErr = queue.RestageIfIdle(context.Background(), pgxTx, queue.ActorDelivery, aggB, nil)
		if restageErr != nil {
			return restageErr
		}
		if !changed {
			t.Fatal("expected delivery restage")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	markConsumed(queue.ActorGraphRun, aggA)
	err = tx.WithGorm(ctxB, gdb, func(pgxTx *gorm.DB) error {
		_, restageErr := queue.RestageIfIdle(ctxB, pgxTx, queue.ActorGraphRun, aggA, nil)
		return restageErr
	})
	assertConflict(t, err)

	var graph, delivery queue.Dispatch
	err = tx.WithGorm(context.Background(), gdb, func(pgxTx *gorm.DB) error {
		var loadErr error
		graph, loadErr = queue.StageForActor(context.Background(), pgxTx, queue.ActorGraphRun, aggA, 0)
		if loadErr != nil {
			return loadErr
		}
		delivery, loadErr = queue.StageForActor(context.Background(), pgxTx, queue.ActorDelivery, aggB, 0)
		return loadErr
	})
	if err != nil {
		t.Fatal(err)
	}
	if graph.MerchantID != merchantA {
		t.Fatalf("graph merchant=%q want %q", graph.MerchantID, merchantA)
	}
	if delivery.MerchantID != merchantB {
		t.Fatalf("delivery merchant=%q want %q", delivery.MerchantID, merchantB)
	}
}

func assertConflict(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected conflict")
	}
	var appErr apperr.Error
	if !errors.As(err, &appErr) {
		got, ok := err.(apperr.Error)
		if !ok {
			t.Fatalf("want apperr.Error, got %T %v", err, err)
		}
		appErr = got
	}
	if appErr.Status != http.StatusConflict {
		t.Fatalf("want 409 conflict, got status=%d detail=%q", appErr.Status, appErr.Detail)
	}
}
