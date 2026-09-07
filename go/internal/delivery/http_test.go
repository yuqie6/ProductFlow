package delivery

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
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/settings"
	"gorm.io/gorm"
)

type deliveryServer struct {
	pool    *pgxpool.Pool
	db      *gorm.DB
	media   media.Store
	srv     *httptest.Server
	client  *http.Client
	cookies []*http.Cookie
}

func newDeliveryServer(t *testing.T) *deliveryServer {
	t.Helper()
	pool, gdb := testdb.Open(t)
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
	HTTP{Service: Service{DB: gdb, Media: mediaStore}, Settings: settingsStore}.Register(engine)
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	ds := &deliveryServer{pool: pool, db: gdb, media: mediaStore, srv: srv, client: &http.Client{}}
	ds.cookies = auth.MustAuthenticate(t, ds.client, srv.URL)
	_, _ = ds.pool.Exec(context.Background(), `
		INSERT INTO app_settings (key, value, created_at, updated_at)
		VALUES ('admin_access_required', 'true', NOW(), NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`)
	return ds
}

func (ds *deliveryServer) do(t *testing.T, method, path string, body io.Reader, contentType string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, ds.srv.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for _, c := range ds.cookies {
		req.AddCookie(c)
	}
	resp, err := ds.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func (ds *deliveryServer) doJSON(t *testing.T, method, path string, payload any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return ds.do(t, method, path, bytes.NewReader(raw), "application/json")
}

func (ds *deliveryServer) decode(t *testing.T, resp *http.Response, dest any) {
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

func (ds *deliveryServer) mustStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("status %d want %d %s", resp.StatusCode, want, raw)
	}
}

