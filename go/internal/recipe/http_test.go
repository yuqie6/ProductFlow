package recipe_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/recipe"
	"github.com/yuqie6/productflow/internal/settings"
	"gorm.io/gorm"
)

type recipeServer struct {
	pool    *pgxpool.Pool
	db      *gorm.DB
	srv     *httptest.Server
	client  *http.Client
	cookies []*http.Cookie
}

func newRecipeServer(t *testing.T) *recipeServer {
	t.Helper()
	pool, gdb := testdb.Open(t)
	return newRecipeServerWithDB(t, pool, gdb)
}

func newRecipeServerWithDB(t *testing.T, pool *pgxpool.Pool, gdb *gorm.DB) *recipeServer {
	t.Helper()
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
	auth.MountTest(engine, gdb, settingsStore, auth.TestAdminKey)
	mediaStore := media.Store{Files: storage.Local{Root: root}}
	product.HTTP{Service: product.Service{DB: gdb, Media: mediaStore}, Settings: settingsStore}.Register(engine)
	graph.HTTP{Service: graph.Service{DB: gdb, Products: product.GraphGuard{}}, Settings: settingsStore}.Register(engine)
	recipe.HTTP{Service: recipe.Service{DB: gdb, Products: product.GraphGuard{}}, Settings: settingsStore}.Register(engine)
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	rs := &recipeServer{pool: pool, db: gdb, srv: srv, client: &http.Client{}}
	rs.cookies = auth.MustAuthenticate(t, rs.client, srv.URL)
	_, _ = rs.pool.Exec(context.Background(), `
		INSERT INTO app_settings (key, value, created_at, updated_at)
		VALUES ('admin_access_required', 'true', NOW(), NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`)
	return rs
}

func (rs *recipeServer) do(t *testing.T, method, path string, body io.Reader, contentType string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, rs.srv.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for _, c := range rs.cookies {
		req.AddCookie(c)
	}
	resp, err := rs.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func (rs *recipeServer) doJSON(t *testing.T, method, path string, payload any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return rs.do(t, method, path, bytes.NewReader(raw), "application/json")
}

func (rs *recipeServer) decode(t *testing.T, resp *http.Response, dest any) {
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

func (rs *recipeServer) mustStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("status %d want %d %s", resp.StatusCode, want, raw)
	}
}

func (rs *recipeServer) detail(t *testing.T, resp *http.Response) string {
	t.Helper()
	var body struct {
		Detail string `json:"detail"`
	}
	rs.decode(t, resp, &body)
	return body.Detail
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

func (rs *recipeServer) createV2(t *testing.T, name string) string {
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
	resp := rs.do(t, http.MethodPost, "/api/v2/products", &buf, w.FormDataContentType())
	rs.mustStatus(t, resp, http.StatusCreated)
	var created struct {
		Product struct {
			ID string `json:"id"`
		} `json:"product"`
	}
	rs.decode(t, resp, &created)
	return created.Product.ID
}

type directGraphJSON struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
	Nodes    []struct {
		ID       string `json:"id"`
		NodeType string `json:"node_type"`
	} `json:"nodes"`
}

func (rs *recipeServer) createDirectGraph(t *testing.T, name string) (productID string, g directGraphJSON) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("name", name)
	_ = w.WriteField("image_types", `[{"key":"hero","quantity":1}]`)
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
	resp := rs.do(t, http.MethodPost, "/api/v3/products", &buf, w.FormDataContentType())
	rs.mustStatus(t, resp, http.StatusCreated)
	var created struct {
		Product struct {
			ID string `json:"id"`
		} `json:"product"`
		Graph directGraphJSON `json:"graph"`
	}
	rs.decode(t, resp, &created)
	return created.Product.ID, created.Graph
}

