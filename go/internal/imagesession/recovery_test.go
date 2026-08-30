package imagesession

import (
	"context"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/clockid"
)

func markStaleRunning(t *testing.T, ss *sessionServer, taskID, phase string, completed int, activeIdx *int) string {
	t.Helper()
	attempt := clockid.New()
	_, err := ss.pool.Exec(context.Background(), `
		UPDATE image_session_generation_tasks SET
			status = 'running',
			active_attempt_id = $2,
			started_at = NOW() - INTERVAL '2 hours',
			finished_at = NULL,
			progress_updated_at = NOW() - INTERVAL '2 hours',
			progress_phase = $3,
			completed_candidates = $4,
			active_candidate_index = $5,
			is_retryable = TRUE
		WHERE id = $1
	`, taskID, attempt, phase, completed, activeIdx)
	if err != nil {
		t.Fatal(err)
	}
	return attempt
}

func TestRecoverUnfinishedMarksUnknownWhenActiveCandidateIndexSet(t *testing.T) {
	ss := newSessionServer(t)
	session, taskID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "active index", "size": "1024x1024", "generation_count": 2,
	})
	idx := 1
	markStaleRunning(t, ss, taskID, "running", 0, &idx)

	summary, err := RecoverUnfinished(context.Background(), ss.pool, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if summary.UnknownTasks < 1 {
		t.Fatalf("summary %+v", summary)
	}
	got := loadSessionDetail(t, ss, session.ID)
	task := got.GenerationTasks[0]
	if task.ID != taskID {
		for i := range got.GenerationTasks {
			if got.GenerationTasks[i].ID == taskID {
				task = got.GenerationTasks[i]
				break
			}
		}
	}
	if task.Status != "unknown" {
		t.Fatalf("status %s summary %+v", task.Status, summary)
	}
	if task.IsRetryable {
		t.Fatal("unknown must not be retryable")
	}
	if task.ActiveCandidateIndex != nil {
		t.Fatalf("active index %+v", task.ActiveCandidateIndex)
	}
}

func TestRecoverUnfinishedDoesNotRequeueAppliedCandidate(t *testing.T) {
	ss := newSessionServer(t)
	session, taskID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "applied covering next", "size": "1024x1024", "generation_count": 3,
	})
	markStaleRunning(t, ss, taskID, "candidate_saved", 1, nil)
	insertAppliedEffect(t, ss, taskID, 1, 3)

	summary, err := RecoverUnfinished(context.Background(), ss.pool, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if summary.UnknownTasks < 1 {
		t.Fatalf("summary %+v", summary)
	}
	got := loadSessionDetail(t, ss, session.ID)
	task := got.GenerationTasks[0]
	if task.ID != taskID {
		for i := range got.GenerationTasks {
			if got.GenerationTasks[i].ID == taskID {
				task = got.GenerationTasks[i]
				break
			}
		}
	}
	if task.Status != "unknown" {
		t.Fatalf("status %s summary %+v", task.Status, summary)
	}
	var effect string
	if err := ss.pool.QueryRow(context.Background(), `
		SELECT effect_result FROM image_session_provider_effects
		WHERE generation_task_id = $1 AND candidate_start_index = 1
	`, taskID).Scan(&effect); err != nil {
		t.Fatal(err)
	}
	if effect != "applied" {
		t.Fatalf("applied effect mutated to %s", effect)
	}
	var pending int
	if err := ss.pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM async_dispatches WHERE aggregate_id = $1 AND status = 'pending'
	`, taskID).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Fatalf("requeued applied candidate, pending=%d", pending)
	}
}

func TestRecoverUnfinishedRequeuesIdleRunningWithoutProviderBoundary(t *testing.T) {
	ss := newSessionServer(t)
	_, taskID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "safe requeue", "size": "1024x1024", "generation_count": 1,
	})
	markStaleRunning(t, ss, taskID, "running", 0, nil)

	summary, err := RecoverUnfinished(context.Background(), ss.pool, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if summary.StaleRunningTasks < 1 {
		t.Fatalf("summary %+v", summary)
	}
	var status, phase string
	var activeIdx *int
	if err := ss.pool.QueryRow(context.Background(), `
		SELECT status, progress_phase, active_candidate_index
		FROM image_session_generation_tasks WHERE id = $1
	`, taskID).Scan(&status, &phase, &activeIdx); err != nil {
		t.Fatal(err)
	}
	if status != "queued" || phase != "requeued_after_idle" {
		t.Fatalf("status %s phase %s", status, phase)
	}
	if activeIdx != nil {
		t.Fatalf("active index %+v", activeIdx)
	}
}
