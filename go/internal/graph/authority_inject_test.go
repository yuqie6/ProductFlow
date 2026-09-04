package graph_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/product"
)

func TestAdoptSkipsOverwriteWhenPromptEditedDuringBriefCook(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	before := loadProjection(t, gs, productID, graphID)
	promptNode := nodeOfType(t, before, graph.NodeImagePrompt)
	prompt := &midRunSiblingEditor{
		gs: gs, productID: productID, graphID: graphID,
		nodeID: promptNode.ID, baseRevision: before.Revision,
		edit: func(cfg map[string]any) map[string]any {
			delete(cfg, "document_origin")
			prompt, _ := cfg["prompt"].(map[string]any)
			if prompt == nil {
				prompt = map[string]any{}
			}
			composition, _ := prompt["composition"].(map[string]any)
			if composition == nil {
				composition = map[string]any{}
			}
			composition["layout"] = "用户中途改构图"
			prompt["composition"] = composition
			cfg["prompt"] = prompt
			return cfg
		},
	}
	images := &countingImage{}
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{
		"scope": "graph",
	})
	gs.mustStatus(t, resp, 201)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)
	gs.executeLocallyWithTimeout(t, run.ID, graph.Executor{
		DB: gs.db,
		Deps: graph.Dependencies{
			Prompt: prompt,
			Image:  images,
			Assets: product.Service{DB: gs.db, Media: gs.media},
		},
	}, canvasInjectExecuteTimeout)
	if prompt.err != nil {
		t.Fatal(prompt.err)
	}
	if !prompt.edited {
		t.Fatal("expected mid-run prompt edit")
	}
	view := loadProjection(t, gs, productID, graphID)
	gotNode := nodeOfType(t, view, graph.NodeImagePrompt)
	got, _ := gotNode.Config["prompt"].(map[string]any)
	composition, _ := got["composition"].(map[string]any)
	if composition["layout"] != "用户中途改构图" {
		t.Fatalf("prompt composition %+v", got)
	}
	if gotNode.DocumentOrigin == nil || *gotNode.DocumentOrigin != graph.OriginAuthored {
		t.Fatalf("origin %+v", gotNode.DocumentOrigin)
	}
	if gotNode.PendingCandidateArtifactID == nil {
		t.Fatal("graph run after sibling edit must leave generated prompt as a candidate")
	}
}

func TestAdoptSkipsOverwriteWhenVisualEditedDuringGraphRun(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	before := loadProjection(t, gs, productID, graphID)
	visual := nodeOfType(t, before, graph.NodeVisualSystem)
	prompt := &midRunVisualEditor{
		gs: gs, productID: productID, graphID: graphID,
		nodeID: visual.ID, baseRevision: before.Revision,
	}
	images := &countingImage{}
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{
		"scope": "graph",
	})
	gs.mustStatus(t, resp, 201)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)
	gs.executeLocallyWithTimeout(t, run.ID, graph.Executor{
		DB: gs.db,
		Deps: graph.Dependencies{
			Prompt: prompt,
			Image:  images,
			Assets: product.Service{DB: gs.db, Media: gs.media},
		},
	}, canvasInjectExecuteTimeout)
	if prompt.err != nil {
		t.Fatal(prompt.err)
	}
	if !prompt.edited {
		t.Fatal("expected mid-run visual edit")
	}
	view := loadProjection(t, gs, productID, graphID)
	got := nodeOfType(t, view, graph.NodeVisualSystem)
	overlay, _ := got.Config["visual_overlay"].(map[string]any)
	style, _ := overlay["style"].([]any)
	if len(style) != 1 || style[0] != "用户中途改风格" {
		t.Fatalf("visual overlay %+v", got.Config["visual_overlay"])
	}
	if got.DocumentOrigin == nil || *got.DocumentOrigin != graph.OriginAuthored {
		t.Fatalf("origin %+v", got.DocumentOrigin)
	}
	if got.PendingCandidateArtifactID == nil {
		t.Fatal("graph run after visual edit must leave generated overlay as a candidate")
	}
}

func TestAdoptSkipsOverwriteWhenUndoDuringBriefCook(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	view := loadProjection(t, gs, productID, graphID)
	brief := nodeOfType(t, view, graph.NodeCreativeBrief)
	cfg := cloneConfig(t, brief.Config)
	delete(cfg, "document_origin")
	cfg["goal"] = "undo-before-run"
	patchNodeConfig(t, gs, productID, graphID, brief.ID, "先手填", view.Revision, cfg)
	prompt := &midRunUndoEditor{
		countingPrompt: authorityCountingPrompt(),
		gs:             gs, productID: productID, graphID: graphID,
	}
	images := &countingImage{}
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{
		"scope": "node", "node_id": brief.ID, "force": true, "document_action": "rewrite",
	})
	gs.mustStatus(t, resp, 201)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)
	gs.executeLocallyWithTimeout(t, run.ID, graph.Executor{
		DB: gs.db,
		Deps: graph.Dependencies{
			Prompt: prompt,
			Image:  images,
			Assets: product.Service{DB: gs.db, Media: gs.media},
		},
	}, canvasInjectExecuteTimeout)
	if prompt.err != nil {
		t.Fatal(prompt.err)
	}
	if !prompt.undone {
		t.Fatal("expected mid-run undo")
	}
	after := loadProjection(t, gs, productID, graphID)
	got := nodeOfType(t, after, graph.NodeCreativeBrief)
	if jsonEqual(got.Config["goal"], mockAuthorityBriefGoal) {
		t.Fatal("undo during cook must not let mock brief overwrite live goal")
	}
	if got.PendingCandidateArtifactID == nil {
		t.Fatal("undo during rewrite must leave generated brief as a candidate")
	}
}

