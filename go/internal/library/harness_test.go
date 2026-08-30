package library

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
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/settings"
	"gorm.io/gorm"
)

type libraryServer struct {
	pool    *pgxpool.Pool
	db      *gorm.DB
	root    string
	srv     *httptest.Server
	client  *http.Client
	cookies []*http.Cookie
}

func newLibraryServer(t *testing.T) *libraryServer {
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
	auth.HTTP{AdminAccessKey: "k", Store: settingsStore}.Register(engine)
	mediaStore := media.Store{Files: storage.Local{Root: root}}
	product.HTTP{Service: product.Service{DB: gdb, Media: mediaStore}, Settings: settingsStore}.Register(engine)
	HTTP{Service: Service{DB: gdb, Media: mediaStore}, Settings: settingsStore}.Register(engine)
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	ls := &libraryServer{pool: pool, db: gdb, root: root, srv: srv, client: &http.Client{}}
	login, err := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/session", strings.NewReader(`{"admin_key":"k"}`))
	if err != nil {
		t.Fatal(err)
	}
	login.Header.Set("Content-Type", "application/json")
	resp, err := ls.client.Do(login)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login %d", resp.StatusCode)
	}
	ls.cookies = resp.Cookies()
	_, _ = ls.pool.Exec(context.Background(), `
		INSERT INTO app_settings (key, value, created_at, updated_at)
		VALUES ('admin_access_required', 'true', NOW(), NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`)
	return ls
}

func (ls *libraryServer) do(t *testing.T, method, path string, body io.Reader, contentType string, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, ls.srv.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	for _, c := range ls.cookies {
		req.AddCookie(c)
	}
	resp, err := ls.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func (ls *libraryServer) doJSON(t *testing.T, method, path string, payload any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return ls.do(t, method, path, bytes.NewReader(raw), "application/json", nil)
}

func (ls *libraryServer) decode(t *testing.T, resp *http.Response, dest any) {
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

func (ls *libraryServer) mustStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("status %d want %d %s", resp.StatusCode, want, raw)
	}
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

func multipartFiles(t *testing.T, files []struct {
	name string
	data []byte
}, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	for _, file := range files {
		part, err := w.CreateFormFile("files", file.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(file.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, w.FormDataContentType()
}

func (ls *libraryServer) createProduct(t *testing.T, name string) product.CreateResponse {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("name", name)
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
	resp := ls.do(t, http.MethodPost, "/api/v2/products", &buf, w.FormDataContentType(), nil)
	ls.mustStatus(t, resp, http.StatusCreated)
	var created product.CreateResponse
	ls.decode(t, resp, &created)
	return created
}

func (ls *libraryServer) createDirect(t *testing.T, name string) product.DirectCreateResponse {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("name", name)
	_ = w.WriteField("image_types", `[{"key":"hero","quantity":1}]`)
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
	resp := ls.do(t, http.MethodPost, "/api/v3/products", &buf, w.FormDataContentType(), nil)
	ls.mustStatus(t, resp, http.StatusCreated)
	var created product.DirectCreateResponse
	ls.decode(t, resp, &created)
	return created
}

func (ls *libraryServer) uploadOne(t *testing.T, filename string, headers map[string]string) AssetResponse {
	t.Helper()
	body, contentType := multipartFiles(t, []struct {
		name string
		data []byte
	}{{filename, pngBytes(t, 8, 6)}}, nil)
	resp := ls.do(t, http.MethodPost, "/api/media-library/upload", body, contentType, headers)
	ls.mustStatus(t, resp, http.StatusCreated)
	var items []AssetResponse
	ls.decode(t, resp, &items)
	if len(items) != 1 {
		t.Fatalf("%+v", items)
	}
	return items[0]
}
