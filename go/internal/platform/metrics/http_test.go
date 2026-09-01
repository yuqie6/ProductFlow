package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
			t.Fatalf("missing queued recovery backlog domain %q in metrics", domain)
		}
		if !strings.Contains(body, `productflow_recovery_stale_running{domain="`+domain+`"}`) {
			t.Fatalf("missing stale recovery backlog domain %q in metrics", domain)
		}
	}
	for _, metric := range []string{
		"productflow_graph_sse_connections",
		"productflow_notify_listener_connections",
	} {
		if !strings.Contains(body, metric+" ") {
			t.Fatalf("missing connection metric %q in %s", metric, body)
		}
	}
}

func TestRecoveryHistogramsEmitStableDomains(t *testing.T) {
	ObserveRecovery("graph", 25*time.Millisecond, false)
	ObserveRecovery("graph", 2*time.Second, true)
	ObserveRecoveryLock("graph", 5*time.Millisecond)
	var b strings.Builder
	writeRecoveryHistograms(&b)
	got := b.String()
	for _, want := range []string{
		`# TYPE productflow_recovery_duration_seconds histogram`,
		`productflow_recovery_duration_seconds_bucket{domain="graph",le="0.025"}`,
		`productflow_recovery_duration_seconds_count{domain="graph"}`,
		`productflow_recovery_lock_acquire_duration_seconds_bucket{domain="graph",le="0.01"}`,
		`productflow_recovery_errors_total{domain="graph"}`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing metric line %q in %s", want, got)
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
