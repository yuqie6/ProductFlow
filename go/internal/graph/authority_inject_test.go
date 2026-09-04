package graph_test

import (
	"context"
	"testing"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/product"
)

func TestAdoptSkipsOverwriteWhenPromptEditedDuringBriefCook(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	view := loadProjection(t, gs, productID, graphID)
	brief := nodeOfType(t, view, graph.NodeCreativeBrief)
	prompt := &midRunSiblingEditor{gs: gs, t: t, productID: productID, graphID: graphID, target: graph.NodeImagePrompt}
	images := &countingImage{}
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{
		"scope": "node", "node_id": brief.ID,
	})
	gs.mustStatus(t, resp, 201)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)
	gs.executeLocally(t, run.ID, graph.Executor{
		DB: gs.db,
		Deps: graph.Dependencies{
			Prompt: prompt,
			Image:  images,
			Assets: product.Service{DB: gs.db, Media: gs.media},
		},
	})
	if !prompt.edited {
		t.Fatal("expected mid-run prompt edit")
	}
	view = loadProjection(t, gs, productID, graphID)
	promptNode := nodeOfType(t, view, graph.NodeImagePrompt)
	got, _ := promptNode.Config["prompt"].(map[string]any)
	composition, _ := got["composition"].(map[string]any)
	if composition["layout"] != "用户中途改构图" {
		t.Fatalf("prompt composition %+v", got)
	}
	if promptNode.DocumentOrigin == nil || *promptNode.DocumentOrigin != graph.OriginAuthored {
		t.Fatalf("origin %+v", promptNode.DocumentOrigin)
	}
	executeGraphRun(t, gs, productID, graphID, map[string]any{
		"scope": "node", "node_id": promptNode.ID, "force": true, "document_action": "complete",
	}, &prompt.countingPrompt, images)
	after := loadProjection(t, gs, productID, graphID)
	cooked := nodeOfType(t, after, graph.NodeImagePrompt)
	got, _ = cooked.Config["prompt"].(map[string]any)
	composition, _ = got["composition"].(map[string]any)
	if composition["layout"] != "用户中途改构图" {
		t.Fatalf("prompt cook overwrote sibling edit %+v", got)
	}
	if cooked.PendingCandidateArtifactID == nil {
		t.Fatal("prompt cook after sibling edit must remain a candidate")
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
		gs:             gs, t: t, productID: productID, graphID: graphID,
	}
	images := &countingImage{}
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{
		"scope": "node", "node_id": brief.ID, "force": true, "document_action": "rewrite",
	})
	gs.mustStatus(t, resp, 201)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)
	gs.executeLocally(t, run.ID, graph.Executor{
		DB: gs.db,
		Deps: graph.Dependencies{
			Prompt: prompt,
			Image:  images,
			Assets: product.Service{DB: gs.db, Media: gs.media},
		},
	})
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
	t                  *testing.T
	productID, graphID string
	target             graph.NodeType
	edited             bool
}

func (p *midRunSiblingEditor) GenerateCreativeBrief(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	view := loadProjection(p.t, p.gs, p.productID, p.graphID)
	node := nodeOfType(p.t, view, p.target)
	cfg := cloneConfig(p.t, node.Config)
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
	patchNodeConfig(p.t, p.gs, p.productID, p.graphID, node.ID, "中途改 prompt", view.Revision, cfg)
	p.edited = true
	return p.countingPrompt.GenerateCreativeBrief(ctx, req)
}

type midRunUndoEditor struct {
	countingPrompt
	gs                 *graphServer
	t                  *testing.T
	productID, graphID string
	undone             bool
}

func (p *midRunUndoEditor) GenerateCreativeBrief(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	resp := p.gs.do(p.t, "POST", "/api/v3/products/"+p.productID+"/workflows/"+p.graphID+"/undo", nil, "")
	if resp.StatusCode != 200 && resp.StatusCode != 409 {
		p.t.Fatalf("undo status %d", p.gs.readStatus(resp))
	}
	if resp.StatusCode == 200 {
		p.gs.decode(p.t, resp, &graph.Projection{})
	} else {
		p.gs.readStatus(resp)
	}
	p.undone = true
	return p.countingPrompt.GenerateCreativeBrief(ctx, req)
}
