package graph_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"sync"
	"testing"
	"time"

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
		if node.AttemptCount != 1 {
			t.Fatalf("node %s attempt_count %d", node.ID, node.AttemptCount)
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

type terminalRunPrompt struct {
	graph.MockPromptProvider
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (p *terminalRunPrompt) waitForRelease(ctx context.Context) error {
	p.once.Do(func() { close(p.entered) })
	select {
	case <-p.release:
		return errors.New("provider failed after run became terminal")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *terminalRunPrompt) GenerateCreativeBrief(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	return graph.PromptResult{}, p.waitForRelease(ctx)
}

func (p *terminalRunPrompt) GenerateVisualOverlay(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	return graph.PromptResult{}, p.waitForRelease(ctx)
}

func (p *terminalRunPrompt) GeneratePrompt(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	return graph.PromptResult{}, p.waitForRelease(ctx)
}

func TestProviderErrorDoesNotOverwriteTerminalRunOrNode(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "graph"})
	gs.mustStatus(t, resp, 201)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)

	prompt := &terminalRunPrompt{entered: make(chan struct{}), release: make(chan struct{})}
	executor := graph.Executor{
		DB: gs.db,
		Deps: graph.Dependencies{
			Prompt: prompt,
			Image:  graph.MockImageProvider{},
			Assets: product.Service{DB: gs.db, Media: gs.media},
		},
	}
	done := make(chan error, 1)
	go func() { done <- executor.ExecuteRun(context.Background(), run.ID) }()
	select {
	case <-prompt.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("provider was not called")
	}
	if _, err := gs.pool.Exec(context.Background(), `
		UPDATE workflow_graph_runs
		SET status = 'failed', failure_reason = '测试终止'
		WHERE id = $1
	`, run.ID); err != nil {
		t.Fatal(err)
	}
	close(prompt.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("terminal run should be consumed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("executor did not finish")
	}

	got := gs.do(t, "GET", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/"+run.ID, nil, "")
	gs.mustStatus(t, got, 200)
	var finished graph.GraphRunResponse
	gs.decode(t, got, &finished)
	if finished.Status != "failed" {
		t.Fatalf("status %s", finished.Status)
	}
	for _, nodeRun := range finished.NodeRuns {
		if nodeRun.Status == "unknown" {
			t.Fatalf("terminal run node was overwritten by late provider error: %+v", nodeRun)
		}
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

func TestGraphRunLeaseTakesOverExpiredOwner(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "graph"})
	gs.mustStatus(t, resp, 201)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)
	if _, err := gs.pool.Exec(context.Background(), `
		UPDATE workflow_graph_runs
		SET execution_lease_token = 'crashed-worker', execution_lease_expires_at = NOW() + interval '1 hour'
		WHERE id = $1
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
	if err := executor.ExecuteRun(context.Background(), run.ID); !errors.Is(err, queue.ErrBusy) {
		t.Fatalf("live lease must return busy, got %v", err)
	}
	if _, err := gs.pool.Exec(context.Background(), `
		UPDATE workflow_graph_runs SET execution_lease_expires_at = NOW() - interval '1 second' WHERE id = $1
	`, run.ID); err != nil {
		t.Fatal(err)
	}
	gs.executeLocally(t, run.ID, executor)
	got := gs.do(t, "GET", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/"+run.ID, nil, "")
	gs.mustStatus(t, got, 200)
	var finished graph.GraphRunResponse
	gs.decode(t, got, &finished)
	if finished.Status != "succeeded" {
		t.Fatalf("taken-over run status %s reason %+v", finished.Status, finished.FailureReason)
	}
}

func TestGraphRecoveryLeavesAValidExecutionLeaseAlone(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "graph"})
	gs.mustStatus(t, resp, 201)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)
	if _, err := gs.pool.Exec(context.Background(), `
		UPDATE workflow_graph_runs
		SET execution_lease_token = 'live-worker', execution_lease_expires_at = NOW() + interval '1 hour'
		WHERE id = $1
	`, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := gs.pool.Exec(context.Background(), `
		UPDATE workflow_graph_node_runs
		SET status = 'running', started_at = NOW() - interval '1 hour', progress_updated_at = NOW() - interval '1 hour'
		WHERE graph_run_id = $1
	`, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RecoverUnfinishedGraphRuns(context.Background(), gs.pool, time.Minute, product.GraphGuard{}); err != nil {
		t.Fatal(err)
	}

	got := gs.do(t, "GET", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/"+run.ID, nil, "")
	gs.mustStatus(t, got, 200)
	var current graph.GraphRunResponse
	gs.decode(t, got, &current)
	if current.Status != graph.RunStatusRunning || len(current.NodeRuns) != len(run.NodeRuns) {
		t.Fatalf("valid lease was recovered: status=%s nodes=%+v", current.Status, current.NodeRuns)
	}
	for _, nodeRun := range current.NodeRuns {
		if nodeRun.Status != graph.NodeRunRunning {
			t.Fatalf("valid lease changed node %s to %s", nodeRun.ID, nodeRun.Status)
		}
	}
}

func TestGraphRunLeaseFencesLateProviderResult(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "graph"})
	gs.mustStatus(t, resp, 201)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)
	images := &delayedImage{entered: make(chan struct{}), release: make(chan struct{})}
	executor := graph.Executor{
		DB:       gs.db,
		Products: product.GraphGuard{},
		Deps: graph.Dependencies{
			Prompt: graph.MockPromptProvider{},
			Image:  images,
			Assets: product.Service{DB: gs.db, Media: gs.media},
		},
	}
	gs.reclaimRun(t, run.ID)
	done := make(chan error, 1)
	go func() { done <- executor.ExecuteRun(context.Background(), run.ID) }()
	select {
	case <-images.entered:
	case <-time.After(8 * time.Second):
		t.Fatal("image provider was not called")
	}
	if _, err := gs.pool.Exec(context.Background(), `
		UPDATE workflow_graph_runs
		SET execution_lease_token = 'takeover-worker', execution_lease_expires_at = NOW() + interval '1 hour'
		WHERE id = $1
	`, run.ID); err != nil {
		t.Fatal(err)
	}
	close(images.release)
	select {
	case err := <-done:
		if !errors.Is(err, queue.ErrBusy) {
			t.Fatalf("stale executor must retry after fencing, got %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("stale executor did not finish")
	}
	got := gs.do(t, "GET", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/"+run.ID, nil, "")
	gs.mustStatus(t, got, 200)
	var current graph.GraphRunResponse
	gs.decode(t, got, &current)
	if current.Status != "running" {
		t.Fatalf("late provider changed run status to %s", current.Status)
	}
}

func TestExecuteWaitingGraphRunConsumesUntilRecovery(t *testing.T) {
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
	if err := executor.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatalf("orphaned running nodes must consume this dispatch, got %v", err)
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

type timingPrompt struct {
	graph.MockPromptProvider
	mu           sync.Mutex
	promptStarts []time.Time
	promptDones  []time.Time
}

func (p *timingPrompt) GeneratePrompt(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	p.mu.Lock()
	n := len(p.promptStarts)
	p.promptStarts = append(p.promptStarts, time.Now())
	p.mu.Unlock()
	delay := 80 * time.Millisecond
	if n >= 1 {
		delay = 400 * time.Millisecond
	}
	time.Sleep(delay)
	p.mu.Lock()
	p.promptDones = append(p.promptDones, time.Now())
	p.mu.Unlock()
	return p.MockPromptProvider.GeneratePrompt(ctx, req)
}

type timingImage struct {
	graph.MockImageProvider
	mu      sync.Mutex
	started []time.Time
}

func (p *timingImage) GenerateImage(ctx context.Context, req graph.ImageRequest) (graph.ImageResult, error) {
	p.mu.Lock()
	p.started = append(p.started, time.Now())
	p.mu.Unlock()
	return p.MockImageProvider.GenerateImage(ctx, req)
}

func TestExecuteGraphRunPipelinesImageAfterFirstPrompt(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("name", "流水重叠跑图")
	_ = w.WriteField("image_types", `[{"key":"hero","quantity":1},{"key":"scene","quantity":1}]`)
	part, err := w.CreateFormFile("images", "hero.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(pngBytes(t)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	resp := gs.do(t, "POST", "/api/v3/products", &buf, w.FormDataContentType())
	gs.mustStatus(t, resp, 201)
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Product struct {
			ID string `json:"id"`
		} `json:"product"`
		Graph struct {
			ID string `json:"id"`
		} `json:"graph"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	productID, graphID := payload.Product.ID, payload.Graph.ID
	runResp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "graph"})
	gs.mustStatus(t, runResp, 201)
	var run graph.GraphRunResponse
	gs.decode(t, runResp, &run)
	prompt := &timingPrompt{}
	image := &timingImage{}
	executor := graph.Executor{
		DB: gs.db,
		Deps: graph.Dependencies{
			Prompt: prompt,
			Image:  image,
			Assets: product.Service{DB: gs.db, Media: gs.media},
		},
	}
	gs.executeLocally(t, run.ID, executor)
	if len(prompt.promptDones) < 2 || len(image.started) < 1 {
		t.Fatalf("prompts %d images %d", len(prompt.promptDones), len(image.started))
	}
	lastPrompt := prompt.promptDones[0]
	for _, done := range prompt.promptDones[1:] {
		if done.After(lastPrompt) {
			lastPrompt = done
		}
	}
	firstImage := image.started[0]
	for _, started := range image.started[1:] {
		if started.Before(firstImage) {
			firstImage = started
		}
	}
	if !firstImage.Before(lastPrompt) {
		t.Fatalf("image started at %s after last prompt %s; expected overlap", firstImage, lastPrompt)
	}
}

