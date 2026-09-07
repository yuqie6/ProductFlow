package quota_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/quota"
	"gorm.io/gorm"
)

func TestReserveSettleReleaseUnknown(t *testing.T) {
	svc, merchantID := newQuotaFixture(t, 100)

	ctx := context.Background()
	hold, acct, err := svc.Reserve(ctx, merchantID, "op-reserve-1", 40, quota.DefaultPriceVersionID)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if hold.Status != quota.StatusReserved || hold.AmountUnits != 40 {
		t.Fatalf("hold=%+v", hold)
	}
	if acct.AvailableUnits != 60 || acct.ReservedUnits != 40 {
		t.Fatalf("acct after reserve=%+v", acct)
	}

	// Idempotent reserve replay
	hold2, acct2, err := svc.Reserve(ctx, merchantID, "op-reserve-1", 40, quota.DefaultPriceVersionID)
	if err != nil {
		t.Fatalf("reserve replay: %v", err)
	}
	if hold2.ID != hold.ID {
		t.Fatalf("replay hold id changed: %s vs %s", hold.ID, hold2.ID)
	}
	if acct2.AvailableUnits != 60 || acct2.ReservedUnits != 40 {
		t.Fatalf("replay mutated balance: %+v", acct2)
	}

	settled, acct3, err := svc.Settle(ctx, merchantID, "op-reserve-1", 25)
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	if settled.Status != quota.StatusSettled || settled.SettledUnits == nil || *settled.SettledUnits != 25 {
		t.Fatalf("settled=%+v", settled)
	}
	if acct3.AvailableUnits != 75 || acct3.ReservedUnits != 0 {
		t.Fatalf("acct after settle=%+v", acct3)
	}

	// Idempotent settle replay
	_, acct4, err := svc.Settle(ctx, merchantID, "op-reserve-1", 25)
	if err != nil {
		t.Fatalf("settle replay: %v", err)
	}
	if acct4.AvailableUnits != 75 || acct4.ReservedUnits != 0 {
		t.Fatalf("settle replay mutated balance: %+v", acct4)
	}

	// Cancel / release path
	_, _, err = svc.Reserve(ctx, merchantID, "op-cancel-1", 30, "")
	if err != nil {
		t.Fatalf("reserve cancel: %v", err)
	}
	released, acct5, err := svc.Release(ctx, merchantID, "op-cancel-1")
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if released.Status != quota.StatusReleased {
		t.Fatalf("released=%+v", released)
	}
	if acct5.AvailableUnits != 75 || acct5.ReservedUnits != 0 {
		t.Fatalf("acct after release=%+v", acct5)
	}
	_, acct6, err := svc.Release(ctx, merchantID, "op-cancel-1")
	if err != nil {
		t.Fatalf("release replay: %v", err)
	}
	if acct6.AvailableUnits != 75 {
		t.Fatalf("release replay mutated: %+v", acct6)
	}

	// Unknown stays pending reconciliation and keeps reserved liability
	_, _, err = svc.Reserve(ctx, merchantID, "op-unknown-1", 20, "")
	if err != nil {
		t.Fatalf("reserve unknown: %v", err)
	}
	unknown, acct7, err := svc.MarkUnknown(ctx, merchantID, "op-unknown-1")
	if err != nil {
		t.Fatalf("mark unknown: %v", err)
	}
	if unknown.Status != quota.StatusPendingReconciliation {
		t.Fatalf("unknown=%+v", unknown)
	}
	if acct7.AvailableUnits != 55 || acct7.ReservedUnits != 20 {
		t.Fatalf("unknown must keep reserved liability: %+v", acct7)
	}
	// Must not auto-release as zero cost
	if _, _, err := svc.Release(ctx, merchantID, "op-unknown-1"); err == nil {
		t.Fatal("expected release of unknown to fail")
	} else {
		var ae apperr.Error
		if !errors.As(err, &ae) || ae.Status != 409 {
			t.Fatalf("expected 409 conflict, got %v", err)
		}
	}
	final, err := svc.GetAccount(ctx, merchantID)
	if err != nil {
		t.Fatal(err)
	}
	if final.AvailableUnits != 55 || final.ReservedUnits != 20 {
		t.Fatalf("final balance=%+v", final)
	}

	// Later settle from pending_reconciliation is allowed
	_, acct8, err := svc.Settle(ctx, merchantID, "op-unknown-1", 20)
	if err != nil {
		t.Fatalf("settle unknown: %v", err)
	}
	if acct8.AvailableUnits != 55 || acct8.ReservedUnits != 0 {
		t.Fatalf("after reconcile settle=%+v", acct8)
	}
}

func TestConcurrentDoubleReserveCannotExceedBalance(t *testing.T) {
	svc, merchantID := newQuotaFixture(t, 100)
	ctx := context.Background()

	const workers = 20
	const each = int64(80)
	var okCount atomic.Int64
	var conflictCount atomic.Int64
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			// Distinct keys so the race is on balance, not idempotency.
			key := "concurrent-" + clockid.New()
			_, _, err := svc.Reserve(ctx, merchantID, key, each, "")
			if err == nil {
				okCount.Add(1)
				return
			}
			var ae apperr.Error
			if errors.As(err, &ae) && ae.Status == 409 {
				conflictCount.Add(1)
				return
			}
			t.Errorf("unexpected reserve err: %v", err)
		}()
	}
	wg.Wait()

	if okCount.Load() != 1 {
		t.Fatalf("expected exactly 1 successful reserve, got %d (conflicts=%d)", okCount.Load(), conflictCount.Load())
	}
	if conflictCount.Load() != workers-1 {
		t.Fatalf("expected %d conflicts, got %d", workers-1, conflictCount.Load())
	}
	acct, err := svc.GetAccount(ctx, merchantID)
	if err != nil {
		t.Fatal(err)
	}
	if acct.AvailableUnits != 20 || acct.ReservedUnits != 80 {
		t.Fatalf("balance exceeded or wrong: %+v", acct)
	}
}

