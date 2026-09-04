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
	prompt := &midRunSiblingEditor{gs: gs, productID: productID, graphID: graphID, target: graph.NodeImagePrompt}
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
	promptNode := nodeOfType(t, view, graph.NodeImagePrompt)
	got, _ := promptNode.Config["prompt"].(map[string]any)
	composition, _ := got["composition"].(map[string]any)
	if composition["layout"] != "用户中途改构图" {
		t.Fatalf("prompt composition %+v", got)
	}
	if promptNode.DocumentOrigin == nil || *promptNode.DocumentOrigin != graph.OriginAuthored {
		t.Fatalf("origin %+v", promptNode.DocumentOrigin)
	}
	if promptNode.PendingCandidateArtifactID == nil {
		t.Fatal("graph run after sibling edit must leave generated prompt as a candidate")
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

type midRunSiblingEditor struct {
	countingPrompt
	gs                 *graphServer
	productID, graphID string
	target             graph.NodeType
	edited             bool
	err                error
}

func (p *midRunSiblingEditor) GenerateCreativeBrief(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	err := injectAuthoredNodeConfig(ctx, p.gs, p.productID, p.graphID, p.target, "中途改 prompt", func(cfg map[string]any) map[string]any {
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
	})
	if err != nil {
		p.err = err
		return p.countingPrompt.GenerateCreativeBrief(ctx, req)
	}
	p.edited = true
	return p.countingPrompt.GenerateCreativeBrief(ctx, req)
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
	if status != http.StatusOK && status != http.StatusConflict {
		p.err = fmt.Errorf("undo status %d", status)
		return p.countingPrompt.GenerateCreativeBrief(ctx, req)
	}
	p.undone = true
	return p.countingPrompt.GenerateCreativeBrief(ctx, req)
}
