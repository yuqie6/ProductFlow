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

func TestImageCheckpointTransactionBoundaries(t *testing.T) {
	for _, mode := range []string{"rollback", "stale_consumer"} {
		t.Run(mode, func(t *testing.T) {
			pool, db := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_cptx_%d", time.Now().UnixNano()))
			ss := newSessionServerWithDatabase(t, pool, db)
			session, taskID := createQueuedGeneration(t, ss, map[string]any{"prompt": mode, "size": "1024x1024", "generation_count": 2})
			ctx := context.Background()
			var dispatch queue.Dispatch
			if err := db.Transaction(func(tx *gorm.DB) error {
				var err error
				dispatch, err = queue.StageForActor(ctx, tx, queue.ActorImageSession, taskID, 0)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := queue.RunDispatcherOnce(ctx, pool, func(string, string) error { return nil }, 10); err != nil {
				t.Fatal(err)
			}
			oldToken, ok, err := queue.ClaimForConsumption(ctx, pool, dispatch.ID, taskID, queue.DefaultConsumerLeaseSeconds)
			if err != nil || !ok {
				t.Fatalf("claim old: %t %v", ok, err)
			}
			executor := Executor{DB: db, Media: ss.media, Provider: MockChatProvider{}}
			claimed, attempt, _, err := executor.claim(ctx, taskID)
			if err != nil || !claimed {
				t.Fatalf("claim task: %t %v", claimed, err)
			}
			if mode == "rollback" {
				injected := errors.New("checkpoint envelope write failed")
				const callback = "test:checkpoint-envelope-failure"
				if err := db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
					if tx.Statement.Table == "async_dispatches" {
						tx.AddError(injected)
					}
				}); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { db.Callback().Update().Remove(callback) })
				err := executor.runGeneration(ctx, taskID, attempt, session.ID)
				if !errors.Is(err, injected) {
					t.Fatalf("checkpoint error=%v", err)
				}
				task := generationTaskByID(t, loadSessionDetail(t, ss, session.ID), taskID)
				var row schema.ImageSessionGenerationTasks
				if err := db.Where("id = ?", taskID).Take(&row).Error; err != nil {
					t.Fatal(err)
				}
				var envelope schema.AsyncDispatches
				if err := db.Where("id = ?", dispatch.ID).Take(&envelope).Error; err != nil {
					t.Fatal(err)
				}
				if task.Status != "running" || task.CompletedCandidates != 1 || row.ActiveAttemptID == nil || *row.ActiveAttemptID != attempt || envelope.Status != queue.StatusSent || envelope.LeaseToken == nil || *envelope.LeaseToken != oldToken {
					t.Fatalf("checkpoint not rolled back: task=%+v envelope=%+v", task, envelope)
				}
				return
			}
			if err := executor.runGeneration(ctx, taskID, attempt, session.ID); !errors.Is(err, queue.ErrLater) {
				t.Fatalf("yield: %v", err)
			}
			time.Sleep(time.Duration(queue.DefaultLaterRetrySeconds) * time.Second)
			if summary, err := queue.RunDispatcherOnce(ctx, pool, func(string, string) error { return nil }, 10); err != nil || summary.Sent != 1 {
				t.Fatalf("redelivery=%+v err=%v", summary, err)
			}
			newToken, ok, err := queue.ClaimForConsumption(ctx, pool, dispatch.ID, taskID, queue.DefaultConsumerLeaseSeconds)
			if err != nil || !ok || newToken == oldToken {
				t.Fatalf("claim new: %t %v", ok, err)
			}
			if changed, err := queue.ReleaseForRetry(ctx, pool, dispatch.ID, taskID, oldToken, 0); err != nil || changed {
				t.Fatalf("old release changed=%t err=%v", changed, err)
			}
			if changed, err := queue.MarkConsumed(ctx, pool, dispatch.ID, taskID, oldToken); err != nil || changed {
				t.Fatalf("old consume changed=%t err=%v", changed, err)
			}
			var envelope schema.AsyncDispatches
			if err := db.Where("id = ?", dispatch.ID).Take(&envelope).Error; err != nil {
				t.Fatal(err)
			}
			if envelope.Status != queue.StatusSent || envelope.LeaseToken == nil || *envelope.LeaseToken != newToken {
				t.Fatalf("new lease changed: %+v", envelope)
			}
		})
	}
}