func TestConcurrentIdempotentReserveSameKey(t *testing.T) {
	svc, merchantID := newQuotaFixture(t, 50)
	ctx := context.Background()
	const workers = 16
	var wg sync.WaitGroup
	wg.Add(workers)
	ids := make([]string, workers)
	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()
			hold, _, err := svc.Reserve(ctx, merchantID, "same-key", 30, "")
			if err != nil {
				t.Errorf("reserve: %v", err)
				return
			}
			ids[i] = hold.ID
		}()
	}
	wg.Wait()
	first := ""
	for _, id := range ids {
		if id == "" {
			t.Fatal("missing hold id")
		}
		if first == "" {
			first = id
		} else if id != first {
			t.Fatalf("idempotent race created multiple holds: %s vs %s", first, id)
		}
	}
	acct, err := svc.GetAccount(ctx, merchantID)
	if err != nil {
		t.Fatal(err)
	}
	if acct.AvailableUnits != 20 || acct.ReservedUnits != 30 {
		t.Fatalf("double reserved: %+v", acct)
	}
}

func TestAdjustIdempotent(t *testing.T) {
	svc, merchantID := newQuotaFixture(t, 0)
	ctx := context.Background()
	acct, err := svc.Adjust(ctx, merchantID, "grant-1", 100, "trial", "op-user")
	if err != nil {
		t.Fatal(err)
	}
	if acct.AvailableUnits != 100 {
		t.Fatalf("%+v", acct)
	}
	acct2, err := svc.Adjust(ctx, merchantID, "grant-1", 100, "trial", "op-user")
	if err != nil {
		t.Fatal(err)
	}
	if acct2.AvailableUnits != 100 {
		t.Fatalf("adjust replay doubled: %+v", acct2)
	}
}

func TestDefaultPriceCatalogSeeded(t *testing.T) {
	svc, _ := newQuotaFixture(t, 0)
	ctx := context.Background()
	version, err := svc.GetDefaultPriceVersion(ctx)
	if err != nil {
		t.Fatalf("default version: %v", err)
	}
	if version.ID != quota.DefaultPriceVersionID || !version.IsDefault {
		t.Fatalf("default version=%+v", version)
	}
	if version.Currency != quota.CurrencyInternalUnits {
		t.Fatalf("currency=%q", version.Currency)
	}
	wantCodes := map[string]int64{
		quota.EntryImageSessionGenerate: 1,
		quota.EntryGraphImageGeneration: 1,
		quota.EntryAgentModelRequest:    1,
		quota.EntryLocalEdit:            1,
		quota.EntryProductSourceNote:    1,
	}
	if len(version.Entries) != len(wantCodes) {
		t.Fatalf("entries=%+v", version.Entries)
	}
	for _, e := range version.Entries {
		price, ok := wantCodes[e.EntryCode]
		if !ok || e.UnitPrice != price {
			t.Fatalf("entry %+v", e)
		}
	}
}

func TestReserveRejectsUnknownPriceVersion(t *testing.T) {
	svc, merchantID := newQuotaFixture(t, 50)
	ctx := context.Background()
	_, _, err := svc.Reserve(ctx, merchantID, "bad-pv", 10, "pv-does-not-exist")
	if err == nil {
		t.Fatal("expected unknown price version rejection")
	}
	var ae apperr.Error
	if !errors.As(err, &ae) || ae.Status != 400 || ae.Detail != "未知价格版本" {
		t.Fatalf("got %v", err)
	}
	acct, err := svc.GetAccount(ctx, merchantID)
	if err != nil {
		t.Fatal(err)
	}
	if acct.AvailableUnits != 50 || acct.ReservedUnits != 0 {
		t.Fatalf("balance mutated on reject: %+v", acct)
	}
}

func TestReserveKnownPriceVersionSucceeds(t *testing.T) {
	svc, merchantID := newQuotaFixture(t, 50)
	ctx := context.Background()
	hold, acct, err := svc.Reserve(ctx, merchantID, "known-pv", 10, quota.DefaultPriceVersionID)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if hold.PriceVersionID != quota.DefaultPriceVersionID {
		t.Fatalf("hold=%+v", hold)
	}
	if acct.AvailableUnits != 40 || acct.ReservedUnits != 10 || acct.PriceVersionID != quota.DefaultPriceVersionID {
		t.Fatalf("acct=%+v", acct)
	}
}

func newQuotaFixture(t *testing.T, initialAvailable int64) (*quota.Service, string) {
	t.Helper()
	gdb := testdb.Gorm(t)
	merchantID := seedMerchant(t, gdb)
	svc := &quota.Service{DB: gdb}
	if initialAvailable != 0 {
		if _, err := svc.Adjust(context.Background(), merchantID, "seed-"+clockid.New(), initialAvailable, "fixture", ""); err != nil {
			t.Fatalf("seed adjust: %v", err)
		}
	}
	return svc, merchantID
}

func seedMerchant(t *testing.T, gdb *gorm.DB) string {
	t.Helper()
	id := clockid.New()
	now := time.Now().UTC()
	row := schema.Merchants{
		ID:        id,
		Name:      "quota-test-" + id[:8],
		Status:    "active",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := gdb.Create(&row).Error; err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	return id
}
