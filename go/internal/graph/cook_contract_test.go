package graph_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/product"
)

type countingPrompt struct {
	graph.MockPromptProvider
	mu      sync.Mutex
	briefs  int
	visuals int
	prompts int
	last    graph.PromptRequest
}

func (p *countingPrompt) GenerateCreativeBrief(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	p.mu.Lock()
	p.briefs++
	p.mu.Unlock()
	return p.MockPromptProvider.GenerateCreativeBrief(ctx, req)
}

func (p *countingPrompt) GenerateVisualOverlay(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	p.mu.Lock()
	p.visuals++
	p.mu.Unlock()
	return p.MockPromptProvider.GenerateVisualOverlay(ctx, req)
}

func (p *countingPrompt) GeneratePrompt(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	p.mu.Lock()
	p.prompts++
	p.last = req
	p.mu.Unlock()
	return p.MockPromptProvider.GeneratePrompt(ctx, req)
}

func (p *countingPrompt) promptCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.prompts
}

type countingImage struct {
	graph.MockImageProvider
	mu    sync.Mutex
	calls int
	last  graph.ImageRequest
}

func (c *countingImage) GenerateImage(ctx context.Context, req graph.ImageRequest) (graph.ImageResult, error) {
	c.mu.Lock()
	c.calls++
	c.last = req
	c.mu.Unlock()
	return c.MockImageProvider.GenerateImage(ctx, req)
}

func (c *countingImage) imageCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func loadProjection(t *testing.T, gs *graphServer, productID, graphID string) graph.Projection {
	t.Helper()
	resp := gs.do(t, "GET", "/api/v3/products/"+productID+"/workflows/"+graphID, nil, "")
	gs.mustStatus(t, resp, 200)
	var view graph.Projection
	gs.decode(t, resp, &view)
	return view
}

func nodeOfType(t *testing.T, view graph.Projection, nodeType graph.NodeType) graph.NodeView {
	t.Helper()
	for _, node := range view.Nodes {
		if node.NodeType == nodeType {
			return node
		}
	}
	t.Fatalf("missing %s", nodeType)
	return graph.NodeView{}
}

func cloneConfig(t *testing.T, config map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func patchNodeConfig(t *testing.T, gs *graphServer, productID, graphID, nodeID, summary string, revision int, config map[string]any) graph.Projection {
	t.Helper()
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/changesets", map[string]any{
		"base_graph_revision": revision,
		"summary":             summary,
		"operations": []map[string]any{
			{"op": "update_node_config", "node_ref": nodeID, "config": config},
		},
	})
	gs.mustStatus(t, resp, 200)
	var view graph.Projection
	gs.decode(t, resp, &view)
	return view
}

