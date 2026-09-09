package imagesession

import (
	"context"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/quota"
)

func TestGenerationCommandsRollBackQuotaWithDispatch(t *testing.T) {
	for _, retry := range []bool{false, true} {
		name := "generate"
		if retry {
			name = "retry"
		}
		t.Run(name, func(t *testing.T) {
			ss := newSessionServer(t)
			merchantID := auth.MustDevMerchantID(t, ss.db)
			ctx := auth.WithMerchantID(context.Background(), merchantID)
			session, taskID := createQueuedGeneration(t, ss, map[string]any{"prompt": "atomic command", "size": "1024x1024"})
			if !retry {
				var err error
				session, err = ss.svc.Create(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
			}
			if retry {
				if _, err := ss.pool.Exec(ctx, "UPDATE image_session_generation_tasks SET status='failed', attempts=1, is_retryable=true WHERE id=$1", taskID); err != nil {
					t.Fatal(err)
				}
				if _, _, err := (&quota.Service{DB: ss.db}).Release(ctx, merchantID, generationQuotaKey(taskID, 0)); err != nil {
					t.Fatal(err)
				}
			}
			before := loadQuotaAccount(t, ss.db, merchantID)
			var holdsBefore int
			if err := ss.pool.QueryRow(ctx, "SELECT count(*) FROM merchant_quota_holds WHERE merchant_id=$1", merchantID).Scan(&holdsBefore); err != nil {
				t.Fatal(err)
			}
			// NOT VALID leaves other fixtures intact; this isolated package database rejects new River jobs only.
			const constraint = "test_imagesession_command_river_job"
			if _, err := ss.pool.Exec(ctx, "ALTER TABLE river_job ADD CONSTRAINT "+constraint+" CHECK (COALESCE(args ->> 'actor', '') <> 'run_image_session_generation_task') NOT VALID"); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := ss.pool.Exec(context.Background(), "ALTER TABLE river_job DROP CONSTRAINT IF EXISTS "+constraint); err != nil {
					t.Error(err)
				}
			})
			invoke := func() error {
				if retry {
					_, err := ss.svc.Retry(ctx, session.ID, taskID)
					return err
				}
				_, err := ss.svc.Generate(ctx, session.ID, GenerateRequest{Prompt: "new generation", Size: "1024x1024"})
				return err
			}
			if err := invoke(); err == nil || !strings.Contains(err.Error(), constraint) {
				t.Fatalf("expected dispatch constraint error, got %v", err)
			}
			after := loadQuotaAccount(t, ss.db, merchantID)
			if before.AvailableUnits != after.AvailableUnits || before.ReservedUnits != after.ReservedUnits {
				t.Fatal("quota balances partially committed")
			}
			var holdsAfter, tasks, riverJobs int
			if err := ss.pool.QueryRow(ctx, "SELECT count(*) FROM merchant_quota_holds WHERE merchant_id=$1", merchantID).Scan(&holdsAfter); err != nil {
				t.Fatal(err)
			}
			if holdsAfter != holdsBefore {
				t.Fatalf("failed command left hold rows: before=%d after=%d", holdsBefore, holdsAfter)
			}
			if err := ss.pool.QueryRow(ctx, "SELECT count(*) FROM image_session_generation_tasks WHERE session_id=$1", session.ID).Scan(&tasks); err != nil {
				t.Fatal(err)
			}
			wantTasks := 0
			if retry {
				wantTasks = 1
			}
			if tasks != wantTasks {
				t.Fatalf("failed command left %d tasks", tasks)
			}
			if err := ss.pool.QueryRow(ctx, "SELECT count(*) FROM river_job WHERE args ->> 'aggregate_id'=$1", taskID).Scan(&riverJobs); err != nil {
				t.Fatal(err)
			}
			if riverJobs != 0 {
				t.Fatalf("partial river jobs=%d", riverJobs)
			}
			if retry {
				var status string
				var billingSeq int
				if err := ss.pool.QueryRow(ctx, "SELECT status,billing_seq FROM image_session_generation_tasks WHERE id=$1", taskID).Scan(&status, &billingSeq); err != nil {
					t.Fatal(err)
				}
				if status != "failed" || billingSeq != 0 {
					t.Fatalf("partial retry status=%s billing=%d", status, billingSeq)
				}
			}
			if _, err := ss.pool.Exec(ctx, "ALTER TABLE river_job DROP CONSTRAINT "+constraint); err != nil {
				t.Fatal(err)
			}
			if err := invoke(); err != nil {
				t.Fatal(err)
			}
			if retry {
				var billingSeq int
				if err := ss.pool.QueryRow(ctx, "SELECT billing_seq FROM image_session_generation_tasks WHERE id=$1", taskID).Scan(&billingSeq); err != nil || billingSeq != 1 {
					t.Fatalf("committed retry billing=%d err=%v", billingSeq, err)
				}
				if hold := loadQuotaHold(t, ss.db, merchantID, generationQuotaKey(taskID, 1)); hold.Status != quota.StatusReserved {
					t.Fatalf("retry hold=%s", hold.Status)
				}
			}
		})
	}
}