func TestRecipeListRequiresLogin(t *testing.T) {
	rs := newRecipeServer(t)
	req, err := http.NewRequest(http.MethodGet, rs.srv.URL+"/api/v3/workflow-recipes", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := rs.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	rs.mustStatus(t, resp, http.StatusUnauthorized)
	if detail := rs.detail(t, resp); detail != "请先登录" {
		t.Fatalf("%s", detail)
	}
}

func TestRecipeCreationPreviewHTTP(t *testing.T) {
	rs := newRecipeServer(t)
	productID, g := rs.createDirectGraph(t, "creation preview")
	for _, source := range []string{"workflow", "selection"} {
		body := map[string]any{"source_type": source, "expected_graph_revision": g.Revision, "title": "creation preview"}
		if source == "selection" {
			body["node_ids"] = []string{g.Nodes[0].ID}
		}
		saved := rs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+g.ID+"/recipes", body)
		rs.mustStatus(t, saved, http.StatusCreated)
		var rec recipe.RecipeView
		rs.decode(t, saved, &rec)
		path := "/api/v3/workflow-recipes/" + rec.ID + "/creation-preview"
		for _, trial := range []struct {
			body   map[string]any
			status int
		}{
			{map[string]any{}, 400}, {map[string]any{"expected_recipe_version": 0}, 400},
			{map[string]any{"expected_recipe_version": 1, "product_id": productID}, 400},
			{map[string]any{"expected_recipe_version": 2}, 409},
		} {
			resp := rs.doJSON(t, http.MethodPost, path, trial.body)
			rs.mustStatus(t, resp, trial.status)
			rs.decode(t, resp, nil)
		}
		resp := rs.doJSON(t, http.MethodPost, path, map[string]any{"expected_recipe_version": 1})
		if source == "selection" {
			rs.mustStatus(t, resp, 409)
			rs.decode(t, resp, nil)
			continue
		}
		rs.mustStatus(t, resp, 200)
		var preview recipe.PreviewView
		rs.decode(t, resp, &preview)
		if preview.Mode != "create" || preview.BaseGraphRevision != 0 || len(preview.Nodes) != len(g.Nodes) {
			t.Fatalf("unexpected preview %+v", preview)
		}
		archived := rs.do(t, http.MethodDelete, "/api/v3/workflow-recipes/"+rec.ID+"?expected_recipe_version=1", nil, "")
		rs.mustStatus(t, archived, 200)
		rs.decode(t, archived, nil)
		resp = rs.doJSON(t, http.MethodPost, path, map[string]any{"expected_recipe_version": 1})
		rs.mustStatus(t, resp, 409)
		rs.decode(t, resp, nil)
	}
	noAuth, err := http.NewRequest(http.MethodPost, rs.srv.URL+"/api/v3/workflow-recipes/"+clockid.New()+"/creation-preview", strings.NewReader(`{"expected_recipe_version":1}`))
	if err != nil {
		t.Fatal(err)
	}
	noAuth.Header.Set("Content-Type", "application/json")
	resp, err := rs.client.Do(noAuth)
	if err != nil {
		t.Fatal(err)
	}
	rs.mustStatus(t, resp, 401)
	rs.decode(t, resp, nil)
}

