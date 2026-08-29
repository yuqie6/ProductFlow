package localedit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/settings"
)

type editServer struct {
	pool    *pgxpool.Pool
	media   media.Store
	svc     Service
	srv     *httptest.Server
	client  *http.Client
	cookies []*http.Cookie
}

func newEditServer(t *testing.T, provider Provider) *editServer {
	t.Helper()
	pool := testdb.Pool(t)
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
	svc := Service{Pool: pool, Media: mediaStore, Provider: provider}
	product.HTTP{Service: product.Service{Pool: pool, Media: mediaStore}, Settings: settingsStore}.Register(engine)
	HTTP{Service: svc, Settings: settingsStore}.Register(engine)
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	es := &editServer{pool: pool, media: mediaStore, svc: svc, srv: srv, client: &http.Client{}}
	login, err := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/session", strings.NewReader(`{"admin_key":"k"}`))
	if err != nil {
		t.Fatal(err)
	}
	login.Header.Set("Content-Type", "application/json")
	resp, err := es.client.Do(login)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	es.cookies = resp.Cookies()
	_, _ = es.pool.Exec(context.Background(), `
		INSERT INTO app_settings (key, value, created_at, updated_at)
		VALUES ('admin_access_required', 'true', NOW(), NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`)
	return es
}

func (es *editServer) do(t *testing.T, method, path string, body io.Reader, contentType string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, es.srv.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for _, c := range es.cookies {
		req.AddCookie(c)
	}
	resp, err := es.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func (es *editServer) doJSON(t *testing.T, method, path string, payload any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return es.do(t, method, path, bytes.NewReader(raw), "application/json")
}

func (es *editServer) decode(t *testing.T, resp *http.Response, dest any) {
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

func (es *editServer) mustStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("status %d want %d %s", resp.StatusCode, want, raw)
	}
}

func (es *editServer) createProduct(t *testing.T) product.CreateResponse {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("name", "局部修商品")
	part, err := w.CreateFormFile("images", "hero.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(pngBytes(t, 8, 6)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	resp := es.do(t, http.MethodPost, "/api/v2/products", &buf, w.FormDataContentType())
	es.mustStatus(t, resp, http.StatusCreated)
	var created product.CreateResponse
	es.decode(t, resp, &created)
	return created
}

func TestLocalEditCapabilityUnsupportedByDefault(t *testing.T) {
	es := newEditServer(t, MockProvider{})
	resp := es.do(t, http.MethodGet, "/api/v3/local-image-edits/capability", nil, "")
	es.mustStatus(t, resp, http.StatusOK)
	var cap CapabilityResponse
	es.decode(t, resp, &cap)
	if cap.Supported || cap.Reason == nil || *cap.Reason != "图片 provider 未显式声明 masked local edit 能力" {
		t.Fatalf("%+v", cap)
	}
	if cap.Operations == nil {
		t.Fatal("operations must be []")
	}
}

func TestLocalEditCreateSubmitExecuteAndUnknown(t *testing.T) {
	provider := MockProvider{Cap: SupportedCapability("mock-local")}
	es := newEditServer(t, provider)
	created := es.createProduct(t)
	productID := created.Product.ID
	sourceID := created.CreatedAssets[0].ID

	alias := createForm(t, sourceID, true)
	rejected := es.do(t, http.MethodPost, "/api/v3/products/"+productID+"/image-edits", alias.body, alias.contentType)
	es.mustStatus(t, rejected, http.StatusBadRequest)
	var aliasBody struct {
		Detail string `json:"detail"`
	}
	es.decode(t, rejected, &aliasBody)
	if !strings.Contains(aliasBody.Detail, "局部编辑只接受正式字段") {
		t.Fatalf("%s", aliasBody.Detail)
	}

	form := createForm(t, sourceID, false)
	draft := es.do(t, http.MethodPost, "/api/v3/products/"+productID+"/image-edits", form.body, form.contentType)
	es.mustStatus(t, draft, http.StatusCreated)
	var task TaskResponse
	es.decode(t, draft, &task)
	if task.Status != "draft" || task.ProviderAttempts == nil || task.AdoptionEvents == nil || task.References == nil {
		t.Fatalf("%+v", task)
	}

	blockedServer := newEditServer(t, MockProvider{})
	prod2 := blockedServer.createProduct(t)
	form2 := createForm(t, prod2.CreatedAssets[0].ID, false)
	created2 := blockedServer.do(t, http.MethodPost, "/api/v3/products/"+prod2.Product.ID+"/image-edits", form2.body, form2.contentType)
	blockedServer.mustStatus(t, created2, http.StatusCreated)
	var draft2 TaskResponse
	blockedServer.decode(t, created2, &draft2)
	blocked := blockedServer.doJSON(t, http.MethodPost, "/api/v3/products/"+prod2.Product.ID+"/image-edits/"+draft2.ID+"/submit", map[string]any{
		"idempotency_key": "k-unsupported",
	})
	blockedServer.mustStatus(t, blocked, http.StatusBadRequest)
	var blockedBody struct {
		Detail string `json:"detail"`
	}
	blockedServer.decode(t, blocked, &blockedBody)
	if blockedBody.Detail != "图片 provider 未显式声明 masked local edit 能力" {
		t.Fatalf("%s", blockedBody.Detail)
	}

	submitted := es.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/image-edits/"+task.ID+"/submit", map[string]any{
		"idempotency_key": "k-1",
	})
	es.mustStatus(t, submitted, http.StatusAccepted)
	es.decode(t, submitted, &task)
	if task.Status != "queued" {
		t.Fatalf("status %s", task.Status)
	}
	var dispatchStatus string
	if err := es.pool.QueryRow(context.Background(), `
		SELECT status FROM async_dispatches WHERE actor_name = 'run_local_image_edit_task' AND aggregate_id = $1
	`, task.ID).Scan(&dispatchStatus); err != nil {
		t.Fatal(err)
	}
	if dispatchStatus != "pending" {
		t.Fatalf("dispatch %s", dispatchStatus)
	}

	if err := (Executor{Pool: es.pool, Media: es.media, Provider: provider}).Execute(context.Background(), task.ID); err != nil {
		t.Fatal(err)
	}
	got := es.do(t, http.MethodGet, "/api/v3/products/"+productID+"/image-edits/"+task.ID, nil, "")
	es.mustStatus(t, got, http.StatusOK)
	es.decode(t, got, &task)
	if task.Status != "succeeded" || task.ResultAsset == nil {
		t.Fatalf("%+v", task)
	}
	if task.ResultAsset.OriginType != "local_edit" {
		t.Fatalf("origin %s", task.ResultAsset.OriginType)
	}

	form3 := createForm(t, sourceID, false)
	draft3resp := es.do(t, http.MethodPost, "/api/v3/products/"+productID+"/image-edits", form3.body, form3.contentType)
	es.mustStatus(t, draft3resp, http.StatusCreated)
	var task3 TaskResponse
	es.decode(t, draft3resp, &task3)
	queued := es.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/image-edits/"+task3.ID+"/submit", map[string]any{
		"idempotency_key": "k-unknown",
	})
	es.mustStatus(t, queued, http.StatusAccepted)
	failExec := Executor{Pool: es.pool, Media: es.media, Provider: MockProvider{Cap: SupportedCapability("mock-local"), Err: errors.New("boom")}}
	if err := failExec.Execute(context.Background(), task3.ID); err != nil {
		t.Fatal(err)
	}
	got3 := es.do(t, http.MethodGet, "/api/v3/products/"+productID+"/image-edits/"+task3.ID, nil, "")
	es.mustStatus(t, got3, http.StatusOK)
	es.decode(t, got3, &task3)
	if task3.Status != "unknown" || task3.IsRetryable {
		t.Fatalf("unknown %+v", task3)
	}
}

