package imagesession

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"gorm.io/gorm"
)

func TestEffectPersistenceFailureDoesNotConsumeOrRetryProvider(t *testing.T) {
	for _, mode := range []string{"confirmed_failure", "unknown", "applied"} {
		t.Run(mode, func(t *testing.T) {
			ss := newSessionServer(t)
			ctx := context.Background()
			_, taskID := createQueuedGeneration(t, ss, map[string]any{"prompt": "effect persistence", "size": "1024x1024"})
			var dispatch queue.Dispatch
			if err := ss.db.Transaction(func(tx *gorm.DB) error {
				var err error
				dispatch, err = queue.StageForActor(ctx, tx, queue.ActorImageSession, taskID, 0)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if err := ss.db.Model(&schema.AsyncDispatches{}).Where("id=?", dispatch.ID).Update("status", queue.StatusSent).Error; err != nil {
				t.Fatal(err)
			}
			const constraint = "test_imagesession_effect_persistence"
			if _, err := ss.pool.Exec(ctx, "ALTER TABLE image_session_provider_effects ADD CONSTRAINT "+constraint+" CHECK (generation_task_id <> '"+taskID+"' OR effect_result='pending')"); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := ss.pool.Exec(context.Background(), "ALTER TABLE image_session_provider_effects DROP CONSTRAINT IF EXISTS "+constraint); err != nil {
					t.Error(err)
				}
			})
			var providerErr error
			if mode == "confirmed_failure" {
				providerErr = apperr.Validation("provider rejected request")
			}
			if mode == "unknown" {
				providerErr = errors.New("provider disconnected")
			}
			provider := &countingProvider{MockChatProvider: MockChatProvider{Err: providerErr}}
			e := Executor{DB: ss.db, Media: ss.media, Provider: provider}
			err := queue.Consume(ctx, ss.pool, dispatch.ID, taskID, map[string]queue.ActorFunc{queue.ActorImageSession: e.Execute})
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
				t.Fatalf("effect constraint error was not propagated: %v", err)
			}
			var taskStatus, dispatchStatus string
			if err := ss.pool.QueryRow(ctx, "SELECT status FROM image_session_generation_tasks WHERE id=$1", taskID).Scan(&taskStatus); err != nil {
				t.Fatal(err)
			}
			if err := ss.pool.QueryRow(ctx, "SELECT status FROM async_dispatches WHERE id=$1", dispatch.ID).Scan(&dispatchStatus); err != nil {
				t.Fatal(err)
			}
			if taskStatus != "running" || dispatchStatus != queue.StatusPending {
				t.Fatalf("partial effect failure task=%s dispatch=%s", taskStatus, dispatchStatus)
			}
			if provider.calls != 1 {
				t.Fatalf("provider calls=%d", provider.calls)
			}
			if _, err := ss.pool.Exec(ctx, "ALTER TABLE image_session_provider_effects DROP CONSTRAINT "+constraint); err != nil {
				t.Fatal(err)
			}
			if _, err := ss.pool.Exec(ctx, "UPDATE image_session_generation_tasks SET progress_updated_at=NOW()-interval '1 hour' WHERE id=$1", taskID); err != nil {
				t.Fatal(err)
			}
			if _, err := recoverImageTaskState(ctx, ss.db, taskID, time.Now().Add(-time.Minute)); err != nil {
				t.Fatal(err)
			}
			if err := e.Execute(ctx, taskID); err != nil {
				t.Fatal(err)
			}
			if provider.calls != 1 {
				t.Fatalf("recovery repeated provider calls=%d", provider.calls)
			}
			if err := ss.pool.QueryRow(ctx, "SELECT status FROM image_session_generation_tasks WHERE id=$1", taskID).Scan(&taskStatus); err != nil {
				t.Fatal(err)
			}
			want := "unknown"
			if mode == "applied" {
				want = "succeeded"
			}
			if taskStatus != want {
				t.Fatalf("recovered status=%s want=%s", taskStatus, want)
			}

		})
	}
}
