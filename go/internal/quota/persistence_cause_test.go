package quota_test

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/quota"
)

func TestQuotaPersistenceRetainsDatabaseCause(t *testing.T) {
	for _, operation := range []string{"adjust", "reserve", "settle", "release", "unknown"} {
		for _, table := range []string{"merchant_quota_accounts", "merchant_quota_holds", "merchant_quota_events"} {
			if (operation == "unknown" && table == "merchant_quota_accounts") || (operation == "adjust" && table == "merchant_quota_holds") {
				continue
			} // MarkUnknown does not change balances.
			t.Run(operation+"/"+table, func(t *testing.T) {
				ctx := context.Background()
				svc, merchantID := newQuotaFixture(t, 100)
				key := clockid.New()
				if operation != "reserve" && operation != "adjust" {
					if _, _, err := svc.Reserve(ctx, merchantID, key, 5, quota.DefaultPriceVersionID); err != nil {
						t.Fatal(err)
					}
				}
				before, err := svc.GetAccount(ctx, merchantID)
				if err != nil {
					t.Fatal(err)
				}
				var beforeEvents int64
				if err := svc.DB.Model(&schema.MerchantQuotaEvents{}).Where("merchant_id=?", merchantID).Count(&beforeEvents).Error; err != nil {
					t.Fatal(err)
				}
				constraint := "test_quota_cause"
				ddl := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s CHECK (merchant_id <> '%s') NOT VALID", table, constraint, merchantID)
				if err := svc.DB.Exec(ddl).Error; err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := svc.DB.Exec("ALTER TABLE " + table + " DROP CONSTRAINT " + constraint).Error; err != nil {
						t.Error(err)
					}
				})
				switch operation {
				case "adjust":
					_, err = svc.Adjust(ctx, merchantID, key, 5, "cause regression", "")
				case "reserve":
					_, _, err = svc.Reserve(ctx, merchantID, key, 5, quota.DefaultPriceVersionID)
				case "settle":
					_, _, err = svc.Settle(ctx, merchantID, key, 5)
				case "release":
					_, _, err = svc.Release(ctx, merchantID, key)
				case "unknown":
					_, _, err = svc.MarkUnknown(ctx, merchantID, key)
				}
				var pgErr *pgconn.PgError
				var appErr apperr.Error
				if !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != constraint {
					t.Fatalf("database cause lost: %v", err)
				}
				if !errors.As(err, &appErr) || appErr.Status != 500 {
					t.Fatalf("public error contract lost: %v", err)
				}
				recorder := httptest.NewRecorder()
				httpCtx, _ := gin.CreateTestContext(recorder)
				httpx.AbortErr(httpCtx, err)
				if recorder.Code != 500 || !strings.Contains(recorder.Body.String(), appErr.Detail) || strings.Contains(recorder.Body.String(), constraint) || strings.Contains(recorder.Body.String(), "SQLSTATE") {
					t.Fatalf("public response=%d %s", recorder.Code, recorder.Body.String())
				}

				after, err := svc.GetAccount(ctx, merchantID)
				if err != nil {
					t.Fatal(err)
				}
				var afterEvents, holds int64
				if err := svc.DB.Model(&schema.MerchantQuotaEvents{}).Where("merchant_id=?", merchantID).Count(&afterEvents).Error; err != nil {
					t.Fatal(err)
				}
				if err := svc.DB.Model(&schema.MerchantQuotaHolds{}).Where("merchant_id=? AND idempotency_key=?", merchantID, key).Count(&holds).Error; err != nil {
					t.Fatal(err)
				}
				if before.AvailableUnits != after.AvailableUnits || before.ReservedUnits != after.ReservedUnits || beforeEvents != afterEvents {
					t.Fatal("failed quota transaction changed account or events")
				}
				if operation == "reserve" || operation == "adjust" {
					if holds != 0 {
						t.Fatalf("failed reserve left %d holds", holds)
					}
				} else {
					var hold schema.MerchantQuotaHolds
					if err := svc.DB.Where("merchant_id=? AND idempotency_key=?", merchantID, key).Take(&hold).Error; err != nil {
						t.Fatal(err)
					}
					if hold.Status != quota.StatusReserved {
						t.Fatalf("failed operation changed hold: %s", hold.Status)
					}
				}
			})
		}
	}
}
