package imagesession

import (
	"context"
	"errors"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/clockid"
)

func TestLateEffectResultCannotOverwriteCurrentAttempt(t *testing.T) {
	for _, result := range []string{"failed", "unknown", "applied"} {
		t.Run(result, func(t *testing.T) {
			ss := newSessionServer(t)
			ctx := context.Background()
			_, taskID := createQueuedGeneration(t, ss, map[string]any{"prompt": "effect fence", "size": "1024x1024"})
			current := markStaleRunning(t, ss, taskID, "provider_call", 0, nil)
			e := Executor{DB: ss.db}
			if _, _, err := e.ensureEffect(ctx, taskID, current, 1, 1, clockid.New(), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "fixture", map[string]any{}); err != nil {
				t.Fatal(err)
			}
			if err := e.markEffect(ctx, taskID, clockid.New(), 1, result, "late worker"); !errors.Is(err, errStale) {
				t.Fatalf("late effect expected fence, got %v", err)
			}
			var stored, attempt string
			if err := ss.pool.QueryRow(ctx, "SELECT effect_result,attempt_id FROM image_session_provider_effects WHERE generation_task_id=$1 AND candidate_start_index=1", taskID).Scan(&stored, &attempt); err != nil {
				t.Fatal(err)
			}
			if stored != "pending" || attempt != current {
				t.Fatalf("current effect changed: result=%s attempt=%s", stored, attempt)
			}
		})
	}
}
