package graph_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/settings"
	"gorm.io/gorm"
)

type graphServer struct {
	pool    *pgxpool.Pool
	db      *gorm.DB
	media   media.Store
	srv     *httptest.Server
	client  *http.Client
	cookies []*http.Cookie
}

func newGraphServer(t *testing.T) *graphServer {
	t.Helper()
	pool, gdb := testdb.Open(t)
	return startGraphServer(t, pool, gdb)
}

func newIsolatedGraphServer(t *testing.T) *graphServer {
	t.Helper()
	raw := os.Getenv("DATABASE_URL")
	if raw == "" {
		t.Skip("DATABASE_URL not set")
	}
	name := fmt.Sprintf("pf_gexec_%d", time.Now().UnixNano()%1_000_000_000)
	_, pool, gdb := isolatedMigratedDB(t, testdb.Pool(t), raw, name)
	return startGraphServer(t, pool, gdb)
}

func startGraphServer(t *testing.T, pool *pgxpool.Pool, gdb *gorm.DB) *graphServer {
	root := t.TempDir()
	engine := httpx.NewEngine(nil)
	engine.Use(httpx.Session(httpx.NewCookieStore(httpx.SessionConfig{Secret: "test-session-secret-key"})))
	settingsStore := settings.NewStore(pool, config.Config{
		AdminAccessRequired:      true,
		UploadMaxImageBytes:      10 * 1024 * 1024,
		UploadMaxPixels:          16_000_000,
		UploadAllowedMIMETypes:   "image/png,image/jpeg,image/webp",
		UploadMaxBatchFiles:      20,
		UploadMaxBatchBytes:      50 * 1024 * 1024,
		UploadMaxReferenceImages: 6,
	})
	auth.HTTP{AdminAccessKey: "k", Store: settingsStore}.Register(engine)
	mediaStore := media.Store{Files: storage.Local{Root: root}}
	product.HTTP{Service: product.Service{DB: gdb, Media: mediaStore}, Settings: settingsStore}.Register(engine)
	graph.HTTP{Service: graph.Service{DB: gdb, Pool: pool, Products: product.GraphGuard{}}, Settings: settingsStore}.Register(engine)
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	gs := &graphServer{pool: pool, db: gdb, media: mediaStore, srv: srv, client: &http.Client{}}
	login, err := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/session", strings.NewReader(`{"admin_key":"k"}`))
	if err != nil {
		t.Fatal(err)
	}
	login.Header.Set("Content-Type", "application/json")
	resp, err := gs.client.Do(login)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login %d", resp.StatusCode)
	}
	gs.cookies = resp.Cookies()
	var previousCapacity *string
	_ = gs.pool.QueryRow(context.Background(), `SELECT value FROM app_settings WHERE key = 'generation_max_concurrent_tasks'`).Scan(&previousCapacity)
	_, _ = gs.pool.Exec(context.Background(), `
		INSERT INTO app_settings (key, value, created_at, updated_at)
		VALUES ('admin_access_required', 'true', NOW(), NOW()),
		       ('generation_max_concurrent_tasks', '20', NOW(), NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`)
	t.Cleanup(func() {
		if previousCapacity == nil {
			_, _ = gs.pool.Exec(context.Background(), `DELETE FROM app_settings WHERE key = 'generation_max_concurrent_tasks'`)
			return
		}
		_, _ = gs.pool.Exec(context.Background(), `
			UPDATE app_settings SET value = $1, updated_at = NOW() WHERE key = 'generation_max_concurrent_tasks'
		`, *previousCapacity)
	})
	return gs
}

func (gs *graphServer) dropRunDispatch(t *testing.T, runID string) {
	t.Helper()
	if _, err := gs.pool.Exec(context.Background(), `DELETE FROM async_dispatches WHERE aggregate_id = $1`, runID); err != nil {
		t.Fatal(err)
	}
}