func TestLocalEditRetryConflictOnDraft(t *testing.T) {
	es := newEditServer(t, MockProvider{Cap: SupportedCapability("mock-local")})
	created := es.createProduct(t)
	form := createForm(t, created.CreatedAssets[0].ID, false)
	draft := es.do(t, http.MethodPost, "/api/v3/products/"+created.Product.ID+"/image-edits", form.body, form.contentType)
	es.mustStatus(t, draft, http.StatusCreated)
	var task TaskResponse
	es.decode(t, draft, &task)
	retry := es.doJSON(t, http.MethodPost, "/api/v3/products/"+created.Product.ID+"/image-edits/"+task.ID+"/retry", map[string]any{})
	es.mustStatus(t, retry, http.StatusConflict)
	var body struct {
		Detail string `json:"detail"`
	}
	es.decode(t, retry, &body)
	if body.Detail != "只有可重试的 failed 局部编辑任务才能重试" {
		t.Fatalf("%s", body.Detail)
	}
}

type formPayload struct {
	body        *bytes.Buffer
	contentType string
}

func createForm(t *testing.T, sourceAssetID string, alias bool) formPayload {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("source_asset_id", sourceAssetID)
	_ = w.WriteField("operation", "inpaint")
	_ = w.WriteField("instruction", "擦除选区")
	geo := `{"source_width":8,"source_height":6,"viewport_width":8,"viewport_height":6,"viewport_to_source":[1,0,0,1,0,0]}`
	if alias {
		_ = w.WriteField("mask_geometry", geo)
	} else {
		_ = w.WriteField("mask_geometry_json", geo)
	}
	_ = w.WriteField("reference_asset_ids_json", "[]")
	part, err := w.CreateFormFile("mask", "mask.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(maskPNG(t, 8, 6)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return formPayload{body: &buf, contentType: w.FormDataContentType()}
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

func maskPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	img.SetRGBA(0, 0, color.RGBA{255, 255, 255, 0})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
