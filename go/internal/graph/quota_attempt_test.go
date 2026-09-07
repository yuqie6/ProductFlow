package graph

import (
	"context"
	"testing"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/quota"
)

func TestLateGraphAttemptCannotFinalizeNewQuota(t *testing.T) {
	for _, action := range []string{"release", "settle", "unknown"} {
		t.Run(action, func(t *testing.T) {
			ctx := context.Background()
			_, db := testdb.Open(t)
			merchantID := auth.MustDevMerchantID(t, db)
			q := &quota.Service{DB: db}
			if _, err := q.Adjust(ctx, merchantID, clockid.New(), 10, "fixture", ""); err != nil {
				t.Fatal(err)
			}
			nodeID, oldAttempt, newAttempt := clockid.New(), clockid.New(), clockid.New()
			oldKey, newKey := imageNodeQuotaKey(nodeID, oldAttempt), imageNodeQuotaKey(nodeID, newAttempt)
			if _, _, err := q.Reserve(ctx, merchantID, oldKey, 1, quota.DefaultPriceVersionID); err != nil {
				t.Fatal(err)
			}
			if _, _, err := q.Release(ctx, merchantID, oldKey); err != nil {
				t.Fatal(err)
			}
			if _, _, err := q.Reserve(ctx, merchantID, newKey, 1, quota.DefaultPriceVersionID); err != nil {
				t.Fatal(err)
			}
			before, err := q.GetAccount(ctx, merchantID)
			if err != nil {
				t.Fatal(err)
			}
			e := Executor{DB: db}
			var actionErr error
			switch action {
			case "release":
				actionErr = e.releaseImageQuota(ctx, merchantID, nodeID, oldAttempt)
			case "settle":
				actionErr = e.settleImageQuota(ctx, merchantID, nodeID, oldAttempt)
			case "unknown":
				actionErr = e.markImageQuotaUnknown(ctx, merchantID, nodeID, oldAttempt)
			}
			if action == "release" && actionErr != nil {
				t.Fatal(actionErr)
			}
			if action != "release" && actionErr == nil {
				t.Fatal("released old hold must reject settlement/unknown")
			}
			var hold schema.MerchantQuotaHolds
			if err := db.Where("merchant_id=? AND idempotency_key=?", merchantID, newKey).Take(&hold).Error; err != nil {
				t.Fatal(err)
			}
			if hold.Status != quota.StatusReserved {
				t.Fatalf("late %s changed new hold to %s", action, hold.Status)
			}
			after, err := q.GetAccount(ctx, merchantID)
			if err != nil {
				t.Fatal(err)
			}
			if before.AvailableUnits != after.AvailableUnits || before.ReservedUnits != after.ReservedUnits {
				t.Fatalf("late %s changed balances", action)
			}
		})
	}
}
