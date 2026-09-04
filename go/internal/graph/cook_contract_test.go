package graph_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/product"
)

type countingPrompt struct {
	graph.MockPromptProvider
	mu         sync.Mutex
	briefs     int
	visuals    int
	prompts    int
	last       graph.PromptRequest
	varyPrompt bool
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
	result, err := p.MockPromptProvider.GeneratePrompt(ctx, req)
	if err == nil && p.varyPrompt {
		result.Payload["design_goal"] = fmt.Sprintf("候选提示词 %d", p.promptCalls())
	}
	return result, err
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
	promptNode := nodeOfType(t, view, graph.NodeImagePrompt)
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
	promptNode := nodeOfType(t, view, graph.NodeImagePrompt)
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
	for _, nodeType := range []graph.NodeType{graph.NodeCreativeBrief, graph.NodeVisualSystem, graph.NodeImagePrompt} {
		node := nodeOfType(t, view, nodeType)
		if node.DocumentOrigin == nil || *node.DocumentOrigin != graph.OriginGenerated {
			t.Fatalf("%s origin %+v", nodeType, node.DocumentOrigin)
		}
		if node.ConfigStatus != graph.ConfigReady {
			t.Fatalf("generated %s must remain ready, got %s", nodeType, node.ConfigStatus)
		}
	}
}

func TestGraphRunPromotesEveryImageAcrossItsOwnDocumentRevisions(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraphWithImageTypes(t, `[
		{"key":"hero","quantity":1},
		{"key":"detail","quantity":1},
		{"key":"scene","quantity":1}
	]`)
	prompts := &countingPrompt{}
	images := &countingImage{}
	executeGraphRun(t, gs, productID, graphID, map[string]any{"scope": "graph"}, prompts, images)
	view := loadProjection(t, gs, productID, graphID)
	imageNodes := 0
	for _, node := range view.Nodes {
		if node.NodeType != graph.NodeImageGeneration {
			continue
		}
		imageNodes++
		if node.CurrentArtifactID == nil || node.PreviewAssetID == nil {
			t.Fatalf("successful image %q was not promoted: artifact=%v preview=%v", node.Title, node.CurrentArtifactID, node.PreviewAssetID)
		}
	}
	if imageNodes != 3 {
		t.Fatalf("image nodes %d", imageNodes)
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
	prompts := &countingPrompt{varyPrompt: true}
	images := &countingImage{}
	executeGraphRun(t, gs, productID, graphID, map[string]any{"scope": "graph"}, prompts, images)
	view := loadProjection(t, gs, productID, graphID)
	promptNode := nodeOfType(t, view, graph.NodeImagePrompt)
	before := cloneConfig(t, promptNode.Config)
	firstPrompts := prompts.promptCalls()
	executeGraphRun(t, gs, productID, graphID, map[string]any{
		"scope": "node", "node_id": promptNode.ID, "force": true, "document_action": "replace",
	}, prompts, images)
	if prompts.promptCalls() != firstPrompts+1 {
		t.Fatalf("force replace prompt calls %d -> %d", firstPrompts, prompts.promptCalls())
	}
	after := loadProjection(t, gs, productID, graphID)
	replaced := nodeOfType(t, after, graph.NodeImagePrompt)
	if replaced.DocumentOrigin == nil || *replaced.DocumentOrigin != graph.OriginGenerated {
		t.Fatalf("origin %+v", replaced.DocumentOrigin)
	}
	if replaced.PendingCandidateArtifactID == nil {
		t.Fatal("force replace must stage a document candidate")
	}
	apply := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/nodes/"+replaced.ID+"/candidate/apply", map[string]any{
		"artifact_id":         *replaced.PendingCandidateArtifactID,
		"base_graph_revision": after.Revision,
		"section_keys":        []string{},
	})
	gs.mustStatus(t, apply, 200)
	var applied graph.Projection
	gs.decode(t, apply, &applied)
	published := nodeOfType(t, applied, graph.NodeImagePrompt)
	if published.PendingCandidateArtifactID != nil || applied.Revision <= after.Revision {
		t.Fatalf("candidate was not published: pending=%v revision=%d", published.PendingCandidateArtifactID, applied.Revision)
	}
	undone := gs.do(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/undo", nil, "")
	gs.mustStatus(t, undone, 200)
	var restored graph.Projection
	gs.decode(t, undone, &restored)
	got := nodeOfType(t, restored, graph.NodeImagePrompt)
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

func TestForceRewritePromptDoesNotChangeLiveUntilApply(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	prompts := &countingPrompt{varyPrompt: true}
	images := &countingImage{}
	executeGraphRun(t, gs, productID, graphID, map[string]any{"scope": "graph"}, prompts, images)
	view := loadProjection(t, gs, productID, graphID)
	promptNode := nodeOfType(t, view, graph.NodeImagePrompt)
	before := cloneConfig(t, promptNode.Config)
	executeGraphRun(t, gs, productID, graphID, map[string]any{
		"scope": "node", "node_id": promptNode.ID, "force": true, "document_action": "rewrite",
	}, prompts, images)
	after := loadProjection(t, gs, productID, graphID)
	rewritten := nodeOfType(t, after, graph.NodeImagePrompt)
	if rewritten.PendingCandidateArtifactID == nil {
		t.Fatal("rewrite must stage a document candidate")
	}
	if pythonish(rewritten.Config["prompt"]) != pythonish(before["prompt"]) {
		t.Fatalf("live prompt changed before apply: %+v vs %+v", rewritten.Config["prompt"], before["prompt"])
	}
}

func TestAdoptSkipsOverwriteWhenUserEditsDuringRun(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	prompt := &midRunBriefEditor{gs: gs, t: t, productID: productID, graphID: graphID}
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
	if brief.CurrentArtifactID != nil || brief.PendingCandidateArtifactID == nil {
		t.Fatalf("generated result must remain a candidate: current=%v pending=%v", brief.CurrentArtifactID, brief.PendingCandidateArtifactID)
	}
	if brief.ConfigStatus != graph.ConfigReady {
		t.Fatalf("status %s", brief.ConfigStatus)
	}
	finishedResp := gs.do(t, "GET", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/"+run.ID, nil, "")
	gs.mustStatus(t, finishedResp, 200)
	var finished graph.GraphRunResponse
	gs.decode(t, finishedResp, &finished)
	for _, nodeRun := range finished.NodeRuns {
		if nodeRun.NodeID != nil && *nodeRun.NodeID == brief.ID {
			if nodeRun.Output["disposition"] != "candidate" {
				t.Fatalf("content adoption output %+v", nodeRun.Output)
			}
			return
		}
	}
	t.Fatalf("brief node run not found in %+v", finished.NodeRuns)
}

type midRunBriefEditor struct {
	countingPrompt
	gs        *graphServer
	t         *testing.T
	productID string
	graphID   string
	edited    bool
}

func (p *midRunBriefEditor) GenerateCreativeBrief(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	view := loadProjection(p.t, p.gs, p.productID, p.graphID)
	brief := nodeOfType(p.t, view, graph.NodeCreativeBrief)
	cfg := cloneConfig(p.t, brief.Config)
	delete(cfg, "document_origin")
	cfg["goal"] = "用户中途改过"
	patchNodeConfig(p.t, p.gs, p.productID, p.graphID, brief.ID, "中途改 brief", view.Revision, cfg)
	p.edited = true
	return p.countingPrompt.GenerateCreativeBrief(ctx, req)
}

func pythonish(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}
