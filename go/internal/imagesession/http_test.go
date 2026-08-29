package imagesession

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
	"gorm.io/gorm"
)

type sessionServer struct {
	pool    *pgxpool.Pool
	db      *gorm.DB
	root    string
	media   media.Store
	svc     Service
	srv     *httptest.Server
	client  *http.Client
	cookies []*http.Cookie
}

func newSessionServer(t *testing.T) *sessionServer {
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
		DeletionEnabled:          false,
	})
	auth.HTTP{AdminAccessKey: "k", Store: settingsStore}.Register(engine)
	mediaStore := media.Store{Files: storage.Local{Root: root}}
	svc := Service{DB: gdb, Media: mediaStore, Settings: settingsStore}
	product.HTTP{Service: product.Service{DB: gdb, Media: mediaStore}, Settings: settingsStore}.Register(engine)
	HTTP{Service: svc, Settings: settingsStore}.Register(engine)
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	ss := &sessionServer{pool: pool, db: gdb, root: root, media: mediaStore, svc: svc, srv: srv, client: &http.Client{}}
	login, err := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/session", strings.NewReader(`{"admin_key":"k"}`))
	if err != nil {
		t.Fatal(err)
	}
	login.Header.Set("Content-Type", "application/json")
	resp, err := ss.client.Do(login)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login %d", resp.StatusCode)
	}
	ss.cookies = resp.Cookies()
	var previousCapacity *string
	_ = ss.pool.QueryRow(context.Background(), `SELECT value FROM app_settings WHERE key = 'generation_max_concurrent_tasks'`).Scan(&previousCapacity)
	_, _ = ss.pool.Exec(context.Background(), `
		INSERT INTO app_settings (key, value, created_at, updated_at)
		VALUES ('admin_access_required', 'true', NOW(), NOW()),
		       ('generation_max_concurrent_tasks', '20', NOW(), NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`)
	t.Cleanup(func() {
		if previousCapacity == nil {
			_, _ = ss.pool.Exec(context.Background(), `DELETE FROM app_settings WHERE key = 'generation_max_concurrent_tasks'`)
			return
		}
		_, _ = ss.pool.Exec(context.Background(), `
			UPDATE app_settings SET value = $1, updated_at = NOW() WHERE key = 'generation_max_concurrent_tasks'
		`, *previousCapacity)
	})
	return ss
}

func (ss *sessionServer) dropDispatch(t *testing.T, aggregateID string) {
	t.Helper()
	if _, err := ss.pool.Exec(context.Background(), `DELETE FROM async_dispatches WHERE aggregate_id = $1`, aggregateID); err != nil {
		t.Fatal(err)
	}
}

func (ss *sessionServer) do(t *testing.T, method, path string, body io.Reader, contentType string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, ss.srv.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for _, c := range ss.cookies {
		req.AddCookie(c)
	}
	resp, err := ss.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func (ss *sessionServer) doJSON(t *testing.T, method, path string, payload any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return ss.do(t, method, path, bytes.NewReader(raw), "application/json")
}

func (ss *sessionServer) decode(t *testing.T, resp *http.Response, dest any) {
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

func (ss *sessionServer) mustStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("status %d want %d %s", resp.StatusCode, want, raw)
	}
}

