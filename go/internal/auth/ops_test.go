package auth_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/settings"
)

func registerOpsSettings(engine *gin.Engine, store *settings.Store) {
	settings.HTTP{Store: store, DB: store, OperatorOnly: auth.RequireOperator()}.Register(engine)
}

func ordinaryAccount(t *testing.T, as *authServer, label string) (string, []*http.Cookie) {
	t.Helper()
	now := time.Now().UTC()
	merchant := schema.Merchants{
		ID: clockid.New(), Name: label + "商家", Status: auth.MerchantStatusActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := as.db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = as.db.Where("id = ?", merchant.ID).Delete(&schema.Merchants{}).Error })
	email := fmt.Sprintf("%s-%d@test.local", label, now.UnixNano())
	seedDirectUser(t, as.db, merchant.ID, email, "ordinary-password-ok", label+"账号", false)
	return merchant.ID, loginDirectUser(t, as, email, "ordinary-password-ok")
}

func TestOrdinaryForbiddenOnSettingsAndQueue(t *testing.T) {
	as := newAuthServer(t)
	_ = auth.MustAuthenticate(t, as.client, as.srv.URL)
	_, ordinaryCookies := ordinaryAccount(t, as, "settings-ordinary")

	for _, path := range []string{
		"/api/settings",
		"/api/generation-queue",
		"/api/settings/provider-config",
	} {
		resp := as.do(t, http.MethodGet, path, "", ordinaryCookies)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("%s status %d body %s", path, resp.StatusCode, body)
		}
	}
	removed := as.do(t, http.MethodGet, "/api/settings/lock-state", "", ordinaryCookies)
	removed.Body.Close()
	if removed.StatusCode != http.StatusNotFound {
		t.Fatalf("retired lock-state route %d", removed.StatusCode)
	}
}

