package imagesession

import (
	"context"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/quota"
	"gorm.io/gorm"
)

func mustSeedMerchantQuota(t *testing.T, db *gorm.DB, merchantID string, units int64) {
	t.Helper()
	if units <= 0 {
		return
	}
	svc := &quota.Service{DB: db}
	if _, err := svc.Adjust(context.Background(), merchantID, "seed-imagesession-"+clockid.New(), units, "test fixture", ""); err != nil {
		t.Fatalf("seed quota: %v", err)
	}
}

func loadQuotaAccount(t *testing.T, db *gorm.DB, merchantID string) quota.Account {
	t.Helper()
	acct, err := (&quota.Service{DB: db}).GetAccount(context.Background(), merchantID)
	if err != nil {
		t.Fatal(err)
	}
	return acct
}

func loadQuotaHold(t *testing.T, db *gorm.DB, merchantID, key string) schema.MerchantQuotaHolds {
	t.Helper()
	var row schema.MerchantQuotaHolds
	if err := db.Where("merchant_id = ? AND idempotency_key = ?", merchantID, key).Take(&row).Error; err != nil {
		t.Fatalf("load hold %s: %v", key, err)
	}
	return row
}

func countQuotaEvents(t *testing.T, db *gorm.DB, merchantID, eventType, key string) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&schema.MerchantQuotaEvents{}).
		Where("merchant_id = ? AND event_type = ? AND idempotency_key = ?", merchantID, eventType, key).
		Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}