func TestImageSessionCreateGenerateAndUnknown(t *testing.T) {
	ss := newSessionServer(t)
	created := ss.doJSON(t, http.MethodPost, "/api/image-sessions", map[string]any{})
	ss.mustStatus(t, created, http.StatusCreated)
	var session DetailResponse
	ss.decode(t, created, &session)
	if session.Title != "未命名会话" {
		t.Fatalf("title %s", session.Title)
	}
	if session.Assets == nil || session.Rounds == nil || session.GenerationTasks == nil {
		t.Fatalf("null lists %+v", session)
	}

	listed := ss.do(t, http.MethodGet, "/api/image-sessions", nil, "")
	ss.mustStatus(t, listed, http.StatusOK)

	gen := ss.doJSON(t, http.MethodPost, "/api/image-sessions/"+session.ID+"/generate", map[string]any{
		"prompt": "一只杯子", "size": "1024x1024", "generation_count": 1,
	})
	ss.mustStatus(t, gen, http.StatusAccepted)
	ss.decode(t, gen, &session)
	if len(session.GenerationTasks) != 1 {
		t.Fatalf("tasks %d", len(session.GenerationTasks))
	}
	taskID := session.GenerationTasks[0].ID
	var dispatchStatus string
	if err := ss.pool.QueryRow(context.Background(), `
		SELECT status FROM async_dispatches WHERE actor_name = 'run_image_session_generation_task' AND aggregate_id = $1
	`, taskID).Scan(&dispatchStatus); err != nil {
		t.Fatal(err)
	}
	if dispatchStatus != "pending" && dispatchStatus != "sent" {
		t.Fatalf("dispatch %s", dispatchStatus)
	}
	ss.dropDispatch(t, taskID)

	exec := Executor{DB: ss.db, Media: ss.media, Provider: MockChatProvider{}}
	if err := exec.Execute(context.Background(), taskID); err != nil {
		t.Fatal(err)
	}
	got := ss.do(t, http.MethodGet, "/api/image-sessions/"+session.ID, nil, "")
	ss.mustStatus(t, got, http.StatusOK)
	ss.decode(t, got, &session)
	if session.GenerationTasks[0].Status != "succeeded" {
		t.Fatalf("status %s reason %+v", session.GenerationTasks[0].Status, session.GenerationTasks[0].FailureReason)
	}

	unknown := ss.doJSON(t, http.MethodPost, "/api/image-sessions/"+session.ID+"/generate", map[string]any{
		"prompt": "再来一张", "size": "1024x1024", "generation_count": 1,
		"base_asset_id": session.Rounds[0].GeneratedAsset.ID,
	})
	ss.mustStatus(t, unknown, http.StatusAccepted)
	ss.decode(t, unknown, &session)
	failID := session.GenerationTasks[0].ID
	ss.dropDispatch(t, failID)
	failExec := Executor{DB: ss.db, Media: ss.media, Provider: MockChatProvider{Err: errors.New("provider crashed")}}
	if err := failExec.Execute(context.Background(), failID); err != nil {
		t.Fatal(err)
	}
	got = ss.do(t, http.MethodGet, "/api/image-sessions/"+session.ID, nil, "")
	ss.mustStatus(t, got, http.StatusOK)
	ss.decode(t, got, &session)
	var unknownTask *TaskResponse
	for i := range session.GenerationTasks {
		if session.GenerationTasks[i].ID == failID {
			unknownTask = &session.GenerationTasks[i]
		}
	}
	if unknownTask == nil || unknownTask.Status != "unknown" {
		t.Fatalf("unknown %+v", unknownTask)
	}
	if unknownTask.IsRetryable {
		t.Fatal("unknown must not be retryable")
	}
	if unknownTask.FailureReason == nil || *unknownTask.FailureReason != unknownDetail {
		t.Fatalf("reason %+v", unknownTask.FailureReason)
	}
	if unknownTask.ProgressPhase == nil || *unknownTask.ProgressPhase != unknownPhase {
		t.Fatalf("phase %+v", unknownTask.ProgressPhase)
	}
}

func TestImageSessionTextOutputFailedNotUnknown(t *testing.T) {
	ss := newSessionServer(t)
	created := ss.doJSON(t, http.MethodPost, "/api/image-sessions", map[string]any{})
	ss.mustStatus(t, created, http.StatusCreated)
	var session DetailResponse
	ss.decode(t, created, &session)
	gen := ss.doJSON(t, http.MethodPost, "/api/image-sessions/"+session.ID+"/generate", map[string]any{
		"prompt": "小猫", "size": "1024x1024", "generation_count": 1,
	})
	ss.mustStatus(t, gen, http.StatusAccepted)
	ss.decode(t, gen, &session)
	taskID := session.GenerationTasks[0].ID
	ss.dropDispatch(t, taskID)
	exec := Executor{DB: ss.db, Media: ss.media, Provider: MockChatProvider{Err: ErrTextOutput}}
	for i := 0; i < maxAttempts; i++ {
		if err := exec.Execute(context.Background(), taskID); err != nil {
			t.Fatal(err)
		}
	}
	got := ss.do(t, http.MethodGet, "/api/image-sessions/"+session.ID, nil, "")
	ss.mustStatus(t, got, http.StatusOK)
	ss.decode(t, got, &session)
	task := session.GenerationTasks[0]
	if task.Status != "failed" {
		t.Fatalf("status %s reason %+v", task.Status, task.FailureReason)
	}
	if task.FailureReason == nil || *task.FailureReason != ErrTextOutput.Error() {
		t.Fatalf("reason %+v", task.FailureReason)
	}
}

