package graph_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/product"
)

func TestSubmitRunOnEmptyCanvasReturnsNoProcessingNodes(t *testing.T) {
	gs := newGraphServer(t)
	productID := gs.createProduct(t, "空画布跑图")
	created := gs.do(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows", nil, "")
	gs.mustStatus(t, created, http.StatusCreated)
	var payload struct {
		ID string `json:"id"`
	}
	gs.decode(t, created, &payload)
	resp := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+payload.ID+"/runs", map[string]any{})
	gs.mustStatus(t, resp, http.StatusBadRequest)
	var body struct {
		Detail string `json:"detail"`
	}
	gs.decode(t, resp, &body)
	if body.Detail != "没有可运行的处理节点" {
		t.Fatalf("detail %s", body.Detail)
	}
}

func TestSubmitRunStagesPendingDispatch(t *testing.T) {
	gs := newGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	resp := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "graph"})
	gs.mustStatus(t, resp, http.StatusCreated)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)
	if run.Status != "running" {
		t.Fatalf("status %s", run.Status)
	}
	if run.Scope != "graph" {
		t.Fatalf("scope %s", run.Scope)
	}
	if len(run.NodeRuns) == 0 {
		t.Fatal("expected queued node runs")
	}
	for _, node := range run.NodeRuns {
		if node.Status != "queued" {
			t.Fatalf("node %s", node.Status)
		}
	}
	var dispatchStatus string
	err := gs.pool.QueryRow(context.Background(), `
		SELECT status FROM async_dispatches
		WHERE actor_name = 'run_workflow_graph_run' AND aggregate_id = $1
	`, run.ID).Scan(&dispatchStatus)
	if err != nil {
		t.Fatal(err)
	}
	if dispatchStatus != "pending" {
		t.Fatalf("dispatch %s", dispatchStatus)
	}

	again := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "graph"})
	gs.mustStatus(t, again, http.StatusCreated)
	var replay graph.GraphRunResponse
	gs.decode(t, again, &replay)
	if replay.ID != run.ID {
		t.Fatalf("idempotent id %s vs %s", replay.ID, run.ID)
	}

	listed := gs.do(t, http.MethodGet, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", nil, "")
	gs.mustStatus(t, listed, http.StatusOK)
	var list graph.GraphRunListResponse
	gs.decode(t, listed, &list)
	if len(list.Items) != 1 {
		t.Fatalf("list %d", len(list.Items))
	}

	got := gs.do(t, http.MethodGet, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/"+run.ID, nil, "")
	gs.mustStatus(t, got, http.StatusOK)

	cancelled := gs.do(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/"+run.ID+"/cancel", nil, "")
	gs.mustStatus(t, cancelled, http.StatusOK)
	var cancelledRun graph.GraphRunResponse
	gs.decode(t, cancelled, &cancelledRun)
	if cancelledRun.Status != "cancelled" {
		t.Fatalf("cancel %s", cancelledRun.Status)
	}
	if cancelledRun.FailureReason == nil || *cancelledRun.FailureReason != "已取消" {
		t.Fatalf("reason %+v", cancelledRun.FailureReason)
	}
	for _, node := range cancelledRun.NodeRuns {
		if node.Status != "cancelled" {
			id := ""
			if node.NodeID != nil {
				id = *node.NodeID
			}
			t.Fatalf("cancelled node %s %s", id, node.Status)
		}
	}

	conflict := gs.do(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/"+run.ID+"/cancel", nil, "")
	gs.mustStatus(t, conflict, http.StatusOK)

	retry := gs.do(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/"+run.ID+"/retry", nil, "")
	gs.mustStatus(t, retry, http.StatusBadRequest)
}

func TestSubmitRunRejectsUnknownFieldsAndMissingNodeID(t *testing.T) {
	gs := newGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	extra := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "graph", "foo": 1})
	gs.mustStatus(t, extra, http.StatusBadRequest)
	trailing := gs.do(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", strings.NewReader(`{"scope":"graph"} {}`), "application/json")
	gs.mustStatus(t, trailing, http.StatusBadRequest)
	empty := gs.do(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", strings.NewReader(""), "application/json")
	gs.mustStatus(t, empty, http.StatusBadRequest)
	empty.Body.Close()
	missing := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "node"})
	gs.mustStatus(t, missing, http.StatusBadRequest)
	var body struct {
		Detail string `json:"detail"`
	}
	gs.decode(t, missing, &body)
	if body.Detail != "节点运行范围必须指定 node_id" {
		t.Fatalf("detail %s", body.Detail)
	}
}

func TestGraphProjectionIncludesSourceDraftRevisionID(t *testing.T) {
	gs := newGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	resp := gs.do(t, http.MethodGet, "/api/v3/products/"+productID+"/workflows/"+graphID, nil, "")
	gs.mustStatus(t, resp, http.StatusOK)
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["source_draft_revision_id"]; !ok {
		t.Fatalf("missing source_draft_revision_id: %s", raw)
	}
	if payload["source_draft_revision_id"] != nil {
		t.Fatalf("source_draft_revision_id %+v", payload["source_draft_revision_id"])
	}
}

func TestSubmitNodeRunQueuesWhenPromptArtifactMissing(t *testing.T) {
	gs := newGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	current := gs.do(t, http.MethodGet, "/api/v3/products/"+productID+"/workflows/"+graphID, nil, "")
	gs.mustStatus(t, current, http.StatusOK)
	var graphView graph.Projection
	gs.decode(t, current, &graphView)
	var imageID string
	for _, node := range graphView.Nodes {
		if node.NodeType == graph.NodeImageGeneration {
			imageID = node.ID
			break
		}
	}
	if imageID == "" {
		t.Fatal("missing image_generation")
	}
	resp := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{
		"scope": "node", "node_id": imageID,
	})
	gs.mustStatus(t, resp, http.StatusCreated)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)
	if run.Status != "running" {
		t.Fatalf("status %s reason %+v", run.Status, run.FailureReason)
	}
	if run.FailureReason != nil {
		t.Fatalf("submit must not fail for missing prompt artifact: %+v", run.FailureReason)
	}
	var n int
	if err := gs.pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM async_dispatches
		WHERE actor_name = 'run_workflow_graph_run' AND aggregate_id = $1
	`, run.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("NODE run with missing prompt artifact must still enqueue")
	}
}

func TestSubmitNodeRunExecutesUnboundReferenceAsFailed(t *testing.T) {
	gs := newGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	current := gs.do(t, http.MethodGet, "/api/v3/products/"+productID+"/workflows/"+graphID, nil, "")
	gs.mustStatus(t, current, http.StatusOK)
	var graphView graph.Projection
	gs.decode(t, current, &graphView)
	var visualID string
	for _, node := range graphView.Nodes {
		if node.NodeType == graph.NodeVisualSystem {
			visualID = node.ID
			break
		}
	}
	if visualID == "" {
		t.Fatal("missing visual_system")
	}
	added := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/changesets", map[string]any{
		"base_graph_revision": graphView.Revision,
		"summary":             "连接未绑定素材",
		"operations": []map[string]any{
			{"op": "create_node", "client_ref": "unbound-asset", "node_type": "image_asset", "title": "未绑定", "position_x": 40, "position_y": 40, "config": map[string]any{}},
			{"op": "connect_nodes", "client_ref": "unbound-edge", "source_ref": "unbound-asset", "target_ref": visualID},
		},
	})
	gs.mustStatus(t, added, http.StatusOK)
	resp := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{
		"scope": "node", "node_id": visualID,
	})
	gs.mustStatus(t, resp, http.StatusCreated)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)
	if run.Status != "running" {
		t.Fatalf("submit status %s", run.Status)
	}
	executor := graph.Executor{
		DB: gs.db,
		Deps: graph.Dependencies{
			Prompt: graph.MockPromptProvider{},
			Image:  graph.MockImageProvider{},
			Assets: product.Service{DB: gs.db, Media: gs.media},
		},
	}
	gs.executeLocally(t, run.ID, executor)
	got := gs.do(t, http.MethodGet, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/"+run.ID, nil, "")
	gs.mustStatus(t, got, http.StatusOK)
	gs.decode(t, got, &run)
	if run.Status != "failed" {
		t.Fatalf("status %s reason %+v nodes %+v", run.Status, run.FailureReason, run.NodeRuns)
	}
	found := false
	for _, node := range run.NodeRuns {
		if node.FailureReason != nil && strings.Contains(*node.FailureReason, "参考输入缺少已绑定的图片资产") {
			found = true
		}
	}
	if !found && (run.FailureReason == nil || !strings.Contains(*run.FailureReason, "参考输入缺少已绑定的图片资产")) {
		t.Fatalf("reason %+v nodes %+v", run.FailureReason, run.NodeRuns)
	}
}

func TestSubmitGraphRunRejectsForce(t *testing.T) {
	gs := newGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	resp := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{
		"scope": "graph", "force": true, "regenerate_mode": "replace",
	})
	gs.mustStatus(t, resp, http.StatusBadRequest)
	var body struct {
		Detail string `json:"detail"`
	}
	gs.decode(t, resp, &body)
	if body.Detail != "全图运行不能携带 force" {
		t.Fatalf("detail %s", body.Detail)
	}
}

func TestPreviewGraphRunReturnsPlannedActions(t *testing.T) {
	gs := newGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	resp := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/preview", map[string]any{
		"scope": "graph",
	})
	gs.mustStatus(t, resp, http.StatusOK)
	var preview graph.GraphRunPreviewResponse
	gs.decode(t, resp, &preview)
	if preview.Scope != "graph" || len(preview.Nodes) == 0 {
		t.Fatalf("preview %+v", preview)
	}
	for _, node := range preview.Nodes {
		switch node.Action {
		case graph.PlannedGenerate, graph.PlannedReuse, graph.PlannedFrozen, graph.PlannedBlocked:
		default:
			t.Fatalf("action %s", node.Action)
		}
	}
}

func TestPreviewGraphRunRejectsUnknownTarget(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	resp := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/preview", map[string]any{
		"scope":   "node",
		"node_id": "missing-node",
	})
	gs.mustStatus(t, resp, http.StatusBadRequest)
}

func TestSubmitRunQueuesWhenAnotherRunIsActive(t *testing.T) {
	gs := newGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	first := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "graph"})
	gs.mustStatus(t, first, http.StatusCreated)
	var running graph.GraphRunResponse
	gs.decode(t, first, &running)
	if running.Status != "running" {
		t.Fatalf("first %s", running.Status)
	}
	current := gs.do(t, http.MethodGet, "/api/v3/products/"+productID+"/workflows/"+graphID, nil, "")
	gs.mustStatus(t, current, http.StatusOK)
	var graphView graph.Projection
	gs.decode(t, current, &graphView)
	var imageID, promptID string
	for _, node := range graphView.Nodes {
		if node.NodeType == graph.NodeImageGeneration {
			imageID = node.ID
		}
		if node.NodeType == graph.NodePromptGeneration {
			promptID = node.ID
		}
	}
	if imageID == "" || promptID == "" {
		t.Fatal("missing image_generation or prompt_generation")
	}
	second := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{
		"scope": "node", "node_id": imageID,
	})
	gs.mustStatus(t, second, http.StatusCreated)
	var queued graph.GraphRunResponse
	gs.decode(t, second, &queued)
	if queued.ID == running.ID {
		t.Fatal("queued run must be a new row")
	}
	if queued.Status != "queued" {
		t.Fatalf("queued status %s", queued.Status)
	}
	if len(queued.NodeRuns) != 0 {
		t.Fatalf("queued snapshot must wait until dequeue: %+v", queued.NodeRuns)
	}
	dup := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{
		"scope": "node", "node_id": imageID,
	})
	gs.mustStatus(t, dup, http.StatusCreated)
	var again graph.GraphRunResponse
	gs.decode(t, dup, &again)
	if again.ID != queued.ID {
		t.Fatalf("duplicate queued %s vs %s", again.ID, queued.ID)
	}
	const updatedTitle = "排队期间改名"
	renamed := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/changesets", map[string]any{
		"base_graph_revision": graphView.Revision,
		"summary":             "排队期间修改节点",
		"operations": []map[string]any{
			{"op": "rename_node", "node_ref": imageID, "title": updatedTitle},
		},
	})
	gs.mustStatus(t, renamed, http.StatusOK)
	var renamedGraph graph.Projection
	gs.decode(t, renamed, &renamedGraph)
	third := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{
		"scope": "node", "node_id": promptID,
	})
	gs.mustStatus(t, third, http.StatusCreated)
	var laterQueued graph.GraphRunResponse
	gs.decode(t, third, &laterQueued)
	if laterQueued.Status != "queued" || laterQueued.ID == queued.ID {
		t.Fatalf("later queue item %+v", laterQueued)
	}
	cancelled := gs.do(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/"+running.ID+"/cancel", nil, "")
	gs.mustStatus(t, cancelled, http.StatusOK)
	promoted := gs.do(t, http.MethodGet, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/"+queued.ID, nil, "")
	gs.mustStatus(t, promoted, http.StatusOK)
	gs.decode(t, promoted, &queued)
	if queued.Status != "running" {
		t.Fatalf("promoted %s", queued.Status)
	}
	if len(queued.NodeRuns) == 0 {
		t.Fatal("dequeue must snapshot node runs")
	}
	if queued.GraphRevision != renamedGraph.Revision {
		t.Fatalf("dequeue revision %d, want latest %d", queued.GraphRevision, renamedGraph.Revision)
	}
	if queued.NodeRuns[0].NodeTitle == nil || *queued.NodeRuns[0].NodeTitle != updatedTitle {
		t.Fatalf("dequeue node title %+v, want %q", queued.NodeRuns[0].NodeTitle, updatedTitle)
	}
	stillQueued := gs.do(t, http.MethodGet, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/"+laterQueued.ID, nil, "")
	gs.mustStatus(t, stillQueued, http.StatusOK)
	gs.decode(t, stillQueued, &laterQueued)
	if laterQueued.Status != "queued" {
		t.Fatalf("FIFO promoted later item early: %s", laterQueued.Status)
	}
	cancelledQueued := gs.do(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/"+laterQueued.ID+"/cancel", nil, "")
	gs.mustStatus(t, cancelledQueued, http.StatusOK)
	gs.decode(t, cancelledQueued, &laterQueued)
	if laterQueued.Status != "cancelled" {
		t.Fatalf("cancel queued status %s", laterQueued.Status)
	}
}

func TestRecoverPromotesQueuedRunWhenActiveRunIsAlreadyTerminal(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	first := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "graph"})
	gs.mustStatus(t, first, http.StatusCreated)
	var running graph.GraphRunResponse
	gs.decode(t, first, &running)
	second := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{
		"scope": "node", "node_id": running.NodeRuns[0].NodeID,
	})
	gs.mustStatus(t, second, http.StatusCreated)
	var queued graph.GraphRunResponse
	gs.decode(t, second, &queued)
	if queued.Status != "queued" {
		t.Fatalf("queued status %s", queued.Status)
	}

	if _, err := gs.pool.Exec(context.Background(), `
		UPDATE workflow_graph_node_runs
		SET status = 'succeeded', finished_at = NOW(), active_attempt_id = NULL,
		    progress_updated_at = NOW()
		WHERE graph_run_id = $1
	`, running.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := gs.pool.Exec(context.Background(), `
		UPDATE workflow_graph_runs
		SET status = 'succeeded', finished_at = NOW()
		WHERE id = $1
	`, running.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RecoverUnfinishedGraphRuns(context.Background(), gs.pool, time.Hour, product.GraphGuard{}); err != nil {
		t.Fatal(err)
	}

	promoted := gs.do(t, http.MethodGet, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/"+queued.ID, nil, "")
	gs.mustStatus(t, promoted, http.StatusOK)
	gs.decode(t, promoted, &queued)
	if queued.Status != "running" {
		t.Fatalf("recovery status %s", queued.Status)
	}
	if len(queued.NodeRuns) != 1 {
		t.Fatalf("recovery node_runs %+v", queued.NodeRuns)
	}
}

func TestSubmitSelectionRunQueuesExplicitNodes(t *testing.T) {
	gs := newGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)
	current := gs.do(t, http.MethodGet, "/api/v3/products/"+productID+"/workflows/"+graphID, nil, "")
	gs.mustStatus(t, current, http.StatusOK)
	var graphView graph.Projection
	gs.decode(t, current, &graphView)
	var imageID string
	for _, node := range graphView.Nodes {
		if node.NodeType == graph.NodeImageGeneration {
			imageID = node.ID
			break
		}
	}
	if imageID == "" {
		t.Fatal("missing image_generation")
	}
	resp := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{
		"scope": "selection", "node_ids": []string{imageID},
	})
	gs.mustStatus(t, resp, http.StatusCreated)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)
	if run.Scope != "selection" {
		t.Fatalf("scope %s", run.Scope)
	}
	if len(run.NodeRuns) != 1 || run.NodeRuns[0].NodeID == nil || *run.NodeRuns[0].NodeID != imageID {
		t.Fatalf("node_runs %+v", run.NodeRuns)
	}
}

func (gs *graphServer) createDirectGraph(t *testing.T) (productID, graphID string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("name", "直接创建跑图")
	_ = w.WriteField("image_types", `[{"key":"hero","quantity":1}]`)
	part, err := w.CreateFormFile("images", "hero.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(pngBytes(t)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	resp := gs.do(t, http.MethodPost, "/api/v3/products", &buf, w.FormDataContentType())
	gs.mustStatus(t, resp, http.StatusCreated)
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