type delayedImage struct {
	graph.MockImageProvider
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (d *delayedImage) GenerateImage(ctx context.Context, req graph.ImageRequest) (graph.ImageResult, error) {
	d.once.Do(func() { close(d.entered) })
	select {
	case <-d.release:
		return d.MockImageProvider.GenerateImage(ctx, req)
	case <-ctx.Done():
		return graph.ImageResult{}, ctx.Err()
	}
}

func TestImagePreviewPromotesAfterLayoutRevisionBump(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "graph"})
	gs.mustStatus(t, resp, 201)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)

	images := &delayedImage{entered: make(chan struct{}), release: make(chan struct{})}
	executor := graph.Executor{
		DB:       gs.db,
		Products: product.GraphGuard{},
		Deps: graph.Dependencies{
			Prompt: graph.MockPromptProvider{},
			Image:  images,
			Assets: product.Service{DB: gs.db, Media: gs.media},
		},
	}
	gs.reclaimRun(t, run.ID)
	done := make(chan error, 1)
	go func() { done <- executor.ExecuteRun(context.Background(), run.ID) }()
	select {
	case <-images.entered:
	case <-time.After(8 * time.Second):
		t.Fatal("image provider was not called")
	}

	view := loadProjection(t, gs, productID, graphID)
	imageNode := nodeOfType(t, view, graph.NodeImageGeneration)
	moved := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/changesets", map[string]any{
		"base_graph_revision": view.Revision,
		"summary":             "自动布局",
		"operations": []map[string]any{
			{"op": "move_nodes", "nodes": []any{[]any{imageNode.ID, imageNode.PositionX + 24, imageNode.PositionY}}},
		},
	})
	gs.mustStatus(t, moved, 200)
	close(images.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("executor did not finish")
	}

	live := loadProjection(t, gs, productID, graphID)
	promoted := nodeOfType(t, live, graph.NodeImageGeneration)
	if promoted.CurrentArtifactID == nil || promoted.PreviewAssetID == nil {
		t.Fatalf("layout during run dropped preview: artifact=%v preview=%v revision %d -> %d",
			promoted.CurrentArtifactID, promoted.PreviewAssetID, view.Revision, live.Revision)
	}
	if live.Revision <= view.Revision {
		t.Fatalf("expected layout to bump revision, got %d -> %d", view.Revision, live.Revision)
	}
}
