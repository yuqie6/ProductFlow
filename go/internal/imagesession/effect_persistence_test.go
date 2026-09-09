package imagesession

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
)

func TestEffectPersistenceFailureDoesNotConsumeOrRetryProvider(t *testing.T) {
	for _, mode := range []string{"confirmed_failure", "unknown", "applied"} {
		t.Run(mode, func(t *testing.T) {
			ss := newSessionServer(t)
			ctx := imageSessionQueueContext(t, ss.db)
			_, taskID := createQueuedGeneration(t, ss, map[string]any{"prompt": "effect persistence", "size": "1024x1024"})
			job := stageImageSessionJob(t, ss.db, taskID)
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
			err := executeImageSessionJob(ctx, job, e)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
				t.Fatalf("effect constraint error was not propagated: %v", err)
			}
			var taskStatus string
			if err := ss.pool.QueryRow(ctx, "SELECT status FROM image_session_generation_tasks WHERE id=$1", taskID).Scan(&taskStatus); err != nil {
				t.Fatal(err)
			}
			if taskStatus != "running" || riverImageSessionJobCount(t, ss, taskID) != 1 {
				t.Fatalf("partial effect failure task=%s jobs=%d", taskStatus, riverImageSessionJobCount(t, ss, taskID))
			}
			if provider.calls != 1 {
				t.Fatalf("provider calls=%d", provider.calls)
			}
			if mode == "applied" {
				if _, err := ss.pool.Exec(ctx, "UPDATE image_session_generation_tasks SET progress_updated_at=NOW()-interval '1 hour' WHERE id=$1", taskID); err != nil {
					t.Fatal(err)
				}
				if _, err := recoverImageTaskState(ctx, ss.db, taskID, time.Now().Add(-time.Minute)); err == nil {
					t.Fatal("recovery ignored applied persistence error")
				}
				if err := ss.pool.QueryRow(ctx, "SELECT status FROM image_session_generation_tasks WHERE id=$1", taskID).Scan(&taskStatus); err != nil {
					t.Fatal(err)
				}
				if taskStatus != "running" {
					t.Fatalf("failed recovery committed status=%s", taskStatus)
				}
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
			if mode == "applied" {
				var effect schema.ImageSessionProviderEffects
				if err := ss.db.Where("generation_task_id=? AND candidate_start_index=1", taskID).Take(&effect).Error; err != nil {
					t.Fatalf("recovery lost provider effect: %v", err)
				}
				if effect.EffectResult != "applied" || effect.ReconciliationState != "applied" || effect.RequestJSON == nil {
					t.Fatalf("recovered effect incomplete: %+v", effect)
				}
			}
			if err := executeImageSessionJob(ctx, job, e); err != nil {
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
