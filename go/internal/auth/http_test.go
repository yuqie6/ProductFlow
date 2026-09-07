package auth_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
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
	auth.MountTest(engine, gdb, store, auth.TestAdminKey)
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

func TestInviteAcceptAndRole(t *testing.T) {
	as := newAuthServer(t)
	ownerCookies := auth.MustAuthenticate(t, as.client, as.srv.URL)
	state := as.do(t, http.MethodGet, "/api/auth/session", "", ownerCookies)
	var stateBody struct {
		Memberships []auth.MembershipView `json:"memberships"`
	}
	_ = json.NewDecoder(state.Body).Decode(&stateBody)
	state.Body.Close()
	if len(stateBody.Memberships) != 1 || stateBody.Memberships[0].Role != auth.RoleOwner {
		t.Fatalf("memberships %#v", stateBody.Memberships)
	}
	merchantID := stateBody.Memberships[0].MerchantID
	editorEmail := fmt.Sprintf("editor-%d@test.local", time.Now().UnixNano())
	inviteResp := as.do(t, http.MethodPost, "/api/merchants/"+merchantID+"/invites",
		`{"email":"`+editorEmail+`","role":"editor"}`, ownerCookies)
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
	editorCookies := accept.Cookies()
	accept.Body.Close()
	editorState := as.do(t, http.MethodGet, "/api/auth/session", "", editorCookies)
	var editorBody struct {
		Memberships []auth.MembershipView `json:"memberships"`
		User        struct {
			IsOperator bool `json:"is_operator"`
		} `json:"user"`
	}
	_ = json.NewDecoder(editorState.Body).Decode(&editorBody)
	editorState.Body.Close()
	if editorBody.User.IsOperator || len(editorBody.Memberships) != 1 || editorBody.Memberships[0].Role != auth.RoleEditor {
		t.Fatalf("editor %#v", editorBody)
	}
	deny := as.do(t, http.MethodPost, "/api/merchants/"+merchantID+"/invites",
		fmt.Sprintf(`{"email":"x-%d@test.local","role":"viewer"}`, time.Now().UnixNano()), editorCookies)
	deny.Body.Close()
	if deny.StatusCode != http.StatusForbidden {
		t.Fatalf("editor invite status %d", deny.StatusCode)
	}
}

func TestNonMemberForbidden(t *testing.T) {
	as := newAuthServer(t)
	ownerCookies := auth.MustAuthenticate(t, as.client, as.srv.URL)
	state := as.do(t, http.MethodGet, "/api/auth/session", "", ownerCookies)
	var stateBody struct {
		Memberships []auth.MembershipView `json:"memberships"`
	}
	_ = json.NewDecoder(state.Body).Decode(&stateBody)
	state.Body.Close()
	merchantID := stateBody.Memberships[0].MerchantID

	outsiderEmail := fmt.Sprintf("outsider-%d@test.local", time.Now().UnixNano())
	inviteResp := as.do(t, http.MethodPost, "/api/merchants/"+merchantID+"/invites",
		`{"email":"`+outsiderEmail+`","role":"viewer"}`, ownerCookies)
	var invite auth.InviteResult
	_ = json.NewDecoder(inviteResp.Body).Decode(&invite)
	inviteResp.Body.Close()
	accept := as.do(t, http.MethodPost, "/api/auth/invites/accept",
		`{"token":"`+invite.Token+`","password":"viewer-password-ok"}`, nil)
	outsiderCookies := accept.Cookies()
	var acceptBody struct {
		UserID string `json:"user_id"`
	}
	_ = json.NewDecoder(accept.Body).Decode(&acceptBody)
	accept.Body.Close()

	rev := as.do(t, http.MethodDelete, "/api/merchants/"+merchantID+"/memberships/"+acceptBody.UserID, "", ownerCookies)
	rev.Body.Close()
	if rev.StatusCode != http.StatusOK {
		t.Fatalf("revoke %d", rev.StatusCode)
	}
	again := as.do(t, http.MethodPost, "/api/merchants/"+merchantID+"/invites",
		fmt.Sprintf(`{"email":"y-%d@test.local","role":"viewer"}`, time.Now().UnixNano()), outsiderCookies)
	defer again.Body.Close()
	if again.StatusCode != http.StatusForbidden {
		t.Fatalf("revoked member status %d", again.StatusCode)
	}
}

func TestLastOwnerProtectedConcurrent(t *testing.T) {
	as := newAuthServer(t)
	ownerCookies := auth.MustAuthenticate(t, as.client, as.srv.URL)
	state := as.do(t, http.MethodGet, "/api/auth/session", "", ownerCookies)
	var stateBody struct {
		Memberships []auth.MembershipView `json:"memberships"`
		User        struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	_ = json.NewDecoder(state.Body).Decode(&stateBody)
	state.Body.Close()
	merchantID := stateBody.Memberships[0].MerchantID
	ownerID := stateBody.User.ID

	var conflict atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp := as.do(t, http.MethodDelete, "/api/merchants/"+merchantID+"/memberships/"+ownerID, "", ownerCookies)
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusConflict {
				conflict.Add(1)
			}
		}()
	}
	wg.Wait()
	if conflict.Load() == 0 {
		t.Fatal("expected last-owner conflicts")
	}
	var owners int64
	if err := as.db.Model(&schema.Memberships{}).
		Where("merchant_id = ? AND role = ? AND status = ?", merchantID, auth.RoleOwner, auth.MembershipStatusActive).
		Count(&owners).Error; err != nil {
		t.Fatal(err)
	}
	if owners < 1 {
		t.Fatalf("owner count %d", owners)
	}
}

func TestSecondMerchantRejected(t *testing.T) {
	as := newAuthServer(t)
	cookies := auth.MustAuthenticate(t, as.client, as.srv.URL)
	resp := as.do(t, http.MethodPost, "/api/merchants", `{"name":"第二商家"}`, cookies)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
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