func TestCancelQuotaFailureRollsBackGeneration(t *testing.T) {
	ss := newSessionServer(t)
	merchantID := auth.MustDevMerchantID(t, ss.db)
	ctx := auth.WithMerchantID(context.Background(), merchantID)
	session, taskID := createQueuedGeneration(t, ss, map[string]any{"prompt": "cancel atomicity", "size": "1024x1024"})
	key := generationQuotaKey(taskID, 0)
	const constraint = "test_imagesession_cancel_quota"
	if _, err := ss.pool.Exec(ctx, "ALTER TABLE merchant_quota_holds ADD CONSTRAINT "+constraint+" CHECK (idempotency_key <> '"+key+"' OR status = 'reserved')"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := ss.pool.Exec(context.Background(), "ALTER TABLE merchant_quota_holds DROP CONSTRAINT IF EXISTS "+constraint); err != nil {
			t.Error(err)
		}
	})
	if _, err := ss.svc.Cancel(ctx, session.ID, taskID); err == nil {
		t.Fatal("quota error must propagate")
	}
	var status string
	if err := ss.pool.QueryRow(ctx, "SELECT status FROM image_session_generation_tasks WHERE id=$1", taskID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "queued" {
		t.Fatalf("partial cancel status=%s", status)
	}
	if hold := loadQuotaHold(t, ss.db, merchantID, key); hold.Status != quota.StatusReserved {
		t.Fatalf("hold=%s", hold.Status)
	}
	if _, err := ss.pool.Exec(ctx, "ALTER TABLE merchant_quota_holds DROP CONSTRAINT "+constraint); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := ss.svc.Cancel(ctx, session.ID, taskID); err != nil {
			t.Fatal(err)
		}
	}
	if n := countQuotaEvents(t, ss.db, merchantID, quota.EventRelease, key); n != 1 {
		t.Fatalf("release events=%d", n)
	}
}

func TestConcurrentRetryRetainsWinningReservation(t *testing.T) {
	ss := newSessionServer(t)
	merchantID := auth.MustDevMerchantID(t, ss.db)
	ctx := auth.WithMerchantID(context.Background(), merchantID)
	session, taskID := createQueuedGeneration(t, ss, map[string]any{"prompt": "retry race", "size": "1024x1024"})
	if _, err := ss.pool.Exec(ctx, "UPDATE image_session_generation_tasks SET status='failed', attempts=1, is_retryable=true WHERE id=$1", taskID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := (&quota.Service{DB: ss.db}).Release(ctx, merchantID, generationQuotaKey(taskID, 0)); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { <-start; _, err := ss.svc.Retry(ctx, session.ID, taskID); results <- err }()
	}
	close(start)
	successes := 0
	for i := 0; i < 2; i++ {
		if <-results == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful retries=%d", successes)
	}
	key := generationQuotaKey(taskID, 1)
	if hold := loadQuotaHold(t, ss.db, merchantID, key); hold.Status != quota.StatusReserved {
		t.Fatalf("winning hold=%s", hold.Status)
	}
	if n := countQuotaEvents(t, ss.db, merchantID, quota.EventRelease, key); n != 0 {
		t.Fatalf("losing retry released winning hold: events=%d", n)
	}
	var riverJobs int
	if err := ss.pool.QueryRow(ctx, "SELECT count(*) FROM river_job WHERE args ->> 'aggregate_id'=$1", taskID).Scan(&riverJobs); err != nil {
		t.Fatal(err)
	}
	if riverJobs != 1 {
		t.Fatalf("river jobs=%d", riverJobs)
	}
}
