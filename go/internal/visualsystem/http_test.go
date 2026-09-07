package visualsystem_test

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
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/settings"
	"github.com/yuqie6/productflow/internal/visualsystem"
	"gorm.io/gorm"
)

type vsServer struct {
	pool    *pgxpool.Pool
	db      *gorm.DB
	srv     *httptest.Server
	client  *http.Client
	cookies []*http.Cookie
}

func newVSServer(t *testing.T) *vsServer {
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
	mediaStore := media.Store{Files: storage.Local{Root: t.TempDir()}}
	product.HTTP{Service: product.Service{DB: gdb, Media: mediaStore}, Settings: settingsStore}.Register(engine)
	visualsystem.HTTP{Service: visualsystem.Service{DB: gdb}, Settings: settingsStore}.Register(engine)
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	vs := &vsServer{pool: pool, db: gdb, srv: srv, client: &http.Client{}}
	vs.cookies = auth.MustAuthenticate(t, vs.client, srv.URL)
	_, _ = vs.pool.Exec(context.Background(), `
		INSERT INTO app_settings (key, value, created_at, updated_at)
		VALUES ('admin_access_required', 'true', NOW(), NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`)
	return vs
}

func (vs *vsServer) doJSON(t *testing.T, method, path string, payload any) *http.Response {
	t.Helper()
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, vs.srv.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, c := range vs.cookies {
		req.AddCookie(c)
	}
	resp, err := vs.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func (vs *vsServer) decode(t *testing.T, resp *http.Response, dest any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		t.Fatal(err)
	}
}

func (vs *vsServer) createProduct(t *testing.T) string {
	t.Helper()
	var merchant schema.Merchants
	if err := vs.db.Order("created_at ASC, id ASC").Take(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	id := clockid.New()
	now := time.Now().UTC()
	if err := vs.db.Create(&schema.Products{
		ID: id, MerchantID: merchant.ID, Name: "视觉复用商品",
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	return id
}

func TestVisualSystemSaveSelectAppendDoesNotSilentUpdate(t *testing.T) {
	vs := newVSServer(t)
	created := vs.doJSON(t, http.MethodPost, "/api/v3/visual-systems", map[string]any{
		"name": "杯系列视觉",
		"payload": map[string]any{
			"style": []any{"冷色棚拍"},
		},
	})
	if created.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(created.Body)
		created.Body.Close()
		t.Fatalf("create system %d %s", created.StatusCode, raw)
	}
	var system visualsystem.SystemView
	vs.decode(t, created, &system)
	if system.CurrentVersion == nil || system.CurrentVersion.Version != 1 {
		t.Fatalf("current version %+v", system.CurrentVersion)
	}
	v1 := system.CurrentVersion.ID

	productID := vs.createProduct(t)
	selected := vs.doJSON(t, http.MethodPut, "/api/v3/products/"+productID+"/visual-selection", map[string]any{
		"visual_system_version_id": v1,
	})
	if selected.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(selected.Body)
		selected.Body.Close()
		t.Fatalf("select %d %s", selected.StatusCode, raw)
	}
	selected.Body.Close()

	appended := vs.doJSON(t, http.MethodPost, "/api/v3/visual-systems/"+system.ID+"/versions", map[string]any{
		"payload": map[string]any{"style": []any{"新版暖色"}},
	})
	if appended.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(appended.Body)
		appended.Body.Close()
		t.Fatalf("append %d %s", appended.StatusCode, raw)
	}
	var v2 visualsystem.VersionView
	vs.decode(t, appended, &v2)
	if v2.Version != 2 {
		t.Fatalf("version=%d", v2.Version)
	}

	var sel schema.ProductVisualSelections
	if err := vs.db.Where("product_id = ?", productID).Take(&sel).Error; err != nil {
		t.Fatal(err)
	}
	if sel.VisualSystemVersionID != v1 {
		t.Fatalf("append must not silent-update selection: got %s want %s", sel.VisualSystemVersionID, v1)
	}

	inh := vs.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/visual-inheritance", map[string]any{
		"product_override": map[string]any{"style": []any{"本商品覆盖"}},
	})
	if inh.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(inh.Body)
		inh.Body.Close()
		t.Fatalf("inheritance %d %s", inh.StatusCode, raw)
	}
	var inheritance visualsystem.InheritanceView
	vs.decode(t, inh, &inheritance)
	if inheritance.NewerVersionAvailable == nil || inheritance.NewerVersionAvailable.ID != v2.ID {
		t.Fatalf("expected newer version hint %+v", inheritance.NewerVersionAvailable)
	}
	style, _ := inheritance.EffectivePayload["style"].([]any)
	if len(style) != 1 || style[0] != "本商品覆盖" {
		t.Fatalf("effective %+v", inheritance.EffectivePayload)
	}

	impact := vs.doJSON(t, http.MethodGet, "/api/v3/visual-systems/"+system.ID+"/versions/"+v2.ID+"/impact", nil)
	if impact.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(impact.Body)
		impact.Body.Close()
		t.Fatalf("impact %d %s", impact.StatusCode, raw)
	}
	var impactView visualsystem.ImpactView
	vs.decode(t, impact, &impactView)
	if len(impactView.PinnedToOlder) != 1 || impactView.PinnedToOlder[0].ProductID != productID {
		t.Fatalf("impact %+v", impactView.PinnedToOlder)
	}

	adopt := vs.doJSON(t, http.MethodPut, "/api/v3/products/"+productID+"/visual-selection", map[string]any{
		"visual_system_version_id": v2.ID,
	})
	if adopt.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(adopt.Body)
		adopt.Body.Close()
		t.Fatalf("adopt %d %s", adopt.StatusCode, raw)
	}
	adopt.Body.Close()
	if err := vs.db.Where("product_id = ?", productID).Take(&sel).Error; err != nil {
		t.Fatal(err)
	}
	if sel.VisualSystemVersionID != v2.ID {
		t.Fatalf("explicit adopt failed: %s", sel.VisualSystemVersionID)
	}
}
