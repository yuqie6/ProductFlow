package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/settings"
)

type fakeRuntime struct {
	value settings.Runtime
}

func (f fakeRuntime) Runtime(context.Context) (settings.Runtime, error) {
	return f.value, nil
}

func testEngine(required bool) *httptest.Server {
	engine := httpx.NewEngine(nil)
	store := httpx.NewCookieStore(httpx.SessionConfig{Secret: "test-session-secret-key"})
	engine.Use(httpx.Session(store))
	HTTP{
		AdminAccessKey: "correct-admin-key",
		Store: fakeRuntime{value: settings.Runtime{
			AdminAccessRequired: required,
		}},
	}.Register(engine)
	settings.HTTP{
		Store: fakeRuntime{value: settings.Runtime{
			AdminAccessRequired:         required,
			ImageGenerationMaxDimension: 3840,
			ImageToolAllowedFields:      []string{"model"},
		}},
		SettingsAccessToken: "settings-token",
	}.Register(engine)
	return httptest.NewServer(engine)
}

func TestLoginWrongKey(t *testing.T) {
	srv := testEngine(true)
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/auth/session", "application/json", strings.NewReader(`{"admin_key":"nope"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var payload map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	if payload["detail"] != "管理员密钥不正确" {
		t.Fatalf("payload %#v", payload)
	}
}

func TestLoginAndRuntime(t *testing.T) {
	srv := testEngine(true)
	defer srv.Close()
	client := &http.Client{}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/session", strings.NewReader(`{"admin_key":"correct-admin-key"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login %d", resp.StatusCode)
	}
	rt, err := http.NewRequest(http.MethodGet, srv.URL+"/api/settings/runtime", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range resp.Cookies() {
		rt.AddCookie(cookie)
	}
	got, err := client.Do(rt)
	if err != nil {
		t.Fatal(err)
	}
	defer got.Body.Close()
	if got.StatusCode != http.StatusOK {
		t.Fatalf("runtime %d", got.StatusCode)
	}
}

func TestRuntimeRequiresLogin(t *testing.T) {
	srv := testEngine(true)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/settings/runtime")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", resp.StatusCode)
	}
}
