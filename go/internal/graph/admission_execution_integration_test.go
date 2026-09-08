package graph_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/generation"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/product"
)

// TestQueueConsumeGraphAdmissionUsesRealRuns exercises the worker boundary with
// two actual graph snapshots and dispatch rows. The first run must yield after
// one node when the other merchant has an available generation envelope; the
// second run then gets a real dispatcher turn, and the first run can continue
// after its retry delay is advanced by the test.
func TestQueueConsumeGraphAdmissionUsesRealRuns(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	fixture := auth.SeedDualMerchants(t, gs.db, gs.client, gs.srv.URL)
	mustSeedMerchantQuota(t, gs.db, fixture.MerchantAID, 10_000)
	mustSeedMerchantQuota(t, gs.db, fixture.MerchantBID, 10_000)
	if _, err := gs.pool.Exec(context.Background(), `
		UPDATE app_settings
		SET value = '1', updated_at = NOW()
		WHERE key = $1
	`, generation.MaxConcurrentSettingKey); err != nil {
		t.Fatal(err)
	}

	originalCookies := gs.cookies
	gs.cookies = fixture.CookiesA
	productA, graphA := gs.createDirectGraphWithImageTypes(t, `[{"key":"hero","quantity":1},{"key":"detail","quantity":1},{"key":"scene","quantity":1}]`)
	runA := submitAdmissionGraphRun(t, gs, productA, graphA)
	if len(runA.NodeRuns) < 2 {
		t.Fatalf("real graph fixture must have multiple node runs, got %d", len(runA.NodeRuns))
	}

	dispatchA := loadGraphAdmissionDispatch(t, gs, runA.ID)
	if dispatchA.MerchantID != fixture.MerchantAID || dispatchA.AggregateID != runA.ID {
		t.Fatalf("A dispatch identity merchant=%q aggregate=%q run=%q", dispatchA.MerchantID, dispatchA.AggregateID, runA.ID)
	}
	summary, err := queue.RunDispatcherOnce(context.Background(), gs.pool, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Sent != 1 {
		t.Fatalf("dispatcher did not send A: %+v", summary)
	}
	dispatchA = loadGraphAdmissionDispatch(t, gs, runA.ID)
	if dispatchA.Status != queue.StatusSent {
		t.Fatalf("A dispatch status after dispatcher=%q", dispatchA.Status)
	}

	gs.cookies = fixture.CookiesB
	productB, graphB := gs.createDirectGraphWithImageTypes(t, `[{"key":"hero","quantity":1},{"key":"detail","quantity":1},{"key":"scene","quantity":1}]`)
	runB := submitAdmissionGraphRun(t, gs, productB, graphB)
	dispatchB := loadGraphAdmissionDispatch(t, gs, runB.ID)
	if dispatchB.MerchantID != fixture.MerchantBID || dispatchB.AggregateID != runB.ID {
		t.Fatalf("B dispatch identity merchant=%q aggregate=%q run=%q", dispatchB.MerchantID, dispatchB.AggregateID, runB.ID)
	}
	if dispatchB.Status != queue.StatusPending {
		t.Fatalf("B must wait in PENDING before A yields: %q", dispatchB.Status)
	}
	gs.cookies = originalCookies

	executor := graph.Executor{
		DB:       gs.db,
		Products: product.GraphGuard{},
		Deps: graph.Dependencies{
			Prompt: graph.MockPromptProvider{},
			Image:  graph.MockImageProvider{},
			Assets: product.Service{DB: gs.db, Media: gs.media},
		},
	}
	allRunIDs := []string{runA.ID, runB.ID}
	allDispatchIDs := []string{dispatchA.ID, dispatchB.ID}
	beforeA := loadGraphAdmissionProgress(t, gs, runA.ID)
	beforeB := loadGraphAdmissionProgress(t, gs, runB.ID)
	started := time.Now()
	var firstActorErr error
	if err := queue.Consume(context.Background(), gs.pool, dispatchA.ID, runA.ID, map[string]queue.ActorFunc{
		queue.ActorGraphRun: func(ctx context.Context, aggregateID string) error {
			firstActorErr = executor.ExecuteRun(ctx, aggregateID)
			return firstActorErr
		},
	}); err != nil {
		t.Fatalf("A queue consume: %v", err)
	}
	if !errors.Is(firstActorErr, queue.ErrLater) {
		t.Fatalf("A must yield through ExecuteRun ErrLater, got %v", firstActorErr)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("A yield did not terminate promptly: %s", elapsed)
	}
	afterA := loadGraphAdmissionProgress(t, gs, runA.ID)
	afterB := loadGraphAdmissionProgress(t, gs, runB.ID)
	t.Logf("round=0 dispatch=%s actor_error=%v A before=%+v after=%+v B before=%+v after=%+v", dispatchA.ID, firstActorErr, beforeA, afterA, beforeB, afterB)
	if !progressChanged(beforeA, afterA) {
		t.Fatalf("A made no progress before yielding: before=%+v after=%+v", beforeA, afterA)
	}
	assertGraphAdmissionProgress(t, gs, runA.ID, true)
	dispatchA = loadGraphAdmissionDispatch(t, gs, runA.ID)
	if dispatchA.Status != queue.StatusPending || dispatchA.SentAt != nil || dispatchA.LeaseToken != nil {
		t.Fatalf("A ErrLater must clear SENT/lease: status=%q sent_at=%v lease=%v", dispatchA.Status, dispatchA.SentAt, dispatchA.LeaseToken)
	}

	// ReleaseForRetry uses a one-second delay. Advancing only these test rows'
	// availability makes each next dispatcher turn deterministic without
	// waiting or changing business timestamps on either run. The dispatcher
	// still chooses which real merchant gets the next SENT envelope.
	const maxRounds = 32
	for round := 1; round <= maxRounds; round++ {
		if graphAdmissionComplete(t, gs, allRunIDs) {
			break
		}
		advanceGraphAdmissionDispatches(t, gs, allDispatchIDs)
		beforeA = loadGraphAdmissionProgress(t, gs, runA.ID)
		beforeB = loadGraphAdmissionProgress(t, gs, runB.ID)
		summary, err := queue.RunDispatcherOnce(context.Background(), gs.pool, nil, 1)
		if err != nil {
			t.Fatal(err)
		}
		if summary.Sent != 1 {
			t.Fatalf("round %d dispatcher made no progress: %+v A=%+v B=%+v", round, summary, beforeA, beforeB)
		}
		sent, sentRunID := findSentGraphAdmissionDispatch(t, gs, allRunIDs)
		var actorErr error
		if err := queue.Consume(context.Background(), gs.pool, sent.ID, sentRunID, map[string]queue.ActorFunc{
			queue.ActorGraphRun: func(ctx context.Context, aggregateID string) error {
				actorErr = executor.ExecuteRun(ctx, aggregateID)
				return actorErr
			},
		}); err != nil {
			t.Fatalf("round %d consume run %s: %v", round, sentRunID, err)
		}
		afterA = loadGraphAdmissionProgress(t, gs, runA.ID)
		afterB = loadGraphAdmissionProgress(t, gs, runB.ID)
		t.Logf("round=%d dispatch=%s run=%s actor_error=%v A before=%+v after=%+v B before=%+v after=%+v", round, sent.ID, sentRunID, actorErr, beforeA, afterA, beforeB, afterB)
		if actorErr != nil && !errors.Is(actorErr, queue.ErrLater) {
			t.Fatalf("round %d actor error for run %s: %v", round, sentRunID, actorErr)
		}
		if !progressChanged(beforeA, afterA) && !progressChanged(beforeB, afterB) && !graphAdmissionComplete(t, gs, allRunIDs) {
			t.Fatalf("round %d made no graph progress: A before=%+v after=%+v B before=%+v after=%+v", round, beforeA, afterA, beforeB, afterB)
		}
	}
	if !graphAdmissionComplete(t, gs, allRunIDs) {
		t.Fatalf("graph admission did not converge within %d rounds: A=%+v dispatch=%+v B=%+v dispatch=%+v", maxRounds, loadGraphAdmissionProgress(t, gs, runA.ID), loadGraphAdmissionDispatch(t, gs, runA.ID), loadGraphAdmissionProgress(t, gs, runB.ID), loadGraphAdmissionDispatch(t, gs, runB.ID))
	}
	assertGraphAdmissionComplete(t, gs, allRunIDs)
}

func submitAdmissionGraphRun(t *testing.T, gs *graphServer, productID, graphID string) graph.GraphRunResponse {
	t.Helper()
	resp := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{
		"scope": graph.RunScopeGraph,
	})
	gs.mustStatus(t, resp, http.StatusCreated)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)
	if run.Status != graph.RunStatusRunning || len(run.NodeRuns) == 0 {
		t.Fatalf("submitted graph run %+v", run)
	}
	return run
}

