package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/testdb"
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

func TestSnapshotIncludesRecoveryBacklog(t *testing.T) {
	_, gdb := testdb.Open(t)
	body, err := snapshot(gdb)
	if err != nil {
		t.Fatal(err)
	}
	for _, domain := range recoveryBacklogDomains {
		if !strings.Contains(body, `productflow_recovery_queued_backlog{domain="`+domain+`"}`) {
			t.Fatalf("missing recovery backlog domain %q in metrics", domain)
		}
	}
}

func TestWriteRecoveryBacklogEmitsStableDomains(t *testing.T) {
	var b strings.Builder
	writeRecoveryBacklog(&b, []recoveryBacklogCount{{Domain: "graph", Count: 3}})
	got := b.String()
	for _, want := range []string{
		`productflow_recovery_queued_backlog{domain="agent"} 0`,
		`productflow_recovery_queued_backlog{domain="graph"} 3`,
		`productflow_recovery_queued_backlog{domain="local_image_edit"} 0`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing metric line %q in %s", want, got)
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
