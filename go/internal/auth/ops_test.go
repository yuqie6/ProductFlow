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
	settings.HTTP{
		Store: store, DB: store, SettingsAccessToken: "settings-token",
		OperatorOnly: auth.RequireOperatorIf(func(c *gin.Context) (bool, error) {
			runtime, err := store.Runtime(c.Request.Context())
			if err != nil {
				return false, err
			}
			return runtime.AdminAccessRequired, nil
		}),
	}.Register(engine)
}

func inviteEditor(t *testing.T, as *authServer, ownerCookies []*http.Cookie, merchantID string) []*http.Cookie {
	t.Helper()
	email := fmt.Sprintf("editor-ops-%d@test.local", time.Now().UnixNano())
	inviteResp := as.do(t, http.MethodPost, "/api/merchants/"+merchantID+"/invites",
		`{"email":"`+email+`","role":"editor"}`, ownerCookies)
	var invite auth.InviteResult
	_ = json.NewDecoder(inviteResp.Body).Decode(&invite)
	inviteResp.Body.Close()
	if inviteResp.StatusCode != http.StatusOK || invite.Token == "" {
		t.Fatalf("invite %d %#v", inviteResp.StatusCode, invite)
	}
	accept := as.do(t, http.MethodPost, "/api/auth/invites/accept",
		`{"token":"`+invite.Token+`","password":"editor-password-ok","display_name":"编辑"}`, nil)
	if accept.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(accept.Body)
		accept.Body.Close()
		t.Fatalf("accept %d %s", accept.StatusCode, body)
	}
	cookies := accept.Cookies()
	accept.Body.Close()
	return cookies
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
	editorCookies := inviteEditor(t, as, ownerCookies, merchantID)

	for _, path := range []string{
		"/api/settings/runtime",
		"/api/settings/lock-state",
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
	unlock := as.do(t, http.MethodPost, "/api/settings/unlock", `{"token":"settings-token"}`, editorCookies)
	unlock.Body.Close()
	if unlock.StatusCode != http.StatusForbidden {
		t.Fatalf("editor unlock %d", unlock.StatusCode)
	}
}

func TestOperatorSettingsExportHasNoMerchantSecrets(t *testing.T) {
	as := newAuthServer(t)
	opCookies := auth.MustAuthenticate(t, as.client, as.srv.URL)
	unlock := as.do(t, http.MethodPost, "/api/settings/unlock", `{"token":"settings-token"}`, opCookies)
	if unlock.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(unlock.Body)
		unlock.Body.Close()
		t.Fatalf("unlock %d %s", unlock.StatusCode, raw)
	}
	opCookies = mergeCookies(opCookies, unlock.Cookies())
	unlock.Body.Close()
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

func mergeCookies(existing, next []*http.Cookie) []*http.Cookie {
	out := append([]*http.Cookie{}, existing...)
	for _, cookie := range next {
		replaced := false
		for i, cur := range out {
			if cur.Name == cookie.Name {
				out[i] = cookie
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, cookie)
		}
	}
	return out
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
	editorCookies := inviteEditor(t, as, opCookies, merchantID)

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

	inviteAsEditor := as.do(t, http.MethodPost, "/api/merchants/"+merchantID+"/invites",
		fmt.Sprintf(`{"email":"after-suspend-ed-%d@test.local","role":"viewer"}`, time.Now().UnixNano()),
		editorCookies)
	inviteAsEditor.Body.Close()
	if inviteAsEditor.StatusCode != http.StatusForbidden {
		t.Fatalf("editor invite after suspend %d", inviteAsEditor.StatusCode)
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
	editorCookies := inviteEditor(t, as, opCookies, stateBody.Memberships[0].MerchantID)
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
	editorCookies := inviteEditor(t, as, opCookies, merchantID)
	resp := as.do(t, http.MethodPatch, "/api/merchants/"+merchantID+"/status",
		`{"status":"suspended"}`, editorCookies)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("editor set status %d", resp.StatusCode)
	}
}