func loadGraphAdmissionDispatch(t *testing.T, gs *graphServer, runID string) schema.AsyncDispatches {
	t.Helper()
	var dispatch schema.AsyncDispatches
	if err := gs.db.Where("actor_name = ? AND aggregate_id = ?", queue.ActorGraphRun, runID).Take(&dispatch).Error; err != nil {
		t.Fatalf("load dispatch for run %s: %v", runID, err)
	}
	return dispatch
}

type graphAdmissionProgress struct {
	RunStatus string
	Total     int
	Terminal  int
	Queued    int
	Running   int
	Succeeded int
}

func loadGraphAdmissionProgress(t *testing.T, gs *graphServer, runID string) graphAdmissionProgress {
	t.Helper()
	var progress graphAdmissionProgress
	if err := gs.pool.QueryRow(context.Background(), `
		SELECT r.status,
		       COUNT(*),
		       COUNT(*) FILTER (WHERE n.status IN ('succeeded', 'skipped', 'failed', 'unknown', 'cancelled')),
		       COUNT(*) FILTER (WHERE n.status = 'queued'),
		       COUNT(*) FILTER (WHERE n.status = 'running'),
		       COUNT(*) FILTER (WHERE n.status IN ('succeeded', 'skipped'))
		FROM workflow_graph_runs r
		LEFT JOIN workflow_graph_node_runs n ON n.graph_run_id = r.id
		WHERE r.id = $1
		GROUP BY r.status
	`, runID).Scan(&progress.RunStatus, &progress.Total, &progress.Terminal, &progress.Queued, &progress.Running, &progress.Succeeded); err != nil {
		t.Fatalf("load graph progress %s: %v", runID, err)
	}
	return progress
}