func (gs *graphServer) reclaimRun(t *testing.T, runID string) {
	t.Helper()
	ctx := context.Background()
	gs.dropRunDispatch(t, runID)
	if _, err := gs.pool.Exec(ctx, `
		DELETE FROM workflow_graph_provider_effects
		WHERE node_run_id IN (SELECT id FROM workflow_graph_node_runs WHERE graph_run_id = $1)
	`, runID); err != nil {
		t.Fatal(err)
	}
	if _, err := gs.pool.Exec(ctx, `
		UPDATE workflow_graph_node_runs SET
			status = 'queued', failure_reason = NULL, finished_at = NULL,
			output_json = NULL, active_attempt_id = NULL,
			progress_phase = NULL, progress_updated_at = NOW()
		WHERE graph_run_id = $1
	`, runID); err != nil {
		t.Fatal(err)
	}
	if _, err := gs.pool.Exec(ctx, `
		UPDATE workflow_graph_runs SET
			status = 'running', failure_reason = NULL, finished_at = NULL, is_retryable = TRUE
		WHERE id = $1
	`, runID); err != nil {
		t.Fatal(err)
	}
}

func (gs *graphServer) executeLocally(t *testing.T, runID string, exec graph.Executor) {
	t.Helper()
	if err := gs.tryExecuteLocally(t, runID, exec); err != nil {
		t.Fatal(err)
	}
}

func (gs *graphServer) executeLocallyWithTimeout(t *testing.T, runID string, exec graph.Executor, timeout time.Duration) {
	t.Helper()
	if err := gs.tryExecuteLocallyWithTimeout(t, runID, exec, timeout); err != nil {
		t.Fatal(err)
	}
}

func (gs *graphServer) tryExecuteLocally(t *testing.T, runID string, exec graph.Executor) error {
	t.Helper()
	return gs.tryExecuteLocallyWithTimeout(t, runID, exec, 2*time.Minute)
}