func TestSettingsOperatorGuardIgnoresAdminAccessRequired(t *testing.T) {
	as := newAuthServer(t)
	opCookies := auth.MustAuthenticate(t, as.client, as.srv.URL)
	_, ordinaryCookies := ordinaryAccount(t, as, "guard-ordinary")
	t.Cleanup(func() {
		_ = as.db.Exec(`
			INSERT INTO app_settings (key, value, created_at, updated_at)
			VALUES ('admin_access_required', 'true', NOW(), NOW())
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
		`).Error
	})
	if err := as.db.Exec(`
		INSERT INTO app_settings (key, value, created_at, updated_at)
		VALUES ('admin_access_required', 'false', NOW(), NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`).Error; err != nil {
		t.Fatal(err)
	}

	anonymousState := as.do(t, http.MethodGet, "/api/auth/session", "", nil)
	var state struct {
		Authenticated bool `json:"authenticated"`
	}
	if err := json.NewDecoder(anonymousState.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	anonymousState.Body.Close()
	if state.Authenticated {
		t.Fatal("anonymous session reported authenticated with old gate disabled")
	}
	wrongLogin := as.do(t, http.MethodPost, "/api/auth/session", `{"email":"`+auth.TestOperatorEmail+`","password":"wrong-password"}`, nil)
	wrongLogin.Body.Close()
	if wrongLogin.StatusCode != http.StatusUnauthorized {
		t.Fatalf("disabled gate bypassed password: %d", wrongLogin.StatusCode)
	}
	login := as.do(t, http.MethodPost, "/api/auth/session", `{"email":"`+auth.TestOperatorEmail+`","password":"`+auth.TestPassword+`"}`, nil)
	login.Body.Close()
	if login.StatusCode != http.StatusOK {
		t.Fatalf("disabled gate prevented password login: %d", login.StatusCode)
	}
	loggedIn := as.do(t, http.MethodGet, "/api/auth/session", "", login.Cookies())
	if err := json.NewDecoder(loggedIn.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	loggedIn.Body.Close()
	if !state.Authenticated {
		t.Fatal("successful password login did not establish identity")
	}

	opConfig := as.do(t, http.MethodGet, "/api/settings", "", opCookies)
	opConfig.Body.Close()
	if opConfig.StatusCode != http.StatusOK {
		t.Fatalf("operator config %d", opConfig.StatusCode)
	}

	runtime := as.do(t, http.MethodGet, "/api/settings/runtime", "", ordinaryCookies)
	runtime.Body.Close()
	if runtime.StatusCode != http.StatusOK {
		t.Fatalf("ordinary runtime %d", runtime.StatusCode)
	}
	ordinaryConfig := as.do(t, http.MethodGet, "/api/settings", "", ordinaryCookies)
	ordinaryConfig.Body.Close()
	if ordinaryConfig.StatusCode != http.StatusForbidden {
		t.Fatalf("ordinary config %d", ordinaryConfig.StatusCode)
	}

	anonymousConfig := as.do(t, http.MethodGet, "/api/settings", "", nil)
	anonymousConfig.Body.Close()
	if anonymousConfig.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous config %d", anonymousConfig.StatusCode)
	}
}

func TestOperatorSettingsExportHasNoMerchantSecrets(t *testing.T) {
	as := newAuthServer(t)
	opCookies := auth.MustAuthenticate(t, as.client, as.srv.URL)
	exportResp := as.do(t, http.MethodGet, "/api/settings/export", "", opCookies)
	defer exportResp.Body.Close()
	raw, _ := io.ReadAll(exportResp.Body)
	if exportResp.StatusCode != http.StatusOK {
		t.Fatalf("export %d %s", exportResp.StatusCode, raw)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"merchant_api_key", "merchant_secret", "byok", "byok_api_key", "merchant_keys",
	} {
		if strings.Contains(string(raw), `"`+forbidden+`"`) {
			t.Fatalf("export contains merchant secret field %q", forbidden)
		}
	}
	if _, ok := doc["provider_profiles"]; !ok {
		t.Fatal("missing provider_profiles")
	}
}

func TestOperatorSuspendBlocksMerchantWrites(t *testing.T) {
	pool, gdb := testdb.Open(t)
	engine := httpx.NewEngine(nil)
	engine.Use(httpx.Session(httpx.NewCookieStore(httpx.SessionConfig{Secret: "test-session-secret-key"})))
	store := settings.NewStore(pool, config.Config{
		AdminAccessRequired:      true,
		UploadMaxImageBytes:      10 * 1024 * 1024,
		UploadMaxPixels:          16_000_000,
		UploadAllowedMIMETypes:   "image/png,image/jpeg,image/webp",
		UploadMaxBatchFiles:      20,
		UploadMaxBatchBytes:      50 * 1024 * 1024,
		UploadMaxReferenceImages: 6,
	})
	if err := gdb.Exec(`
		INSERT INTO app_settings (key, value, created_at, updated_at)
		VALUES ('admin_access_required', 'true', NOW(), NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`).Error; err != nil {
		t.Fatal(err)
	}
	auth.MountTest(engine, gdb, store, auth.TestAdminKey)
	registerOpsSettings(engine, store)
	product.HTTP{
		Service:  product.Service{DB: gdb},
		Settings: store,
	}.Register(engine)
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	as := &authServer{srv: srv, client: &http.Client{}, db: gdb}

	opCookies := auth.MustAuthenticate(t, as.client, as.srv.URL)
	merchantID, ordinaryCookies := ordinaryAccount(t, as, "suspend-ordinary")

	now := time.Now().UTC()
	productID := fmt.Sprintf("ops-product-%d", now.UnixNano())
	if err := gdb.Create(&schema.Products{
		ID: productID, MerchantID: merchantID, Name: "启停测商品", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	folderOK := as.do(t, http.MethodPost, "/api/v2/products/"+productID+"/image-folders",
		`{"name":"停用前文件夹"}`, ordinaryCookies)
	folderOK.Body.Close()
	if folderOK.StatusCode != http.StatusCreated {
		t.Fatalf("folder before suspend %d", folderOK.StatusCode)
	}

	suspend := as.do(t, http.MethodPatch, "/api/ops/merchants/"+merchantID+"/status",
		`{"status":"suspended"}`, opCookies)
	var suspended map[string]string
	_ = json.NewDecoder(suspend.Body).Decode(&suspended)
	suspend.Body.Close()
	if suspend.StatusCode != http.StatusOK || suspended["status"] != auth.MerchantStatusSuspended {
		t.Fatalf("suspend %d %#v", suspend.StatusCode, suspended)
	}

	denied := as.do(t, http.MethodPost, "/api/v2/products/"+productID+"/image-folders",
		`{"name":"停用后文件夹"}`, ordinaryCookies)
	body, _ := io.ReadAll(denied.Body)
	denied.Body.Close()
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("write after suspend %d %s", denied.StatusCode, body)
	}
	if !bytes.Contains(body, []byte("商家已停用")) {
		t.Fatalf("detail %s", body)
	}

	listOK := as.do(t, http.MethodGet, "/api/v2/products", "", ordinaryCookies)
	listOK.Body.Close()
	if listOK.StatusCode != http.StatusOK {
		t.Fatalf("read after suspend %d", listOK.StatusCode)
	}

	reactivate := as.do(t, http.MethodPatch, "/api/ops/merchants/"+merchantID+"/status",
		`{"status":"active"}`, opCookies)
	reactivate.Body.Close()
	if reactivate.StatusCode != http.StatusOK {
		t.Fatalf("reactivate %d", reactivate.StatusCode)
	}
	folderAgain := as.do(t, http.MethodPost, "/api/v2/products/"+productID+"/image-folders",
		`{"name":"恢复后文件夹"}`, ordinaryCookies)
	folderAgain.Body.Close()
	if folderAgain.StatusCode != http.StatusCreated {
		t.Fatalf("folder after reactivate %d", folderAgain.StatusCode)
	}
}

func TestOperatorProviderConfigOnly(t *testing.T) {
	as := newAuthServer(t)
	opCookies := auth.MustAuthenticate(t, as.client, as.srv.URL)
	doc := as.do(t, http.MethodGet, "/api/settings/provider-config", "", opCookies)
	defer doc.Body.Close()
	if doc.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(doc.Body)
		t.Fatalf("provider config %d %s", doc.StatusCode, raw)
	}

	_, ordinaryCookies := ordinaryAccount(t, as, "provider-ordinary")
	deny := as.do(t, http.MethodGet, "/api/settings/provider-config", "", ordinaryCookies)
	deny.Body.Close()
	if deny.StatusCode != http.StatusForbidden {
		t.Fatalf("ordinary provider config %d", deny.StatusCode)
	}
}

func TestRemovedSupportRoutesReturnNotFound(t *testing.T) {
	as := newAuthServer(t)
	opCookies := auth.MustAuthenticate(t, as.client, as.srv.URL)
	for _, route := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/ops/support-contract"},
		{http.MethodPost, "/api/ops/support-sessions"},
		{http.MethodGet, "/api/ops/support-sessions/removed"},
		{http.MethodPost, "/api/ops/support-sessions/removed/end"},
	} {
		resp := as.do(t, route.method, route.path, "", opCookies)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("removed route %s %s returned %d", route.method, route.path, resp.StatusCode)
		}
	}
}

func TestOrdinaryCannotSetMerchantStatus(t *testing.T) {
	as := newAuthServer(t)
	merchantID, ordinaryCookies := ordinaryAccount(t, as, "status-ordinary")
	resp := as.do(t, http.MethodPatch, "/api/ops/merchants/"+merchantID+"/status",
		`{"status":"suspended"}`, ordinaryCookies)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("ordinary set status %d", resp.StatusCode)
	}
}
