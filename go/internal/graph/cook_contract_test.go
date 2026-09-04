package graph_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

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

const canvasInjectExecuteTimeout = 45 * time.Second

func cloneJSONMap(config map[string]any) map[string]any {
	raw, err := json.Marshal(config)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func waitGraphRevisionGreater(ctx context.Context, gs *graphServer, productID, graphID string, baseRevision int) error {
	deadline := time.Now().Add(10 * time.Second)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		resp, err := gs.doContext(ctx, "GET", "/api/v3/products/"+productID+"/workflows/"+graphID, nil, "")
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			gs.readStatus(resp)
			return fmt.Errorf("load graph status %d", resp.StatusCode)
		}
		var view graph.Projection
		if err := decodeHTTP(resp, &view); err != nil {
			return err
		}
		if view.Revision > baseRevision {
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("graph revision still %d, want > %d", view.Revision, baseRevision)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// injectAuthoredNodeConfig 模拟检查器一次保存：用开跑时的 base_graph_revision 发一次 ChangeSet，不重试 409。
func injectAuthoredNodeConfig(ctx context.Context, gs *graphServer, productID, graphID, nodeID, summary string, baseRevision int, edit func(map[string]any) map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	resp, err := gs.doContext(ctx, "GET", "/api/v3/products/"+productID+"/workflows/"+graphID, nil, "")
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		gs.readStatus(resp)
		return fmt.Errorf("load graph status %d", resp.StatusCode)
	}
	var view graph.Projection
	if err := decodeHTTP(resp, &view); err != nil {
		return err
	}
	var node *graph.NodeView
	for i := range view.Nodes {
		if view.Nodes[i].ID == nodeID {
			node = &view.Nodes[i]
			break
		}
	}
	if node == nil {
		return fmt.Errorf("missing node %s", nodeID)
	}
	cfg := edit(cloneJSONMap(node.Config))
	patch, err := gs.doJSONContext(ctx, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/changesets", map[string]any{
		"base_graph_revision": baseRevision,
		"summary":             summary,
		"operations": []map[string]any{
			{"op": "update_node_config", "node_ref": node.ID, "config": cfg},
		},
	})
	if err != nil {
		return err
	}
	status := gs.readStatus(patch)
	if status != http.StatusOK {
		return fmt.Errorf("changeset status %d", status)
	}
	return nil
}

func decodeHTTP(resp *http.Response, dest any) error {
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if dest == nil {
		return nil
	}
	return json.Unmarshal(raw, dest)
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

func TestToNodeAndSelectionAfterAuthoredPromptKeepLive(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	view := loadProjection(t, gs, productID, graphID)
	promptNode := nodeOfType(t, view, graph.NodeImagePrompt)
	imageNode := nodeOfType(t, view, graph.NodeImageGeneration)
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
	composition["layout"] = "手填后再跑到此"
	prompt["composition"] = composition
	cfg["prompt"] = prompt
	patchNodeConfig(t, gs, productID, graphID, promptNode.ID, "手填提示词", view.Revision, cfg)
	prompts := &countingPrompt{}
	images := &countingImage{}
	executeGraphRun(t, gs, productID, graphID, map[string]any{
		"scope": "to_node", "node_id": imageNode.ID,
	}, prompts, images)
	if prompts.promptCalls() != 0 {
		t.Fatalf("to_node after authored prompt called provider %d", prompts.promptCalls())
	}
	afterToNode := loadProjection(t, gs, productID, graphID)
	got := nodeOfType(t, afterToNode, graph.NodeImagePrompt)
	gotPrompt, _ := got.Config["prompt"].(map[string]any)
	gotComp, _ := gotPrompt["composition"].(map[string]any)
	if gotComp["layout"] != "手填后再跑到此" {
		t.Fatalf("to_node overwrote prompt %+v", gotPrompt)
	}
	if got.DocumentOrigin == nil || *got.DocumentOrigin != graph.OriginAuthored {
		t.Fatalf("origin %+v", got.DocumentOrigin)
	}
	executeGraphRun(t, gs, productID, graphID, map[string]any{
		"scope": "selection", "node_ids": []string{imageNode.ID},
	}, prompts, images)
	if prompts.promptCalls() != 0 {
		t.Fatalf("selection after authored prompt called provider %d", prompts.promptCalls())
	}
	afterSelection := loadProjection(t, gs, productID, graphID)
	again := nodeOfType(t, afterSelection, graph.NodeImagePrompt)
	againPrompt, _ := again.Config["prompt"].(map[string]any)
	againComp, _ := againPrompt["composition"].(map[string]any)
	if againComp["layout"] != "手填后再跑到此" {
		t.Fatalf("selection overwrote prompt %+v", againPrompt)
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
	before := loadProjection(t, gs, productID, graphID)
	brief := nodeOfType(t, before, graph.NodeCreativeBrief)
	prompt := &midRunBriefEditor{
		gs: gs, productID: productID, graphID: graphID,
		nodeID: brief.ID, baseRevision: before.Revision,
	}
	images := &countingImage{}
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{
		"scope": "graph",
	})
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
	gs.executeLocallyWithTimeout(t, run.ID, executor, canvasInjectExecuteTimeout)
	if prompt.err != nil {
		t.Fatal(prompt.err)
	}
	if !prompt.edited {
		t.Fatal("expected mid-run brief edit")
	}
	view := loadProjection(t, gs, productID, graphID)
	got := nodeOfType(t, view, graph.NodeCreativeBrief)
	if got.Config["goal"] != "用户中途改过" {
		t.Fatalf("goal %+v", got.Config["goal"])
	}
	if got.DocumentOrigin == nil || *got.DocumentOrigin != graph.OriginAuthored {
		t.Fatalf("origin %+v", got.DocumentOrigin)
	}
	if got.CurrentArtifactID != nil || got.PendingCandidateArtifactID == nil {
		t.Fatalf("generated result must remain a candidate: current=%v pending=%v", got.CurrentArtifactID, got.PendingCandidateArtifactID)
	}
	if got.ConfigStatus != graph.ConfigReady {
		t.Fatalf("status %s", got.ConfigStatus)
	}
	finishedResp := gs.do(t, "GET", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/"+run.ID, nil, "")
	gs.mustStatus(t, finishedResp, 200)
	var finished graph.GraphRunResponse
	gs.decode(t, finishedResp, &finished)
	for _, nodeRun := range finished.NodeRuns {
		if nodeRun.NodeID != nil && *nodeRun.NodeID == got.ID {
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
	gs           *graphServer
	productID    string
	graphID      string
	nodeID       string
	baseRevision int
	edited       bool
	err          error
}

func (p *midRunBriefEditor) GenerateCreativeBrief(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	if err := waitGraphRevisionGreater(ctx, p.gs, p.productID, p.graphID, p.baseRevision); err != nil {
		p.err = err
		return p.countingPrompt.GenerateCreativeBrief(ctx, req)
	}
	err := injectAuthoredNodeConfig(ctx, p.gs, p.productID, p.graphID, p.nodeID, "中途改 brief", p.baseRevision, func(cfg map[string]any) map[string]any {
		delete(cfg, "document_origin")
		cfg["goal"] = "用户中途改过"
		return cfg
	})
	if err != nil {
		p.err = err
		return p.countingPrompt.GenerateCreativeBrief(ctx, req)
	}
	p.edited = true
	return p.countingPrompt.GenerateCreativeBrief(ctx, req)
}

func pythonish(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}
