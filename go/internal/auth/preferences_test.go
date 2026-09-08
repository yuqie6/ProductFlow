package auth_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/settings"
	"gorm.io/gorm"
)

func newPreferencesAuthServer(t *testing.T) *authServer {
	t.Helper()
	pool, gdb := openOpsReadsAuthDB(t, fmt.Sprintf("pf_prefs_0908_auth_%d", time.Now().UnixNano()))
	engine := httpx.NewEngine(nil)
	engine.Use(httpx.Session(httpx.NewCookieStore(httpx.SessionConfig{Secret: "preferences-test-session-secret"})))
	store := settings.NewStore(pool, config.Config{AdminAccessRequired: true})
	auth.MountTest(engine, gdb, store, auth.TestAdminKey)
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	return &authServer{srv: srv, client: &http.Client{}, db: gdb}
}

func TestAccountPreferencesPersistAcrossSessionsAndIgnoreMerchantStatus(t *testing.T) {
	as := newPreferencesAuthServer(t)
	anonymous := as.do(t, http.MethodGet, "/api/auth/session", "", nil)
	if anonymous.StatusCode != http.StatusOK {
		anonymous.Body.Close()
		t.Fatalf("anonymous session status %d", anonymous.StatusCode)
	}
	var anonymousState map[string]json.RawMessage
	if err := json.NewDecoder(anonymous.Body).Decode(&anonymousState); err != nil {
		anonymous.Body.Close()
		t.Fatal(err)
	}
	anonymous.Body.Close()
	if _, ok := anonymousState["preferences"]; ok {
		t.Fatalf("anonymous session exposed preferences: %#v", anonymousState)
	}
	merchant := seedPreferencesMerchant(t, as.db, "preferences-merchant", auth.MerchantStatusActive)
	user := seedDirectUser(t, as.db, merchant.ID, "preferences-user@test.local", "preferences-password", "偏好用户", false)
	cookies := loginDirectUser(t, as, user.Email, "preferences-password")

	viewResp := as.do(t, http.MethodGet, "/api/account", "", cookies)
	if viewResp.StatusCode != http.StatusOK {
		viewResp.Body.Close()
		t.Fatalf("account status %d", viewResp.StatusCode)
	}
	view := accountResponse(t, viewResp)
	if view.Preferences.Locale != "zh-CN" || view.Preferences.Theme != "system" {
		t.Fatalf("default account preferences %#v", view.Preferences)
	}

	stateResp := as.do(t, http.MethodGet, "/api/auth/session", "", cookies)
	if stateResp.StatusCode != http.StatusOK {
		stateResp.Body.Close()
		t.Fatalf("session state status %d", stateResp.StatusCode)
	}
	var state struct {
		Authenticated bool                    `json:"authenticated"`
		Preferences   auth.AccountPreferences `json:"preferences"`
	}
	if err := json.NewDecoder(stateResp.Body).Decode(&state); err != nil {
		stateResp.Body.Close()
		t.Fatal(err)
	}
	stateResp.Body.Close()
	if !state.Authenticated || state.Preferences.Locale != "zh-CN" || state.Preferences.Theme != "system" {
		t.Fatalf("default session preferences %#v", state)
	}

	for _, body := range []string{
		`{}`,
		`null`,
		`{"locale":null}`,
		`{"locale":"fr-FR"}`,
		`{"theme":"sepia"}`,
		`{"locale":"en-US","unexpected":true}`,
	} {
		resp := as.do(t, http.MethodPatch, "/api/account/preferences", body, cookies)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid preferences body %s status %d", body, resp.StatusCode)
		}
	}

	updated := as.do(t, http.MethodPatch, "/api/account/preferences", `{"locale":"en-US"}`, cookies)
	if updated.StatusCode != http.StatusOK {
		updated.Body.Close()
		t.Fatalf("locale update status %d", updated.StatusCode)
	}
	var preferences auth.AccountPreferences
	if err := json.NewDecoder(updated.Body).Decode(&preferences); err != nil {
		updated.Body.Close()
		t.Fatal(err)
	}
	updated.Body.Close()
	if preferences.Locale != "en-US" || preferences.Theme != "system" {
		t.Fatalf("partial locale update %#v", preferences)
	}

	updated = as.do(t, http.MethodPatch, "/api/account/preferences", `{"theme":"dark"}`, cookies)
	if updated.StatusCode != http.StatusOK {
		updated.Body.Close()
		t.Fatalf("theme update status %d", updated.StatusCode)
	}
	if err := json.NewDecoder(updated.Body).Decode(&preferences); err != nil {
		updated.Body.Close()
		t.Fatal(err)
	}
	updated.Body.Close()
	if preferences.Locale != "en-US" || preferences.Theme != "dark" {
		t.Fatalf("partial theme update %#v", preferences)
	}

	var stored struct {
		Locale string
		Theme  string
	}
	if err := as.db.Table("users").Select("locale", "theme").Where("id = ?", user.ID).Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Locale != "en-US" || stored.Theme != "dark" {
		t.Fatalf("stored preferences %#v", stored)
	}

	// Preference writes remain available while the directly owned merchant is suspended.
	if err := as.db.Table("merchants").Where("id = ?", merchant.ID).Update("status", auth.MerchantStatusSuspended).Error; err != nil {
		t.Fatal(err)
	}
	resp := as.do(t, http.MethodPatch, "/api/account/preferences", `{"locale":"ja-JP"}`, cookies)
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("suspended preference status %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = as.do(t, http.MethodPatch, "/api/account/merchant", `{"name":"blocked"}`, cookies)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("suspended merchant update status %d", resp.StatusCode)
	}

	secondCookies := loginDirectUser(t, as, user.Email, "preferences-password")
	stateResp = as.do(t, http.MethodGet, "/api/auth/session", "", secondCookies)
	if stateResp.StatusCode != http.StatusOK {
		stateResp.Body.Close()
		t.Fatalf("fresh session state status %d", stateResp.StatusCode)
	}
	if err := json.NewDecoder(stateResp.Body).Decode(&state); err != nil {
		stateResp.Body.Close()
		t.Fatal(err)
	}
	stateResp.Body.Close()
	if state.Preferences.Locale != "ja-JP" || state.Preferences.Theme != "dark" {
		t.Fatalf("fresh session preferences %#v", state.Preferences)
	}
}

