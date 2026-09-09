package imagesession

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/quota"
)

func TestStaleAttemptFailureCannotRequeueOrFinalizeQuota(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		name := "auto_retry"
		if terminal {
			name = "terminal"
		}
		t.Run(name, func(t *testing.T) {
			ss := newSessionServer(t)
			session, taskID := createQueuedGeneration(t, ss, map[string]any{"prompt": "attempt fence", "size": "1024x1024"})
			current := markStaleRunning(t, ss, taskID, "claimed", 0, nil)
			var cause error = errors.New("retryable failure from old worker")
			if terminal {
				cause = apperr.Validation("invalid input from old worker")
			}
			if err := (Executor{DB: ss.db}).finishFailed(context.Background(), taskID, clockid.New(), session.ID, cause); err != nil {
				t.Fatal(err)
			}
			var status, active string
			if err := ss.pool.QueryRow(context.Background(), "SELECT status,active_attempt_id FROM image_session_generation_tasks WHERE id=$1", taskID).Scan(&status, &active); err != nil {
				t.Fatal(err)
			}
			if status != "running" || active != current {
				t.Fatalf("current attempt changed: status=%s active=%s", status, active)
			}
			var riverJobs int
			if err := ss.pool.QueryRow(context.Background(), "SELECT count(*) FROM river_job WHERE args ->> 'aggregate_id'=$1", taskID).Scan(&riverJobs); err != nil {
				t.Fatal(err)
			}
			if riverJobs != 0 {
				t.Fatalf("stale worker created %d river jobs", riverJobs)
			}
			merchantID := auth.MustDevMerchantID(t, ss.db)
			hold := loadQuotaHold(t, ss.db, merchantID, generationQuotaKey(taskID, 0))
			if hold.Status != quota.StatusReserved {
				t.Fatalf("stale worker changed hold to %s", hold.Status)
			}
		})
	}
}

func TestTerminalFailureQuotaWriteRollsBackTask(t *testing.T) {
	ctx := context.Background()
	ss := newSessionServer(t)
	session, taskID := createQueuedGeneration(t, ss, map[string]any{"prompt": "atomic failure", "size": "1024x1024"})
	attemptID := markStaleRunning(t, ss, taskID, "claimed", 0, nil)
	merchantID := auth.MustDevMerchantID(t, ss.db)
	key := generationQuotaKey(taskID, 0)
	before := loadQuotaAccount(t, ss.db, merchantID)
	const constraint = "test_imagesession_terminal_quota_failure"
	if _, err := ss.pool.Exec(ctx, "ALTER TABLE merchant_quota_holds ADD CONSTRAINT "+constraint+" CHECK (idempotency_key <> '"+key+"' OR status = 'reserved')"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := ss.pool.Exec(context.Background(), "ALTER TABLE merchant_quota_holds DROP CONSTRAINT IF EXISTS "+constraint); err != nil {
			t.Error(err)
		}
	})
	executor := Executor{DB: ss.db}
	cause := apperr.Validation("invalid generation input")
	if err := executor.finishFailed(ctx, taskID, attemptID, session.ID, cause); err == nil {
		t.Fatal("expected quota write error")
	}
	var status, active string
	if err := ss.pool.QueryRow(ctx, "SELECT status,active_attempt_id FROM image_session_generation_tasks WHERE id=$1", taskID).Scan(&status, &active); err != nil {
		t.Fatal(err)
	}
	if status != "running" || active != attemptID {
		t.Fatalf("partial terminal committed: status=%s active=%s", status, active)
	}
	after := loadQuotaAccount(t, ss.db, merchantID)
	if before.AvailableUnits != after.AvailableUnits || before.ReservedUnits != after.ReservedUnits {
		t.Fatal("quota balances did not roll back")
	}
	if _, err := ss.pool.Exec(ctx, "ALTER TABLE merchant_quota_holds DROP CONSTRAINT "+constraint); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := executor.finishFailed(ctx, taskID, attemptID, session.ID, cause); err != nil {
			t.Fatal(err)
		}
	}
	if err := ss.pool.QueryRow(ctx, "SELECT status FROM image_session_generation_tasks WHERE id=$1", taskID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "failed" {
		t.Fatalf("status=%s", status)
	}
	if hold := loadQuotaHold(t, ss.db, merchantID, key); hold.Status != quota.StatusReleased {
		t.Fatalf("hold=%s", hold.Status)
	}
	if n := countQuotaEvents(t, ss.db, merchantID, quota.EventRelease, key); n != 1 {
		t.Fatalf("release events=%d", n)
	}
}

