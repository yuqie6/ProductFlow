package product

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/settings"
	"gorm.io/gorm"
)

type productServer struct {
	pool    *pgxpool.Pool
	db      *gorm.DB
	svc     Service
	srv     *httptest.Server
	client  *http.Client
	cookies []*http.Cookie
}

func newProductServer(t *testing.T) *productServer {
	return newProductServerWith(t, Service{})
}

func newProductServerWith(t *testing.T, overlay Service) *productServer {
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
	svc := Service{DB: gdb, Media: media.Store{Files: storage.Local{Root: root}}}
	if overlay.SourceNote != nil {
		svc.SourceNote = overlay.SourceNote
	}
	if overlay.Now != nil {
		svc.Now = overlay.Now
	}
	if overlay.Canvas != nil {
		svc.Canvas = overlay.Canvas
	}
	HTTP{Service: svc, Settings: settingsStore}.Register(engine)
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	ps := &productServer{pool: pool, db: gdb, svc: svc, srv: srv, client: &http.Client{}}
	login, err := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/session", strings.NewReader(`{"admin_key":"k"}`))
	if err != nil {
		t.Fatal(err)
	}
	login.Header.Set("Content-Type", "application/json")
	resp, err := ps.client.Do(login)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login %d", resp.StatusCode)
	}
	ps.cookies = resp.Cookies()
	ps.setSetting(t, "admin_access_required", "true")
	return ps
}

func (ps *productServer) setSetting(t *testing.T, key, value string) {
	t.Helper()
	_, err := ps.pool.Exec(context.Background(), `
		INSERT INTO app_settings (key, value, created_at, updated_at)
		VALUES ($1, $2, NOW(), NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`, key, value)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = ps.pool.Exec(context.Background(), `DELETE FROM app_settings WHERE key = $1`, key)
	})
}

func (ps *productServer) do(t *testing.T, method, path string, body io.Reader, contentType string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, ps.srv.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for _, c := range ps.cookies {
		req.AddCookie(c)
	}
	resp, err := ps.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func (ps *productServer) doJSON(t *testing.T, method, path string, payload any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return ps.do(t, method, path, bytes.NewReader(raw), "application/json")
}

func (ps *productServer) decode(t *testing.T, resp *http.Response, dest any) {
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

func (ps *productServer) createV2(t *testing.T, name string, extra map[string]string, files int) CreateResponse {
	t.Helper()
	fields := map[string]string{"name": name}
	for k, v := range extra {
		fields[k] = v
	}
	body, contentType := multipartPNGs(t, fields, files)
	resp := ps.do(t, http.MethodPost, "/api/v2/products", body, contentType)
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("create %d %s", resp.StatusCode, raw)
	}
	var created CreateResponse
	ps.decode(t, resp, &created)
	return created
}

func (ps *productServer) setDeletion(t *testing.T, enabled bool) {
	t.Helper()
	value := "false"
	if enabled {
		value = "true"
	}
	ps.setSetting(t, "deletion_enabled", value)
}

func (ps *productServer) enableDeletion(t *testing.T) {
	t.Helper()
	ps.setDeletion(t, true)
}

func multipartPNGs(t *testing.T, fields map[string]string, n int) (*bytes.Buffer, string) {
	t.Helper()
	if n < 1 {
		n = 1
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	for i := 0; i < n; i++ {
		part, err := w.CreateFormFile("images", "cup.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(pngFile(t, 8, 6)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, w.FormDataContentType()
}

func freeze(now time.Time) func() time.Time {
	return func() time.Time { return now }
}
