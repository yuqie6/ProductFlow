package graph_test

import (
	"context"
	"errors"
	"testing"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/product"
)

func TestExecuteGraphRunWithMockProvidersSucceeds(t *testing.T) {
	gs := newGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "graph"})
	gs.mustStatus(t, resp, 201)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)

	executor := graph.Executor{
		Pool: gs.pool,
		Deps: graph.Dependencies{
			Prompt: graph.MockPromptProvider{},
			Image:  graph.MockImageProvider{},
			Assets: product.Service{Pool: gs.pool, Media: media.Store{Files: storage.Local{Root: t.TempDir()}}},
		},
	}
	if err := executor.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	got := gs.do(t, "GET", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/"+run.ID, nil, "")
	gs.mustStatus(t, got, 200)
	var finished graph.GraphRunResponse
	gs.decode(t, got, &finished)
	if finished.Status != "succeeded" {
		t.Fatalf("status %s reason %+v nodes %+v", finished.Status, finished.FailureReason, finished.NodeRuns)
	}
	for _, node := range finished.NodeRuns {
		if node.Status != "succeeded" {
			t.Fatalf("node %s %s", node.ID, node.Status)
		}
	}
}

func TestExecuteGraphRunMarksUnknownWhenProviderFailsAfterIntent(t *testing.T) {
	gs := newGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "graph"})
	gs.mustStatus(t, resp, 201)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)

	executor := graph.Executor{
		Pool: gs.pool,
		Deps: graph.Dependencies{
			Prompt: graph.MockPromptProvider{Err: errors.New("provider crashed")},
			Image:  graph.MockImageProvider{},
			Assets: product.Service{Pool: gs.pool, Media: media.Store{Files: storage.Local{Root: t.TempDir()}}},
		},
	}
	if err := executor.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	got := gs.do(t, "GET", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/"+run.ID, nil, "")
	gs.mustStatus(t, got, 200)
	var finished graph.GraphRunResponse
	gs.decode(t, got, &finished)
	if finished.Status != "unknown" {
		t.Fatalf("status %s reason %+v", finished.Status, finished.FailureReason)
	}
	if finished.IsRetryable {
		t.Fatal("unknown must not be retryable")
	}
}
