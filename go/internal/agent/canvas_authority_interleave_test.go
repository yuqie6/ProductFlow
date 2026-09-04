package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/product"
)

func TestApplyGraphToolDuringBriefCookDoesNotOverwriteLive(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	_, _ = as.pool.Exec(context.Background(), `
		INSERT INTO app_settings (key, value, created_at, updated_at)
		VALUES ('generation_max_concurrent_tasks', '20', NOW(), NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`)
	productID, graphID := createDirectGraphForAgent(t, as)
	ensure := as.do(t, http.MethodPost, "/api/v2/products/"+productID+"/agent-workbench", nil, "", http.Header{
		"Idempotency-Key": []string{clockid.New()},
	})
	as.mustStatus(t, ensure, http.StatusOK)
	var bench WorkbenchResponse
	as.decode(t, ensure, &bench)
	if bench.Conversation.ID == "" {
		t.Fatal("missing product-workflow conversation")
	}

	gotGraph := as.do(t, http.MethodGet, "/api/v3/products/"+productID+"/workflows/"+graphID, nil, "", nil)
	as.mustStatus(t, gotGraph, http.StatusOK)
	var start graph.Projection
	as.decode(t, gotGraph, &start)
	var briefID string
	for _, node := range start.Nodes {
		if node.NodeType == graph.NodeCreativeBrief {
			briefID = node.ID
			break
		}
	}
	if briefID == "" {
		t.Fatal("missing brief")
	}

	prompt := &agentMidRunGraphTool{
		as: as, t: t, productID: productID, graphID: graphID, conversationID: bench.Conversation.ID,
	}
	resp := as.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{
		"scope": "node", "node_id": briefID, "force": true, "document_action": "rewrite",
	})
	as.mustStatus(t, resp, http.StatusCreated)
	var run graph.GraphRunResponse
	as.decode(t, resp, &run)
	executeAgentGraphRun(t, as, run.ID, graph.Executor{
		DB: as.db,
		Deps: graph.Dependencies{
			Prompt: prompt,
			Image:  graph.MockImageProvider{},
			Assets: product.Service{DB: as.db, Media: as.svc.Media},
		},
	})
	if !prompt.edited {
		t.Fatal("expected apply_graph_change_set_v1 during brief cook")
	}

	got := as.do(t, http.MethodGet, "/api/v3/products/"+productID+"/workflows/"+graphID, nil, "", nil)
	as.mustStatus(t, got, http.StatusOK)
	var view graph.Projection
	as.decode(t, got, &view)
	var brief graph.NodeView
	for _, node := range view.Nodes {
		if node.NodeType == graph.NodeCreativeBrief {
			brief = node
			break
		}
	}
	if brief.ID == "" {
		t.Fatal("missing brief")
	}
	if brief.Config["goal"] != "Agent工具中途改过" {
		t.Fatalf("goal %+v", brief.Config["goal"])
	}
	if brief.PendingCandidateArtifactID == nil {
		t.Fatal("tool write during cook must leave generated brief as a candidate")
	}
}

type agentMidRunGraphTool struct {
	graph.MockPromptProvider
	as                                 *agentServer
	t                                  *testing.T
	productID, graphID, conversationID string
	edited                             bool
}

func (p *agentMidRunGraphTool) GenerateCreativeBrief(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error) {
	got := p.as.do(p.t, http.MethodGet, "/api/v3/products/"+p.productID+"/workflows/"+p.graphID, nil, "", nil)
	p.as.mustStatus(p.t, got, http.StatusOK)
	var view graph.Projection
	p.as.decode(p.t, got, &view)
	var brief graph.NodeView
	for _, node := range view.Nodes {
		if node.NodeType == graph.NodeCreativeBrief {
			brief = node
			break
		}
	}
	cfg := cloneAgentConfig(brief.Config)
	delete(cfg, "document_origin")
	cfg["goal"] = "Agent工具中途改过"
	apply := p.as.doJSONAuth(p.t, http.MethodPost, "/api/internal/v1/agent-conversations/"+p.conversationID+"/graph/apply-change-set", map[string]any{
		"change_set": map[string]any{
			"base_graph_revision": view.Revision,
			"summary":             "Agent 工具中途改 brief",
			"operations": []map[string]any{
				{"op": "update_node_config", "node_ref": brief.ID, "config": cfg},
			},
		},
	}, http.Header{
		"Authorization":   []string{"Bearer tok"},
		"Idempotency-Key": []string{clockid.New()},
	})
	p.as.mustStatus(p.t, apply, http.StatusOK)
	apply.Body.Close()
	p.edited = true
	return p.MockPromptProvider.GenerateCreativeBrief(ctx, req)
}

func createDirectGraphForAgent(t *testing.T, as *agentServer) (productID, graphID string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 6))
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("name", "Agent 交错画布")
	_ = w.WriteField("image_types", `[{"key":"hero","quantity":1}]`)
	part, err := w.CreateFormFile("images", "hero.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(pngBuf.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	resp := as.do(t, http.MethodPost, "/api/v3/products", &buf, w.FormDataContentType(), nil)
	as.mustStatus(t, resp, http.StatusCreated)
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
	return payload.Product.ID, payload.Graph.ID
}

func executeAgentGraphRun(t *testing.T, as *agentServer, runID string, exec graph.Executor) {
	t.Helper()
	if exec.Products == nil {
		exec.Products = product.GraphGuard{}
	}
	for i := 0; i < 30; i++ {
		reclaimAgentGraphRun(t, as, runID)
		err := exec.ExecuteRun(context.Background(), runID)
		if err == nil {
			return
		}
		if errors.Is(err, queue.ErrBusy) || errors.Is(err, queue.ErrLater) {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		t.Fatal(err)
	}
	t.Fatal("could not execute graph run")
}

func reclaimAgentGraphRun(t *testing.T, as *agentServer, runID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := as.pool.Exec(ctx, `DELETE FROM async_dispatches WHERE aggregate_id = $1`, runID); err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(ctx, `
		DELETE FROM workflow_graph_provider_effects
		WHERE node_run_id IN (SELECT id FROM workflow_graph_node_runs WHERE graph_run_id = $1)
	`, runID); err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(ctx, `
		UPDATE workflow_graph_node_runs SET
			status = 'queued', failure_reason = NULL, finished_at = NULL,
			output_json = NULL, active_attempt_id = NULL,
			progress_phase = NULL, progress_updated_at = NOW()
		WHERE graph_run_id = $1
	`, runID); err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(ctx, `
		UPDATE workflow_graph_runs SET
			status = 'running', failure_reason = NULL, finished_at = NULL, is_retryable = TRUE
		WHERE id = $1
	`, runID); err != nil {
		t.Fatal(err)
	}
}

func cloneAgentConfig(config map[string]any) map[string]any {
	raw, err := json.Marshal(config)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	return out
}