func TestRecipeAPISavesFragmentPreviewAndApply(t *testing.T) {
	rs := newRecipeServer(t)
	listed := rs.do(t, http.MethodGet, "/api/v3/workflow-recipes", nil, "")
	rs.mustStatus(t, listed, http.StatusOK)
	rs.decode(t, listed, nil)

	productID, g := rs.createDirectGraph(t, "配方 API 商品")
	save := rs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+g.ID+"/recipes", map[string]any{
		"source_type":             "workflow",
		"expected_graph_revision": g.Revision,
		"title":                   "API 配方",
		"description":             "用户主动保存",
	})
	rs.mustStatus(t, save, http.StatusCreated)
	var body map[string]any
	rs.decode(t, save, &body)
	if body["kind"] != "workflow_recipe" {
		t.Fatalf("%+v", body)
	}
	current := body["current_version"].(map[string]any)
	payload := current["payload"].(map[string]any)
	if payload["schema_version"] != float64(3) {
		t.Fatalf("%+v", payload)
	}
	if _, ok := payload["image_types"]; ok {
		t.Fatal("image_types leaked")
	}
	nodeTypes := map[string]struct{}{}
	for _, raw := range payload["nodes"].([]any) {
		nodeTypes[raw.(map[string]any)["node_type"].(string)] = struct{}{}
	}
	for _, want := range []string{"product_source", "image_prompt", "image_generation"} {
		if _, ok := nodeTypes[want]; !ok {
			t.Fatalf("missing %s in %+v", want, nodeTypes)
		}
	}

	listedAfter := rs.do(t, http.MethodGet, "/api/v3/workflow-recipes", nil, "")
	rs.mustStatus(t, listedAfter, http.StatusOK)
	var summaries []map[string]any
	rs.decode(t, listedAfter, &summaries)
	found := false
	for _, item := range summaries {
		if item["id"] == body["id"] {
			found = true
			if _, ok := item["versions"]; ok {
				t.Fatal("list must not include versions")
			}
		}
	}
	if !found {
		t.Fatal("saved recipe missing from list")
	}

	stale := rs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+g.ID+"/recipes", map[string]any{
		"source_type":             "workflow",
		"expected_graph_revision": 0,
		"title":                   "过期保存",
	})
	rs.mustStatus(t, stale, http.StatusConflict)
	if !strings.Contains(rs.detail(t, stale), "工作流已变化") {
		t.Fatal("stale revision")
	}

	otherID, otherGraph := rs.createDirectGraph(t, "配方应用目标")
	blocked := rs.doJSON(t, http.MethodPost, "/api/v3/products/"+otherID+"/workflow-recipes/"+body["id"].(string)+"/preview", map[string]any{
		"expected_recipe_version": 1,
	})
	rs.mustStatus(t, blocked, http.StatusConflict)
	if !strings.Contains(rs.detail(t, blocked), "完整配方不能合并") {
		t.Fatal("full recipe merge")
	}

	var promptID, imageID string
	for _, node := range g.Nodes {
		switch node.NodeType {
		case "image_prompt":
			promptID = node.ID
		case "image_generation":
			imageID = node.ID
		}
	}
	fragment := rs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+g.ID+"/recipes", map[string]any{
		"source_type":             "selection",
		"node_ids":                []string{promptID, imageID},
		"expected_graph_revision": g.Revision,
		"title":                   "API 片段",
	})
	rs.mustStatus(t, fragment, http.StatusCreated)
	var fragmentBody map[string]any
	rs.decode(t, fragment, &fragmentBody)
	if fragmentBody["kind"] != "recipe_fragment" {
		t.Fatalf("%+v", fragmentBody)
	}

	preview := rs.doJSON(t, http.MethodPost, "/api/v3/products/"+otherID+"/workflow-recipes/"+fragmentBody["id"].(string)+"/preview", map[string]any{
		"expected_recipe_version": 1,
	})
	rs.mustStatus(t, preview, http.StatusOK)
	var previewBody map[string]any
	rs.decode(t, preview, &previewBody)
	if previewBody["mode"] != "merge" {
		t.Fatalf("%+v", previewBody)
	}
	if len(previewBody["nodes"].([]any)) == 0 {
		t.Fatal("preview nodes")
	}

	wrongDigest := rs.doJSON(t, http.MethodPost, "/api/v3/products/"+otherID+"/workflow-recipes/"+fragmentBody["id"].(string)+"/apply", map[string]any{
		"expected_recipe_version": 1,
		"expected_graph_revision": previewBody["base_graph_revision"],
		"preview_digest":          strings.Repeat("0", 64),
		"idempotency_key":         "wrong-preview-digest",
	})
	rs.mustStatus(t, wrongDigest, http.StatusConflict)
	if !strings.Contains(rs.detail(t, wrongDigest), "预览已变化") {
		t.Fatal("wrong digest")
	}

	applied := rs.doJSON(t, http.MethodPost, "/api/v3/products/"+otherID+"/workflow-recipes/"+fragmentBody["id"].(string)+"/apply", map[string]any{
		"expected_recipe_version": 1,
		"expected_graph_revision": previewBody["base_graph_revision"],
		"preview_digest":          previewBody["preview_digest"],
		"idempotency_key":         "api-apply-1",
	})
	rs.mustStatus(t, applied, http.StatusCreated)
	var appliedBody map[string]any
	rs.decode(t, applied, &appliedBody)
	if appliedBody["mode"] != "merge" {
		t.Fatalf("%+v", appliedBody)
	}
	if _, ok := appliedBody["draft"]; ok {
		t.Fatal("draft leaked")
	}
	graphBody := appliedBody["graph"].(map[string]any)
	if graphBody["id"] != otherGraph.ID {
		t.Fatalf("%s != %s", graphBody["id"], otherGraph.ID)
	}
	if len(appliedBody["added_node_ids"].([]any)) == 0 {
		t.Fatal("added nodes")
	}

	replay := rs.doJSON(t, http.MethodPost, "/api/v3/products/"+otherID+"/workflow-recipes/"+fragmentBody["id"].(string)+"/apply", map[string]any{
		"expected_recipe_version": 1,
		"expected_graph_revision": previewBody["base_graph_revision"],
		"preview_digest":          previewBody["preview_digest"],
		"idempotency_key":         "api-apply-1",
	})
	rs.mustStatus(t, replay, http.StatusCreated)
	var replayBody map[string]any
	rs.decode(t, replay, &replayBody)
	if replayBody["created"] != false {
		t.Fatalf("replay %+v", replayBody)
	}

	mismatch := rs.doJSON(t, http.MethodPost, "/api/v3/products/"+otherID+"/workflow-recipes/"+fragmentBody["id"].(string)+"/apply", map[string]any{
		"expected_recipe_version": 2,
		"expected_graph_revision": previewBody["base_graph_revision"],
		"preview_digest":          previewBody["preview_digest"],
		"idempotency_key":         "api-apply-1",
	})
	rs.mustStatus(t, mismatch, http.StatusConflict)
	if !strings.Contains(rs.detail(t, mismatch), "相同 idempotency key") {
		t.Fatal("idempotency mismatch")
	}

	appendResp := rs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+g.ID+"/recipes/"+fragmentBody["id"].(string)+"/versions", map[string]any{
		"source_type":             "selection",
		"node_ids":                []string{promptID, imageID},
		"expected_graph_revision": g.Revision,
		"expected_recipe_version": 1,
		"title":                   "API 片段 v2",
	})
	rs.mustStatus(t, appendResp, http.StatusCreated)
	var appended map[string]any
	rs.decode(t, appendResp, &appended)
	current = appended["current_version"].(map[string]any)
	if current["version"] != float64(2) || current["title"] != "API 片段 v2" {
		t.Fatalf("%+v", current)
	}
	if len(appended["versions"].([]any)) != 2 {
		t.Fatalf("versions %+v", appended["versions"])
	}

	archived := rs.do(t, http.MethodDelete, "/api/v3/workflow-recipes/"+fragmentBody["id"].(string)+"?expected_recipe_version=2", nil, "")
	rs.mustStatus(t, archived, http.StatusOK)
	var archiveBody map[string]any
	rs.decode(t, archived, &archiveBody)
	if archiveBody["changed"] != true {
		t.Fatalf("%+v", archiveBody)
	}
	replayArchive := rs.do(t, http.MethodDelete, "/api/v3/workflow-recipes/"+fragmentBody["id"].(string)+"?expected_recipe_version=2", nil, "")
	rs.mustStatus(t, replayArchive, http.StatusOK)
	rs.decode(t, replayArchive, &archiveBody)
	if archiveBody["changed"] != false {
		t.Fatalf("replay archive %+v", archiveBody)
	}
}

