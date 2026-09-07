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

func editorMember(t *testing.T, as *authServer, ownerCookies []*http.Cookie, merchantID string) []*http.Cookie {
	t.Helper()
	email := fmt.Sprintf("editor-ops-%d@test.local", time.Now().UnixNano())
	_ = ownerCookies
	seedDirectMember(t, as.db, merchantID, email, "editor-password-ok", "编辑", auth.RoleEditor, false)
	return loginDirectMember(t, as, email, "editor-password-ok")
}

func TestMerchantRoleForbiddenOnSettingsAndQueue(t *testing.T) {
	as := newAuthServer(t)
	ownerCookies := auth.MustAuthenticate(t, as.client, as.srv.URL)
	state := as.do(t, http.MethodGet, "/api/auth/session", "", ownerCookies)
	var stateBody struct {
		Memberships []auth.MembershipView `json:"memberships"`
	}
	_ = json.NewDecoder(state.Body).Decode(&stateBody)
	state.Body.Close()
	merchantID := stateBody.Memberships[0].MerchantID
	editorCookies := editorMember(t, as, ownerCookies, merchantID)

	for _, path := range []string{
		"/api/settings",
		"/api/generation-queue",
		"/api/ops/support-contract",
	} {
		resp := as.do(t, http.MethodGet, path, "", editorCookies)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("%s status %d body %s", path, resp.StatusCode, body)
		}
	}
	removed := as.do(t, http.MethodGet, "/api/settings/lock-state", "", editorCookies)
	removed.Body.Close()
	if removed.StatusCode != http.StatusNotFound {
		t.Fatalf("retired lock-state route %d", removed.StatusCode)
	}
}

