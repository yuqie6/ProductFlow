package auth

import (
	"context"
	"errors"
	"testing"
)

func TestEnsureMerchantQuotaHook(t *testing.T) {
	h := HTTP{}
	if err := h.ensureMerchantQuota(context.Background(), "m1"); err != nil {
		t.Fatalf("nil hook: %v", err)
	}

	var got string
	h.EnsureMerchantQuota = func(ctx context.Context, merchantID string) error {
		got = merchantID
		return nil
	}
	if err := h.ensureMerchantQuota(context.Background(), "m-seed"); err != nil {
		t.Fatal(err)
	}
	if got != "m-seed" {
		t.Fatalf("got %q", got)
	}

	h.EnsureMerchantQuota = func(ctx context.Context, merchantID string) error {
		return errors.New("seed failed")
	}
	if err := h.ensureMerchantQuota(context.Background(), "m2"); err == nil || err.Error() != "seed failed" {
		t.Fatalf("want seed failed, got %v", err)
	}
}
