package graph_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
)

// B3：本商 changeset/run/SSE 成功；交叉 product/workflow/run 与 SSE 建立统一 404。
func TestMerchantGraphIsolation(t *testing.T) {
	gs := newGraphServer(t)
	productID, graphID := gs.createDirectGraph(t)

	// —— 正测：本商 ——
	current := gs.do(t, http.MethodGet, "/api/v3/products/"+productID+"/workflows/current", nil, "")
	gs.mustStatus(t, current, http.StatusOK)
	var own graph.Projection
	gs.decode(t, current, &own)
	if own.ID != graphID {
		t.Fatalf("own graph %s want %s", own.ID, graphID)
	}

	cs := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/changesets", map[string]any{
		"base_graph_revision": own.Revision,
		"summary":             "本商改标题",
		"operations": []map[string]any{
			{"op": "rename_node", "node_ref": own.Nodes[0].ID, "title": "本商节点"},
		},
	})
	gs.mustStatus(t, cs, http.StatusOK)
	gs.decode(t, cs, &own)

	runResp := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{
		"scope": "graph",
	})
	gs.mustStatus(t, runResp, http.StatusCreated)
	var run graph.GraphRunResponse
	gs.decode(t, runResp, &run)

	getRun := gs.do(t, http.MethodGet, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/"+run.ID, nil, "")
	gs.mustStatus(t, getRun, http.StatusOK)
	getRun.Body.Close()

	listRuns := gs.do(t, http.MethodGet, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", nil, "")
	gs.mustStatus(t, listRuns, http.StatusOK)
	listRuns.Body.Close()

	sseCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sseReq, err := http.NewRequestWithContext(sseCtx, http.MethodGet,
		gs.srv.URL+"/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/"+run.ID+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range gs.cookies {
		sseReq.AddCookie(c)
	}
	sseResp, err := gs.client.Do(sseReq)
	if err != nil {
		t.Fatal(err)
	}
	if sseResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(sseResp.Body)
		sseResp.Body.Close()
		t.Fatalf("own SSE %d %s", sseResp.StatusCode, raw)
	}
	cancel()
	_, _ = io.Copy(io.Discard, sseResp.Body)
	sseResp.Body.Close()

	foreign := seedForeignGraph(t, gs)

	assertCross404 := func(name string, resp *http.Response) {
		t.Helper()
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s want 404 got %d %s", name, resp.StatusCode, raw)
		}
		var body struct {
			Detail string `json:"detail"`
		}
		_ = json.Unmarshal(raw, &body)
		if body.Detail != auth.CrossMerchantDetail {
			t.Fatalf("%s detail=%q want %q body=%s", name, body.Detail, auth.CrossMerchantDetail, raw)
		}
	}

	// —— 反测：他商 product/workflow/run ——
	assertCross404("foreign current", gs.do(t, http.MethodGet, "/api/v3/products/"+foreign.productID+"/workflows/current", nil, ""))
	assertCross404("foreign get", gs.do(t, http.MethodGet, "/api/v3/products/"+foreign.productID+"/workflows/"+foreign.graphID, nil, ""))
	assertCross404("foreign changeset", gs.doJSON(t, http.MethodPost, "/api/v3/products/"+foreign.productID+"/workflows/"+foreign.graphID+"/changesets", map[string]any{
		"base_graph_revision": 1,
		"summary":             "跨商",
		"operations": []map[string]any{
			{"op": "rename_node", "node_ref": "n-missing", "title": "跨商"},
		},
	}))
	assertCross404("foreign create empty", gs.do(t, http.MethodPost, "/api/v3/products/"+foreign.productID+"/workflows", nil, ""))
	assertCross404("foreign submit run", gs.doJSON(t, http.MethodPost, "/api/v3/products/"+foreign.productID+"/workflows/"+foreign.graphID+"/runs", map[string]any{
		"scope": "graph",
	}))
	assertCross404("foreign list runs", gs.do(t, http.MethodGet, "/api/v3/products/"+foreign.productID+"/workflows/"+foreign.graphID+"/runs", nil, ""))
	assertCross404("foreign get run", gs.do(t, http.MethodGet, "/api/v3/products/"+foreign.productID+"/workflows/"+foreign.graphID+"/runs/"+foreign.runID, nil, ""))
	assertCross404("foreign cancel", gs.do(t, http.MethodPost, "/api/v3/products/"+foreign.productID+"/workflows/"+foreign.graphID+"/runs/"+foreign.runID+"/cancel", nil, ""))
	assertCross404("foreign retry", gs.do(t, http.MethodPost, "/api/v3/products/"+foreign.productID+"/workflows/"+foreign.graphID+"/runs/"+foreign.runID+"/retry", nil, ""))

	// 本商 URL + 他商 workflow/run id
	assertStatus404 := func(name string, resp *http.Response) {
		t.Helper()
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s want 404 got %d %s", name, resp.StatusCode, raw)
		}
	}
	assertStatus404("foreign workflow on own product", gs.do(t, http.MethodGet, "/api/v3/products/"+productID+"/workflows/"+foreign.graphID, nil, ""))
	assertStatus404("foreign run on own workflow", gs.do(t, http.MethodGet, "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/"+foreign.runID, nil, ""))

	// SSE：跨商建立即 404（含游标）
	assertCross404("foreign SSE", gs.do(t, http.MethodGet,
		"/api/v3/products/"+foreign.productID+"/workflows/"+foreign.graphID+"/runs/"+foreign.runID+"/events?after=1", nil, ""))
	assertStatus404("foreign SSE on own product", gs.do(t, http.MethodGet,
		"/api/v3/products/"+productID+"/workflows/"+graphID+"/runs/"+foreign.runID+"/events?after=1", nil, ""))
}

type foreignGraphFixture struct {
	merchantID string
	productID  string
	graphID    string
	runID      string
}

func seedForeignGraph(t *testing.T, gs *graphServer) foreignGraphFixture {
	t.Helper()
	now := time.Now().UTC()
	foreignMerchant := schema.Merchants{
		ID: clockid.New(), Name: "夹具他商-B3-graph", Status: "active", CreatedAt: now, UpdatedAt: now,
	}
	if err := gs.db.Create(&foreignMerchant).Error; err != nil {
		t.Fatal(err)
	}
	productID := clockid.New()
	graphID := clockid.New()
	runID := clockid.New()
	if err := gs.db.Create(&schema.Products{
		ID: productID, MerchantID: foreignMerchant.ID, Name: "他商图商品", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gs.db.Create(&schema.WorkflowGraphs{
		ID: graphID, ProductID: productID, Title: "他商图", Active: true,
		SchemaVersion: 3, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gs.db.Create(&schema.WorkflowGraphRuns{
		ID: runID, GraphID: graphID, Status: "running", RunScope: "graph",
		GraphRevision: 1, SnapshotJSON: `{"nodes":[],"edges":[]}`, IsRetryable: true, StartedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = gs.db.Where("id = ?", runID).Delete(&schema.WorkflowGraphRuns{}).Error
		_ = gs.db.Where("id = ?", graphID).Delete(&schema.WorkflowGraphs{}).Error
		_ = gs.db.Where("id = ?", productID).Delete(&schema.Products{}).Error
		_ = gs.db.Where("id = ?", foreignMerchant.ID).Delete(&schema.Merchants{}).Error
	})
	return foreignGraphFixture{
		merchantID: foreignMerchant.ID,
		productID:  productID,
		graphID:    graphID,
		runID:      runID,
	}
}