func executeGraphRun(t *testing.T, gs *graphServer, productID, graphID string, body map[string]any, prompt *countingPrompt, images *countingImage) graph.GraphRunResponse {
	t.Helper()
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", body)
	gs.mustStatus(t, resp, 201)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)
	executor := graph.Executor{
		DB: gs.db,
		Deps: graph.Dependencies{
			Prompt: prompt,
			Image:  images,
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
	return finished
}

func TestHandFilledPromptNodeScopeImageDoesNotCallPromptProvider(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	view := loadProjection(t, gs, productID, graphID)
	promptNode := nodeOfType(t, view, graph.NodePromptGeneration)
	imageNode := nodeOfType(t, view, graph.NodeImageGeneration)
	cfg := cloneConfig(t, promptNode.Config)
	delete(cfg, "document_origin")
	cfg["prompt"] = map[string]any{
		"design_goal": "手填构图",
		"composition": map[string]any{"layout": "左侧留白", "product_share_percent": 55},
	}
	view = patchNodeConfig(t, gs, productID, graphID, promptNode.ID, "手填提示词", view.Revision, cfg)
	prompts := &countingPrompt{}
	images := &countingImage{}
	executeGraphRun(t, gs, productID, graphID, map[string]any{"scope": "node", "node_id": imageNode.ID}, prompts, images)
	if prompts.promptCalls() != 0 {
		t.Fatalf("prompt provider calls %d", prompts.promptCalls())
	}
	if images.imageCalls() != 1 {
		t.Fatalf("image provider calls %d", images.imageCalls())
	}
	composition, _ := images.last.Prompt["composition"].(map[string]any)
	if composition["layout"] != "左侧留白" {
		t.Fatalf("image prompt %+v", images.last.Prompt)
	}
}

func TestGraphRunAfterAuthoredLayoutEditSkipsPromptProvider(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	prompts := &countingPrompt{}
	images := &countingImage{}
	executeGraphRun(t, gs, productID, graphID, map[string]any{"scope": "graph"}, prompts, images)
	firstPrompts := prompts.promptCalls()
	firstImages := images.imageCalls()
	if firstPrompts == 0 || firstImages == 0 {
		t.Fatalf("first run prompt=%d image=%d", firstPrompts, firstImages)
	}
	view := loadProjection(t, gs, productID, graphID)
	promptNode := nodeOfType(t, view, graph.NodePromptGeneration)
	cfg := cloneConfig(t, promptNode.Config)
	delete(cfg, "document_origin")
	prompt, _ := cfg["prompt"].(map[string]any)
	if prompt == nil {
		prompt = map[string]any{}
	}
	composition, _ := prompt["composition"].(map[string]any)
	if composition == nil {
		composition = map[string]any{}
	}
	composition["layout"] = "左侧留白"
	prompt["composition"] = composition
	cfg["prompt"] = prompt
	patchNodeConfig(t, gs, productID, graphID, promptNode.ID, "改构图", view.Revision, cfg)
	executeGraphRun(t, gs, productID, graphID, map[string]any{"scope": "graph"}, prompts, images)
	if prompts.promptCalls() != firstPrompts {
		t.Fatalf("prompt rerun %d -> %d", firstPrompts, prompts.promptCalls())
	}
	if images.imageCalls() != firstImages+1 {
		t.Fatalf("image rerun %d -> %d", firstImages, images.imageCalls())
	}
	composition, _ = images.last.Prompt["composition"].(map[string]any)
	if composition["layout"] != "左侧留白" {
		t.Fatalf("image prompt %+v", images.last.Prompt)
	}
}

func TestGeneratedContentNodeRemainsReadyAfterAdopt(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	prompts := &countingPrompt{}
	images := &countingImage{}
	executeGraphRun(t, gs, productID, graphID, map[string]any{"scope": "graph"}, prompts, images)
	view := loadProjection(t, gs, productID, graphID)
	for _, nodeType := range []graph.NodeType{graph.NodeCreativeBrief, graph.NodeVisualSystem, graph.NodePromptGeneration} {
		node := nodeOfType(t, view, nodeType)
		if node.DocumentOrigin == nil || *node.DocumentOrigin != graph.OriginGenerated {
			t.Fatalf("%s origin %+v", nodeType, node.DocumentOrigin)
		}
		if node.ConfigStatus != graph.ConfigReady {
			t.Fatalf("generated %s must remain ready, got %s", nodeType, node.ConfigStatus)
		}
	}
}

func TestToNodeAfterGeneratedContentDoesNotCallPromptProvider(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	prompts := &countingPrompt{}
	images := &countingImage{}
	executeGraphRun(t, gs, productID, graphID, map[string]any{"scope": "graph"}, prompts, images)
	firstPrompts := prompts.promptCalls()
	view := loadProjection(t, gs, productID, graphID)
	imageNode := nodeOfType(t, view, graph.NodeImageGeneration)
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{
		"scope": "to_node", "node_id": imageNode.ID,
	})
	gs.mustStatus(t, resp, 201)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)
	executor := graph.Executor{
		DB: gs.db,
		Deps: graph.Dependencies{
			Prompt: prompts,
			Image:  images,
			Assets: product.Service{DB: gs.db, Media: gs.media},
		},
	}
	gs.executeLocally(t, run.ID, executor)
	if prompts.promptCalls() != firstPrompts {
		t.Fatalf("to_node must not regenerate prompt: %d -> %d", firstPrompts, prompts.promptCalls())
	}
}

