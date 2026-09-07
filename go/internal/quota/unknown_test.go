package quota_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/quota"
)

func TestResolveUnknownSettlesPendingAndRejectsRelease(t *testing.T) {
	svc, merchantID := newQuotaFixture(t, 100)
	ctx := context.Background()

	if _, _, err := svc.Reserve(ctx, merchantID, "op-resolve-1", 30, ""); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if _, _, err := svc.MarkUnknown(ctx, merchantID, "op-resolve-1"); err != nil {
		t.Fatalf("mark unknown: %v", err)
	}

	hold, acct, err := svc.ResolveUnknown(ctx, merchantID, "op-resolve-1", 12, "provider confirmed partial", "op-user-1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if hold.Status != quota.StatusSettled || hold.SettledUnits == nil || *hold.SettledUnits != 12 {
		t.Fatalf("hold=%+v", hold)
	}
	// available: 100-30+18 refund = 88; reserved 0
	if acct.AvailableUnits != 88 || acct.ReservedUnits != 0 {
		t.Fatalf("acct=%+v", acct)
	}

	// Idempotent replay
	hold2, acct2, err := svc.ResolveUnknown(ctx, merchantID, "op-resolve-1", 12, "provider confirmed partial", "op-user-1")
	if err != nil {
		t.Fatalf("resolve replay: %v", err)
	}
	if hold2.ID != hold.ID || acct2.AvailableUnits != 88 {
		t.Fatalf("replay mutated: hold=%+v acct=%+v", hold2, acct2)
	}

	// Different actual after settle → conflict
	if _, _, err := svc.ResolveUnknown(ctx, merchantID, "op-resolve-1", 30, "retry", "op-user-1"); err == nil {
		t.Fatal("expected conflict on different settle amount")
	} else {
		var ae apperr.Error
		if !errors.As(err, &ae) || ae.Status != 409 {
			t.Fatalf("expected 409, got %v", err)
		}
	}

	// Release still forbidden (already settled → also conflict)
	if _, _, err := svc.Release(ctx, merchantID, "op-resolve-1"); err == nil {
		t.Fatal("expected release rejection")
	}
}