func TestAccountMerchantNameOwnsOnlyDirectMerchant(t *testing.T) {
	as := newPreferencesAuthServer(t)
	merchant := seedPreferencesMerchant(t, as.db, "merchant-own", auth.MerchantStatusActive)
	user := seedDirectUser(t, as.db, merchant.ID, "merchant-own-user@test.local", "merchant-own-password", "商家用户", false)
	cookies := loginDirectUser(t, as, user.Email, "merchant-own-password")

	updated := as.do(t, http.MethodPatch, "/api/account/merchant", `{"name":"  新商家名  "}`, cookies)
	if updated.StatusCode != http.StatusOK {
		updated.Body.Close()
		t.Fatalf("merchant update status %d", updated.StatusCode)
	}
	var view auth.MerchantView
	if err := json.NewDecoder(updated.Body).Decode(&view); err != nil {
		updated.Body.Close()
		t.Fatal(err)
	}
	updated.Body.Close()
	if view.ID != merchant.ID || view.Name != "新商家名" || view.Status != auth.MerchantStatusActive {
		t.Fatalf("merchant update view %#v", view)
	}
	var stored string
	if err := as.db.Table("merchants").Select("name").Where("id = ?", merchant.ID).Scan(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored != "新商家名" {
		t.Fatalf("stored merchant name %q", stored)
	}

	for _, body := range []string{
		`{}`,
		`null`,
		`{"name":""}`,
		`{"name":"   "}`,
		`{"name":"` + strings.Repeat("界", 161) + `"}`,
		`{"name":"valid","merchant_id":"other"}`,
	} {
		resp := as.do(t, http.MethodPatch, "/api/account/merchant", body, cookies)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid merchant body status body=%s status=%d", body, resp.StatusCode)
		}
	}

	operator := seedOpsReadOperatorWithoutMerchant(t, as.db, "preferences-operator@test.local")
	operatorCookies := loginDirectUser(t, as, operator.Email, "ops-read-operator-password")
	operatorAccount := as.do(t, http.MethodGet, "/api/account", "", operatorCookies)
	if operatorAccount.StatusCode != http.StatusOK {
		operatorAccount.Body.Close()
		t.Fatalf("operator account status %d", operatorAccount.StatusCode)
	}
	operatorView := accountResponse(t, operatorAccount)
	if operatorView.Merchant != nil || operatorView.Preferences.Locale != "zh-CN" || operatorView.Preferences.Theme != "system" {
		t.Fatalf("operator account view %#v", operatorView)
	}
	noMerchant := as.do(t, http.MethodPatch, "/api/account/merchant", `{"name":"should fail"}`, operatorCookies)
	noMerchant.Body.Close()
	if noMerchant.StatusCode != http.StatusForbidden {
		t.Fatalf("operator without merchant update status %d", noMerchant.StatusCode)
	}
}

