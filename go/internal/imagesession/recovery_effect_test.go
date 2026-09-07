package imagesession

import (
	"context"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/clockid"
)

func TestRecoveryDoesNotTrustCompletedCountWithoutSavedRounds(t *testing.T) {
	ss := newSessionServer(t)
	ctx := context.Background()
	_, taskID := createQueuedGeneration(t, ss, map[string]any{"prompt": "unproven checkpoint", "size": "1024x1024"})
	attempt := markStaleRunning(t, ss, taskID, "candidate_saved", 1, nil)
	if _, _, err := (Executor{DB: ss.db}).ensureEffect(ctx, taskID, attempt, 1, 1, clockid.New(), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "fixture", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	result, err := recoverImageTaskState(ctx, ss.db, taskID, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if result.outcome != "unknown" {
		t.Fatalf("unproven result allowed recovery=%s", result.outcome)
	}
	var status string
	if err := ss.pool.QueryRow(ctx, "SELECT effect_result FROM image_session_provider_effects WHERE generation_task_id=$1", taskID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "unknown" {
		t.Fatalf("unproven effect=%s", status)
	}
}
