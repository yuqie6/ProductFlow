package imagesession

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/queue"
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
			ctx := context.Background()
			var dispatch queue.Dispatch
			if err := db.Transaction(func(tx *gorm.DB) error {
				var err error
				dispatch, err = queue.StageForActor(ctx, tx, queue.ActorImageSession, taskID, 0)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if summary, err := queue.RunDispatcherOnce(ctx, pool, func(string, string) error { return nil }, 10); err != nil || summary.Sent != 1 {
				t.Fatalf("dispatch=%+v err=%v", summary, err)
			}
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
			err := queue.Consume(ctx, pool, dispatch.ID, taskID, map[string]queue.ActorFunc{queue.ActorImageSession: executor.Execute})
			if !errors.Is(err, injected) || writes != 1 {
				t.Fatalf("consume error=%v failed writes=%d", err, writes)
			}
			task := generationTaskByID(t, loadSessionDetail(t, ss, session.ID), taskID)
			var envelope schema.AsyncDispatches
			if err := db.Where("id = ?", dispatch.ID).Take(&envelope).Error; err != nil {
				t.Fatal(err)
			}
			if task.Status != "running" || envelope.Status != queue.StatusPending || envelope.LeaseToken != nil || envelope.ConsumedAt != nil {
				t.Fatalf("uncommitted failure acknowledged: task=%s envelope=%+v", task.Status, envelope)
			}
			if len(task.ProviderEffects) != 1 || task.ProviderEffects[0].EffectResult != "failed" {
				t.Fatalf("confirmed provider failure changed: %+v", task.ProviderEffects)
			}
		})
	}
}
