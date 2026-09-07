package imagesession

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/testdb"
)

func TestProviderIntentFailureDoesNotCrossCallBoundary(t *testing.T) {
	for _, fault := range []string{"intent", "phase"} {
		t.Run(fault, func(t *testing.T) {
			pool, db := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_preparation_%d", time.Now().UnixNano()))
			ss := newSessionServerWithDatabase(t, pool, db)
			ctx := context.Background()
			_, taskID := createQueuedGeneration(t, ss, map[string]any{"prompt": "atomic provider intent", "size": "1024x1024"})
			const constraint = "test_imagesession_provider_intent"
			table := "image_session_provider_effects"
			check := "generation_task_id <> '" + taskID + "'"
			if fault == "phase" {
				table = "image_session_generation_tasks"
				check = "id <> '" + taskID + "' OR progress_phase <> 'candidate_started'"
			}
			if _, err := ss.pool.Exec(ctx, "ALTER TABLE "+table+" ADD CONSTRAINT "+constraint+" CHECK ("+check+")"); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := ss.pool.Exec(context.Background(), "ALTER TABLE "+table+" DROP CONSTRAINT IF EXISTS "+constraint); err != nil {
					t.Error(err)
				}
			})
			provider := &countingProvider{}
			e := Executor{DB: ss.db, Media: ss.media, Provider: provider}
			if err := e.Execute(ctx, taskID); err == nil {
				t.Fatal("expected intent persistence failure")
			}
			var phase string
			var candidate *int
			if err := ss.pool.QueryRow(ctx, "SELECT progress_phase,active_candidate_index FROM image_session_generation_tasks WHERE id=$1", taskID).Scan(&phase, &candidate); err != nil {
				t.Fatal(err)
			}
			if phase != "running" || candidate != nil {
				t.Fatalf("unissued call crossed boundary: phase=%s candidate=%v", phase, candidate)
			}
			var effects int
			if err := ss.pool.QueryRow(ctx, "SELECT COUNT(*) FROM image_session_provider_effects WHERE generation_task_id=$1", taskID).Scan(&effects); err != nil {
				t.Fatal(err)
			}
			if effects != 0 {
				t.Fatalf("rolled-back preparation left %d intents", effects)
			}
			if provider.calls != 0 {
				t.Fatal("provider invoked before intent commit")
			}
			if _, err := ss.pool.Exec(ctx, "ALTER TABLE "+table+" DROP CONSTRAINT "+constraint); err != nil {
				t.Fatal(err)
			}
			if _, err := ss.pool.Exec(ctx, "UPDATE image_session_generation_tasks SET progress_updated_at=NOW()-interval '1 hour' WHERE id=$1", taskID); err != nil {
				t.Fatal(err)
			}
			recovered, err := recoverImageTaskState(ctx, ss.db, taskID, time.Now().Add(-time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			if recovered.outcome != "requeued" {
				t.Fatalf("unissued call recovered as %s", recovered.outcome)
			}
			if err := e.Execute(ctx, taskID); err != nil {
				t.Fatal(err)
			}
			if provider.calls != 1 {
				t.Fatalf("provider calls=%d", provider.calls)
			}
		})
	}
}
