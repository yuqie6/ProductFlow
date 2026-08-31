package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSafeLabelRejectsUnboundedValues(t *testing.T) {
	for _, value := range []string{"running", "awaiting_confirmation", "not_applied"} {
		if !safeLabel(value) {
			t.Fatalf("expected safe label %q", value)
		}
	}
	for _, value := range []string{"", "user-123", `failed\"} 1`, "含用户文本"} {
		if safeLabel(value) {
			t.Fatalf("expected unsafe label %q", value)
		}
	}
}

func TestMetricsRequiresBearerScheme(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	Register(engine, nil, "secret")

	for _, authorization := range []string{"", "secret", "Basic secret", "Bearer wrong"} {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		req.Header.Set("Authorization", authorization)
		res := httptest.NewRecorder()
		engine.ServeHTTP(res, req)
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("authorization %q returned %d", authorization, res.Code)
		}
	}
}
