package graph_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/providers"
	"github.com/yuqie6/productflow/internal/providers/adapt"
)

type tracedFailedImageClient struct {
	providers.MockImage
	calls int
}

func (c *tracedFailedImageClient) Generate(context.Context, providers.GenerateRequest) (providers.GenerateResult, error) {
	c.calls++
	return providers.GenerateResult{}, fmt.Errorf("request trace-123: %w", providers.ErrTimeout)
}

func TestGraphPersistsRealAdapterFailureCause(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "graph"})
	gs.mustStatus(t, resp, 201)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)
	client := &tracedFailedImageClient{}
	e := graph.Executor{DB: gs.db, Products: product.GraphGuard{}, Deps: graph.Dependencies{Prompt: graph.MockPromptProvider{}, Image: adapt.GraphImage(client), Assets: product.Service{DB: gs.db, Media: gs.media}}}
	if err := gs.tryExecuteLocally(t, run.ID, e); err != nil {
		t.Fatal(err)
	}
	var status, reason, detail string
	if err := gs.pool.QueryRow(context.Background(), `SELECT r.status,n.failure_reason,e.detail FROM workflow_graph_runs r JOIN workflow_graph_node_runs n ON n.graph_run_id=r.id JOIN workflow_graph_provider_effects e ON e.node_run_id=n.id WHERE r.id=$1 AND n.status='unknown'`, run.ID).Scan(&status, &reason, &detail); err != nil {
		t.Fatal(err)
	}
	if status != "unknown" || !strings.Contains(reason, "trace-123") || !strings.Contains(detail, "trace-123") {
		t.Fatalf("status=%s reason=%q detail=%q", status, reason, detail)
	}
	if strings.Count(reason, graph.ProviderUnknownDetail) != 1 {
		t.Fatalf("duplicated unknown detail: %q", reason)
	}
	if err := e.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	if client.calls != 1 {
		t.Fatalf("provider calls=%d", client.calls)
	}
}
