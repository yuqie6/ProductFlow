package graph_test

import (
	"context"
	"errors"
	"testing"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/product"
)

func TestAgentChangeSetDuringBriefCookStaysCandidate(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	prompt := &midRunAgentEditor{gs: gs, productID: productID, graphID: graphID}
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
		t.Fatal("expected Agent ChangeSet during brief cook")
	}
	view := loadProjection(t, gs, productID, graphID)
	brief := nodeOfType(t, view, graph.NodeCreativeBrief)
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
	productID, graphID string
	edited             bool
	err                error
}

func (p *midRunAgentEditor) GenerateCreativeBrief(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	svc := p.gs.graphService()
	for range 10 {
		if err := ctx.Err(); err != nil {
			p.err = err
			return p.countingPrompt.GenerateCreativeBrief(ctx, req)
		}
		view, err := svc.Get(ctx, p.productID, p.graphID)
		if err != nil {
			p.err = err
			return p.countingPrompt.GenerateCreativeBrief(ctx, req)
		}
		var brief graph.NodeView
		for _, node := range view.Nodes {
			if node.NodeType == graph.NodeCreativeBrief {
				brief = node
				break
			}
		}
		if brief.ID == "" {
			p.err = errMissingBrief
			return p.countingPrompt.GenerateCreativeBrief(ctx, req)
		}
		cfg := cloneJSONMap(brief.Config)
		delete(cfg, "document_origin")
		cfg["goal"] = "Agent中途改过"
		_, err = svc.ApplyAgentChangeSet(ctx, p.productID, p.graphID, graph.ChangeSet{
			BaseGraphRevision: view.Revision,
			Summary:           "Agent 中途改 brief",
			Operations: []graph.Operation{
				graph.UpdateNodeConfigOp{NodeRef: brief.ID, Config: cfg},
			},
		})
		if err == nil {
			p.edited = true
			return p.countingPrompt.GenerateCreativeBrief(ctx, req)
		}
		var app apperr.Error
		if errors.As(err, &app) && app.Status == 409 {
			continue
		}
		p.err = err
		return p.countingPrompt.GenerateCreativeBrief(ctx, req)
	}
	p.err = errChangeSetConflicted
	return p.countingPrompt.GenerateCreativeBrief(ctx, req)
}

var (
	errMissingBrief        = errors.New("missing brief")
	errChangeSetConflicted = errors.New("changeset still conflicted")
)
