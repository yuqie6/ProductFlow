package imagesession

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/quota"
	"gorm.io/gorm"
)

func TestImageCheckpointTransactionBoundaries(t *testing.T) {
	for _, mode := range []string{"rollback", "continuation"} {
		t.Run(mode, func(t *testing.T) {
			pool, db := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_cptx_%d", time.Now().UnixNano()))
			ss := newSessionServerWithDatabase(t, pool, db)
			session, taskID := createQueuedGeneration(t, ss, map[string]any{"prompt": mode, "size": "1024x1024", "generation_count": 2})
			job := stageImageSessionJob(t, db, taskID)
			ctx := imageSessionQueueContext(t, db)
			executor := Executor{DB: db, Media: ss.media, Provider: MockChatProvider{}}
			if mode == "rollback" {
				injected := errors.New("checkpoint queue write failed")
				const callback = "test:checkpoint-queue-write-failure"
				if err := db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
					values, ok := tx.Statement.Dest.(map[string]any)
					if ok && tx.Statement.Table == "image_session_generation_tasks" && values["status"] == "queued" {
						tx.AddError(injected)
					}
				}); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = db.Callback().Update().Remove(callback) })
				if err := executeImageSessionJob(ctx, job, executor); !errors.Is(err, injected) {
					t.Fatalf("checkpoint error=%v", err)
				}
				var row schema.ImageSessionGenerationTasks
				if err := db.Where("id = ?", taskID).Take(&row).Error; err != nil {
					t.Fatal(err)
				}
				if row.Status != "running" || row.CompletedCandidates != 1 || row.ActiveAttemptID == nil {
					t.Fatalf("checkpoint not rolled back: %+v", row)
				}
				if riverImageSessionJobCount(t, ss, taskID) != 1 {
					t.Fatalf("queue jobs=%d", riverImageSessionJobCount(t, ss, taskID))
				}
				return
			}

			if err := executeImageSessionJob(ctx, job, executor); err == nil {
				t.Fatal("first candidate must yield")
			} else {
				var snooze *river.JobSnoozeError
				if !errors.As(err, &snooze) {
					t.Fatalf("yield: %v", err)
				}
			}
			if err := executeImageSessionJob(ctx, job, executor); err != nil {
				t.Fatal(err)
			}
			task := generationTaskByID(t, loadSessionDetail(t, ss, session.ID), taskID)
			if task.Status != "succeeded" || task.CompletedCandidates != 2 || task.Attempts != 1 {
				t.Fatalf("continuation task=%+v", task)
			}
		})
	}
}

func TestImageSessionOldRiverJobCannotExecuteAfterExplicitRetry(t *testing.T) {
	ss := newSessionServer(t)
	ctx := imageSessionQueueContext(t, ss.db)
	session, taskID := createQueuedGeneration(t, ss, map[string]any{"prompt": "old river job", "size": "1024x1024"})
	oldJob := stageImageSessionJob(t, ss.db, taskID)
	if _, err := ss.pool.Exec(ctx, `
		UPDATE image_session_generation_tasks
		SET status='failed', attempts=1, is_retryable=TRUE
		WHERE id=$1
	`, taskID); err != nil {
		t.Fatal(err)
	}
	merchantID := auth.MustDevMerchantID(t, ss.db)
	if _, _, err := (&quota.Service{DB: ss.db}).Release(ctx, merchantID, generationQuotaKey(taskID, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := ss.svc.Retry(ctx, session.ID, taskID); err != nil {
		t.Fatal(err)
	}
	if err := executeImageSessionJob(ctx, oldJob, Executor{DB: ss.db, Media: ss.media, Provider: MockChatProvider{}}); err != nil {
		t.Fatalf("old job should be acknowledged as superseded: %v", err)
	}
	var status, executionID string
	if err := ss.pool.QueryRow(ctx, `SELECT status, queue_execution_id FROM image_session_generation_tasks WHERE id=$1`, taskID).Scan(&status, &executionID); err != nil {
		t.Fatal(err)
	}
	if status != "queued" || executionID == oldJob.Args.ExecutionID {
		t.Fatalf("old job changed current execution: status=%s execution=%s old=%s", status, executionID, oldJob.Args.ExecutionID)
	}
	var raw []byte
	var newID int64
	if err := ss.pool.QueryRow(ctx, `
		SELECT id, args FROM river_job
		WHERE kind='productflow_task' AND args ->> 'actor'=$1 AND args ->> 'aggregate_id'=$2
		  AND args ->> 'execution_id'=$3
		ORDER BY id DESC LIMIT 1
	`, queue.ActorImageSession, taskID, executionID).Scan(&newID, &raw); err != nil {
		t.Fatal(err)
	}
	var args queue.TaskArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		t.Fatal(err)
	}
	newJob := queue.RiverJob{ID: newID, Args: args}
	if err := executeImageSessionJob(ctx, newJob, Executor{DB: ss.db, Media: ss.media, Provider: MockChatProvider{}}); err != nil {
		t.Fatal(err)
	}
	if got := generationTaskByID(t, loadSessionDetail(t, ss, session.ID), taskID); got.Status != "succeeded" {
		t.Fatalf("new retry status=%s", got.Status)
	}
}
