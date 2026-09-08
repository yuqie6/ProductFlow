package quota_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/quota"
	"gorm.io/gorm"
)

func TestConcurrentFirstAccountCreationSeedsOnce(t *testing.T) {
	svc, merchantID := newQuotaFixture(t, 0)
	trial := int64(75)
	svc.TrialUnits = &trial
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Both real transactions must observe the missing row before either inserts.
	ready := make(chan struct{})
	var reads atomic.Int32
	const callback = "test:concurrent_first_quota_account"
	if err := svc.DB.Callback().Query().After("gorm:query").Register(callback, func(db *gorm.DB) {
		if db.Statement.Table != "merchant_quota_accounts" || !errors.Is(db.Error, gorm.ErrRecordNotFound) {
			return
		}
		if reads.Add(1) == 2 {
			close(ready)
		}
		select {
		case <-ready:
		case <-ctx.Done():
			db.AddError(ctx.Err())
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.DB.Callback().Query().Remove(callback) })
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := svc.EnsureAccount(ctx, merchantID)
			results <- err
		}()
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Errorf("concurrent ensure: %v", err)
		}
	}
	if reads.Load() != 2 {
		t.Fatalf("missing-account reads=%d", reads.Load())
	}
	var account schema.MerchantQuotaAccounts
	if err := svc.DB.Where("merchant_id = ?", merchantID).Take(&account).Error; err != nil {
		t.Fatal(err)
	}
	if account.AvailableUnits != trial || account.ReservedUnits != 0 {
		t.Fatalf("account=%+v", account)
	}
	var events []schema.MerchantQuotaEvents
	if err := svc.DB.Where("merchant_id = ?", merchantID).Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].IdempotencyKey != quota.TrialSeedIdempotencyKey || events[0].AmountUnits != trial {
		t.Fatalf("trial events=%+v", events)
	}
}
