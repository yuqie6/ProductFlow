package localedit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/quota"
)

type missingHoldProvider struct {
	MockProvider
	afterCall func()
	calls     int
}

func (p *missingHoldProvider) Edit(ctx context.Context, req EditRequest) (EditResult, error) {
	p.calls++
	p.afterCall()
	return p.MockProvider.Edit(ctx, req)
}

func TestUnknownRequiresOriginalHoldAcrossFinishCancelAndRecovery(t *testing.T) {
	ctx := context.Background()
	p := &missingHoldProvider{MockProvider: MockProvider{Cap: SupportedCapability("mock-local"), Err: errors.New("provider timeout")}}
	es := newEditServer(t, p)
	created := es.createProduct(t)
	taskID := createQueuedLocalEdit(t, es, created, "unknown-missing-hold")
	var hold schema.MerchantQuotaHolds
	p.afterCall = func() {
		if err := es.db.Where("idempotency_key LIKE ?", "local-edit:"+taskID+":%").Take(&hold).Error; err != nil {
			t.Fatal(err)
		}
		// Fault injection hides the committed reservation from its exact key.
		// Keep its balances and events intact so restoration is lossless.
		if err := es.db.Model(&hold).Update("idempotency_key", "hidden:"+hold.ID).Error; err != nil {
			t.Fatal(err)
		}
	}
	e := Executor{DB: es.db, Media: es.media, Provider: p}
	err := e.Execute(ctx, taskID)
	if !apperr.IsNotFound(err) {
		t.Fatalf("unknown finish accepted missing hold: %v", err)
	}
	assertRunning := func() {
		t.Helper()
		var task schema.LocalImageEditTasks
		if err := es.db.Where("id = ?", taskID).Take(&task).Error; err != nil {
			t.Fatal(err)
		}
		if task.Status != "running" {
			t.Fatalf("missing ledger committed terminal %s", task.Status)
		}
	}
	assertRunning()
	_, err = (Service{DB: es.db}).Cancel(ctx, created.Product.ID, taskID, nil)
	if !apperr.IsNotFound(err) {
		t.Fatalf("cancel accepted missing hold: %v", err)
	}
	assertRunning()
	future := time.Now().UTC().Add(2 * time.Hour)
	if _, err := recoverLocalEditState(ctx, es.db, taskID, time.Minute, future); !apperr.IsNotFound(err) {
		t.Fatalf("recovery accepted missing hold: %v", err)
	}
	assertRunning()
	var task schema.LocalImageEditTasks
	if err := es.db.Where("id = ?", taskID).Take(&task).Error; err != nil {
		t.Fatal(err)
	}
	if task.ActiveAttemptID == nil {
		t.Fatal("missing original attempt")
	}
	if err := es.db.Model(&hold).Update("idempotency_key", editQuotaKey(taskID, *task.ActiveAttemptID)).Error; err != nil {
		t.Fatal(err)
	}
	if state, err := recoverLocalEditState(ctx, es.db, taskID, time.Minute, future); err != nil || state != "unknown" {
		t.Fatalf("restored recovery=%s err=%v", state, err)
	}
	if err := e.Execute(ctx, taskID); err != nil {
		t.Fatal(err)
	}
	if p.calls != 1 {
		t.Fatalf("provider repeated %d times", p.calls)
	}
	if err := es.db.Where("id = ?", hold.ID).Take(&hold).Error; err != nil {
		t.Fatal(err)
	}
	if hold.Status != quota.StatusPendingReconciliation {
		t.Fatalf("restored hold=%s", hold.Status)
	}
}
