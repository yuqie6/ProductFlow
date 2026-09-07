package imagesession

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/canonjson"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/testdb"
)

func TestEffectReuseDistinguishesUncertainFromConfirmedFailure(t *testing.T) {
	for _, previous := range []string{"pending", "unknown", "failed", "applied"} {
		t.Run(previous, func(t *testing.T) {
			pool, db := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_effectreuse_%d", time.Now().UnixNano()))
			ss := newSessionServerWithDatabase(t, pool, db)
			ctx := context.Background()
			_, taskID := createQueuedGeneration(t, ss, map[string]any{"prompt": "effect reuse", "size": "1024x1024"})
			old := markStaleRunning(t, ss, taskID, "provider_call", 0, nil)
			e := Executor{DB: ss.db}
			oldReq := map[string]any{"prompt": "original"}
			oldHash, err := canonjson.SHA256Hex(oldReq)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := e.ensureEffect(ctx, taskID, old, 1, 1, clockid.New(), oldHash, "original-provider", oldReq); err != nil {
				t.Fatal(err)
			}
			if previous != "pending" {
				if err := e.markEffect(ctx, taskID, old, 1, previous, "previous result"); err != nil {
					t.Fatal(err)
				}
			}
			next := markStaleRunning(t, ss, taskID, "provider_call", 0, nil)
			nextReq := map[string]any{"prompt": "retry request"}
			nextHash, err := canonjson.SHA256Hex(nextReq)
			if err != nil {
				t.Fatal(err)
			}
			result, count, err := e.ensureEffect(ctx, taskID, next, 1, 2, clockid.New(), nextHash, "retry-provider", nextReq)
			if previous == "pending" || previous == "unknown" {
				if !isUnknown(err) {
					t.Fatalf("uncertain effect allowed provider: result=%s err=%v", result, err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			var effect schema.ImageSessionProviderEffects
			if err := ss.db.Where("generation_task_id=? AND candidate_start_index=1", taskID).Take(&effect).Error; err != nil {
				t.Fatal(err)
			}
			switch previous {
			case "failed":
				if result != "pending" || count != 2 || effect.AttemptID != next || effect.ProviderName != "retry-provider" || effect.RequestHash != nextHash || effect.ReconciliationState != "not_requested" || effect.Detail != nil {
					t.Fatalf("retry did not reset intent: %+v result=%s count=%d", effect, result, count)
				}
			case "applied":
				if result != "applied" || count != 1 || effect.AttemptID != old || effect.RequestHash != oldHash {
					t.Fatalf("applied result changed: %+v", effect)
				}
			default:
				if effect.EffectResult != "unknown" || effect.AttemptID != old || effect.RequestHash != oldHash {
					t.Fatalf("uncertain attempt evidence changed: %+v", effect)
				}
			}
		})
	}
}
