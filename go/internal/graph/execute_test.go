package graph_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/queue"
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

func TestExecuteGraphRunImageOutputIncludesProductImageAssetID(t *testing.T) {
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
		t.Fatalf("status %s", finished.Status)
	}
	var imageRuns int
	for _, node := range finished.NodeRuns {
		ctx := node.CompiledContext
		if ctx == nil {
			t.Fatalf("compiled_context missing on %s", node.ID)
		}
		if _, ok := ctx["incoming_edge_ids"]; !ok {
			t.Fatalf("compiled_context missing incoming_edge_ids: %+v", ctx)
		}
		if _, ok := ctx["input_digest"]; !ok {
			t.Fatalf("compiled_context missing input_digest: %+v", ctx)
		}
		if node.Output["product_image_asset_id"] != nil {
			imageRuns++
			assetID, _ := node.Output["product_image_asset_id"].(string)
			if assetID == "" {
				t.Fatalf("empty product_image_asset_id %+v", node.Output)
			}
			if _, ok := ctx["prompt_artifact_id"]; !ok {
				t.Fatalf("image compiled_context missing prompt_artifact_id: %+v", ctx)
			}
			if _, ok := ctx["prompt_edge_id"]; !ok {
				t.Fatalf("image compiled_context missing prompt_edge_id: %+v", ctx)
			}
			if _, ok := ctx["fact_count"]; ok {
				t.Fatalf("image compiled_context must not include fact_count from whole snapshot: %+v", ctx)
			}
			var promptArtifact string
			for _, item := range node.InputTrace {
				if item.Role == "prompt" && item.ArtifactID != nil {
					promptArtifact = *item.ArtifactID
				}
			}
			if promptArtifact == "" {
				t.Fatalf("image input_trace missing prompt artifact identity: %+v", node.InputTrace)
			}
		}
	}
	if imageRuns == 0 {
		t.Fatal("expected at least one image node_run with product_image_asset_id")
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

func TestExecuteMissingGraphRunConsumes(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	executor := graph.Executor{
		DB: gs.db,
		Deps: graph.Dependencies{
			Prompt: graph.MockPromptProvider{},
			Image:  graph.MockImageProvider{},
			Assets: product.Service{DB: gs.db, Media: gs.media},
		},
	}
	if err := executor.ExecuteRun(context.Background(), "ffffffffffffffffffffffffffffffff"); err != nil {
		t.Fatalf("missing graph run must consume, not fail: %v", err)
	}
}

func TestExecuteWaitingGraphRunDoesNotConsume(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "graph"})
	gs.mustStatus(t, resp, 201)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)
	if _, err := gs.pool.Exec(context.Background(), `
		UPDATE workflow_graph_node_runs SET
			status = 'running', started_at = NOW(), progress_phase = 'claimed', progress_updated_at = NOW()
		WHERE graph_run_id = $1
	`, run.ID); err != nil {
		t.Fatal(err)
	}
	executor := graph.Executor{
		DB: gs.db,
		Deps: graph.Dependencies{
			Prompt: graph.MockPromptProvider{},
			Image:  graph.MockImageProvider{},
			Assets: product.Service{DB: gs.db, Media: gs.media},
		},
	}
	err := executor.ExecuteRun(context.Background(), run.ID)
	if !errors.Is(err, queue.ErrLater) {
		t.Fatalf("in-flight graph run with no ready nodes must retry later, got %v", err)
	}
}

func TestExecuteTerminalGraphRunConsumes(t *testing.T) {
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
	if err := executor.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatalf("terminal graph run must consume, not busy-retry: %v", err)
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
