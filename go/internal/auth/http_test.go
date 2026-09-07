package auth_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/settings"
	"gorm.io/gorm"
)

type authServer struct {
	srv    *httptest.Server
	client *http.Client
	db     *gorm.DB
}

func newAuthServer(t *testing.T) *authServer {
	t.Helper()
	pool, gdb := testdb.Open(t)
	engine := httpx.NewEngine(nil)
	engine.Use(httpx.Session(httpx.NewCookieStore(httpx.SessionConfig{Secret: "test-session-secret-key"})))
	store := settings.NewStore(pool, config.Config{AdminAccessRequired: true})
	if err := gdb.Exec(`
		INSERT INTO app_settings (key, value, created_at, updated_at)
		VALUES ('admin_access_required', 'true', NOW(), NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`).Error; err != nil {
		t.Fatal(err)
	}
	auth.MountTest(engine, gdb, store, auth.TestAdminKey)
	settings.HTTP{
		Store: store, DB: store, OperatorOnly: auth.RequireOperator(),
	}.Register(engine)
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	return &authServer{srv: srv, client: &http.Client{}, db: gdb}
}

func (as *authServer) do(t *testing.T, method, path, body string, cookies []*http.Cookie) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, as.srv.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := as.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func readDetail(t *testing.T, resp *http.Response) string {
	t.Helper()
	var payload map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	return payload["detail"]
}

func TestLoginMalformedJSON(t *testing.T) {
	as := newAuthServer(t)
	for _, body := range []string{``, `{`, `not-json`, `{"email":"a@b.c"}{}`, `{"email":"a@b.c","password":"x","extra":1}`} {
		resp := as.do(t, http.MethodPost, "/api/auth/session", body, nil)
		if resp.StatusCode != http.StatusBadRequest {
			resp.Body.Close()
			t.Fatalf("body %q status %d", body, resp.StatusCode)
		}
		detail := readDetail(t, resp)
		resp.Body.Close()
		if detail != "请求体无效" {
			t.Fatalf("body %q detail %q", body, detail)
		}
	}
}

func TestLoginWrongPassword(t *testing.T) {
	as := newAuthServer(t)
	_ = auth.MustAuthenticate(t, as.client, as.srv.URL)
	resp := as.do(t, http.MethodPost, "/api/auth/session", `{"email":"`+auth.TestOperatorEmail+`","password":"wrong-password"}`, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestLoginAndRuntime(t *testing.T) {
	as := newAuthServer(t)
	cookies := auth.MustAuthenticate(t, as.client, as.srv.URL)
	resp := as.do(t, http.MethodGet, "/api/settings/runtime", "", cookies)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("runtime %d", resp.StatusCode)
	}
}

func TestRuntimeRequiresLogin(t *testing.T) {
	as := newAuthServer(t)
	resp := as.do(t, http.MethodGet, "/api/settings/runtime", "", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestSessionRevokeReturns401(t *testing.T) {
	as := newAuthServer(t)
	cookies := auth.MustAuthenticate(t, as.client, as.srv.URL)
	resp := as.do(t, http.MethodDelete, "/api/auth/session", "", cookies)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout %d", resp.StatusCode)
	}
	resp = as.do(t, http.MethodGet, "/api/settings/runtime", "", cookies)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked session status %d", resp.StatusCode)
	}
}

func TestSessionProjectsDirectMerchant(t *testing.T) {
	as := newAuthServer(t)
	operatorCookies := auth.MustAuthenticate(t, as.client, as.srv.URL)
	state := as.do(t, http.MethodGet, "/api/auth/session", "", operatorCookies)
	var operatorBody map[string]json.RawMessage
	if err := json.NewDecoder(state.Body).Decode(&operatorBody); err != nil {
		state.Body.Close()
		t.Fatal(err)
	}
	state.Body.Close()
	if _, ok := operatorBody["memberships"]; ok {
		t.Fatal("session still exposes memberships")
	}
	var operatorMerchant auth.MerchantView
	if err := json.Unmarshal(operatorBody["merchant"], &operatorMerchant); err != nil {
		t.Fatal(err)
	}
	if operatorMerchant.ID == "" {
		t.Fatalf("operator merchant %#v", operatorMerchant)
	}

	now := time.Now().UTC()
	merchant := schema.Merchants{ID: "auth-http-merchant-" + time.Now().Format("150405.000000000"), Name: "独立普通账号商家", Status: auth.MerchantStatusActive, CreatedAt: now, UpdatedAt: now}
	if err := as.db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = as.db.Where("id = ?", merchant.ID).Delete(&schema.Merchants{}).Error })
	email := "direct-" + time.Now().Format("150405.000000000") + "@test.local"
	user := seedDirectUser(t, as.db, merchant.ID, email, "direct-password-ok", "独立账号", false)
	if user.MerchantID == nil || *user.MerchantID != merchant.ID {
		t.Fatalf("direct user merchant %#v", user.MerchantID)
	}
	cookies := loginDirectUser(t, as, email, "direct-password-ok")
	userState := as.do(t, http.MethodGet, "/api/auth/session", "", cookies)
	var userBody map[string]json.RawMessage
	if err := json.NewDecoder(userState.Body).Decode(&userBody); err != nil {
		userState.Body.Close()
		t.Fatal(err)
	}
	userState.Body.Close()
	if _, ok := userBody["memberships"]; ok {
		t.Fatal("ordinary session still exposes memberships")
	}
	var directMerchant auth.MerchantView
	if err := json.Unmarshal(userBody["merchant"], &directMerchant); err != nil {
		t.Fatal(err)
	}
	if directMerchant.ID != merchant.ID || directMerchant.Name != merchant.Name || directMerchant.Status != merchant.Status {
		t.Fatalf("direct merchant %#v", directMerchant)
	}
}

