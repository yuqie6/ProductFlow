package imagesession

import (
	"context"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/queue"
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
	drainImageRecovery(t, ss)
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
	drainImageRecovery(t, ss)
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
	drainImageRecovery(t, ss)
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

func TestRecoverUnfinishedLimitHasMoreAndIsolation(t *testing.T) {
	ss := newSessionServer(t)
	drainImageRecovery(t, ss)

	_, firstID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "limit-a", "size": "1024x1024", "generation_count": 1,
	})
	_, secondID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "limit-b", "size": "1024x1024", "generation_count": 1,
	})
	stampCreatedAt(t, ss, firstID, time.Unix(1, 0).UTC())
	stampCreatedAt(t, ss, secondID, time.Unix(2, 0).UTC())

	first, err := recoverUnfinished(context.Background(), ss.pool, time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.QueuedTasks != 1 || first.EnqueuedTasks != 1 || !first.HasMore {
		t.Fatalf("first round %+v", first)
	}
	if pendingDispatchCount(t, ss, firstID) != 1 {
		t.Fatal("first task must commit before the rest of the batch")
	}
	if pendingDispatchCount(t, ss, secondID) != 0 {
		t.Fatal("second task must wait for the next round")
	}

	second, err := recoverUnfinished(context.Background(), ss.pool, time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	if second.QueuedTasks != 1 || second.EnqueuedTasks != 1 || second.HasMore {
		t.Fatalf("second round %+v", second)
	}
	if pendingDispatchCount(t, ss, secondID) != 1 {
		t.Fatal("second task must restage on the second round")
	}
}

func TestRecoverUnfinishedContinuesAfterOneRestageFailure(t *testing.T) {
	ss := newSessionServer(t)
	drainImageRecovery(t, ss)

	_, firstID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "ok-a", "size": "1024x1024", "generation_count": 1,
	})
	_, badID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "bad", "size": "1024x1024", "generation_count": 1,
	})
	_, thirdID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "ok-c", "size": "1024x1024", "generation_count": 1,
	})
	stampCreatedAt(t, ss, firstID, time.Unix(1, 0).UTC())
	stampCreatedAt(t, ss, badID, time.Unix(2, 0).UTC())
	stampCreatedAt(t, ss, thirdID, time.Unix(3, 0).UTC())
	poisonDispatchIdentity(t, ss, badID)
	t.Cleanup(func() {
		_, _ = ss.pool.Exec(context.Background(), `DELETE FROM async_dispatches WHERE aggregate_id = $1`, badID)
		_, _ = ss.pool.Exec(context.Background(), `
			UPDATE image_session_generation_tasks
			SET is_retryable = FALSE, status = 'cancelled'
			WHERE id = $1
		`, badID)
	})

	summary, err := recoverUnfinished(context.Background(), ss.pool, time.Minute, 25)
	if err == nil {
		t.Fatal("poisoned identity must surface")
	}
	if summary.EnqueuedTasks != 2 {
		t.Fatalf("neighbors must still commit, summary %+v err=%v", summary, err)
	}
	if pendingDispatchCount(t, ss, firstID) != 1 || pendingDispatchCount(t, ss, thirdID) != 1 {
		t.Fatalf("first/third pending missing, err=%v", err)
	}
	if pendingDispatchCount(t, ss, badID) != 0 {
		t.Fatal("poisoned task must not restage")
	}
}

func drainImageRecovery(t *testing.T, ss *sessionServer) {
	t.Helper()
	if _, err := ss.pool.Exec(context.Background(), `
		DELETE FROM async_dispatches
		WHERE status = 'consumed'
		  AND delivery_key LIKE $1
		  AND actor_name <> $2
	`, queue.ActorImageSession+":%", queue.ActorImageSession); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		summary, err := recoverUnfinished(context.Background(), ss.pool, time.Minute, 25)
		if err != nil && !summary.HasMore {
			return
		}
		if err != nil {
			continue
		}
		if !summary.HasMore {
			return
		}
	}
	t.Fatal("image session recovery drain did not empty")
}

func stampCreatedAt(t *testing.T, ss *sessionServer, taskID string, createdAt time.Time) {
	t.Helper()
	if _, err := ss.pool.Exec(context.Background(), `
		UPDATE image_session_generation_tasks SET created_at = $2 WHERE id = $1
	`, taskID, createdAt); err != nil {
		t.Fatal(err)
	}
}

func pendingDispatchCount(t *testing.T, ss *sessionServer, aggregateID string) int {
	t.Helper()
	var n int
	if err := ss.pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM async_dispatches WHERE aggregate_id = $1 AND status = 'pending'
	`, aggregateID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func poisonDispatchIdentity(t *testing.T, ss *sessionServer, taskID string) {
	t.Helper()
	if _, err := ss.pool.Exec(context.Background(), `
		INSERT INTO async_dispatches (
			id, delivery_key, actor_name, aggregate_id, status, available_at, attempts, created_at, updated_at
		) VALUES ($1, $2, $3, $4, 'consumed', NOW(), 0, NOW(), NOW())
	`, clockid.New(), queue.DeliveryKey(queue.ActorImageSession, taskID), queue.ActorGraphRun, taskID); err != nil {
		t.Fatal(err)
	}
}