func TestForceReplacePromptIsUndoable(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	prompts := &countingPrompt{}
	images := &countingImage{}
	executeGraphRun(t, gs, productID, graphID, map[string]any{"scope": "graph"}, prompts, images)
	view := loadProjection(t, gs, productID, graphID)
	promptNode := nodeOfType(t, view, graph.NodePromptGeneration)
	before := cloneConfig(t, promptNode.Config)
	firstPrompts := prompts.promptCalls()
	executeGraphRun(t, gs, productID, graphID, map[string]any{
		"scope": "node", "node_id": promptNode.ID, "force": true, "regenerate_mode": "replace",
	}, prompts, images)
	if prompts.promptCalls() != firstPrompts+1 {
		t.Fatalf("force replace prompt calls %d -> %d", firstPrompts, prompts.promptCalls())
	}
	after := loadProjection(t, gs, productID, graphID)
	replaced := nodeOfType(t, after, graph.NodePromptGeneration)
	if replaced.DocumentOrigin == nil || *replaced.DocumentOrigin != graph.OriginGenerated {
		t.Fatalf("origin %+v", replaced.DocumentOrigin)
	}
	undone := gs.do(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/undo", nil, "")
	gs.mustStatus(t, undone, 200)
	var restored graph.Projection
	gs.decode(t, undone, &restored)
	got := nodeOfType(t, restored, graph.NodePromptGeneration)
	if pythonish(got.Config["prompt"]) != pythonish(before["prompt"]) {
		t.Fatalf("undo prompt %+v vs %+v", got.Config["prompt"], before["prompt"])
	}
}

func TestTwoImageNodeRunsDoNotCallPromptProvider(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	prompts := &countingPrompt{}
	images := &countingImage{}
	executeGraphRun(t, gs, productID, graphID, map[string]any{"scope": "graph"}, prompts, images)
	firstPrompts := prompts.promptCalls()
	view := loadProjection(t, gs, productID, graphID)
	var imageIDs []string
	for _, node := range view.Nodes {
		if node.NodeType == graph.NodeImageGeneration {
			imageIDs = append(imageIDs, node.ID)
		}
	}
	if len(imageIDs) == 0 {
		t.Fatal("missing image")
	}
	for _, imageID := range imageIDs {
		executeGraphRun(t, gs, productID, graphID, map[string]any{"scope": "node", "node_id": imageID, "force": true}, prompts, images)
	}
	if prompts.promptCalls() != firstPrompts {
		t.Fatalf("shot node runs must not call prompt provider: %d -> %d", firstPrompts, prompts.promptCalls())
	}
}

func TestAdoptSkipsOverwriteWhenUserEditsDuringRun(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	prompt := &midRunBriefEditor{gs: gs, t: t, graphID: graphID}
	images := &countingImage{}
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "graph"})
	gs.mustStatus(t, resp, 201)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)
	executor := graph.Executor{
		DB: gs.db,
		Deps: graph.Dependencies{
			Prompt: prompt,
			Image:  images,
			Assets: product.Service{DB: gs.db, Media: gs.media},
		},
	}
	gs.executeLocally(t, run.ID, executor)
	if !prompt.edited {
		t.Fatal("expected mid-run brief edit")
	}
	view := loadProjection(t, gs, productID, graphID)
	brief := nodeOfType(t, view, graph.NodeCreativeBrief)
	if brief.Config["goal"] != "用户中途改过" {
		t.Fatalf("goal %+v", brief.Config["goal"])
	}
	if brief.DocumentOrigin == nil || *brief.DocumentOrigin != graph.OriginAuthored {
		t.Fatalf("origin %+v", brief.DocumentOrigin)
	}
	if brief.CurrentArtifactID == nil {
		t.Fatal("generated artifact must be kept")
	}
	if brief.ConfigStatus != graph.ConfigStale {
		t.Fatalf("status %s", brief.ConfigStatus)
	}
	finishedResp := gs.do(t, "GET", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/"+run.ID, nil, "")
	gs.mustStatus(t, finishedResp, 200)
	var finished graph.GraphRunResponse
	gs.decode(t, finishedResp, &finished)
	for _, nodeRun := range finished.NodeRuns {
		if nodeRun.NodeID != nil && *nodeRun.NodeID == brief.ID {
			if nodeRun.Output["adopted"] != false || nodeRun.Output["stale"] != true {
				t.Fatalf("content adoption output %+v", nodeRun.Output)
			}
			return
		}
	}
	t.Fatalf("brief node run not found in %+v", finished.NodeRuns)
}

type midRunBriefEditor struct {
	countingPrompt
	gs      *graphServer
	t       *testing.T
	graphID string
	edited  bool
}

func (p *midRunBriefEditor) GenerateCreativeBrief(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	_, err := p.gs.pool.Exec(context.Background(), `
		UPDATE workflow_graph_nodes
		SET config_json = (jsonb_set(config_json::jsonb, '{goal}', to_jsonb('用户中途改过'::text)))::json,
		    document_origin = 'authored'
		WHERE graph_id = $1 AND node_type = 'creative_brief'`, p.graphID)
	if err != nil {
		p.t.Fatal(err)
	}
	_, err = p.gs.pool.Exec(context.Background(), `
		UPDATE workflow_graphs SET revision = revision + 1, updated_at = NOW() WHERE id = $1`, p.graphID)
	if err != nil {
		p.t.Fatal(err)
	}
	p.edited = true
	return p.countingPrompt.GenerateCreativeBrief(ctx, req)
}

func pythonish(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}
