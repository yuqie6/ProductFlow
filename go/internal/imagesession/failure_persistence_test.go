package imagesession

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"gorm.io/gorm"
)

func TestConsumeDoesNotAcknowledgeFailedTaskPersistence(t *testing.T) {
	for _, phase := range []string{"auto_retry_queued", "failed"} {
		t.Run(phase, func(t *testing.T) {
			pool, db := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_failpersist_%d", time.Now().UnixNano()))
			ss := newSessionServerWithDatabase(t, pool, db)
			session, taskID := createQueuedGeneration(t, ss, map[string]any{"prompt": phase, "size": "1024x1024"})
			if phase == "failed" {
				if err := db.Model(&schema.ImageSessionGenerationTasks{}).Where("id = ?", taskID).
					Update("attempts", maxAttempts-1).Error; err != nil {
					t.Fatal(err)
				}
			}
			ctx := imageSessionQueueContext(t, db)
			job := stageImageSessionJob(t, db, taskID)
			injected := errors.New("task failure persistence unavailable")
			const callback = "test:failure-persistence"
			writes := 0
			if err := db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
				values, ok := tx.Statement.Dest.(map[string]any)
				if ok && tx.Statement.Table == "image_session_generation_tasks" && values["progress_phase"] == phase {
					writes++
					tx.AddError(injected)
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Callback().Update().Remove(callback) })
			executor := Executor{DB: db, Media: ss.media, Provider: MockChatProvider{Err: ErrRateLimit}}
			err := executeImageSessionJob(ctx, job, executor)
			if !errors.Is(err, injected) || writes != 1 {
				t.Fatalf("consume error=%v failed writes=%d", err, writes)
			}
			task := generationTaskByID(t, loadSessionDetail(t, ss, session.ID), taskID)
			if task.Status != "running" || riverImageSessionJobCount(t, ss, taskID) != 1 {
				t.Fatalf("uncommitted failure changed task/job: task=%s jobs=%d", task.Status, riverImageSessionJobCount(t, ss, taskID))
			}
			if len(task.ProviderEffects) != 1 || task.ProviderEffects[0].EffectResult != "failed" {
				t.Fatalf("confirmed provider failure changed: %+v", task.ProviderEffects)
			}
		})
	}
}

func TestConsumeCancellationKeepsUnknownAndReleasesEnvelope(t *testing.T) {
	pool, db := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_cancel_finalize_%d", time.Now().UnixNano()))
	ss := newSessionServerWithDatabase(t, pool, db)
	session, taskID := createQueuedGeneration(t, ss, map[string]any{"prompt": "取消后的信封收尾", "size": "1024x1024"})
	ctx := imageSessionQueueContext(t, ss.db)
	job := stageImageSessionJob(t, ss.db, taskID)
	actorCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	executor := Executor{DB: ss.db, Media: ss.media, Provider: contextCancelProvider{cancel: cancel}}
	if err := executeImageSessionJob(actorCtx, job, executor); err != nil {
		t.Fatal(err)
	}
	task := generationTaskByID(t, loadSessionDetail(t, ss, session.ID), taskID)
	if task.Status != "unknown" || task.IsRetryable || len(task.ProviderEffects) != 1 || task.ProviderEffects[0].EffectResult != "unknown" {
		t.Fatalf("uncertain provider result changed: %+v", task)
	}
	if riverImageSessionJobCount(t, ss, taskID) != 1 {
		t.Fatalf("unknown job count=%d", riverImageSessionJobCount(t, ss, taskID))
	}
	// A repeated delivery reaches the business executor but must not replay the
	// provider after the durable unknown result.
	if err := executeImageSessionJob(ctx, job, executor); err != nil {
		t.Fatal(err)
	}
}
