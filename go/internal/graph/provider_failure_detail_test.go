package graph

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/quota"
)

func TestProviderUnknownRetainsFailureCause(t *testing.T) {
	for _, kind := range []string{"prompt", "image"} {
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
					return ImageResult{}, errors.New(detail)
				})
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