func assertGraphAdmissionProgress(t *testing.T, gs *graphServer, runID string, mustRemainQueued bool) {
	t.Helper()
	progress := loadGraphAdmissionProgress(t, gs, runID)
	if progress.Succeeded == 0 {
		t.Fatalf("run %s made no node progress: %+v", runID, progress)
	}
	if mustRemainQueued && progress.Queued == 0 {
		t.Fatalf("run %s did not yield with queued work: %+v", runID, progress)
	}
	if mustRemainQueued && progress.RunStatus != graph.RunStatusRunning {
		t.Fatalf("run %s must remain running while capacity is deferred: %+v", runID, progress)
	}
}

func progressChanged(before, after graphAdmissionProgress) bool {
	return before != after
}

func advanceGraphAdmissionDispatches(t *testing.T, gs *graphServer, dispatchIDs []string) {
	t.Helper()
	for _, dispatchID := range dispatchIDs {
		if _, err := gs.pool.Exec(context.Background(), `
			UPDATE async_dispatches SET available_at = NOW(), updated_at = NOW()
			WHERE id = $1 AND status = 'pending'
		`, dispatchID); err != nil {
			t.Fatal(err)
		}
	}
}

func findSentGraphAdmissionDispatch(t *testing.T, gs *graphServer, runIDs []string) (schema.AsyncDispatches, string) {
	t.Helper()
	var found schema.AsyncDispatches
	foundRunID := ""
	for _, runID := range runIDs {
		dispatch := loadGraphAdmissionDispatch(t, gs, runID)
		if dispatch.Status != queue.StatusSent {
			continue
		}
		if foundRunID != "" {
			t.Fatalf("multiple SENT graph dispatches: %s and %s", foundRunID, runID)
		}
		found = dispatch
		foundRunID = runID
	}
	if foundRunID == "" {
		t.Fatalf("dispatcher reported SENT but no graph dispatch is SENT for runs %v", runIDs)
	}
	return found, foundRunID
}

func graphAdmissionComplete(t *testing.T, gs *graphServer, runIDs []string) bool {
	t.Helper()
	for _, runID := range runIDs {
		progress := loadGraphAdmissionProgress(t, gs, runID)
		dispatch := loadGraphAdmissionDispatch(t, gs, runID)
		if progress.RunStatus != graph.RunStatusSucceeded || progress.Terminal != progress.Total || dispatch.Status != queue.StatusConsumed {
			return false
		}
	}
	return true
}

func assertGraphAdmissionComplete(t *testing.T, gs *graphServer, runIDs []string) {
	t.Helper()
	for _, runID := range runIDs {
		progress := loadGraphAdmissionProgress(t, gs, runID)
		dispatch := loadGraphAdmissionDispatch(t, gs, runID)
		if progress.RunStatus != graph.RunStatusSucceeded || progress.Total == 0 || progress.Terminal != progress.Total || progress.Queued != 0 || progress.Running != 0 {
			t.Fatalf("run %s did not finish all nodes: progress=%+v", runID, progress)
		}
		if dispatch.Status != queue.StatusConsumed {
			t.Fatalf("dispatch for run %s status=%q want consumed", runID, dispatch.Status)
		}
	}
}