func TestCancelGraphRunKeepsMidRunInspectorSave(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	before := loadProjection(t, gs, productID, graphID)
	brief := nodeOfType(t, before, graph.NodeCreativeBrief)
	prompt := &midRunBriefCancelEditor{
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
	prompt.runID = run.ID
	err := gs.tryExecuteLocallyWithTimeout(t, run.ID, graph.Executor{
		DB: gs.db,
		Deps: graph.Dependencies{
			Prompt: prompt,
			Image:  images,
			Assets: product.Service{DB: gs.db, Media: gs.media},
		},
	}, canvasInjectExecuteTimeout)
	if prompt.err != nil {
		t.Fatal(prompt.err)
	}
	if !prompt.edited || !prompt.cancelled {
		t.Fatalf("expected mid-run save and cancel edited=%v cancelled=%v exec=%v", prompt.edited, prompt.cancelled, err)
	}
	view := loadProjection(t, gs, productID, graphID)
	got := nodeOfType(t, view, graph.NodeCreativeBrief)
	if got.Config["goal"] != "用户中途改过" {
		t.Fatalf("cancel must keep inspector save: %+v", got.Config["goal"])
	}
	if got.DocumentOrigin == nil || *got.DocumentOrigin != graph.OriginAuthored {
		t.Fatalf("origin %+v", got.DocumentOrigin)
	}
}

type midRunSiblingEditor struct {
	countingPrompt
	gs                 *graphServer
	productID, graphID string
	nodeID             string
	baseRevision       int
	edit               func(map[string]any) map[string]any
	edited             bool
	err                error
}

func (p *midRunSiblingEditor) GenerateCreativeBrief(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	if err := waitGraphRevisionGreater(ctx, p.gs, p.productID, p.graphID, p.baseRevision); err != nil {
		p.err = err
		return p.countingPrompt.GenerateCreativeBrief(ctx, req)
	}
	err := injectAuthoredNodeConfig(ctx, p.gs, p.productID, p.graphID, p.nodeID, "中途改检查器", p.baseRevision, p.edit)
	if err != nil {
		p.err = err
		return p.countingPrompt.GenerateCreativeBrief(ctx, req)
	}
	p.edited = true
	return p.countingPrompt.GenerateCreativeBrief(ctx, req)
}

type midRunVisualEditor struct {
	countingPrompt
	gs                 *graphServer
	productID, graphID string
	nodeID             string
	baseRevision       int
	edited             bool
	err                error
}

func (p *midRunVisualEditor) GenerateVisualOverlay(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	if err := waitGraphRevisionGreater(ctx, p.gs, p.productID, p.graphID, p.baseRevision); err != nil {
		p.err = err
		return p.countingPrompt.GenerateVisualOverlay(ctx, req)
	}
	err := injectAuthoredNodeConfig(ctx, p.gs, p.productID, p.graphID, p.nodeID, "中途改视觉", p.baseRevision, func(cfg map[string]any) map[string]any {
		delete(cfg, "document_origin")
		overlay, _ := cfg["visual_overlay"].(map[string]any)
		if overlay == nil {
			overlay = map[string]any{}
		}
		overlay["style"] = []any{"用户中途改风格"}
		cfg["visual_overlay"] = overlay
		return cfg
	})
	if err != nil {
		p.err = err
		return p.countingPrompt.GenerateVisualOverlay(ctx, req)
	}
	p.edited = true
	return p.countingPrompt.GenerateVisualOverlay(ctx, req)
}

type midRunUndoEditor struct {
	countingPrompt
	gs                 *graphServer
	productID, graphID string
	undone             bool
	err                error
}

func (p *midRunUndoEditor) GenerateCreativeBrief(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	resp, err := p.gs.doContext(ctx, "POST", "/api/v3/products/"+p.productID+"/workflows/"+p.graphID+"/undo", nil, "")
	if err != nil {
		p.err = err
		return p.countingPrompt.GenerateCreativeBrief(ctx, req)
	}
	status := p.gs.readStatus(resp)
	if status != http.StatusOK {
		p.err = fmt.Errorf("undo status %d", status)
		return p.countingPrompt.GenerateCreativeBrief(ctx, req)
	}
	p.undone = true
	return p.countingPrompt.GenerateCreativeBrief(ctx, req)
}

type midRunBriefCancelEditor struct {
	countingPrompt
	gs                 *graphServer
	productID, graphID string
	nodeID             string
	runID              string
	baseRevision       int
	edited             bool
	cancelled          bool
	err                error
}

func (p *midRunBriefCancelEditor) GenerateCreativeBrief(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
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
	if p.runID == "" {
		p.err = fmt.Errorf("missing run id")
		return p.countingPrompt.GenerateCreativeBrief(ctx, req)
	}
	cancel, err := p.gs.doContext(ctx, "POST", "/api/v3/products/"+p.productID+"/workflows/"+p.graphID+"/runs/"+p.runID+"/cancel", nil, "")
	if err != nil {
		p.err = err
		return p.countingPrompt.GenerateCreativeBrief(ctx, req)
	}
	status := p.gs.readStatus(cancel)
	if status != http.StatusOK {
		p.err = fmt.Errorf("cancel status %d", status)
		return p.countingPrompt.GenerateCreativeBrief(ctx, req)
	}
	p.cancelled = true
	return p.countingPrompt.GenerateCreativeBrief(ctx, req)
}
