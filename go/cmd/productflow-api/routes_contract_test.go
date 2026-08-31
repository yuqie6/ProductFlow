package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/agent"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/delivery"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/imagesession"
	"github.com/yuqie6/productflow/internal/library"
	"github.com/yuqie6/productflow/internal/localedit"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/recipe"
	"github.com/yuqie6/productflow/internal/settings"
)

var pathParamPattern = regexp.MustCompile(`\{[^}]+\}|:[^/]+`)

// Historical sealed snapshot includes FastAPI docs UI. Go keeps /healthz and adds /healthz/ready.
var sealedDocsOnly = map[string]bool{
	"GET /docs":                 true,
	"GET /docs/oauth2-redirect": true,
	"GET /openapi.json":         true,
	"GET /redoc":                true,
}

var goOpsExtras = map[string]bool{
	"GET /healthz/ready":                                                           true,
	"GET /api/v2/agent-control/events":                                             true,
	"GET /api/v2/agent-conversations/{}/turns/{}/events/page":                      true,
	"GET /api/v2/products/{}/agent-conversations/{}/turns/{}/events/page":          true,
	"GET /api/internal/v1/agent-conversations/{}/workflow-runs/{}":                 true,
	"GET /api/image-sessions/{}/events":                                            true,
	"GET /api/v3/products/{}/workflows/{}/runs/{}/events":                          true,
	"GET /api/v3/products/{}/workflows/{}/nodes/{}/candidate":                      true,
	"POST /api/internal/v1/agent-conversations/{}/turn-executions/{}/events/batch": true,
	"POST /api/v3/products/{}/workflows/{}/nodes/{}/candidate/apply":               true,
	"POST /api/v3/products/{}/workflows/{}/nodes/{}/candidate/discard":             true,
	"POST /api/v3/products/{}/workflows/{}/runs/preview":                           true,
}

// Historical snapshot still lists retired Agent library-effect and single-event routes.
var sealedRetired = map[string]bool{
	"POST /api/internal/v1/agent-conversations/{}/asset-moves":                         true,
	"POST /api/internal/v1/agent-conversations/{}/asset-moves/prepare":                 true,
	"POST /api/internal/v1/agent-conversations/{}/asset-moves/reconcile":               true,
	"POST /api/internal/v1/agent-conversations/{}/asset-renames":                       true,
	"POST /api/internal/v1/agent-conversations/{}/asset-renames/prepare":               true,
	"POST /api/internal/v1/agent-conversations/{}/asset-renames/reconcile":             true,
	"POST /api/internal/v1/agent-conversations/{}/folder-creates":                      true,
	"POST /api/internal/v1/agent-conversations/{}/folder-creates/prepare":              true,
	"POST /api/internal/v1/agent-conversations/{}/folder-creates/reconcile":            true,
	"POST /api/internal/v1/agent-conversations/{}/folder-renames":                      true,
	"POST /api/internal/v1/agent-conversations/{}/folder-renames/prepare":              true,
	"POST /api/internal/v1/agent-conversations/{}/folder-renames/reconcile":            true,
	"POST /api/internal/v1/agent-conversations/{}/library-organization-draft/validate": true,
	"POST /api/internal/v1/agent-conversations/{}/turn-executions/{}/events":           true,
}

type sealedRoute struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

type contractSettings struct{}

func (contractSettings) Runtime(context.Context) (settings.Runtime, error) {
	return settings.Runtime{AdminAccessRequired: true}, nil
}

func (contractSettings) UploadLimits(context.Context) (media.Limits, error) {
	return media.Limits{}, nil
}

