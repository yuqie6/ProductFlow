package settings_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/settings"
)

type settingsServer struct {
	store   *settings.Store
	srv     *httptest.Server
	client  *http.Client
	cookies []*http.Cookie
}

func newSettingsServer(t *testing.T) *settingsServer {
	t.Helper()
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
	ss := &settingsServer{store: store, srv: srv, client: &http.Client{}}
	ss.cookies = auth.MustAuthenticate(t, ss.client, srv.URL)
	_, _ = pool.Exec(context.Background(), `
		INSERT INTO app_settings (key, value, created_at, updated_at)
		VALUES ('admin_access_required', 'true', NOW(), NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`)
	return ss
}

func (ss *settingsServer) do(t *testing.T, method, path string, body io.Reader) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, ss.srv.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for _, cookie := range ss.cookies {
		req.AddCookie(cookie)
	}
	resp, err := ss.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if set := resp.Cookies(); len(set) > 0 {
		for _, cookie := range set {
			replaced := false
			for i, existing := range ss.cookies {
				if existing.Name == cookie.Name {
					ss.cookies[i] = cookie
					replaced = true
					break
				}
			}
			if !replaced {
				ss.cookies = append(ss.cookies, cookie)
			}
		}
	}
	return resp
}

func TestSettingsUnlockAndConfig(t *testing.T) {
	ss := newSettingsServer(t)
	locked := ss.do(t, http.MethodGet, "/api/settings", nil)
	locked.Body.Close()
	if locked.StatusCode != http.StatusForbidden {
		t.Fatalf("locked %d", locked.StatusCode)
	}
	bad := ss.do(t, http.MethodPost, "/api/settings/unlock", strings.NewReader(`{"token":"nope"}`))
	bad.Body.Close()
	if bad.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad token %d", bad.StatusCode)
	}
	ok := ss.do(t, http.MethodPost, "/api/settings/unlock", strings.NewReader(`{"token":"settings-token"}`))
	ok.Body.Close()
	if ok.StatusCode != http.StatusOK {
		t.Fatalf("unlock %d", ok.StatusCode)
	}
	cfg := ss.do(t, http.MethodGet, "/api/settings", nil)
	defer cfg.Body.Close()
	if cfg.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(cfg.Body)
		t.Fatalf("config %d %s", cfg.StatusCode, raw)
	}
	var view settings.ConfigResponse
	if err := json.NewDecoder(cfg.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	if len(view.Items) == 0 {
		t.Fatal("empty config")
	}
	queue := ss.do(t, http.MethodGet, "/api/generation-queue", nil)
	defer queue.Body.Close()
	if queue.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(queue.Body)
		t.Fatalf("queue %d %s", queue.StatusCode, raw)
	}
}

