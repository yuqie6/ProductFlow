package graph

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/quota"
	"gorm.io/gorm"
)

func TestProviderUnknownRetainsFailureCause(t *testing.T) {
	for _, kind := range []string{"prompt", "image", "image_missing_hold"} {
		t.Run(kind, func(t *testing.T) {
			pool, db := testdb.Open(t)
			ctx := WithProductGuard(context.Background(), cmdTestProducts{})
			attempt := clockid.New()
			runID := insertStaleRunningGraphRun(t, pool, "claimed", &attempt, time.Now().UTC())
			var nodeID string
			if err := pool.QueryRow(ctx, "SELECT id FROM workflow_graph_node_runs WHERE graph_run_id=$1", runID).Scan(&nodeID); err != nil {
				t.Fatal(err)
			}
			merchantID := auth.MustDevMerchantID(t, db)
			if _, err := (&quota.Service{DB: db}).Adjust(ctx, merchantID, clockid.New(), 10, "provider cause fixture", ""); err != nil {
				t.Fatal(err)
			}
			e := Executor{DB: db}
			node := graphNodeRunRow{ID: nodeID, ActiveAttemptID: &attempt}
			const detail = "provider response interrupted: upstream request trace-123"
			calls := 0
			var err error
			if kind == "prompt" {
				_, _, err = e.callProvider(ctx, runID, node, "fixture", PromptRequest{InputDigest: "digest", NodeType: NodeCreativeBrief}, func(context.Context, PromptRequest) (PromptResult, error) {
					calls++
					return PromptResult{}, errors.New(detail)
				})
			} else {
				_, _, err = e.callImageProvider(ctx, runID, node, "fixture", ImageRequest{InputDigest: "digest"}, func(context.Context, ImageRequest) (ImageResult, error) {
					calls++
					if kind == "image_missing_hold" {
						if _, err := pool.Exec(ctx, "UPDATE merchant_quota_holds SET idempotency_key=$1 WHERE merchant_id=$2 AND idempotency_key=$3", "hidden:"+nodeID, merchantID, imageNodeQuotaKey(nodeID, attempt)); err != nil {
							t.Fatal(err)
						}
					}
					return ImageResult{}, errors.New(detail)
				})
			}
			if kind == "image_missing_hold" {
				// Restore the exact reservation even when this regression fails.
				if _, restoreErr := pool.Exec(ctx, "UPDATE merchant_quota_holds SET idempotency_key=$1 WHERE merchant_id=$2 AND idempotency_key=$3", imageNodeQuotaKey(nodeID, attempt), merchantID, "hidden:"+nodeID); restoreErr != nil {
					t.Fatal(restoreErr)
				}
				if !apperr.IsNotFound(err) {
					t.Fatalf("paid unknown accepted missing reservation: %v", err)
				}
				var runStatus, nodeStatus, effectStatus string
				if err := pool.QueryRow(ctx, "SELECT r.status,n.status,e.effect_result FROM workflow_graph_runs r JOIN workflow_graph_node_runs n ON n.graph_run_id=r.id JOIN workflow_graph_provider_effects e ON e.node_run_id=n.id WHERE r.id=$1", runID).Scan(&runStatus, &nodeStatus, &effectStatus); err != nil {
					t.Fatal(err)
				}
				if runStatus != "running" || nodeStatus != "running" || effectStatus != "pending" {
					t.Fatalf("partial unknown: run=%s node=%s effect=%s", runStatus, nodeStatus, effectStatus)
				}
				if err := e.markUnknownCommitted(ctx, runID, nodeID, &attempt, detail); err != nil {
					t.Fatal(err)
				}
				if calls != 1 {
					t.Fatalf("provider repeated %d times", calls)
				}
				return
			}
			if !isProviderUnknown(err) {
				t.Fatalf("error=%v", err)
			}
			var runStatus, nodeStatus, reason, effectStatus, effectDetail string
			if err := pool.QueryRow(ctx, "SELECT r.status,n.status,n.failure_reason,e.effect_result,e.detail FROM workflow_graph_runs r JOIN workflow_graph_node_runs n ON n.graph_run_id=r.id JOIN workflow_graph_provider_effects e ON e.node_run_id=n.id AND e.attempt_id=$2 WHERE r.id=$1", runID, attempt).Scan(&runStatus, &nodeStatus, &reason, &effectStatus, &effectDetail); err != nil {
				t.Fatal(err)
			}
			if runStatus != RunStatusUnknown || nodeStatus != NodeRunUnknown || effectStatus != "unknown" || calls != 1 {
				t.Fatalf("run=%s node=%s effect=%s calls=%d", runStatus, nodeStatus, effectStatus, calls)
			}
			if !strings.Contains(reason, detail) || !strings.Contains(effectDetail, detail) {
				t.Fatalf("cause lost: node=%q effect=%q", reason, effectDetail)
			}
		})
	}
}

// Fixtures starting after the provider boundary need the committed intent too.
func recordGraphEffectFixture(t *testing.T, db *gorm.DB, nodeID, attemptID string, quotaKey *string) {
	t.Helper()
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, err := ensureProviderEffectIntent(context.Background(), tx, nodeID, attemptID, strings.Repeat("a", 64), "fixture", []byte("{}"), quotaKey)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