func TestSettingsOperatorGuardIgnoresAdminAccessRequired(t *testing.T) {
	as := newAuthServer(t)
	opCookies := auth.MustAuthenticate(t, as.client, as.srv.URL)
	t.Cleanup(func() {
		_ = as.db.Exec(`
			INSERT INTO app_settings (key, value, created_at, updated_at)
			VALUES ('admin_access_required', 'true', NOW(), NOW())
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
		`).Error
	})
	state := as.do(t, http.MethodGet, "/api/auth/session", "", opCookies)
	var stateBody struct {
		Memberships []auth.MembershipView `json:"memberships"`
	}
	if err := json.NewDecoder(state.Body).Decode(&stateBody); err != nil {
		state.Body.Close()
		t.Fatal(err)
	}
	state.Body.Close()
	if len(stateBody.Memberships) != 1 {
		t.Fatalf("memberships %#v", stateBody.Memberships)
	}
	editorCookies := editorMember(t, as, opCookies, stateBody.Memberships[0].MerchantID)
	if err := as.db.Exec(`
		INSERT INTO app_settings (key, value, created_at, updated_at)
		VALUES ('admin_access_required', 'false', NOW(), NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`).Error; err != nil {
		t.Fatal(err)
	}

	opConfig := as.do(t, http.MethodGet, "/api/settings", "", opCookies)
	opConfig.Body.Close()
	if opConfig.StatusCode != http.StatusOK {
		t.Fatalf("operator config %d", opConfig.StatusCode)
	}

	runtime := as.do(t, http.MethodGet, "/api/settings/runtime", "", editorCookies)
	runtime.Body.Close()
	if runtime.StatusCode != http.StatusOK {
		t.Fatalf("ordinary runtime %d", runtime.StatusCode)
	}
	ordinaryConfig := as.do(t, http.MethodGet, "/api/settings", "", editorCookies)
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
	state := as.do(t, http.MethodGet, "/api/auth/session", "", opCookies)
	var stateBody struct {
		Memberships []auth.MembershipView `json:"memberships"`
	}
	_ = json.NewDecoder(state.Body).Decode(&stateBody)
	state.Body.Close()
	merchantID := stateBody.Memberships[0].MerchantID
	if stateBody.Memberships[0].MerchantStatus != auth.MerchantStatusActive {
		t.Fatalf("merchant_status %#v", stateBody.Memberships[0])
	}
	editorCookies := editorMember(t, as, opCookies, merchantID)

	now := time.Now().UTC()
	productID := fmt.Sprintf("ops-product-%d", now.UnixNano())
	if err := gdb.Create(&schema.Products{
		ID: productID, MerchantID: merchantID, Name: "启停测商品", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	folderOK := as.do(t, http.MethodPost, "/api/v2/products/"+productID+"/image-folders",
		`{"name":"停用前文件夹"}`, editorCookies)
	folderOK.Body.Close()
	if folderOK.StatusCode != http.StatusCreated {
		t.Fatalf("folder before suspend %d", folderOK.StatusCode)
	}

	suspend := as.do(t, http.MethodPatch, "/api/merchants/"+merchantID+"/status",
		`{"status":"suspended"}`, opCookies)
	var suspended map[string]string
	_ = json.NewDecoder(suspend.Body).Decode(&suspended)
	suspend.Body.Close()
	if suspend.StatusCode != http.StatusOK || suspended["status"] != auth.MerchantStatusSuspended {
		t.Fatalf("suspend %d %#v", suspend.StatusCode, suspended)
	}

	denied := as.do(t, http.MethodPost, "/api/v2/products/"+productID+"/image-folders",
		`{"name":"停用后文件夹"}`, editorCookies)
	body, _ := io.ReadAll(denied.Body)
	denied.Body.Close()
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("write after suspend %d %s", denied.StatusCode, body)
	}
	if !bytes.Contains(body, []byte("商家已停用")) {
		t.Fatalf("detail %s", body)
	}

	memberAsEditor := as.do(t, http.MethodPost, "/api/merchants/"+merchantID+"/memberships/unknown/restore", "", editorCookies)
	memberAsEditor.Body.Close()
	if memberAsEditor.StatusCode != http.StatusForbidden {
		t.Fatalf("editor membership after suspend %d", memberAsEditor.StatusCode)
	}

	listOK := as.do(t, http.MethodGet, "/api/v2/products", "", editorCookies)
	listOK.Body.Close()
	if listOK.StatusCode != http.StatusOK {
		t.Fatalf("read after suspend %d", listOK.StatusCode)
	}

	reactivate := as.do(t, http.MethodPatch, "/api/merchants/"+merchantID+"/status",
		`{"status":"active"}`, opCookies)
	reactivate.Body.Close()
	if reactivate.StatusCode != http.StatusOK {
		t.Fatalf("reactivate %d", reactivate.StatusCode)
	}
	folderAgain := as.do(t, http.MethodPost, "/api/v2/products/"+productID+"/image-folders",
		`{"name":"恢复后文件夹"}`, editorCookies)
	folderAgain.Body.Close()
	if folderAgain.StatusCode != http.StatusCreated {
		t.Fatalf("folder after reactivate %d", folderAgain.StatusCode)
	}
}

func TestSupportContractDraftOpOnly(t *testing.T) {
	as := newAuthServer(t)
	opCookies := auth.MustAuthenticate(t, as.client, as.srv.URL)
	doc := as.do(t, http.MethodGet, "/api/ops/support-contract", "", opCookies)
	defer doc.Body.Close()
	if doc.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(doc.Body)
		t.Fatalf("contract %d %s", doc.StatusCode, raw)
	}
	var contract auth.SupportAccessContract
	if err := json.NewDecoder(doc.Body).Decode(&contract); err != nil {
		t.Fatal(err)
	}
	if contract.ContractVersion != auth.SupportContractVersion || contract.Implemented {
		t.Fatalf("contract %#v", contract)
	}
	if len(contract.SessionFields) == 0 || len(contract.AuditFields) == 0 {
		t.Fatalf("missing field lists %#v", contract)
	}

	create := as.do(t, http.MethodPost, "/api/ops/support-sessions", `{"merchant_id":"x","purpose":"debug"}`, opCookies)
	body, _ := io.ReadAll(create.Body)
	create.Body.Close()
	if create.StatusCode != http.StatusNotImplemented {
		t.Fatalf("create session %d %s", create.StatusCode, body)
	}
	if !bytes.Contains(body, []byte(auth.SupportNotImplemented)) {
		t.Fatalf("detail %s", body)
	}

	state := as.do(t, http.MethodGet, "/api/auth/session", "", opCookies)
	var stateBody struct {
		Memberships []auth.MembershipView `json:"memberships"`
	}
	_ = json.NewDecoder(state.Body).Decode(&stateBody)
	state.Body.Close()
	editorCookies := editorMember(t, as, opCookies, stateBody.Memberships[0].MerchantID)
	deny := as.do(t, http.MethodGet, "/api/ops/support-contract", "", editorCookies)
	deny.Body.Close()
	if deny.StatusCode != http.StatusForbidden {
		t.Fatalf("editor support-contract %d", deny.StatusCode)
	}
}

func TestMerchantRoleCannotSetStatus(t *testing.T) {
	as := newAuthServer(t)
	opCookies := auth.MustAuthenticate(t, as.client, as.srv.URL)
	state := as.do(t, http.MethodGet, "/api/auth/session", "", opCookies)
	var stateBody struct {
		Memberships []auth.MembershipView `json:"memberships"`
	}
	_ = json.NewDecoder(state.Body).Decode(&stateBody)
	state.Body.Close()
	merchantID := stateBody.Memberships[0].MerchantID
	editorCookies := editorMember(t, as, opCookies, merchantID)
	resp := as.do(t, http.MethodPatch, "/api/merchants/"+merchantID+"/status",
		`{"status":"suspended"}`, editorCookies)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("editor set status %d", resp.StatusCode)
	}
}