func TestTerminalQuotaTransitionsAreAtomic(t *testing.T) {
	for _, mode := range []string{"success", "unknown", "recovery"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			ss := newSessionServer(t)
			session, taskID := createQueuedGeneration(t, ss, map[string]any{"prompt": "terminal consistency", "size": "1024x1024"})
			attemptID := markStaleRunning(t, ss, taskID, "provider_call", 0, nil)
			merchantID := auth.MustDevMerchantID(t, ss.db)
			key := generationQuotaKey(taskID, 0)
			const constraint = "test_imagesession_terminal_transition"
			if _, err := ss.pool.Exec(ctx, "ALTER TABLE merchant_quota_holds ADD CONSTRAINT "+constraint+" CHECK (idempotency_key <> '"+key+"' OR status = 'reserved')"); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := ss.pool.Exec(context.Background(), "ALTER TABLE merchant_quota_holds DROP CONSTRAINT IF EXISTS "+constraint); err != nil {
					t.Error(err)
				}
			})
			e := Executor{DB: ss.db}
			groupID := clockid.New()
			finish := func() error {
				switch mode {
				case "success":
					return e.finishSucceeded(ctx, taskID, attemptID, session.ID, groupID)
				case "unknown":
					return e.finishUnknown(ctx, taskID, attemptID, session.ID)
				default:
					_, err := recoverImageTaskState(ctx, ss.db, taskID, time.Now().UTC().Add(-time.Minute))
					return err
				}
			}
			if err := finish(); err == nil {
				t.Fatal("quota write error must propagate")
			}
			var status, active string
			if err := ss.pool.QueryRow(ctx, "SELECT status,active_attempt_id FROM image_session_generation_tasks WHERE id=$1", taskID).Scan(&status, &active); err != nil {
				t.Fatal(err)
			}
			if status != "running" || active != attemptID {
				t.Fatalf("partial task state=%s active=%s", status, active)
			}
			if hold := loadQuotaHold(t, ss.db, merchantID, key); hold.Status != quota.StatusReserved {
				t.Fatalf("partial hold=%s", hold.Status)
			}
			if _, err := ss.pool.Exec(ctx, "ALTER TABLE merchant_quota_holds DROP CONSTRAINT "+constraint); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if err := finish(); err != nil {
					t.Fatal(err)
				}
			}
			if err := ss.pool.QueryRow(ctx, "SELECT status FROM image_session_generation_tasks WHERE id=$1", taskID).Scan(&status); err != nil {
				t.Fatal(err)
			}
			wantStatus, wantHold, event := "unknown", quota.StatusPendingReconciliation, quota.EventMarkUnknown
			if mode == "success" {
				wantStatus, wantHold, event = "succeeded", quota.StatusSettled, quota.EventSettle
			}
			if status != wantStatus {
				t.Fatalf("task=%s want=%s", status, wantStatus)
			}
			if hold := loadQuotaHold(t, ss.db, merchantID, key); hold.Status != wantHold {
				t.Fatalf("hold=%s want=%s", hold.Status, wantHold)
			}
			if n := countQuotaEvents(t, ss.db, merchantID, event, key); n != 1 {
				t.Fatalf("terminal events=%d", n)
			}
		})
	}
}
