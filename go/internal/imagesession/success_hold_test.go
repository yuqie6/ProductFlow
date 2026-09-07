package imagesession

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/canonjson"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/quota"
)

func TestGenerationSettlementRequiresQuotaHold(t *testing.T) {
	for _, terminal := range []string{"succeeded", "failed"} {
		for _, retry := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/retry=%t", terminal, retry), func(t *testing.T) {
				pool, db := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_successhold_%d", time.Now().UnixNano()))
				ss := newSessionServerWithDatabase(t, pool, db)
				session, taskID := createQueuedGeneration(t, ss, map[string]any{"prompt": "hold contract", "size": "1024x1024"})
				ctx := context.Background()
				merchantID := auth.MustDevMerchantID(t, db)
				key := generationQuotaKey(taskID, 0)
				if retry {
					if _, _, err := (&quota.Service{DB: db}).Settle(ctx, merchantID, key, imageSessionQuotaUnits); err != nil {
						t.Fatal(err)
					}
					if err := db.Model(&schema.ImageSessionGenerationTasks{}).Where("id = ?", taskID).Updates(map[string]any{"status": "failed", "attempts": 1, "is_retryable": true}).Error; err != nil {
						t.Fatal(err)
					}
					if _, err := ss.svc.Retry(ctx, session.ID, taskID); err != nil {
						t.Fatal(err)
					}
					key = generationQuotaKey(taskID, 1)
				}
				e := Executor{DB: db}
				claimed, attemptID, _, err := e.claim(ctx, taskID)
				if err != nil || !claimed {
					t.Fatalf("claim=%t err=%v", claimed, err)
				}
				// Make only this disposable database's hold unavailable to the task without
				// changing its balance. Restoring the same hold lets the terminal write retry.
				unavailableKey := "unavailable:" + clockid.New()
				if err := db.Model(&schema.MerchantQuotaHolds{}).Where("merchant_id = ? AND idempotency_key = ?", merchantID, key).Update("idempotency_key", unavailableKey).Error; err != nil {
					t.Fatal(err)
				}
				before := loadQuotaAccount(t, db, merchantID)
				groupID := clockid.New()
				finish := func() error { return e.finishSucceeded(ctx, taskID, attemptID, session.ID, groupID) }
				if terminal == "failed" {
					req := map[string]any{}
					hash, err := canonjson.SHA256Hex(req)
					if err != nil {
						t.Fatal(err)
					}
					if _, _, err := e.ensureEffect(ctx, taskID, attemptID, 1, 1, clockid.New(), hash, "test-provider", req); err != nil {
						t.Fatal(err)
					}
					if err := e.markEffect(ctx, taskID, attemptID, 1, "failed", "confirmed failure"); err != nil {
						t.Fatal(err)
					}
					finish = func() error {
						return e.finishFailed(ctx, taskID, attemptID, session.ID, apperr.Validation("confirmed failure"))
					}
				}
				if err := finish(); !apperr.IsNotFound(err) {
					t.Fatalf("missing hold must reject terminal commit: %v", err)
				}
				var task schema.ImageSessionGenerationTasks
				if err := db.Where("id = ?", taskID).Take(&task).Error; err != nil {
					t.Fatal(err)
				}
				if task.Status != "running" || task.ActiveAttemptID == nil || *task.ActiveAttemptID != attemptID || task.FinishedAt != nil || task.ResultGenerationGroupID != nil {
					t.Fatalf("terminal write did not roll back: %+v", task)
				}
				after := loadQuotaAccount(t, db, merchantID)
				if before.AvailableUnits != after.AvailableUnits || before.ReservedUnits != after.ReservedUnits {
					t.Fatal("missing hold changed balance")
				}
				if err := db.Model(&schema.MerchantQuotaHolds{}).Where("merchant_id = ? AND idempotency_key = ?", merchantID, unavailableKey).Update("idempotency_key", key).Error; err != nil {
					t.Fatal(err)
				}
				if err := finish(); err != nil {
					t.Fatal(err)
				}
				if err := db.Where("id = ?", taskID).Take(&task).Error; err != nil {
					t.Fatal(err)
				}
				if task.Status != terminal {
					t.Fatalf("status=%s", task.Status)
				}
				if hold := loadQuotaHold(t, db, merchantID, key); hold.Status != quota.StatusSettled {
					t.Fatalf("hold=%+v", hold)
				}

			})
		}
	}
}
