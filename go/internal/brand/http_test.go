package brand_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/brand"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/settings"
	"github.com/yuqie6/productflow/internal/visualsystem"
	"gorm.io/gorm"
)

type brandServer struct {
	pool    *pgxpool.Pool
	db      *gorm.DB
	srv     *httptest.Server
	client  *http.Client
	cookies []*http.Cookie
}

func newBrandServer(t *testing.T) *brandServer {
	t.Helper()
	pool, gdb := testdb.Open(t)
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
	visualsystem.HTTP{Service: visualsystem.Service{DB: gdb}, Settings: settingsStore}.Register(engine)
	brand.HTTP{Service: brand.Service{DB: gdb}, Settings: settingsStore}.Register(engine)
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	bs := &brandServer{pool: pool, db: gdb, srv: srv, client: &http.Client{}}
	bs.cookies = auth.MustAuthenticate(t, bs.client, srv.URL)
	_, _ = bs.pool.Exec(context.Background(), `
		INSERT INTO app_settings (key, value, created_at, updated_at)
		VALUES ('admin_access_required', 'true', NOW(), NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`)
	return bs
}

func (bs *brandServer) doJSON(t *testing.T, method, path string, payload any, cookies []*http.Cookie) *http.Response {
	t.Helper()
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, bs.srv.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	use := cookies
	if use == nil {
		use = bs.cookies
	}
	for _, c := range use {
		req.AddCookie(c)
	}
	resp, err := bs.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func (bs *brandServer) decode(t *testing.T, resp *http.Response, dest any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		t.Fatal(err)
	}
}

func TestBrandCRUDSameMerchant(t *testing.T) {
	bs := newBrandServer(t)

	created := bs.doJSON(t, http.MethodPost, "/api/v3/brands", map[string]any{"name": "杯品牌"}, nil)
	if created.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(created.Body)
		created.Body.Close()
		t.Fatalf("create %d %s", created.StatusCode, raw)
	}
	var view brand.View
	bs.decode(t, created, &view)
	if view.Name != "杯品牌" || view.ID == "" || view.MerchantID == "" {
		t.Fatalf("create view %+v", view)
	}

	listed := bs.doJSON(t, http.MethodGet, "/api/v3/brands", nil, nil)
	if listed.StatusCode != http.StatusOK {
		t.Fatalf("list %d", listed.StatusCode)
	}
	var list []brand.View
	bs.decode(t, listed, &list)
	if len(list) < 1 {
		t.Fatalf("list empty %+v", list)
	}

	got := bs.doJSON(t, http.MethodGet, "/api/v3/brands/"+view.ID, nil, nil)
	if got.StatusCode != http.StatusOK {
		t.Fatalf("get %d", got.StatusCode)
	}
	var one brand.View
	bs.decode(t, got, &one)
	if one.ID != view.ID {
		t.Fatalf("get %+v", one)
	}

	sys := bs.doJSON(t, http.MethodPost, "/api/v3/visual-systems", map[string]any{
		"name": "挂接方案", "payload": map[string]any{"style": []any{"冷色"}},
	}, nil)
	if sys.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(sys.Body)
		sys.Body.Close()
		t.Fatalf("visual system %d %s", sys.StatusCode, raw)
	}
	var system visualsystem.SystemView
	bs.decode(t, sys, &system)

	patched := bs.doJSON(t, http.MethodPatch, "/api/v3/brands/"+view.ID, map[string]any{
		"name": "杯品牌改名", "visual_system_id": system.ID,
	}, nil)
	if patched.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(patched.Body)
		patched.Body.Close()
		t.Fatalf("patch %d %s", patched.StatusCode, raw)
	}
	var updated brand.View
	bs.decode(t, patched, &updated)
	if updated.Name != "杯品牌改名" || updated.VisualSystemID == nil || *updated.VisualSystemID != system.ID {
		t.Fatalf("updated %+v", updated)
	}

	cleared := bs.doJSON(t, http.MethodPatch, "/api/v3/brands/"+view.ID, map[string]any{
		"visual_system_id": nil,
	}, nil)
	if cleared.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(cleared.Body)
		cleared.Body.Close()
		t.Fatalf("clear %d %s", cleared.StatusCode, raw)
	}
	var clearedView brand.View
	bs.decode(t, cleared, &clearedView)
	if clearedView.VisualSystemID != nil {
		t.Fatalf("expected cleared visual_system_id %+v", clearedView)
	}
}

func TestBrandCrossMerchantDenied(t *testing.T) {
	bs := newBrandServer(t)
	dual := auth.SeedDualMerchants(t, bs.db, bs.client, bs.srv.URL)

	created := bs.doJSON(t, http.MethodPost, "/api/v3/brands", map[string]any{"name": "商家A品牌"}, dual.CookiesA)
	if created.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(created.Body)
		created.Body.Close()
		t.Fatalf("create A %d %s", created.StatusCode, raw)
	}
	var viewA brand.View
	bs.decode(t, created, &viewA)

	cross := bs.doJSON(t, http.MethodGet, "/api/v3/brands/"+viewA.ID, nil, dual.CookiesB)
	raw, _ := io.ReadAll(cross.Body)
	cross.Body.Close()
	if cross.StatusCode != http.StatusNotFound {
		t.Fatalf("cross get %d %s", cross.StatusCode, raw)
	}
	var body struct {
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body.Detail != auth.CrossMerchantDetail {
		t.Fatalf("detail=%q", body.Detail)
	}

	listed := bs.doJSON(t, http.MethodGet, "/api/v3/brands", nil, dual.CookiesB)
	var listB []brand.View
	bs.decode(t, listed, &listB)
	for _, item := range listB {
		if item.ID == viewA.ID || item.MerchantID == dual.MerchantAID {
			t.Fatalf("merchant B listed A brand %+v", item)
		}
	}

	now := time.Now().UTC()
	sysA := schema.VisualSystems{
		ID: clockid.New(), MerchantID: dual.MerchantAID, Name: "A方案",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := bs.db.Create(&sysA).Error; err != nil {
		t.Fatal(err)
	}
	createdB := bs.doJSON(t, http.MethodPost, "/api/v3/brands", map[string]any{
		"name": "商家B品牌", "visual_system_id": sysA.ID,
	}, dual.CookiesB)
	rawB, _ := io.ReadAll(createdB.Body)
	createdB.Body.Close()
	if createdB.StatusCode != http.StatusNotFound {
		t.Fatalf("cross visual link %d %s", createdB.StatusCode, rawB)
	}
}