func (gs *graphServer) tryExecuteLocallyWithTimeout(t *testing.T, runID string, exec graph.Executor, timeout time.Duration) error {
	t.Helper()
	if exec.Products == nil {
		exec.Products = product.GraphGuard{}
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		gs.reclaimRun(t, runID)
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		err := exec.ExecuteRun(ctx, runID)
		cancel()
		if err == nil {
			return nil
		}
		if errors.Is(err, queue.ErrBusy) || errors.Is(err, queue.ErrLater) {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		return err
	}
	return errors.New("could not execute graph run locally")
}

func (gs *graphServer) do(t *testing.T, method, path string, body io.Reader, contentType string) *http.Response {
	t.Helper()
	resp, err := gs.doContext(context.Background(), method, path, body, contentType)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func (gs *graphServer) doContext(ctx context.Context, method, path string, body io.Reader, contentType string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, gs.srv.URL+path, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for _, c := range gs.cookies {
		req.AddCookie(c)
	}
	return gs.client.Do(req)
}

func (gs *graphServer) doJSON(t *testing.T, method, path string, payload any) *http.Response {
	t.Helper()
	resp, err := gs.doJSONContext(context.Background(), method, path, payload)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func (gs *graphServer) doJSONContext(ctx context.Context, method, path string, payload any) (*http.Response, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return gs.doContext(ctx, method, path, bytes.NewReader(raw), "application/json")
}

func (gs *graphServer) decode(t *testing.T, resp *http.Response, dest any) {
	t.Helper()
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if dest == nil {
		return
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
}

func (gs *graphServer) mustStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("status %d want %d %s", resp.StatusCode, want, raw)
	}
}

func (gs *graphServer) graphService() graph.Service {
	return graph.Service{DB: gs.db, Pool: gs.pool, Products: product.GraphGuard{}}
}

func (gs *graphServer) readStatus(resp *http.Response) int {
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func (gs *graphServer) createProduct(t *testing.T, name string) string {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("name", name)
	part, err := w.CreateFormFile("images", "cup.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(pngBytes(t)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	resp := gs.do(t, http.MethodPost, "/api/v2/products", &buf, w.FormDataContentType())
	gs.mustStatus(t, resp, http.StatusCreated)
	var created struct {
		Product struct {
			ID string `json:"id"`
		} `json:"product"`
	}
	gs.decode(t, resp, &created)
	return created.Product.ID
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 6))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestCatalogHTTPMatchesDocument(t *testing.T) {
	gs := newGraphServer(t)
	resp := gs.do(t, http.MethodGet, "/api/v3/node-catalog", nil, "")
	gs.mustStatus(t, resp, http.StatusOK)
	var payload map[string]any
	gs.decode(t, resp, &payload)
	want, err := json.Marshal(graph.CatalogJSON())
	if err != nil {
		t.Fatal(err)
	}
	var expected map[string]any
	if err := json.Unmarshal(want, &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(payload, expected) {
		got, _ := json.Marshal(payload)
		t.Fatalf("catalog mismatch\n got %s\nwant %s", got, want)
	}
	if payload["version"].(float64) != float64(graph.CatalogVersion) {
		t.Fatalf("version %+v", payload["version"])
	}
	nodes := payload["nodes"].([]any)
	if len(nodes) != 6 {
		t.Fatalf("nodes %d", len(nodes))
	}
	first := nodes[0].(map[string]any)
	if first["node_type"] != "product_source" || first["kind"] != "source" {
		t.Fatalf("%+v", first)
	}
	image := nodes[5].(map[string]any)
	if image["node_type"] != "image_generation" || image["kind"] != "effect" {
		t.Fatalf("%+v", image)
	}
	prompt := nodes[4].(map[string]any)
	if prompt["node_type"] != "image_prompt" || prompt["kind"] != "document" {
		t.Fatalf("%+v", prompt)
	}
	actions := prompt["document_actions"].([]any)
	if len(actions) != 3 || actions[0] != "complete" || actions[1] != "rewrite" || actions[2] != "replace" {
		t.Fatalf("document_actions %+v", actions)
	}
	fields := image["config_fields"].([]any)
	var generation map[string]any
	for _, raw := range fields {
		field := raw.(map[string]any)
		if field["key"] == "generation_spec" {
			generation = field
			break
		}
	}
	if generation == nil {
		t.Fatal("missing generation_spec")
	}
	def := generation["default"].(map[string]any)
	if def["aspect_ratio"] != "1:1" {
		t.Fatalf("default %+v", def)
	}
	var aspect map[string]any
	for _, raw := range generation["fields"].([]any) {
		field := raw.(map[string]any)
		if field["key"] == "aspect_ratio" {
			aspect = field
			break
		}
	}
	if aspect["control"] != "aspect_ratio" || aspect["label_key"] != "agentWorkbench.nodeEditor.aspectRatio" || aspect["panel"] != "basic" {
		t.Fatalf("aspect %+v", aspect)
	}
}

func TestEmptyCanvasCreateHTTPPersistsAndRejectsSecond(t *testing.T) {
	gs := newGraphServer(t)
	productID := gs.createProduct(t, "空白建图商品")
	created := gs.do(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows", nil, "")
	gs.mustStatus(t, created, http.StatusCreated)
	var payload graph.Projection
	gs.decode(t, created, &payload)
	if payload.SchemaVersion != 3 || payload.Revision != 1 || len(payload.Nodes) != 0 || len(payload.Edges) != 0 {
		t.Fatalf("%+v", payload)
	}
	if payload.CanUndo || payload.CanRedo {
		t.Fatalf("empty flags %+v", payload)
	}
	if payload.LastOperationGroupID != nil {
		t.Fatalf("empty graph must not record operation group")
	}

	current := gs.do(t, http.MethodGet, "/api/v3/products/"+productID+"/workflows/current", nil, "")
	gs.mustStatus(t, current, http.StatusOK)
	var currentPayload graph.Projection
	gs.decode(t, current, &currentPayload)
	if currentPayload.ID != payload.ID {
		t.Fatalf("%s != %s", currentPayload.ID, payload.ID)
	}

	added := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+payload.ID+"/changesets", map[string]any{
		"base_graph_revision": 1,
		"summary":             "添加商品资料",
		"operations": []map[string]any{
			{
				"op":         "create_node",
				"client_ref": "n1",
				"node_type":  "product_source",
				"title":      "商品资料",
				"position_x": 120,
				"position_y": 80,
				"config":     map[string]any{"source_product_id": productID},
			},
		},
	})
	gs.mustStatus(t, added, http.StatusOK)
	var addedPayload graph.Projection
	gs.decode(t, added, &addedPayload)
	if addedPayload.Revision != 2 || len(addedPayload.Nodes) != 1 || addedPayload.Nodes[0].NodeType != graph.NodeProductSource {
		t.Fatalf("%+v", addedPayload)
	}
	if !addedPayload.CanUndo || addedPayload.CanRedo {
		t.Fatalf("undo flags %+v", addedPayload)
	}
	if addedPayload.Nodes[0].SourceProduct == nil || addedPayload.Nodes[0].SourceProduct.ID != productID {
		t.Fatalf("source %+v", addedPayload.Nodes[0].SourceProduct)
	}

	duplicate := gs.do(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows", nil, "")
	gs.mustStatus(t, duplicate, http.StatusConflict)
	var detail struct {
		Detail string `json:"detail"`
	}
	gs.decode(t, duplicate, &detail)
	if !strings.Contains(detail.Detail, "已有") {
		t.Fatalf("%s", detail.Detail)
	}
}

func TestChangeSetStaleRevisionConflict(t *testing.T) {
	gs := newGraphServer(t)
	productID := gs.createProduct(t, "revision 冲突")
	created := gs.do(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows", nil, "")
	gs.mustStatus(t, created, http.StatusCreated)
	var payload graph.Projection
	gs.decode(t, created, &payload)

	body := map[string]any{
		"base_graph_revision": 1,
		"summary":             "添加商品资料",
		"operations": []map[string]any{
			{"op": "create_node", "client_ref": "n1", "node_type": "product_source", "title": "商品资料", "config": map[string]any{"source_product_id": productID}},
		},
	}
	first := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+payload.ID+"/changesets", body)
	gs.mustStatus(t, first, http.StatusOK)
	gs.decode(t, first, nil)

	second := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+payload.ID+"/changesets", body)
	gs.mustStatus(t, second, http.StatusConflict)
	var detail struct {
		Detail string `json:"detail"`
	}
	gs.decode(t, second, &detail)
	if !strings.Contains(detail.Detail, "revision") {
		t.Fatalf("%s", detail.Detail)
	}
}

func TestUndoRedoHTTP(t *testing.T) {
	gs := newGraphServer(t)
	productID := gs.createProduct(t, "撤销重做")
	created := gs.do(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows", nil, "")
	gs.mustStatus(t, created, http.StatusCreated)
	var payload graph.Projection
	gs.decode(t, created, &payload)

	added := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+payload.ID+"/changesets", map[string]any{
		"base_graph_revision": 1,
		"summary":             "添加商品资料",
		"operations": []map[string]any{
			{"op": "create_node", "client_ref": "n1", "node_type": "product_source", "title": "商品资料", "config": map[string]any{"source_product_id": productID}},
		},
	})
	gs.mustStatus(t, added, http.StatusOK)
	var live graph.Projection
	gs.decode(t, added, &live)
	nodeID := live.Nodes[0].ID

	renamed := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+payload.ID+"/changesets", map[string]any{
		"base_graph_revision": 2,
		"summary":             "改名",
		"operations": []map[string]any{
			{"op": "rename_node", "node_ref": nodeID, "title": "新商品资料"},
		},
	})
	gs.mustStatus(t, renamed, http.StatusOK)
	gs.decode(t, renamed, &live)
	if live.Nodes[0].Title != "新商品资料" || live.Revision != 3 {
		t.Fatalf("%+v", live.Nodes[0])
	}

	undone := gs.do(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+payload.ID+"/undo", nil, "")
	gs.mustStatus(t, undone, http.StatusOK)
	gs.decode(t, undone, &live)
	if live.Nodes[0].Title != "商品资料" || live.Revision != 4 || live.CanUndo || !live.CanRedo {
		t.Fatalf("undo %+v", live)
	}

	redone := gs.do(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+payload.ID+"/redo", nil, "")
	gs.mustStatus(t, redone, http.StatusOK)
	gs.decode(t, redone, &live)
	if live.Nodes[0].Title != "新商品资料" || !live.CanUndo || live.CanRedo {
		t.Fatalf("redo %+v", live)
	}
}

func TestEmptyGraphUndoConflict(t *testing.T) {
	gs := newGraphServer(t)
	productID := gs.createProduct(t, "空图撤销")
	created := gs.do(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows", nil, "")
	gs.mustStatus(t, created, http.StatusCreated)
	var payload graph.Projection
	gs.decode(t, created, &payload)
	undone := gs.do(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+payload.ID+"/undo", nil, "")
	gs.mustStatus(t, undone, http.StatusConflict)
	var detail struct {
		Detail string `json:"detail"`
	}
	gs.decode(t, undone, &detail)
	if !strings.Contains(detail.Detail, "没有可撤销") {
		t.Fatalf("%s", detail.Detail)
	}
}

func TestMissingProductAndWorkflow(t *testing.T) {
	gs := newGraphServer(t)
	missing := gs.do(t, http.MethodPost, "/api/v3/products/missing-product/workflows", nil, "")
	gs.mustStatus(t, missing, http.StatusNotFound)
	var detail struct {
		Detail string `json:"detail"`
	}
	gs.decode(t, missing, &detail)
	if !strings.Contains(detail.Detail, "商品不存在") {
		t.Fatalf("%s", detail.Detail)
	}

	productID := gs.createProduct(t, "无图商品")
	current := gs.do(t, http.MethodGet, "/api/v3/products/"+productID+"/workflows/current", nil, "")
	gs.mustStatus(t, current, http.StatusNotFound)
	gs.decode(t, current, &detail)
	if !strings.Contains(detail.Detail, "商品工作流不存在") {
		t.Fatalf("%s", detail.Detail)
	}
}

func TestCatalogRequiresLogin(t *testing.T) {
	gs := newGraphServer(t)
	req, err := http.NewRequest(http.MethodGet, gs.srv.URL+"/api/v3/node-catalog", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := gs.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	gs.mustStatus(t, resp, http.StatusUnauthorized)
	var detail struct {
		Detail string `json:"detail"`
	}
	gs.decode(t, resp, &detail)
	if detail.Detail != "请先登录" {
		t.Fatalf("%s", detail.Detail)
	}
}

func TestProposalConfirmAndDiscard(t *testing.T) {
	gs := newGraphServer(t)
	productID := gs.createProduct(t, "提案商品")
	created := gs.do(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows", nil, "")
	gs.mustStatus(t, created, http.StatusCreated)
	var payload graph.Projection
	gs.decode(t, created, &payload)

	changeSet, err := json.Marshal(map[string]any{
		"base_graph_revision": 1,
		"summary":             "提案加节点",
		"actor_type":          "agent",
		"operations": []map[string]any{
			{"op": "create_node", "client_ref": "n1", "node_type": "product_source", "title": "商品资料", "config": map[string]any{"source_product_id": productID}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	proposalID := clockid.New()
	_, err = gs.pool.Exec(context.Background(), `
		INSERT INTO workflow_graph_proposals (
			id, graph_id, status, summary, base_graph_revision, change_set_json, created_at
		) VALUES ($1, $2, 'pending', '提案加节点', 1, $3, NOW())
	`, proposalID, payload.ID, changeSet)
	if err != nil {
		t.Fatal(err)
	}

	current := gs.do(t, http.MethodGet, "/api/v3/products/"+productID+"/workflows/current", nil, "")
	gs.mustStatus(t, current, http.StatusOK)
	gs.decode(t, current, &payload)
	if payload.PendingProposal == nil || payload.PendingProposal.Stale || len(payload.PendingProposal.AddedNodes) != 1 {
		t.Fatalf("pending %+v", payload.PendingProposal)
	}

	confirmed := gs.do(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+payload.ID+"/proposals/"+proposalID+"/confirm", nil, "")
	gs.mustStatus(t, confirmed, http.StatusOK)
	gs.decode(t, confirmed, &payload)
	if payload.Revision != 2 || len(payload.Nodes) != 1 || payload.PendingProposal != nil {
		t.Fatalf("confirm %+v", payload)
	}

	again := gs.do(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+payload.ID+"/proposals/"+proposalID+"/confirm", nil, "")
	gs.mustStatus(t, again, http.StatusConflict)
	var detail struct {
		Detail string `json:"detail"`
	}
	gs.decode(t, again, &detail)
	if !strings.Contains(detail.Detail, "已经结束") {
		t.Fatalf("%s", detail.Detail)
	}
}

func TestProposalDiscardLeavesLiveGraph(t *testing.T) {
	gs := newGraphServer(t)
	productID := gs.createProduct(t, "丢弃提案")
	created := gs.do(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows", nil, "")
	gs.mustStatus(t, created, http.StatusCreated)
	var payload graph.Projection
	gs.decode(t, created, &payload)
	changeSet, _ := json.Marshal(map[string]any{
		"base_graph_revision": 1,
		"summary":             "提案加节点",
		"actor_type":          "agent",
		"operations": []map[string]any{
			{"op": "create_node", "client_ref": "n1", "node_type": "product_source", "title": "商品资料", "config": map[string]any{"source_product_id": productID}},
		},
	})
	proposalID := clockid.New()
	if _, err := gs.pool.Exec(context.Background(), `
		INSERT INTO workflow_graph_proposals (
			id, graph_id, status, summary, base_graph_revision, change_set_json, created_at
		) VALUES ($1, $2, 'pending', '提案加节点', 1, $3, NOW())
	`, proposalID, payload.ID, changeSet); err != nil {
		t.Fatal(err)
	}
	discarded := gs.do(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+payload.ID+"/proposals/"+proposalID+"/discard", nil, "")
	gs.mustStatus(t, discarded, http.StatusOK)
	gs.decode(t, discarded, &payload)
	if payload.Revision != 1 || len(payload.Nodes) != 0 || payload.PendingProposal != nil {
		t.Fatalf("discard %+v", payload)
	}
}

func TestProcessingNodeStaleWhenArtifactDigestDiffers(t *testing.T) {
	gs := newGraphServer(t)
	productID := gs.createProduct(t, "stale 节点")
	created := gs.do(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows", nil, "")
	gs.mustStatus(t, created, http.StatusCreated)
	var payload graph.Projection
	gs.decode(t, created, &payload)
	added := gs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+payload.ID+"/changesets", map[string]any{
		"base_graph_revision": 1,
		"summary":             "加创作要求",
		"operations": []map[string]any{
			{"op": "create_node", "client_ref": "brief", "node_type": "creative_brief", "title": "创作要求", "config": map[string]any{"goal": "清晰"}},
		},
	})
	gs.mustStatus(t, added, http.StatusOK)
	gs.decode(t, added, &payload)
	nodeID := payload.Nodes[0].ID
	if payload.Nodes[0].ConfigStatus != graph.ConfigReady {
		t.Fatalf("status %s", payload.Nodes[0].ConfigStatus)
	}
	artifactID := clockid.New()
	digest := strings.Repeat("a", 64)
	hash := strings.Repeat("b", 64)
	if _, err := gs.pool.Exec(context.Background(), `
		INSERT INTO workflow_graph_artifacts (
			id, graph_id, node_id, artifact_type, schema_version, graph_revision,
			payload_json, payload_hash, input_digest, created_at
		) VALUES ($1, $2, $3, 'creative_brief', 3, 2, '{"goal":"清晰"}'::jsonb, $4, $5, NOW())
	`, artifactID, payload.ID, nodeID, hash, digest); err != nil {
		t.Fatal(err)
	}
	if _, err := gs.pool.Exec(context.Background(), `
		UPDATE workflow_graph_nodes SET current_artifact_id = $1 WHERE id = $2
	`, artifactID, nodeID); err != nil {
		t.Fatal(err)
	}
	current := gs.do(t, http.MethodGet, "/api/v3/products/"+productID+"/workflows/"+payload.ID, nil, "")
	gs.mustStatus(t, current, http.StatusOK)
	gs.decode(t, current, &payload)
	if payload.Nodes[0].ConfigStatus != graph.ConfigStale {
		t.Fatalf("want stale got %s artifact=%v", payload.Nodes[0].ConfigStatus, payload.Nodes[0].CurrentArtifactID)
	}
}
