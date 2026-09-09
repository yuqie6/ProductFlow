package graph_test

import (
	"context"
	"go.uber.org/zap/zaptest"
	"net/http"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/delivery"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/product"
)

func TestGraphQueuedPromotionBindsPersistentMerchantWithoutContext(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)

	firstResp := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{
		"scope": graph.RunScopeGraph,
	})
	gs.mustStatus(t, firstResp, http.StatusCreated)
	var first graph.GraphRunResponse
	gs.decode(t, firstResp, &first)
	if first.Status != graph.RunStatusRunning || len(first.NodeRuns) == 0 || first.NodeRuns[0].NodeID == nil {
		t.Fatalf("first run %+v", first)
	}

	secondResp := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{
		"scope":   graph.RunScopeNode,
		"node_id": *first.NodeRuns[0].NodeID,
	})
	gs.mustStatus(t, secondResp, http.StatusCreated)
	var queued graph.GraphRunResponse
	gs.decode(t, secondResp, &queued)
	if queued.Status != graph.RunStatusQueued {
		t.Fatalf("queued run status %s", queued.Status)
	}

	if _, err := gs.pool.Exec(context.Background(), `
		UPDATE workflow_graph_node_runs
		SET status = 'succeeded', finished_at = NOW(), active_attempt_id = NULL,
		    progress_updated_at = NOW()
		WHERE graph_run_id = $1
	`, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := gs.pool.Exec(context.Background(), `
		UPDATE workflow_graph_runs
		SET status = 'succeeded', finished_at = NOW()
		WHERE id = $1
	`, first.ID); err != nil {
		t.Fatal(err)
	}

	// Recovery enters with no request merchant. Promotion must derive it from the run's product.
	if _, err := graph.RecoverUnfinishedGraphRuns(context.Background(), gs.pool, time.Hour, product.GraphGuard{}); err != nil {
		t.Fatal(err)
	}

	merchantID := auth.MustDevMerchantID(t, gs.db)
	var dispatchMerchant string
	if err := gs.pool.QueryRow(context.Background(), `
		SELECT args ->> 'merchant_id'
		FROM river_job
		WHERE args ->> 'actor' = $1 AND args ->> 'aggregate_id' = $2
		ORDER BY created_at DESC
		LIMIT 1
	`, queue.ActorGraphRun, queued.ID).Scan(&dispatchMerchant); err != nil {
		t.Fatal(err)
	}
	if dispatchMerchant != merchantID {
		t.Fatalf("promoted graph dispatch merchant %q want %q", dispatchMerchant, merchantID)
	}

	var promotedStatus string
	if err := gs.pool.QueryRow(context.Background(), `
		SELECT status FROM workflow_graph_runs WHERE id = $1
	`, queued.ID).Scan(&promotedStatus); err != nil {
		t.Fatal(err)
	}
	if promotedStatus != graph.RunStatusRunning {
		t.Fatalf("promoted run status %q", promotedStatus)
	}
}

func TestGraphWorkerBindsMerchantForImageSuccessDeliveryEnqueue(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	view := loadProjection(t, gs, productID, graphID)
	image := nodeOfType(t, view, graph.NodeImageGeneration)
	config := cloneConfig(t, image.Config)
	config["delivery_spec"] = map[string]any{
		"width": 64, "height": 64, "format": "png", "fit": "contain",
	}
	patchNodeConfig(t, gs, productID, graphID, image.ID, "绑定交付规格", view.Revision, config)

	runResp := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{
		"scope": graph.RunScopeGraph,
	})
	gs.mustStatus(t, runResp, http.StatusCreated)
	var run graph.GraphRunResponse
	gs.decode(t, runResp, &run)

	executor := graph.Executor{
		Log:      zaptest.NewLogger(t),
		DB:       gs.db,
		Products: product.GraphGuard{},
		Deps: graph.Dependencies{
			Prompt:   graph.MockPromptProvider{},
			Image:    graph.MockImageProvider{},
			Assets:   product.Service{DB: gs.db, Media: gs.media},
			Delivery: delivery.Service{DB: gs.db, Media: gs.media},
		},
	}
	if err := executor.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	merchantID := auth.MustDevMerchantID(t, gs.db)
	var dispatchMerchant string
	if err := gs.pool.QueryRow(context.Background(), `
		SELECT d.args ->> 'merchant_id'
		FROM river_job d
		JOIN delivery_rendition_jobs j ON j.id = d.args ->> 'aggregate_id'
		WHERE d.args ->> 'actor' = $1 AND j.product_id = $2
		ORDER BY d.created_at DESC
		LIMIT 1
	`, queue.ActorDelivery, productID).Scan(&dispatchMerchant); err != nil {
		t.Fatal(err)
	}
	if dispatchMerchant != merchantID {
		t.Fatalf("delivery dispatch merchant %q want %q", dispatchMerchant, merchantID)
	}
}
