package quota_test

import (
	"context"
	"errors"
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

func TestExpireUnknownHoldsChargesFullReservedNeverRelease(t *testing.T) {
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
	expired, hasMore, err := svc.ExpireUnknownHolds(ctx, now, 10)
	if err != nil {
		t.Fatalf("expire: %v", err)
	}
	if expired != 1 || hasMore {
		t.Fatalf("expired=%d hasMore=%v", expired, hasMore)
	}

	acct, err := svc.GetAccount(ctx, merchantID)
	if err != nil {
		t.Fatal(err)
	}
	// 100-20-15 + 0 refund on due (full charge) = 65 available; fresh still reserved 15
	if acct.AvailableUnits != 65 || acct.ReservedUnits != 15 {
		t.Fatalf("acct after expire=%+v", acct)
	}

	var due schema.MerchantQuotaHolds
	if err := svc.DB.Where("merchant_id = ? AND idempotency_key = ?", merchantID, "expire-due").Take(&due).Error; err != nil {
		t.Fatal(err)
	}
	if due.Status != quota.StatusSettled || due.SettledUnits == nil || *due.SettledUnits != 20 {
		t.Fatalf("due hold=%+v", due)
	}

	var fresh schema.MerchantQuotaHolds
	if err := svc.DB.Where("merchant_id = ? AND idempotency_key = ?", merchantID, "expire-fresh").Take(&fresh).Error; err != nil {
		t.Fatal(err)
	}
	if fresh.Status != quota.StatusPendingReconciliation {
		t.Fatalf("fresh should stay pending: %+v", fresh)
	}

	// Second pass is no-op (idempotent); still no Release path
	expired2, _, err := svc.ExpireUnknownHolds(ctx, now, 10)
	if err != nil {
		t.Fatalf("expire replay: %v", err)
	}
	if expired2 != 0 {
		t.Fatalf("replay expired=%d", expired2)
	}
	if _, _, err := svc.Release(ctx, merchantID, "expire-due"); err == nil {
		t.Fatal("release of expired settle must fail")
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

	expired, _, err := svc.ExpireUnknownHolds(ctx, time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("expire: %v", err)
	}
	if expired != 0 {
		t.Fatalf("expected skip after op resolve, expired=%d", expired)
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