func TestResolveUnknownRequiresPendingAndReason(t *testing.T) {
	svc, merchantID := newQuotaFixture(t, 50)
	ctx := context.Background()

	if _, _, err := svc.Reserve(ctx, merchantID, "still-reserved", 10, ""); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if _, _, err := svc.ResolveUnknown(ctx, merchantID, "still-reserved", 10, "too early", "op"); err == nil {
		t.Fatal("expected conflict on reserved hold")
	}

	if _, _, err := svc.Reserve(ctx, merchantID, "need-reason", 5, ""); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if _, _, err := svc.MarkUnknown(ctx, merchantID, "need-reason"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if _, _, err := svc.ResolveUnknown(ctx, merchantID, "need-reason", 0, "  ", "op"); err == nil {
		t.Fatal("expected validation on empty reason")
	}

	// Explicit zero after investigation is Settle(0), not Release
	hold, acct, err := svc.ResolveUnknown(ctx, merchantID, "need-reason", 0, "confirmed no provider call billed", "op")
	if err != nil {
		t.Fatalf("resolve zero: %v", err)
	}
	if hold.Status != quota.StatusSettled || hold.SettledUnits == nil || *hold.SettledUnits != 0 {
		t.Fatalf("hold=%+v", hold)
	}
	// still-reserved keeps 10 reserved; need-reason refunded 5 → available 40
	if acct.AvailableUnits != 40 || acct.ReservedUnits != 10 {
		t.Fatalf("acct=%+v", acct)
	}
}

func TestExpireUnknownHoldsReleasesReservationAndPreservesUnknownFact(t *testing.T) {
	ttl := time.Hour
	svc, merchantID := newQuotaFixture(t, 100)
	svc.UnknownHoldTTL = &ttl
	ctx := context.Background()

	if _, _, err := svc.Reserve(ctx, merchantID, "expire-due", 20, ""); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if _, _, err := svc.MarkUnknown(ctx, merchantID, "expire-due"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if _, _, err := svc.Reserve(ctx, merchantID, "expire-fresh", 15, ""); err != nil {
		t.Fatalf("reserve fresh: %v", err)
	}
	if _, _, err := svc.MarkUnknown(ctx, merchantID, "expire-fresh"); err != nil {
		t.Fatalf("mark fresh: %v", err)
	}

	// Backdate only the due hold past TTL
	past := time.Now().UTC().Add(-2 * time.Hour)
	if err := svc.DB.Model(&schema.MerchantQuotaHolds{}).
		Where("merchant_id = ? AND idempotency_key = ?", merchantID, "expire-due").
		Update("updated_at", past).Error; err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	expired := 0
	hasMore := false
	for {
		batchExpired, more, expireErr := svc.ExpireUnknownHolds(ctx, now, 10)
		if expireErr != nil {
			t.Fatalf("expire: %v", expireErr)
		}
		expired += batchExpired
		hasMore = more
		if !hasMore {
			break
		}
	}
	if expired < 1 {
		t.Fatalf("expired=%d hasMore=%v", expired, hasMore)
	}

	acct, err := svc.GetAccount(ctx, merchantID)
	if err != nil {
		t.Fatal(err)
	}
	// 100-20-15 + 20 refund on due = 85 available; fresh still reserved 15.
	if acct.AvailableUnits != 85 || acct.ReservedUnits != 15 {
		t.Fatalf("acct after expire=%+v", acct)
	}

	var due schema.MerchantQuotaHolds
	if err := svc.DB.Where("merchant_id = ? AND idempotency_key = ?", merchantID, "expire-due").Take(&due).Error; err != nil {
		t.Fatal(err)
	}
	if due.Status != quota.StatusReleased || due.SettledUnits != nil {
		t.Fatalf("due hold=%+v", due)
	}
	var events []schema.MerchantQuotaEvents
	if err := svc.DB.Where("merchant_id = ? AND idempotency_key = ?", merchantID, "expire-due").Order("created_at ASC, id ASC").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].EventType != quota.EventReserve || events[1].EventType != quota.EventMarkUnknown || events[2].EventType != quota.EventRelease {
		t.Fatalf("unknown history=%+v", events)
	}
	if events[1].AmountUnits != 20 {
		t.Fatalf("unknown event=%+v", events[1])
	}
	var release schema.MerchantQuotaEvents
	if err := svc.DB.Where("merchant_id = ? AND event_type = ? AND idempotency_key = ?", merchantID, quota.EventRelease, "expire-due").Take(&release).Error; err != nil {
		t.Fatal(err)
	}
	if release.AmountUnits != 20 || release.Reason == nil || *release.Reason != quota.UnknownExpiryReason {
		t.Fatalf("expiry release event=%+v", release)
	}
	var settleEvents int64
	if err := svc.DB.Model(&schema.MerchantQuotaEvents{}).
		Where("merchant_id = ? AND event_type = ? AND idempotency_key = ?", merchantID, quota.EventSettle, "expire-due").Count(&settleEvents).Error; err != nil {
		t.Fatal(err)
	}
	if settleEvents != 0 {
		t.Fatalf("expiry must release, not settle: %d", settleEvents)
	}

	var fresh schema.MerchantQuotaHolds
	if err := svc.DB.Where("merchant_id = ? AND idempotency_key = ?", merchantID, "expire-fresh").Take(&fresh).Error; err != nil {
		t.Fatal(err)
	}
	if fresh.Status != quota.StatusPendingReconciliation {
		t.Fatalf("fresh should stay pending: %+v", fresh)
	}

	// Second pass is no-op (idempotent); no second release event.
	acctBeforeReplay, err := svc.GetAccount(ctx, merchantID)
	if err != nil {
		t.Fatal(err)
	}
	expired2, _, err := svc.ExpireUnknownHolds(ctx, now, 10)
	if err != nil {
		t.Fatalf("expire replay: %v", err)
	}
	acctAfterReplay, err := svc.GetAccount(ctx, merchantID)
	if err != nil {
		t.Fatal(err)
	}
	if acctAfterReplay != acctBeforeReplay {
		t.Fatalf("replay mutated account: before=%+v after=%+v (expired=%d)", acctBeforeReplay, acctAfterReplay, expired2)
	}
	var releaseEvents int64
	if err := svc.DB.Model(&schema.MerchantQuotaEvents{}).
		Where("merchant_id = ? AND event_type = ? AND idempotency_key = ?", merchantID, quota.EventRelease, "expire-due").Count(&releaseEvents).Error; err != nil {
		t.Fatal(err)
	}
	if releaseEvents != 1 {
		t.Fatalf("release events=%d", releaseEvents)
	}
	if _, _, err := svc.Settle(ctx, merchantID, "expire-due", 20); err == nil {
		t.Fatal("late provider result must not settle an expired release")
	} else {
		var ae apperr.Error
		if !errors.As(err, &ae) || ae.Status != 409 {
			t.Fatalf("late settle status: %v", err)
		}
	}
	if _, _, err := svc.ResolveUnknown(ctx, merchantID, "expire-due", 20, "late operator confirmation", "op"); err == nil {
		t.Fatal("operator must not re-open an expired release")
	}
}

func TestExpireUnknownSkipsWhenOpResolvedFirst(t *testing.T) {
	ttl := time.Minute
	svc, merchantID := newQuotaFixture(t, 40)
	svc.UnknownHoldTTL = &ttl
	ctx := context.Background()

	if _, _, err := svc.Reserve(ctx, merchantID, "race-op", 10, ""); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if _, _, err := svc.MarkUnknown(ctx, merchantID, "race-op"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	past := time.Now().UTC().Add(-time.Hour)
	if err := svc.DB.Model(&schema.MerchantQuotaHolds{}).
		Where("merchant_id = ? AND idempotency_key = ?", merchantID, "race-op").
		Update("updated_at", past).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.ResolveUnknown(ctx, merchantID, "race-op", 3, "op won", "op"); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	_, _, err := svc.ExpireUnknownHolds(ctx, time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("expire: %v", err)
	}
	acct, err := svc.GetAccount(ctx, merchantID)
	if err != nil {
		t.Fatal(err)
	}
	// 40-10+7 = 37
	if acct.AvailableUnits != 37 || acct.ReservedUnits != 0 {
		t.Fatalf("acct=%+v", acct)
	}
}

func TestExpireUnknownAndResolveRaceHasOneTerminalLedgerTransition(t *testing.T) {
	ttl := time.Minute
	svc, merchantID := newQuotaFixture(t, 100)
	svc.UnknownHoldTTL = &ttl
	ctx := context.Background()
	const key = "expire-resolve-race"
	if _, _, err := svc.Reserve(ctx, merchantID, key, 20, ""); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if _, _, err := svc.MarkUnknown(ctx, merchantID, key); err != nil {
		t.Fatalf("mark unknown: %v", err)
	}
	past := time.Now().UTC().Add(-time.Hour)
	if err := svc.DB.Model(&schema.MerchantQuotaHolds{}).
		Where("merchant_id = ? AND idempotency_key = ?", merchantID, key).
		Update("updated_at", past).Error; err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	var expired int
	var expireErr error
	var resolved quota.Hold
	var resolveAcct quota.Account
	var resolveErr error
	var lateSettleErr error
	wg.Add(3)
	go func() {
		defer wg.Done()
		<-start
		expired, _, expireErr = svc.ExpireUnknownHolds(ctx, time.Now().UTC(), 10)
	}()
	go func() {
		defer wg.Done()
		<-start
		resolved, resolveAcct, resolveErr = svc.ResolveUnknown(ctx, merchantID, key, 7, "operator confirmed", "op-user")
	}()
	go func() {
		defer wg.Done()
		<-start
		_, _, lateSettleErr = svc.Settle(ctx, merchantID, key, 20)
	}()
	close(start)
	wg.Wait()
	if expireErr != nil {
		t.Fatalf("expire race: %v", expireErr)
	}
	for name, raceErr := range map[string]error{"resolve": resolveErr, "late settle": lateSettleErr} {
		if raceErr == nil {
			continue
		}
		var ae apperr.Error
		if !errors.As(raceErr, &ae) || ae.Status != 409 {
			t.Fatalf("%s race: %v", name, raceErr)
		}
	}

	var hold schema.MerchantQuotaHolds
	if err := svc.DB.Where("merchant_id = ? AND idempotency_key = ?", merchantID, key).Take(&hold).Error; err != nil {
		t.Fatal(err)
	}
	var releaseEvents, settleEvents int64
	if err := svc.DB.Model(&schema.MerchantQuotaEvents{}).
		Where("merchant_id = ? AND event_type = ? AND idempotency_key = ?", merchantID, quota.EventRelease, key).Count(&releaseEvents).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.DB.Model(&schema.MerchantQuotaEvents{}).
		Where("merchant_id = ? AND event_type = ? AND idempotency_key = ?", merchantID, quota.EventSettle, key).Count(&settleEvents).Error; err != nil {
		t.Fatal(err)
	}
	if releaseEvents+settleEvents != 1 {
		t.Fatalf("terminal events release=%d settle=%d hold=%+v expired=%d", releaseEvents, settleEvents, hold, expired)
	}
	if hold.Status == quota.StatusReleased {
		if releaseEvents != 1 || settleEvents != 0 || expired < 1 || resolveErr == nil || lateSettleErr == nil {
			t.Fatalf("expiry winner release=%d settle=%d expired=%d resolveErr=%v lateSettleErr=%v", releaseEvents, settleEvents, expired, resolveErr, lateSettleErr)
		}
		if resolveAcct != (quota.Account{}) || resolved != (quota.Hold{}) {
			t.Fatalf("failed resolve should return zero values hold=%+v acct=%+v", resolved, resolveAcct)
		}
	} else if hold.Status == quota.StatusSettled {
		if releaseEvents != 0 || settleEvents != 1 || (resolveErr == nil) == (lateSettleErr == nil) {
			t.Fatalf("settle winner release=%d settle=%d expired=%d resolveErr=%v lateSettleErr=%v", releaseEvents, settleEvents, expired, resolveErr, lateSettleErr)
		}
		if hold.SettledUnits == nil {
			t.Fatalf("settled race hold missing actual amount: %+v", hold)
		}
		if resolveErr == nil && *hold.SettledUnits != 7 {
			t.Fatalf("operator won with wrong amount: %+v", hold)
		}
		if lateSettleErr == nil && *hold.SettledUnits != 20 {
			t.Fatalf("late result won with wrong amount: %+v", hold)
		}
	} else {
		t.Fatalf("unexpected raced hold=%+v", hold)
	}
	acct, err := svc.GetAccount(ctx, merchantID)
	if err != nil {
		t.Fatal(err)
	}
	if acct.ReservedUnits != 0 {
		t.Fatalf("terminal race kept reservation: %+v", acct)
	}
	if hold.Status == quota.StatusReleased && acct.AvailableUnits != 100 {
		t.Fatalf("release winner account=%+v", acct)
	}
	if hold.Status == quota.StatusSettled {
		wantAvailable := int64(0)
		switch {
		case hold.SettledUnits != nil && *hold.SettledUnits == 7:
			wantAvailable = 93
		case hold.SettledUnits != nil && *hold.SettledUnits == 20:
			wantAvailable = 80
		default:
			t.Fatalf("settled race account has unexpected amount: hold=%+v account=%+v", hold, acct)
		}
		if acct.AvailableUnits != wantAvailable {
			t.Fatalf("settled race account=%+v want available=%d", acct, wantAvailable)
		}
	}

	// A late result cannot reverse an expiry release; a duplicate confirmed result is idempotent.
	if hold.Status == quota.StatusReleased {
		if _, _, err := svc.Settle(ctx, merchantID, key, 20); err == nil {
			t.Fatal("late settle unexpectedly reopened released hold")
		}
	} else if hold.SettledUnits != nil && *hold.SettledUnits == 20 {
		if _, _, err := svc.Settle(ctx, merchantID, key, 20); err != nil {
			t.Fatalf("settle replay after late result: %v", err)
		}
	} else {
		if _, _, err := svc.Settle(ctx, merchantID, key, 20); err == nil {
			t.Fatal("different late result unexpectedly replayed operator settlement")
		}
	}
}

func TestConcurrentExpireUnknownHoldsReleasesOnce(t *testing.T) {
	ttl := time.Minute
	svc, merchantID := newQuotaFixture(t, 10)
	svc.UnknownHoldTTL = &ttl
	ctx := context.Background()

	// This package database persists across test invocations. Drain previously due rows so
	// the two scanner return counts below describe only the hold created by this test.
	for {
		_, hasMore, err := svc.ExpireUnknownHolds(ctx, time.Now().UTC(), 100)
		if err != nil {
			t.Fatalf("drain expired backlog: %v", err)
		}
		if !hasMore {
			break
		}
	}

	const key = "two-expiry-scanners"
	if _, _, err := svc.Reserve(ctx, merchantID, key, 5, ""); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if _, _, err := svc.MarkUnknown(ctx, merchantID, key); err != nil {
		t.Fatalf("mark unknown: %v", err)
	}
	past := time.Now().UTC().Add(-time.Hour)
	if err := svc.DB.Model(&schema.MerchantQuotaHolds{}).
		Where("merchant_id = ? AND idempotency_key = ?", merchantID, key).
		Update("updated_at", past).Error; err != nil {
		t.Fatal(err)
	}

	type result struct {
		expired int
		err     error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			<-start
			expired, _, err := svc.ExpireUnknownHolds(ctx, time.Now().UTC(), 100)
			results <- result{expired: expired, err: err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	totalExpired := 0
	for got := range results {
		if got.err != nil {
			t.Fatalf("concurrent expire: %v", got.err)
		}
		totalExpired += got.expired
	}
	if totalExpired != 1 {
		t.Fatalf("two scanners must report one effective release, total=%d", totalExpired)
	}

	var hold schema.MerchantQuotaHolds
	if err := svc.DB.Where("merchant_id = ? AND idempotency_key = ?", merchantID, key).Take(&hold).Error; err != nil {
		t.Fatal(err)
	}
	if hold.Status != quota.StatusReleased || hold.SettledUnits != nil {
		t.Fatalf("hold=%+v", hold)
	}
	var releaseEvents int64
	if err := svc.DB.Model(&schema.MerchantQuotaEvents{}).
		Where("merchant_id = ? AND event_type = ? AND idempotency_key = ?", merchantID, quota.EventRelease, key).Count(&releaseEvents).Error; err != nil {
		t.Fatal(err)
	}
	if releaseEvents != 1 {
		t.Fatalf("release events=%d", releaseEvents)
	}
	acct, err := svc.GetAccount(ctx, merchantID)
	if err != nil {
		t.Fatal(err)
	}
	if acct.AvailableUnits != 10 || acct.ReservedUnits != 0 {
		t.Fatalf("account=%+v", acct)
	}
}

func TestUnknownHoldTTLFromEnv(t *testing.T) {
	t.Setenv("QUOTA_UNKNOWN_HOLD_TTL", "")
	if got := quota.UnknownHoldTTLFromEnv(); got != quota.DefaultUnknownHoldTTL {
		t.Fatalf("default got %v", got)
	}
	t.Setenv("QUOTA_UNKNOWN_HOLD_TTL", "24h")
	if got := quota.UnknownHoldTTLFromEnv(); got != 24*time.Hour {
		t.Fatalf("got %v", got)
	}
	t.Setenv("QUOTA_UNKNOWN_HOLD_TTL", "0s")
	if got := quota.UnknownHoldTTLFromEnv(); got != quota.DefaultUnknownHoldTTL {
		t.Fatalf("zero fallback got %v", got)
	}
	t.Setenv("QUOTA_UNKNOWN_HOLD_TTL", "nope")
	if got := quota.UnknownHoldTTLFromEnv(); got != quota.DefaultUnknownHoldTTL {
		t.Fatalf("bad fallback got %v", got)
	}
}