func TestProviderProfileAndBinding(t *testing.T) {
	ss := newSettingsServer(t)
	unlock := ss.do(t, http.MethodPost, "/api/settings/unlock", strings.NewReader(`{"token":"settings-token"}`))
	unlock.Body.Close()
	name := "go-test-openai-" + t.Name()
	body, _ := json.Marshal(map[string]any{
		"name": name, "provider_type": "openai_compatible", "api_key": "sk-test",
		"capabilities": []string{"text_responses", "image_images"}, "enabled": true,
	})
	created := ss.do(t, http.MethodPost, "/api/settings/provider-profiles", bytes.NewReader(body))
	defer created.Body.Close()
	if created.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(created.Body)
		t.Fatalf("create profile %d %s", created.StatusCode, raw)
	}
	var profile settings.ProviderProfile
	if err := json.NewDecoder(created.Body).Decode(&profile); err != nil {
		t.Fatal(err)
	}
	if !profile.HasAPIKey {
		t.Fatal("has_api_key")
	}
	prev, err := ss.store.ProviderConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var previous *settings.ProviderBindingView
	for i := range prev.Bindings {
		if prev.Bindings[i].Purpose == "prompt" {
			item := prev.Bindings[i]
			previous = &item
			break
		}
	}
	t.Cleanup(func() {
		if previous != nil {
			restore, _ := json.Marshal(map[string]any{
				"provider_kind": previous.ProviderKind, "provider_profile_id": previous.ProviderProfileID,
				"model_settings": previous.ModelSettings, "config": previous.Config,
			})
			resp := ss.do(t, http.MethodPatch, "/api/settings/provider-bindings/prompt", bytes.NewReader(restore))
			resp.Body.Close()
		}
		del := ss.do(t, http.MethodDelete, "/api/settings/provider-profiles/"+profile.ID, nil)
		del.Body.Close()
	})
	bindBody, _ := json.Marshal(map[string]any{
		"provider_kind": "openai", "provider_profile_id": profile.ID,
		"model_settings": map[string]any{"model": "gpt-4.1"},
		"config":         map[string]any{},
	})
	bound := ss.do(t, http.MethodPatch, "/api/settings/provider-bindings/prompt", bytes.NewReader(bindBody))
	defer bound.Body.Close()
	if bound.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(bound.Body)
		t.Fatalf("bind %d %s", bound.StatusCode, raw)
	}
	resolved, err := ss.store.ResolvePrompt(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Kind != "openai" || resolved.Model != "gpt-4.1" || resolved.APIKey != "sk-test" {
		t.Fatalf("resolved %+v", resolved)
	}
}

func TestSettingsUnknownJSON(t *testing.T) {
	ss := newSettingsServer(t)
	unlock := ss.do(t, http.MethodPost, "/api/settings/unlock", strings.NewReader(`{"token":"settings-token"}`))
	unlock.Body.Close()
	resp := ss.do(t, http.MethodPatch, "/api/settings", strings.NewReader(`{"values":{},"nope":1}`))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestUpdateProfilePersistsNameAndEnabled(t *testing.T) {
	ss := newSettingsServer(t)
	unlock := ss.do(t, http.MethodPost, "/api/settings/unlock", strings.NewReader(`{"token":"settings-token"}`))
	unlock.Body.Close()
	name := "go-test-update-" + t.Name()
	body, _ := json.Marshal(map[string]any{
		"name": name, "provider_type": "openai_compatible", "api_key": "sk-test",
		"capabilities": []string{"text_responses"}, "enabled": true,
	})
	created := ss.do(t, http.MethodPost, "/api/settings/provider-profiles", bytes.NewReader(body))
	defer created.Body.Close()
	if created.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(created.Body)
		t.Fatalf("create profile %d %s", created.StatusCode, raw)
	}
	var profile settings.ProviderProfile
	if err := json.NewDecoder(created.Body).Decode(&profile); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		del := ss.do(t, http.MethodDelete, "/api/settings/provider-profiles/"+profile.ID, nil)
		del.Body.Close()
	})
	updatedName := name + "-renamed"
	nameJSON, err := json.Marshal(updatedName)
	if err != nil {
		t.Fatal(err)
	}
	patch, err := ss.store.UpdateProfile(context.Background(), profile.ID, map[string]json.RawMessage{
		"name":    nameJSON,
		"enabled": json.RawMessage(`false`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if patch.Name != updatedName {
		t.Fatalf("response name %q want %q", patch.Name, updatedName)
	}
	if patch.Enabled {
		t.Fatal("response enabled still true")
	}
	cfg, err := ss.store.ProviderConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var persisted *settings.ProviderProfile
	for i := range cfg.Profiles {
		if cfg.Profiles[i].ID == profile.ID {
			item := cfg.Profiles[i]
			persisted = &item
			break
		}
	}
	if persisted == nil {
		t.Fatal("updated profile missing from provider config")
	}
	if persisted.Name != updatedName {
		t.Fatalf("persisted name %q want %q", persisted.Name, updatedName)
	}
	if persisted.Enabled {
		t.Fatal("persisted enabled still true")
	}
}
