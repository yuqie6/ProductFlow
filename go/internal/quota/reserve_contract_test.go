package quota_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/quota"
)

func TestReserveReplayRequiresOriginalPriceVersion(t *testing.T) {
	for _, status := range []string{"reserved", "settled", "released", "unknown"} {
		t.Run(status, func(t *testing.T) {
			ctx := context.Background()
			svc, merchantID := newQuotaFixture(t, 100)
			version := schema.QuotaPriceVersions{ID: clockid.New(), Label: "Replay contract", Currency: quota.CurrencyInternalUnits, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
			if err := svc.DB.Create(&version).Error; err != nil {
				t.Fatal(err)
			}
			key := clockid.New()
			hold, _, err := svc.Reserve(ctx, merchantID, key, 10, "")
			if err != nil {
				t.Fatal(err)
			}
			switch status {
			case "settled":
				_, _, err = svc.Settle(ctx, merchantID, key, 6)
			case "released":
				_, _, err = svc.Release(ctx, merchantID, key)
			case "unknown":
				_, _, err = svc.MarkUnknown(ctx, merchantID, key)
			}
			if err != nil {
				t.Fatal(err)
			}
			before, err := svc.GetAccount(ctx, merchantID)
			if err != nil {
				t.Fatal(err)
			}
			var eventsBefore int64
			if err := svc.DB.Model(&schema.MerchantQuotaEvents{}).Where("merchant_id = ?", merchantID).Count(&eventsBefore).Error; err != nil {
				t.Fatal(err)
			}
			_, _, err = svc.Reserve(ctx, merchantID, key, 10, version.ID)
			var appErr apperr.Error
			if !errors.As(err, &appErr) || appErr.Status != 409 {
				t.Errorf("different price version accepted: %v", err)
			}
			replayed, after, err := svc.Reserve(ctx, merchantID, key, 10, "  "+quota.DefaultPriceVersionID+"  ")
			if err != nil {
				t.Fatal(err)
			}
			if replayed.ID != hold.ID || replayed.PriceVersionID != hold.PriceVersionID || after != before {
				t.Fatalf("replay changed hold/account: hold=%+v before=%+v after=%+v", replayed, before, after)
			}
			var eventsAfter, holds int64
			if err := svc.DB.Model(&schema.MerchantQuotaEvents{}).Where("merchant_id = ?", merchantID).Count(&eventsAfter).Error; err != nil {
				t.Fatal(err)
			}
			if err := svc.DB.Model(&schema.MerchantQuotaHolds{}).Where("merchant_id = ?", merchantID).Count(&holds).Error; err != nil {
				t.Fatal(err)
			}
			if eventsAfter != eventsBefore || holds != 1 {
				t.Fatalf("replay wrote ledger: events=%d/%d holds=%d", eventsAfter, eventsBefore, holds)
			}
		})
	}
}