func TestDirectMerchantOwnershipAndOperatorMerchantList(t *testing.T) {
	as := newAuthServer(t)
	operatorCookies := auth.MustAuthenticate(t, as.client, as.srv.URL)
	now := time.Now().UTC()
	merchant := schema.Merchants{ID: "auth-owner-merchant-" + time.Now().Format("150405.000000000"), Name: "归属校验商家", Status: auth.MerchantStatusActive, CreatedAt: now, UpdatedAt: now}
	if err := as.db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = as.db.Where("id = ?", merchant.ID).Delete(&schema.Merchants{}).Error })
	email := "owner-check-" + time.Now().Format("150405.000000000") + "@test.local"
	user := seedDirectUser(t, as.db, merchant.ID, email, "owner-check-password", "归属账号", false)
	svc := auth.Service{DB: as.db}
	view, err := svc.OwnMerchant(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view == nil || view.ID != merchant.ID {
		t.Fatalf("own merchant %#v", view)
	}
	if err := svc.RequireOwnMerchant(t.Context(), user.ID, merchant.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.RequireOwnMerchant(t.Context(), user.ID, "other-merchant"); err == nil || !strings.Contains(err.Error(), auth.CrossMerchantDetail) {
		t.Fatalf("cross merchant error %v", err)
	}

	list := as.do(t, http.MethodGet, "/api/ops/merchants?page=1&page_size=2", "", operatorCookies)
	defer list.Body.Close()
	if list.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(list.Body)
		t.Fatalf("operator merchant list %d %s", list.StatusCode, raw)
	}
	var page auth.MerchantPage
	if err := json.NewDecoder(list.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if page.Page != 1 || page.PageSize != 2 || page.Total < 2 || len(page.Items) > 2 {
		t.Fatalf("merchant page %#v", page)
	}
	for _, item := range page.Items {
		if item.ID == "" || item.Name == "" || item.Status == "" {
			t.Fatalf("unbounded merchant item %#v", item)
		}
	}
	for _, query := range []string{"?page_size=0", "?page_size=101", "?page=0"} {
		resp := as.do(t, http.MethodGet, "/api/ops/merchants"+query, "", operatorCookies)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid merchant list query %s status %d", query, resp.StatusCode)
		}
	}
	unauthenticated := as.do(t, http.MethodGet, "/api/ops/merchants", "", nil)
	unauthenticated.Body.Close()
	if unauthenticated.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous merchant list %d", unauthenticated.StatusCode)
	}
}

func TestLegacyMerchantRoutesRemoved(t *testing.T) {
	as := newAuthServer(t)
	cookies := auth.MustAuthenticate(t, as.client, as.srv.URL)
	for _, route := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/merchants"},
		{http.MethodPost, "/api/merchants"},
		{http.MethodPatch, "/api/merchants/removed/status"},
		{http.MethodDelete, "/api/merchants/removed/memberships/removed"},
		{http.MethodPost, "/api/merchants/removed/memberships/removed/restore"},
	} {
		resp := as.do(t, route.method, route.path, "", cookies)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("retired route %s %s returned %d", route.method, route.path, resp.StatusCode)
		}
	}
}

func TestSecondMerchantRejected(t *testing.T) {
	as := newAuthServer(t)
	cookies := auth.MustAuthenticate(t, as.client, as.srv.URL)
	resp := as.do(t, http.MethodPost, "/api/merchants", `{"name":"第二商家"}`, cookies)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestAdminKeyLoginRemoved(t *testing.T) {
	as := newAuthServer(t)
	resp := as.do(t, http.MethodPost, "/api/auth/session", `{"admin_key":"k"}`, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("admin_key login should be invalid body, got %d", resp.StatusCode)
	}
}
