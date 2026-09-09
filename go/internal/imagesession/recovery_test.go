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
		SELECT COUNT(*) FROM river_job
		WHERE kind = 'productflow_task' AND args ->> 'actor' = $1 AND args ->> 'aggregate_id' = $2
		  AND state <> 'completed'
	`, queue.ActorImageSession, taskID).Scan(&pending); err != nil {
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

func TestRecoverUnfinishedSkipsLockedCandidatePrefix(t *testing.T) {
	ss := newSessionServer(t)
	drainImageRecovery(t, ss)
	var ids []string
	for i := 0; i < recoveryBatchLimit+1; i++ {
		_, id := createQueuedGeneration(t, ss, map[string]any{
			"prompt": "locked recovery prefix", "size": "1024x1024", "generation_count": 1,
		})
		stampCreatedAt(t, ss, id, time.Unix(int64(i+1), 0).UTC())
		ids = append(ids, id)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	lock, err := ss.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(context.Background())
	if _, err := lock.Exec(ctx, `SELECT id FROM image_session_generation_tasks WHERE id=ANY($1) FOR UPDATE`, ids[:recoveryBatchLimit]); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		summary, err := RecoverUnfinished(ctx, ss.pool, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		if i == 0 {
			want = 1
		}
		if summary.EnqueuedTasks != want {
			t.Fatalf("cycle %d enqueued %d tasks, want %d", i, summary.EnqueuedTasks, want)
		}
	}
	if got := pendingDispatchCount(t, ss, ids[recoveryBatchLimit]); got != 1 {
		t.Fatalf("unlocked task after %d locked candidates has %d dispatches after 3 cycles, want 1", recoveryBatchLimit, got)
	}
	for _, id := range ids[:recoveryBatchLimit] {
		if pendingDispatchCount(t, ss, id) != 0 {
			t.Fatal("locked task was restaged")
		}
	}
	if err := lock.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if summary, err := RecoverUnfinished(ctx, ss.pool, time.Minute); err != nil || summary.EnqueuedTasks != recoveryBatchLimit {
		t.Fatalf("released tasks were not recovered: %+v err=%v", summary, err)
	}
}

func TestRecoverUnfinishedLeavesRecentHeartbeatRunning(t *testing.T) {
	ss := newSessionServer(t)
	drainImageRecovery(t, ss)
	session, taskID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "fresh heartbeat", "size": "1024x1024", "generation_count": 1,
	})
	markStaleRunning(t, ss, taskID, "running", 0, nil)
	stampHeartbeat(t, ss, taskID, 0)

	summary, err := RecoverUnfinished(context.Background(), ss.pool, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if summary.StaleRunningTasks != 0 || summary.UnknownTasks != 0 || summary.EnqueuedTasks != 0 {
		t.Fatalf("fresh heartbeat must not recover, summary %+v", summary)
	}
	got := generationTaskByID(t, loadSessionDetail(t, ss, session.ID), taskID)
	if got.Status != "running" {
		t.Fatalf("status %s", got.Status)
	}
}

func TestRecoverUnfinishedDefaultStaleUsesNinetyMinutes(t *testing.T) {
	ss := newSessionServer(t)
	drainImageRecovery(t, ss)
	if DefaultStaleRunningAfter != 90*time.Minute {
		t.Fatalf("default stale %s", DefaultStaleRunningAfter)
	}

	_, freshID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "under default stale", "size": "1024x1024", "generation_count": 1,
	})
	markStaleRunning(t, ss, freshID, "running", 0, nil)
	stampHeartbeat(t, ss, freshID, DefaultStaleRunningAfter-time.Minute)

	_, staleID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "past default stale", "size": "1024x1024", "generation_count": 1,
	})
	markStaleRunning(t, ss, staleID, "running", 0, nil)
	stampHeartbeat(t, ss, staleID, DefaultStaleRunningAfter+time.Minute)

	summary, err := RecoverUnfinished(context.Background(), ss.pool, 0)
	if err != nil {
		t.Fatal(err)
	}
	if summary.StaleRunningTasks < 1 {
		t.Fatalf("expired heartbeat must requeue, summary %+v", summary)
	}
	var freshStatus, staleStatus, stalePhase string
	if err := ss.pool.QueryRow(context.Background(), `
		SELECT status FROM image_session_generation_tasks WHERE id = $1
	`, freshID).Scan(&freshStatus); err != nil {
		t.Fatal(err)
	}
	if err := ss.pool.QueryRow(context.Background(), `
		SELECT status, progress_phase FROM image_session_generation_tasks WHERE id = $1
	`, staleID).Scan(&staleStatus, &stalePhase); err != nil {
		t.Fatal(err)
	}
	if freshStatus != "running" {
		t.Fatalf("heartbeat inside default stale recovered early: %s", freshStatus)
	}
	if staleStatus != "queued" || stalePhase != "requeued_after_idle" {
		t.Fatalf("heartbeat past default stale status=%s phase=%s", staleStatus, stalePhase)
	}
}

func TestRecoverUnfinishedLateWriterDoesNotSucceedAfterUnknown(t *testing.T) {
	ss := newSessionServer(t)
	drainImageRecovery(t, ss)
	session, taskID := createQueuedGeneration(t, ss, map[string]any{
		"prompt": "late writer", "size": "1024x1024", "generation_count": 1,
	})
	idx := 1
	attempt := markStaleRunning(t, ss, taskID, "running", 0, &idx)

	summary, err := RecoverUnfinished(context.Background(), ss.pool, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if summary.UnknownTasks < 1 {
		t.Fatalf("summary %+v", summary)
	}
	if err := (Executor{DB: ss.db, Media: ss.media}).finishSucceeded(context.Background(), taskID, attempt, session.ID, clockid.New()); err != nil {
		t.Fatal(err)
	}
	got := generationTaskByID(t, loadSessionDetail(t, ss, session.ID), taskID)
	if got.Status != "unknown" || got.IsRetryable {
		t.Fatalf("late writer mutated unknown: %+v", got)
	}

	if err := (Executor{DB: ss.db, Media: ss.media}).Execute(context.Background(), taskID); err != nil {
		t.Fatal(err)
	}
	got = generationTaskByID(t, loadSessionDetail(t, ss, session.ID), taskID)
	if got.Status != "unknown" {
		t.Fatalf("execute revived %s", got.Status)
	}
}

func drainImageRecovery(t *testing.T, ss *sessionServer) {
	t.Helper()
	if _, err := ss.pool.Exec(context.Background(), `
		DELETE FROM river_job
		WHERE kind = 'productflow_task' AND args ->> 'actor' = $1
	`, queue.ActorImageSession); err != nil {
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

func stampHeartbeat(t *testing.T, ss *sessionServer, taskID string, age time.Duration) {
	t.Helper()
	at := time.Now().UTC().Add(-age)
	if _, err := ss.pool.Exec(context.Background(), `
		UPDATE image_session_generation_tasks
		SET progress_updated_at = $2, started_at = $2
		WHERE id = $1
	`, taskID, at); err != nil {
		t.Fatal(err)
	}
}

func generationTaskByID(t *testing.T, session DetailResponse, taskID string) TaskResponse {
	t.Helper()
	for _, task := range session.GenerationTasks {
		if task.ID == taskID {
			return task
		}
	}
	t.Fatalf("missing task %s", taskID)
	return TaskResponse{}
}

func pendingDispatchCount(t *testing.T, ss *sessionServer, aggregateID string) int {
	t.Helper()
	var n int
	if err := ss.pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM river_job
		WHERE kind = 'productflow_task' AND args ->> 'actor' = $1 AND args ->> 'aggregate_id' = $2
		  AND state <> 'completed'
	`, queue.ActorImageSession, aggregateID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func poisonDispatchIdentity(t *testing.T, ss *sessionServer, taskID string) {
	t.Helper()
	const constraint = "test_imagesession_recovery_river_reject"
	if _, err := ss.pool.Exec(context.Background(),
		"ALTER TABLE river_job ADD CONSTRAINT "+constraint+" CHECK (args ->> 'aggregate_id' <> '"+taskID+"') NOT VALID"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := ss.pool.Exec(context.Background(), "ALTER TABLE river_job DROP CONSTRAINT IF EXISTS "+constraint); err != nil {
			t.Error(err)
		}
	})
}
