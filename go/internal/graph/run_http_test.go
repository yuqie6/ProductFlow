package graph_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/yuqie6/productflow/internal/graph"
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