func TestRecipeApplyCreateModeAndFragmentNeedsGraph(t *testing.T) {
	rs := newRecipeServer(t)
	sourceID, g := rs.createDirectGraph(t, "完整配方来源")
	save := rs.doJSON(t, http.MethodPost, "/api/v3/products/"+sourceID+"/workflows/"+g.ID+"/recipes", map[string]any{
		"source_type":             "workflow",
		"expected_graph_revision": g.Revision,
		"title":                   "结构配方",
	})
	rs.mustStatus(t, save, http.StatusCreated)
	var saved map[string]any
	rs.decode(t, save, &saved)

	targetID := rs.createV2(t, "目标商品")
	preview := rs.doJSON(t, http.MethodPost, "/api/v3/products/"+targetID+"/workflow-recipes/"+saved["id"].(string)+"/preview", map[string]any{
		"expected_recipe_version": 1,
	})
	rs.mustStatus(t, preview, http.StatusOK)
	var previewBody map[string]any
	rs.decode(t, preview, &previewBody)
	if previewBody["mode"] != "create" {
		t.Fatalf("%+v", previewBody)
	}
	applied := rs.doJSON(t, http.MethodPost, "/api/v3/products/"+targetID+"/workflow-recipes/"+saved["id"].(string)+"/apply", map[string]any{
		"expected_recipe_version": 1,
		"expected_graph_revision": previewBody["base_graph_revision"],
		"preview_digest":          previewBody["preview_digest"],
		"idempotency_key":         "target-apply-1",
	})
	rs.mustStatus(t, applied, http.StatusCreated)
	var appliedBody map[string]any
	rs.decode(t, applied, &appliedBody)
	if appliedBody["created"] != true || appliedBody["mode"] != "create" {
		t.Fatalf("%+v", appliedBody)
	}
	graphBody := appliedBody["graph"].(map[string]any)
	if graphBody["schema_version"] != float64(3) {
		t.Fatalf("%+v", graphBody)
	}

	var promptID, imageID string
	for _, node := range g.Nodes {
		switch node.NodeType {
		case "image_prompt":
			promptID = node.ID
		case "image_generation":
			imageID = node.ID
		}
	}
	fragment := rs.doJSON(t, http.MethodPost, "/api/v3/products/"+sourceID+"/workflows/"+g.ID+"/recipes", map[string]any{
		"source_type":             "selection",
		"node_ids":                []string{promptID, imageID},
		"expected_graph_revision": g.Revision,
		"title":                   "v3 目标片段",
	})
	rs.mustStatus(t, fragment, http.StatusCreated)
	var fragmentBody map[string]any
	rs.decode(t, fragment, &fragmentBody)
	emptyTarget := rs.createV2(t, "片段目标商品")
	blocked := rs.doJSON(t, http.MethodPost, "/api/v3/products/"+emptyTarget+"/workflow-recipes/"+fragmentBody["id"].(string)+"/apply", map[string]any{
		"expected_recipe_version": 1,
		"expected_graph_revision": 0,
		"preview_digest":          strings.Repeat("a", 64),
		"idempotency_key":         "fragment-v3-conflict",
	})
	rs.mustStatus(t, blocked, http.StatusConflict)
	if !strings.Contains(rs.detail(t, blocked), "片段配方需要已有 schema-v3 工作流") {
		t.Fatal("fragment needs graph")
	}
}

