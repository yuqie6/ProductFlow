package graph_test

import (
	"context"
	"testing"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/product"
)

func TestAgentChangeSetDuringBriefCookStaysCandidate(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	view := loadProjection(t, gs, productID, graphID)
	brief := nodeOfType(t, view, graph.NodeCreativeBrief)
	prompt := &midRunAgentEditor{gs: gs, t: t, productID: productID, graphID: graphID}
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
	if !prompt.edited {
		t.Fatal("expected Agent ChangeSet during brief cook")
	}
	view = loadProjection(t, gs, productID, graphID)
	brief = nodeOfType(t, view, graph.NodeCreativeBrief)
	if brief.Config["goal"] != "Agent中途改过" {
		t.Fatalf("goal %+v", brief.Config["goal"])
	}
	if brief.DocumentOrigin == nil || *brief.DocumentOrigin != graph.OriginAuthored {
		t.Fatalf("origin %+v", brief.DocumentOrigin)
	}
	if brief.PendingCandidateArtifactID == nil {
		t.Fatal("Agent edit during cook must leave generated brief as a candidate")
	}
}

type midRunAgentEditor struct {
	countingPrompt
	gs                 *graphServer
	t                  *testing.T
	productID, graphID string
	edited             bool
}

func (p *midRunAgentEditor) GenerateCreativeBrief(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	view := loadProjection(p.t, p.gs, p.productID, p.graphID)
	brief := nodeOfType(p.t, view, graph.NodeCreativeBrief)
	cfg := cloneConfig(p.t, brief.Config)
	delete(cfg, "document_origin")
	cfg["goal"] = "Agent中途改过"
	_, err := p.gs.graphService().ApplyAgentChangeSet(context.Background(), p.productID, p.graphID, graph.ChangeSet{
		BaseGraphRevision: view.Revision,
		Summary:           "Agent 中途改 brief",
		Operations: []graph.Operation{
			graph.UpdateNodeConfigOp{NodeRef: brief.ID, Config: cfg},
		},
	})
	if err != nil {
		p.t.Fatal(err)
	}
	p.edited = true
	return p.countingPrompt.GenerateCreativeBrief(ctx, req)
}
