package graph_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/agent"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/product"
)

func TestPersistedInvalidConfigRejectsHTTPAndAgentRunConsumers(t *testing.T) {
	gs := newGraphServer(t)
	ctx := context.Background()
	productID, graphID := gs.createDirectGraph(t)
	view := loadProjection(t, gs, productID, graphID)
	image := nodeOfType(t, view, graph.NodeImageGeneration)
	svc := agent.RecoveryService(gs.pool, gs.db)
	workspace, err := svc.EnsureWorkbench(ctx, productID, clockid.New(), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PrepareWorkflowRunRequest(ctx, workspace.Conversation.ID, view.Revision, nil, nil); err != nil {
		t.Fatalf("current direct-create contract must prepare: %v", err)
	}
	config := cloneConfig(t, image.Config)
	config["generation_spec"].(map[string]any)["text_policy"] = "none"
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a persisted retired shape without granting it a current write path.
	if _, err := gs.pool.Exec(ctx, `UPDATE workflow_graph_nodes SET config_json = $2 WHERE id = $1`, image.ID, string(raw)); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"graph", "node", "to_node", "selection"} {
		t.Run(scope, func(t *testing.T) {
			input := map[string]any{"scope": scope}
			if scope == "node" || scope == "to_node" {
				input["node_id"] = image.ID
			} else if scope == "selection" {
				input["node_ids"] = []string{image.ID}
			}
			base := "/api/v3/products/" + productID + "/workflows/" + graphID + "/runs"
			previewResp := gs.doJSON(t, http.MethodPost, base+"/preview", input)
			gs.mustStatus(t, previewResp, http.StatusOK)
			var preview graph.GraphRunPreviewResponse
			gs.decode(t, previewResp, &preview)
			blocked := false
			for _, node := range preview.Nodes {
				if node.NodeID == image.ID {
					blocked = node.Action == graph.PlannedBlocked && strings.Contains(node.Reason, "text_policy")
				}
			}
			if !blocked {
				t.Fatalf("preview must identify invalid image: %+v", preview)
			}
			resp := gs.doJSON(t, http.MethodPost, base, input)
			gs.mustStatus(t, resp, http.StatusBadRequest)
			var body struct {
				Detail string `json:"detail"`
			}
			gs.decode(t, resp, &body)
			if !strings.Contains(body.Detail, "text_policy") {
				t.Fatalf("error must preserve field cause: %+v", body)
			}
		})
	}
	_, err = svc.PrepareWorkflowRunRequest(ctx, workspace.Conversation.ID, view.Revision, nil, nil)
	var appErr apperr.Error
	if !errors.As(err, &appErr) || appErr.Status != http.StatusConflict {
		t.Fatalf("Agent must reject invalid graph before confirming a run: %v", err)
	}
	var count int
	if err := gs.pool.QueryRow(ctx, `SELECT count(*) FROM workflow_graph_runs WHERE graph_id = $1`, graphID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rejected requests persisted %d runs", count)
	}
}

func TestAgentIntakePersistsCurrentNodeAndTextContracts(t *testing.T) {
	gs := newGraphServer(t)
	ctx := context.Background()
	svc := agent.RecoveryService(gs.pool, gs.db)
	svc.Product.Media = gs.media
	created, err := svc.Product.CreateAgentDraft(ctx, "当前节点合同", clockid.New(), nil)
	if err != nil {
		t.Fatal(err)
	}
	assets, err := svc.Product.AddImages(ctx, created.Product.ID, []product.Upload{{Content: pngBytes(t), Filename: "reference.png", MIMEType: "image/png"}})
	if err != nil {
		t.Fatal(err)
	}
	selection := json.RawMessage(`{"schema_version":1,"image_types":[{"key":"hero","quantity":1,"order":0},{"key":"selling_point","quantity":1,"order":1}]}`)
	result, err := svc.FinalizeProductIntake(ctx, created.Conversation.ID, clockid.New(), selection, []string{assets[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	if result["graph_expanded"] != true {
		t.Fatalf("intake must expand the name-only graph: %+v", result)
	}
	live, err := graph.TryLive(ctx, gs.db, created.Product.ID)
	if err != nil || live == nil {
		t.Fatalf("live graph: %v", err)
	}
	for _, node := range live.Applied.Nodes {
		if _, err := graph.NormalizeNodeConfig(node.NodeType, node.Config); err != nil {
			t.Fatalf("intake persisted invalid %s config: %v", node.NodeType, err)
		}
		if node.NodeType == graph.NodeImagePrompt && node.Config["text_settings"] == nil {
			t.Fatalf("prompt must own text defaults: %+v", node.Config)
		}
	}
}
