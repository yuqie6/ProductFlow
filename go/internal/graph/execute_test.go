package graph_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/product"
	"gorm.io/gorm"
)

func TestExecuteGraphRunWithMockProvidersSucceeds(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "graph"})
	gs.mustStatus(t, resp, 201)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)
	executor := graph.Executor{
		DB: gs.db,
		Deps: graph.Dependencies{
			Prompt: graph.MockPromptProvider{},
			Image:  graph.MockImageProvider{},
			Assets: product.Service{DB: gs.db, Media: gs.media},
		},
	}
	gs.executeLocally(t, run.ID, executor)
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
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "graph"})
	gs.mustStatus(t, resp, 201)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)
	executor := graph.Executor{
		DB: gs.db,
		Deps: graph.Dependencies{
			Prompt: graph.MockPromptProvider{Err: graph.ErrProviderUnknown()},
			Image:  graph.MockImageProvider{},
			Assets: product.Service{DB: gs.db, Media: gs.media},
		},
	}
	gs.executeLocally(t, run.ID, executor)
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

func TestExecuteGraphRunMarksUnknownWhenProviderRejectsAfterCall(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "graph"})
	gs.mustStatus(t, resp, 201)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)
	executor := graph.Executor{
		DB: gs.db,
		Deps: graph.Dependencies{
			Prompt: graph.MockPromptProvider{Err: errors.New("供应商拒绝请求（HTTP 400）")},
			Image:  graph.MockImageProvider{},
			Assets: product.Service{DB: gs.db, Media: gs.media},
		},
	}
	gs.executeLocally(t, run.ID, executor)
	got := gs.do(t, "GET", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/"+run.ID, nil, "")
	gs.mustStatus(t, got, 200)
	var finished graph.GraphRunResponse
	gs.decode(t, got, &finished)
	if finished.Status != "unknown" {
		t.Fatalf("status %s reason %+v nodes %+v", finished.Status, finished.FailureReason, finished.NodeRuns)
	}
	if finished.IsRetryable {
		t.Fatal("unknown must not be retryable")
	}
}

type captureImage struct {
	graph.MockImageProvider
	last graph.ImageRequest
}

func (c *captureImage) GenerateImage(ctx context.Context, req graph.ImageRequest) (graph.ImageResult, error) {
	c.last = req
	return c.MockImageProvider.GenerateImage(ctx, req)
}

type capturePrompt struct {
	graph.MockPromptProvider
	last graph.PromptRequest
}

func (c *capturePrompt) GeneratePrompt(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	c.last = req
	return c.MockPromptProvider.GeneratePrompt(ctx, req)
}

func TestExecuteGraphRunLoadsReferenceBytesForImageProvider(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "graph"})
	gs.mustStatus(t, resp, 201)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)
	images := &captureImage{}
	prompts := &capturePrompt{}
	executor := graph.Executor{
		DB: gs.db,
		Deps: graph.Dependencies{
			Prompt: prompts,
			Image:  images,
			Assets: product.Service{DB: gs.db, Media: gs.media},
		},
	}
	gs.executeLocally(t, run.ID, executor)
	if len(images.last.References) == 0 {
		t.Fatal("image provider must receive reference image bytes")
	}
	for _, ref := range images.last.References {
		if len(ref.Bytes) == 0 || ref.MIME == "" {
			t.Fatalf("empty reference %+v", ref)
		}
	}
	if images.last.ImageTypeKey != "hero" {
		t.Fatalf("image type %q", images.last.ImageTypeKey)
	}
	if len(images.last.IncomingEdgeIDs) == 0 {
		t.Fatal("image request must include incoming edge ids")
	}
	if len(prompts.last.References) == 0 {
		t.Fatal("prompt provider must receive reference image bytes")
	}
	for _, ref := range prompts.last.References {
		if len(ref.Bytes) == 0 {
			t.Fatalf("empty prompt reference %+v", ref)
		}
	}
	if prompts.last.ImageTypeKey != "hero" {
		t.Fatalf("prompt image type %q", prompts.last.ImageTypeKey)
	}
	if !prompts.last.GenerateFromContext {
		t.Fatal("fresh template prompt config is a generation seed")
	}
	if prompts.last.CurrentPrompt == nil {
		t.Fatal("prompt request must include current_prompt seed")
	}
}

type failDelivery struct{}

func (failDelivery) QueueAfterImageSuccess(context.Context, *gorm.DB, string, string) error {
	return errors.New("delivery queue down")
}

func TestImageNodeSucceedsWhenDeliveryQueueFails(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "graph"})
	gs.mustStatus(t, resp, 201)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)
	executor := graph.Executor{
		DB: gs.db,
		Deps: graph.Dependencies{
			Prompt:   graph.MockPromptProvider{},
			Image:    graph.MockImageProvider{},
			Assets:   product.Service{DB: gs.db, Media: gs.media},
			Delivery: failDelivery{},
		},
	}
	gs.executeLocally(t, run.ID, executor)
	got := gs.do(t, "GET", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/"+run.ID, nil, "")
	gs.mustStatus(t, got, 200)
	var finished graph.GraphRunResponse
	gs.decode(t, got, &finished)
	if finished.Status != "succeeded" {
		t.Fatalf("status %s reason %+v nodes %+v", finished.Status, finished.FailureReason, finished.NodeRuns)
	}
	var raw []byte
	if err := gs.pool.QueryRow(context.Background(), `
		SELECT payload_json FROM workflow_graph_artifacts WHERE artifact_type = 'image' LIMIT 1
	`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Measured struct {
			Matched *bool `json:"aspect_matched"`
			Width   int   `json:"measured_width"`
			Height  int   `json:"measured_height"`
		} `json:"measured_output"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Measured.Matched == nil || payload.Measured.Width <= 0 || payload.Measured.Height <= 0 {
		t.Fatalf("measured %+v raw %s", payload.Measured, raw)
	}
}

func TestBlockedDownstreamReasonMatchesPython(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "graph"})
	gs.mustStatus(t, resp, 201)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)
	executor := graph.Executor{
		DB: gs.db,
		Deps: graph.Dependencies{
			Prompt: graph.MockPromptProvider{Err: graph.ErrProviderUnknown()},
			Image:  graph.MockImageProvider{},
			Assets: product.Service{DB: gs.db, Media: gs.media},
		},
	}
	gs.executeLocally(t, run.ID, executor)
	got := gs.do(t, "GET", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/"+run.ID, nil, "")
	gs.mustStatus(t, got, 200)
	var finished graph.GraphRunResponse
	gs.decode(t, got, &finished)
	found := false
	for _, node := range finished.NodeRuns {
		if node.FailureReason != nil && *node.FailureReason == "上游处理节点未成功" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected blocked reason, nodes %+v", finished.NodeRuns)
	}
}