func (ds *deliveryServer) createProduct(t *testing.T) product.CreateResponse {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("name", "交付商品")
	part, err := w.CreateFormFile("images", "hero.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(pngBytes(t, 32, 32)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	resp := ds.do(t, http.MethodPost, "/api/v2/products", &buf, w.FormDataContentType())
	ds.mustStatus(t, resp, http.StatusCreated)
	var created product.CreateResponse
	ds.decode(t, resp, &created)
	return created
}

func (ds *deliveryServer) attachArtifact(t *testing.T, productID, assetID string) {
	t.Helper()
	ds.attachArtifactLineage(t, productID, assetID)
}

func (ds *deliveryServer) attachArtifactLineage(t *testing.T, productID, assetID string) (graphID, runID, nodeRunID string) {
	t.Helper()
	graphID = clockid.New()
	nodeID := clockid.New()
	runID = clockid.New()
	nodeRunID = clockid.New()
	artifactID := clockid.New()
	digest := strings.Repeat("a", 64)
	hash := strings.Repeat("b", 64)
	if _, err := ds.pool.Exec(context.Background(), `
		INSERT INTO workflow_graphs (id, product_id, title, active, schema_version, revision, created_at, updated_at)
		VALUES ($1, $2, '交付图', TRUE, 3, 1, NOW(), NOW())
	`, graphID, productID); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.pool.Exec(context.Background(), `
		INSERT INTO workflow_graph_nodes (
			id, graph_id, node_type, title, position_x, position_y, config_json, created_at, updated_at
		) VALUES ($1, $2, 'image_generation', '出图', 0, 0, '{}'::jsonb, NOW(), NOW())
	`, nodeID, graphID); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.pool.Exec(context.Background(), `
		INSERT INTO workflow_graph_runs (
			id, graph_id, status, run_scope, graph_revision, snapshot_json, is_retryable, started_at, finished_at
		) VALUES ($1, $2, 'succeeded', 'graph', 1, '{}'::json, TRUE, NOW(), NOW())
	`, runID, graphID); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.pool.Exec(context.Background(), `
		INSERT INTO workflow_graph_node_runs (
			id, graph_run_id, node_id, status, sort_order, started_at, finished_at
		) VALUES ($1, $2, $3, 'succeeded', 0, NOW(), NOW())
	`, nodeRunID, runID, nodeID); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.pool.Exec(context.Background(), `
		INSERT INTO workflow_graph_artifacts (
			id, graph_id, node_id, node_run_id, artifact_type, schema_version, graph_revision,
			payload_json, payload_hash, input_digest, product_image_asset_id, created_at
		) VALUES ($1, $2, $3, $4, 'image', 3, 1, '{}'::jsonb, $5, $6, $7, NOW())
	`, artifactID, graphID, nodeID, nodeRunID, hash, digest, assetID); err != nil {
		t.Fatal(err)
	}
	return graphID, runID, nodeRunID
}

func TestDeliveryPresetsCatalog(t *testing.T) {
	ds := newDeliveryServer(t)
	resp := ds.do(t, http.MethodGet, "/api/v3/delivery-presets", nil, "")
	ds.mustStatus(t, resp, http.StatusOK)
	var catalog PresetCatalog
	ds.decode(t, resp, &catalog)
	if !catalog.SupportsCustom || len(catalog.Items) != 5 {
		t.Fatalf("%+v", catalog)
	}
	if catalog.Items[0].Key != "taobao_tmall_hero" || catalog.Items[0].DeliverySpec.Format != "png" {
		t.Fatalf("%+v", catalog.Items[0])
	}
}

func TestDeliverySubmitExecuteAndRetryConflict(t *testing.T) {
	ds := newDeliveryServer(t)
	created := ds.createProduct(t)
	assetID := created.CreatedAssets[0].ID
	rejected := ds.doJSON(t, http.MethodPost, "/api/v2/product-image-assets/"+assetID+"/renditions", map[string]any{
		"width": 64, "height": 64, "format": "png", "fit": "contain",
	})
	ds.mustStatus(t, rejected, http.StatusBadRequest)
	rejected.Body.Close()

	ds.attachArtifact(t, created.Product.ID, assetID)
	submitted := ds.doJSON(t, http.MethodPost, "/api/v2/product-image-assets/"+assetID+"/renditions", map[string]any{
		"width": 64, "height": 64, "format": "png", "fit": "contain",
	})
	ds.mustStatus(t, submitted, http.StatusAccepted)
	var job JobResponse
	ds.decode(t, submitted, &job)
	if job.Status != "queued" {
		t.Fatalf("status %s", job.Status)
	}
	var dispatchStatus string
	if err := ds.pool.QueryRow(context.Background(), `
		SELECT status FROM async_dispatches WHERE actor_name = 'run_delivery_rendition_job' AND aggregate_id = $1
	`, job.ID).Scan(&dispatchStatus); err != nil {
		t.Fatal(err)
	}
	if dispatchStatus != "pending" {
		t.Fatalf("dispatch %s", dispatchStatus)
	}

	replay := ds.doJSON(t, http.MethodPost, "/api/v2/product-image-assets/"+assetID+"/renditions", map[string]any{
		"width": 64, "height": 64, "format": "png", "fit": "contain",
	})
	ds.mustStatus(t, replay, http.StatusAccepted)
	var same JobResponse
	ds.decode(t, replay, &same)
	if same.ID != job.ID {
		t.Fatalf("idempotent %s vs %s", same.ID, job.ID)
	}

	if err := (Executor{DB: ds.db, Media: ds.media}).Execute(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	got := ds.do(t, http.MethodGet, "/api/v2/delivery-rendition-jobs/"+job.ID, nil, "")
	ds.mustStatus(t, got, http.StatusOK)
	ds.decode(t, got, &job)
	if job.Status != "succeeded" || job.ResultAsset == nil {
		t.Fatalf("%+v", job)
	}
	if job.ResultAsset.OriginType != "workflow_generation" || job.ResultAsset.ParentAssetID == nil {
		t.Fatalf("result %+v", job.ResultAsset)
	}

	listed := ds.do(t, http.MethodGet, "/api/v2/product-image-assets/"+assetID+"/renditions", nil, "")
	ds.mustStatus(t, listed, http.StatusOK)
	var list JobListResponse
	ds.decode(t, listed, &list)
	if len(list.Items) != 1 {
		t.Fatalf("list %d", len(list.Items))
	}

	retry := ds.do(t, http.MethodPost, "/api/v2/delivery-rendition-jobs/"+job.ID+"/retry", nil, "")
	ds.mustStatus(t, retry, http.StatusConflict)
	var body struct {
		Detail string `json:"detail"`
	}
	ds.decode(t, retry, &body)
	if body.Detail != "只有失败的交付派生任务可以重试" {
		t.Fatalf("%s", body.Detail)
	}
}

func TestExecuteTerminalJobDoesNotBusyRetry(t *testing.T) {
	ds := newDeliveryServer(t)
	created := ds.createProduct(t)
	assetID := created.CreatedAssets[0].ID
	ds.attachArtifact(t, created.Product.ID, assetID)
	submitted := ds.doJSON(t, http.MethodPost, "/api/v2/product-image-assets/"+assetID+"/renditions", map[string]any{
		"width": 64, "height": 64, "format": "png", "fit": "contain",
	})
	ds.mustStatus(t, submitted, http.StatusAccepted)
	var job JobResponse
	ds.decode(t, submitted, &job)
	exec := Executor{DB: ds.db, Media: ds.media}
	if err := exec.Execute(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	if err := exec.Execute(context.Background(), job.ID); err != nil {
		t.Fatalf("terminal job must consume, not busy-retry: %v", err)
	}
}

func TestExecuteFailsQueuedJobWhenSourceNotVerified(t *testing.T) {
	ds := newDeliveryServer(t)
	created := ds.createProduct(t)
	assetID := created.CreatedAssets[0].ID
	ds.attachArtifact(t, created.Product.ID, assetID)
	submitted := ds.doJSON(t, http.MethodPost, "/api/v2/product-image-assets/"+assetID+"/renditions", map[string]any{
		"width": 64, "height": 64, "format": "png", "fit": "contain",
	})
	ds.mustStatus(t, submitted, http.StatusAccepted)
	var job JobResponse
	ds.decode(t, submitted, &job)
	if job.Status != "queued" {
		t.Fatalf("status %s", job.Status)
	}

	if _, err := ds.pool.Exec(context.Background(), `
		UPDATE media_objects SET verification_status = 'missing'
		WHERE id = (SELECT media_object_id FROM product_image_assets WHERE id = $1)
	`, assetID); err != nil {
		t.Fatal(err)
	}

	if err := (Executor{DB: ds.db, Media: ds.media}).Execute(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}

	var status string
	var reason *string
	if err := ds.pool.QueryRow(context.Background(), `
		SELECT status, failure_reason FROM delivery_rendition_jobs WHERE id = $1
	`, job.ID).Scan(&status, &reason); err != nil {
		t.Fatal(err)
	}
	if status != "failed" {
		t.Fatalf("status %s want failed (claim must commit before source validation)", status)
	}
	if reason == nil || *reason != "交付派生原图媒体尚未通过核验" {
		t.Fatalf("failure_reason %v", reason)
	}
}

func TestExecuteFailsWhenSourceFileMissing(t *testing.T) {
	ds := newDeliveryServer(t)
	created := ds.createProduct(t)
	assetID := created.CreatedAssets[0].ID
	ds.attachArtifact(t, created.Product.ID, assetID)
	submitted := ds.doJSON(t, http.MethodPost, "/api/v2/product-image-assets/"+assetID+"/renditions", map[string]any{
		"width": 64, "height": 64, "format": "png", "fit": "contain",
	})
	ds.mustStatus(t, submitted, http.StatusAccepted)
	var job JobResponse
	ds.decode(t, submitted, &job)

	abs := resolveDeliveryMedia(t, ds, created.CreatedAssets[0].MediaObjectID)
	if err := os.Remove(abs); err != nil {
		t.Fatal(err)
	}
	if err := (Executor{DB: ds.db, Media: ds.media}).Execute(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	var status string
	var reason *string
	if err := ds.pool.QueryRow(context.Background(), `
		SELECT status, failure_reason FROM delivery_rendition_jobs WHERE id = $1
	`, job.ID).Scan(&status, &reason); err != nil {
		t.Fatal(err)
	}
	if status != "failed" {
		t.Fatalf("status %s", status)
	}
	if reason == nil || *reason != sourceMissingDetail {
		t.Fatalf("failure_reason %v", reason)
	}
}

func resolveDeliveryMedia(t *testing.T, ds *deliveryServer, mediaObjectID string) string {
	t.Helper()
	var path string
	if err := ds.pool.QueryRow(context.Background(), `SELECT storage_path FROM media_objects WHERE id = $1`, mediaObjectID).Scan(&path); err != nil {
		t.Fatal(err)
	}
	abs, err := ds.media.Files.Resolve(path)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestDeliveryUnknownSpecFieldRejected(t *testing.T) {
	ds := newDeliveryServer(t)
	created := ds.createProduct(t)
	resp := ds.doJSON(t, http.MethodPost, "/api/v2/product-image-assets/"+created.CreatedAssets[0].ID+"/renditions", map[string]any{
		"width": 64, "height": 64, "format": "png", "fit": "contain", "foo": 1,
	})
	ds.mustStatus(t, resp, http.StatusBadRequest)
	resp.Body.Close()
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