func seedPreferencesMerchant(t *testing.T, db *gorm.DB, id, status string) schema.Merchants {
	t.Helper()
	merchant := schema.Merchants{
		ID: id, Name: "偏好测试商家", Status: status, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Where("id = ?", merchant.ID).Delete(&schema.Merchants{}).Error })
	return merchant
}

func TestSuspendedMerchantKeepsPersonalAccountSecurityAvailable(t *testing.T) {
	as := newPreferencesAuthServer(t)
	merchant := seedPreferencesMerchant(t, as.db, "suspended-personal-merchant", auth.MerchantStatusSuspended)
	user := seedDirectUser(t, as.db, merchant.ID, "suspended-personal@test.local", "current-password-123", "个人用户", false)
	current := loginDirectUser(t, as, user.Email, "current-password-123")
	other := loginDirectUser(t, as, user.Email, "current-password-123")
	response := as.do(t, http.MethodPatch, "/api/account", `{"display_name":"仍可管理本人资料"}`, current)
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		t.Fatalf("personal profile status %d", response.StatusCode)
	}
	response.Body.Close()
	response = as.do(t, http.MethodPatch, "/api/account/merchant", `{"name":"禁止修改停用商家"}`, current)
	if response.StatusCode != http.StatusForbidden {
		response.Body.Close()
		t.Fatalf("merchant edit status %d", response.StatusCode)
	}
	response.Body.Close()
	var otherSession schema.AuthSessions
	if err := as.db.Where("user_id = ?", user.ID).Order("created_at DESC, id DESC").First(&otherSession).Error; err != nil {
		t.Fatal(err)
	}
	response = as.do(t, http.MethodDelete, "/api/account/sessions/"+otherSession.ID, "", current)
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		t.Fatalf("session revoke status %d", response.StatusCode)
	}
	response.Body.Close()
	response = as.do(t, http.MethodGet, "/api/account", "", other)
	if response.StatusCode != http.StatusUnauthorized {
		response.Body.Close()
		t.Fatalf("revoked session status %d", response.StatusCode)
	}
	response.Body.Close()
	response = as.do(t, http.MethodPost, "/api/account/password", `{"current_password":"current-password-123","new_password":"replacement-password-456"}`, current)
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		t.Fatalf("password change status %d", response.StatusCode)
	}
	response.Body.Close()
	response = as.do(t, http.MethodGet, "/api/account", "", current)
	if response.StatusCode != http.StatusUnauthorized {
		response.Body.Close()
		t.Fatalf("old current session status %d", response.StatusCode)
	}
	response.Body.Close()
	loginDirectUser(t, as, user.Email, "replacement-password-456")
}