func TestOfficialRecipesHidden(t *testing.T) {
	rs := newRecipeServer(t)
	officialID := clockid.New()
	_, err := rs.pool.Exec(context.Background(), `
		INSERT INTO workflow_recipes (id, kind, origin, official_key, created_at, updated_at)
		VALUES ($1, 'recipe_fragment', 'official', $2, NOW(), NOW())
	`, officialID, officialID)
	if err != nil {
		t.Fatal(err)
	}
	listed := rs.do(t, http.MethodGet, "/api/v3/workflow-recipes", nil, "")
	rs.mustStatus(t, listed, http.StatusOK)
	var summaries []map[string]any
	rs.decode(t, listed, &summaries)
	for _, item := range summaries {
		if item["id"] == officialID {
			t.Fatal("official listed")
		}
	}
	got := rs.do(t, http.MethodGet, "/api/v3/workflow-recipes/"+officialID, nil, "")
	rs.mustStatus(t, got, http.StatusNotFound)
	if !strings.Contains(rs.detail(t, got), "资源不存在") {
		t.Fatal("official get")
	}
	productID := rs.createV2(t, "官方配方不可应用")
	preview := rs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflow-recipes/"+officialID+"/preview", map[string]any{
		"expected_recipe_version": 1,
	})
	rs.mustStatus(t, preview, http.StatusNotFound)
	if !strings.Contains(rs.detail(t, preview), "资源不存在") {
		t.Fatal("official preview")
	}
	archived := rs.do(t, http.MethodDelete, "/api/v3/workflow-recipes/"+officialID+"?expected_recipe_version=1", nil, "")
	rs.mustStatus(t, archived, http.StatusNotFound)
}

func TestMissingProductPreview(t *testing.T) {
	rs := newRecipeServer(t)
	productID, g := rs.createDirectGraph(t, "预览缺商品")
	save := rs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+g.ID+"/recipes", map[string]any{
		"source_type":             "workflow",
		"expected_graph_revision": g.Revision,
		"title":                   "缺商品配方",
	})
	rs.mustStatus(t, save, http.StatusCreated)
	var saved map[string]any
	rs.decode(t, save, &saved)
	missing := rs.doJSON(t, http.MethodPost, "/api/v3/products/00000000-0000-4000-8000-000000000099/workflow-recipes/"+saved["id"].(string)+"/preview", map[string]any{
		"expected_recipe_version": 1,
	})
	rs.mustStatus(t, missing, http.StatusNotFound)
	if !strings.Contains(rs.detail(t, missing), "资源不存在") {
		t.Fatal("missing product")
	}
}
