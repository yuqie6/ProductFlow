package imagesession

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/quota"
)

func TestBillingLifecycleKeepsOneActiveReservation(t *testing.T) {
	for _, terminal := range []string{"succeeded", "cancelled", "unknown"} {
		t.Run(terminal, func(t *testing.T) {
			pool, db := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_billingcycle_%d", time.Now().UnixNano()))
			ss := newSessionServerWithDatabase(t, pool, db)
			ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, db))
			session, taskID := createQueuedGeneration(t, ss, map[string]any{"prompt": "billing lifecycle", "size": "1024x1024"})
			merchantID := auth.MustDevMerchantID(t, db)
			activeCount := func(want int64) {
				t.Helper()
				var count int64
				if err := db.Model(&schema.MerchantQuotaHolds{}).Where("merchant_id=? AND idempotency_key LIKE ? AND status IN ?", merchantID, "image-session-generation:"+taskID+"%", []string{quota.StatusReserved, quota.StatusPendingReconciliation}).Count(&count).Error; err != nil {
					t.Fatal(err)
				}
				if count != want {
					t.Fatalf("active holds=%d want=%d", count, want)
				}
			}
			failed := Executor{DB: db, Media: ss.media, Provider: MockChatProvider{Err: ErrRateLimit}}
			initialKey := generationQuotaKey(taskID, 0)
			for attempt := 1; attempt <= maxAttempts; attempt++ {
				activeCount(1)
				if err := failed.Execute(ctx, taskID); err != nil && !errors.Is(err, queue.ErrBusy) && !errors.Is(err, queue.ErrLater) {
					t.Fatal(err)
				}
				task := generationTaskByID(t, loadSessionDetail(t, ss, session.ID), taskID)
				expected := "queued"
				if attempt == maxAttempts {
					expected = "failed"
				}
				if task.Status != expected {
					t.Fatalf("attempt=%d status=%s", attempt, task.Status)
				}
			}
			activeCount(0)
			if hold := loadQuotaHold(t, db, merchantID, initialKey); hold.Status != quota.StatusSettled {
				t.Fatalf("initial=%+v", hold)
			}
			for manual := 0; manual < 2; manual++ {
				var before schema.ImageSessionGenerationTasks
				if err := db.Where("id=?", taskID).Take(&before).Error; err != nil {
					t.Fatal(err)
				}
				key := generationQuotaKey(taskID, before.Attempts)
				if _, err := ss.svc.Retry(ctx, session.ID, taskID); err != nil {
					t.Fatal(err)
				}
				var current schema.ImageSessionGenerationTasks
				if err := db.Where("id=?", taskID).Take(&current).Error; err != nil {
					t.Fatal(err)
				}
				if current.BillingSeq != before.Attempts {
					t.Fatalf("retry did not persist reservation identity: %d", current.BillingSeq)
				}
				activeCount(1)
				if hold := loadQuotaHold(t, db, merchantID, key); hold.Status != quota.StatusReserved {
					t.Fatalf("manual=%d hold=%+v", manual, hold)
				}
				if manual == 0 {
					if err := failed.Execute(ctx, taskID); err != nil && !errors.Is(err, queue.ErrBusy) && !errors.Is(err, queue.ErrLater) {
						t.Fatal(err)
					}
					activeCount(0)
					if hold := loadQuotaHold(t, db, merchantID, key); hold.Status != quota.StatusSettled {
						t.Fatalf("failed retry hold=%+v", hold)
					}
					continue
				}
				expectedHold := quota.StatusSettled
				switch terminal {
				case "cancelled":
					if _, err := ss.svc.Cancel(ctx, session.ID, taskID); err != nil {
						t.Fatal(err)
					}
					expectedHold = quota.StatusReleased
				default:
					provider := MockChatProvider{}
					if terminal == "unknown" {
						provider.Err = ErrTimeout
						expectedHold = quota.StatusPendingReconciliation
					}
					if err := (Executor{DB: db, Media: ss.media, Provider: provider}).Execute(ctx, taskID); err != nil && !errors.Is(err, queue.ErrBusy) && !errors.Is(err, queue.ErrLater) {
						t.Fatal(err)
					}
				}
				task := generationTaskByID(t, loadSessionDetail(t, ss, session.ID), taskID)
				if task.Status != terminal {
					t.Fatalf("terminal=%s want=%s", task.Status, terminal)
				}
				if hold := loadQuotaHold(t, db, merchantID, key); hold.Status != expectedHold {
					t.Fatalf("terminal hold=%+v", hold)
				}
				var effects int64
				if err := db.Model(&schema.ImageSessionProviderEffects{}).Where("generation_task_id=? AND billing_seq=?", taskID, current.BillingSeq).Count(&effects).Error; err != nil {
					t.Fatal(err)
				}
				if (terminal == "cancelled" && effects != 0) || (terminal != "cancelled" && effects == 0) {
					t.Fatalf("current billing effects=%d terminal=%s", effects, terminal)
				}
				if terminal == "unknown" {
					activeCount(1)
				} else {
					activeCount(0)
				}
			}
		})
	}
}

func TestBillingIdentityColumnsMigrateAndPreserveValues(t *testing.T) {
	pool, db := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_billing_schema_%d", time.Now().UnixNano()))
	ss := newSessionServerWithDatabase(t, pool, db)
	ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, db))
	_, taskID := createQueuedGeneration(t, ss, map[string]any{"prompt": "billing migration", "size": "1024x1024"})
	if err := (Executor{DB: db, Media: ss.media, Provider: MockChatProvider{Err: ErrRateLimit}}).Execute(ctx, taskID); err != nil && !errors.Is(err, queue.ErrBusy) && !errors.Is(err, queue.ErrLater) {
		t.Fatal(err)
	}
	for _, table := range []string{"image_session_generation_tasks", "image_session_provider_effects"} {
		if err := db.Exec("ALTER TABLE " + table + " DROP COLUMN billing_seq").Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := schema.Apply(db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"image_session_generation_tasks", "image_session_provider_effects"} {
		var seq int
		if err := db.Table(table).Select("billing_seq").Scan(&seq).Error; err != nil || seq != 0 {
			t.Fatalf("%s initial sequence=%d err=%v", table, seq, err)
		}
		if err := db.Exec("UPDATE " + table + " SET billing_seq=7").Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := schema.Apply(db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"image_session_generation_tasks", "image_session_provider_effects"} {
		var seq int
		if err := db.Table(table).Select("billing_seq").Scan(&seq).Error; err != nil || seq != 7 {
			t.Fatalf("%s persisted sequence=%d err=%v", table, seq, err)
		}
	}
}