func TestSealedHTTPRoutesAreRegistered(t *testing.T) {
	engine := httpx.NewEngine(nil)
	httpx.RegisterHealth(engine, nil)
	fake := contractSettings{}
	registerAPI(engine, apiHandlers{
		Auth:         auth.HTTP{Store: fake},
		Settings:     settings.HTTP{Store: fake},
		Product:      product.HTTP{Settings: fake},
		Library:      library.HTTP{Settings: fake},
		Graph:        graph.HTTP{Settings: fake},
		Recipe:       recipe.HTTP{Settings: fake},
		ImageSession: imagesession.HTTP{Settings: fake},
		Delivery:     delivery.HTTP{Settings: fake},
		LocalEdit:    localedit.HTTP{Settings: fake},
		Agent:        agent.HTTP{Settings: fake, InternalToken: "contract-internal-token"},
	})

	goKeys := map[string]bool{}
	for _, route := range engine.Routes() {
		if route.Method == http.MethodHead {
			continue
		}
		goKeys[route.Method+" "+normalizeRoutePath(route.Path)] = true
	}

	sealed, err := loadSealedRoutes(t)
	if err != nil {
		t.Fatal(err)
	}
	sealedKeys := map[string]bool{}
	for _, route := range sealed {
		key := route.Method + " " + normalizeRoutePath(route.Path)
		sealedKeys[key] = true
	}

	var missing []string
	for key := range sealedKeys {
		if sealedDocsOnly[key] || sealedRetired[key] {
			continue
		}
		if !goKeys[key] {
			missing = append(missing, key)
		}
	}
	var extra []string
	for key := range goKeys {
		if goOpsExtras[key] {
			continue
		}
		if !sealedKeys[key] {
			extra = append(extra, key)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("HTTP route contract drift\nmissing in Go (%d):\n  %s\nextra in Go (%d):\n  %s",
			len(missing), strings.Join(missing, "\n  "),
			len(extra), strings.Join(extra, "\n  "))
	}
}

func TestSealedAdminAndInternalRoutesReturnContractUnauthorized(t *testing.T) {
	engine := httpx.NewEngine(nil)
	store := httpx.NewCookieStore(httpx.SessionConfig{Secret: "contract-test-session-secret"})
	engine.Use(httpx.Session(store))
	httpx.RegisterHealth(engine, nil)
	fake := contractSettings{}
	registerAPI(engine, apiHandlers{
		Auth:         auth.HTTP{AdminAccessKey: "contract-admin-key", Store: fake},
		Settings:     settings.HTTP{Store: fake, SettingsAccessToken: "contract-settings-token"},
		Product:      product.HTTP{Settings: fake},
		Library:      library.HTTP{Settings: fake},
		Graph:        graph.HTTP{Settings: fake},
		Recipe:       recipe.HTTP{Settings: fake},
		ImageSession: imagesession.HTTP{Settings: fake},
		Delivery:     delivery.HTTP{Settings: fake},
		LocalEdit:    localedit.HTTP{Settings: fake},
		Agent:        agent.HTTP{Settings: fake, InternalToken: "contract-internal-token"},
	})

	var failed []string
	for _, route := range engine.Routes() {
		if route.Method == http.MethodHead {
			continue
		}
		if skipUnauthorizedProbe(route.Method, route.Path) {
			continue
		}
		req := httptest.NewRequest(route.Method, requestPathFor(route.Path), nil)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			failed = append(failed, route.Method+" "+route.Path+" -> "+http.StatusText(rec.Code)+" "+rec.Body.String())
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			failed = append(failed, route.Method+" "+route.Path+" invalid json: "+rec.Body.String())
			continue
		}
		detail, _ := payload["detail"].(string)
		want := "请先登录"
		if strings.HasPrefix(route.Path, "/api/internal/") {
			want = "Agent 内部服务认证失败"
		}
		if detail != want {
			failed = append(failed, route.Method+" "+route.Path+" detail="+detail)
		}
	}
	if len(failed) > 0 {
		t.Fatalf("unauthorized contract mismatches (%d):\n  %s", len(failed), strings.Join(failed, "\n  "))
	}
}

func skipUnauthorizedProbe(method, path string) bool {
	_ = method
	if path == "/healthz" || path == "/healthz/ready" {
		return true
	}
	if path == "/api/auth/session" {
		return true
	}
	return false
}

func requestPathFor(ginPath string) string {
	re := regexp.MustCompile(`:[^/]+`)
	path := re.ReplaceAllString(ginPath, "x")
	if path == "" {
		return "/"
	}
	return path
}

func normalizeRoutePath(path string) string {
	normalized := pathParamPattern.ReplaceAllString(path, "{}")
	if len(normalized) > 1 {
		normalized = strings.TrimRight(normalized, "/")
	}
	return normalized
}

func loadSealedRoutes(t *testing.T) ([]sealedRoute, error) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "contracts", "http-routes.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var routes []sealedRoute
	if err := json.Unmarshal(raw, &routes); err != nil {
		return nil, err
	}
	return routes, nil
}