func TestImageSessionAttachToProduct(t *testing.T) {
	ss := newSessionServer(t)
	created := ss.doJSON(t, http.MethodPost, "/api/image-sessions", map[string]any{"title": "附加会话"})
	ss.mustStatus(t, created, http.StatusCreated)
	var session DetailResponse
	ss.decode(t, created, &session)
	gen := ss.doJSON(t, http.MethodPost, "/api/image-sessions/"+session.ID+"/generate", map[string]any{
		"prompt": "杯子", "generation_count": 1,
	})
	ss.mustStatus(t, gen, http.StatusAccepted)
	ss.decode(t, gen, &session)
	ss.dropDispatch(t, session.GenerationTasks[0].ID)
	if err := (Executor{DB: ss.db, Media: ss.media}).Execute(context.Background(), session.GenerationTasks[0].ID); err != nil {
		t.Fatal(err)
	}
	got := ss.do(t, http.MethodGet, "/api/image-sessions/"+session.ID, nil, "")
	ss.mustStatus(t, got, http.StatusOK)
	ss.decode(t, got, &session)
	assetID := session.Rounds[0].GeneratedAsset.ID

	body, ctype := productMultipart(t)
	prod := ss.do(t, http.MethodPost, "/api/v2/products", body, ctype)
	ss.mustStatus(t, prod, http.StatusCreated)
	var createdProd product.CreateResponse
	ss.decode(t, prod, &createdProd)

	attached := ss.doJSON(t, http.MethodPost, "/api/v2/image-sessions/"+session.ID+"/assets/"+assetID+"/attach-to-product", map[string]any{
		"product_id": createdProd.Product.ID,
	})
	ss.mustStatus(t, attached, http.StatusOK)
	var asset product.AssetResponse
	ss.decode(t, attached, &asset)
	if asset.OriginType != "image_session_attach" {
		t.Fatalf("origin %s", asset.OriginType)
	}

	again := ss.doJSON(t, http.MethodPost, "/api/v2/image-sessions/"+session.ID+"/assets/"+assetID+"/attach-to-product", map[string]any{
		"product_id": createdProd.Product.ID,
	})
	ss.mustStatus(t, again, http.StatusOK)
	var replay product.AssetResponse
	ss.decode(t, again, &replay)
	if replay.ID != asset.ID {
		t.Fatalf("idempotent %s vs %s", replay.ID, asset.ID)
	}
}

func TestImageSessionMissingIs404(t *testing.T) {
	ss := newSessionServer(t)
	resp := ss.do(t, http.MethodGet, "/api/image-sessions/00000000-0000-4000-8000-000000000001", nil, "")
	ss.mustStatus(t, resp, http.StatusNotFound)
	var body struct {
		Detail string `json:"detail"`
	}
	ss.decode(t, resp, &body)
	if body.Detail != "连续生图会话不存在" {
		t.Fatalf("%s", body.Detail)
	}
}

func TestImageSessionCreateRejectsUnknownFields(t *testing.T) {
	ss := newSessionServer(t)
	resp := ss.doJSON(t, http.MethodPost, "/api/image-sessions", map[string]any{"title": "a", "foo": 1})
	ss.mustStatus(t, resp, http.StatusBadRequest)
	resp.Body.Close()
}

func productMultipart(t *testing.T) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("name", "附加商品")
	part, err := w.CreateFormFile("images", "cup.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(pngBytes(t, 8, 6)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, w.FormDataContentType()
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
