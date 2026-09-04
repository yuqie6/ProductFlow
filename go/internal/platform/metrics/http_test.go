package metrics

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/generation"
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
		"productflow_generation_max_concurrent_tasks",
		"productflow_generation_admission_running",
		"productflow_advisory_lock_wait_seconds_count",
		"productflow_consume_duration_seconds_count",
	} {
		if !strings.Contains(body, metric+" ") {
			t.Fatalf("missing metric %q in %s", metric, body)
		}
	}
	for _, domain := range recoveryBacklogDomains {
		if !strings.Contains(body, `productflow_running{domain="`+domain+`"}`) {
			t.Fatalf("missing running gauge domain %q in metrics", domain)
		}
	}
	for _, result := range consumeResultNames {
		if !strings.Contains(body, `productflow_consume_results_total{result="`+result+`"}`) {
			t.Fatalf("missing consume result %q in metrics", result)
		}
	}
	for _, domain := range generationAdmissionDomains {
		if !strings.Contains(body, `productflow_generation_admission_denied_total{domain="`+domain+`"}`) {
			t.Fatalf("missing generation admission denied domain %q in metrics", domain)
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

func TestWorkerProcessSeriesHaveStableNames(t *testing.T) {
	ObserveAdvisoryLockWait(8 * time.Millisecond)
	ObserveConsumeResult("consumed")
	ObserveConsumeResult("busy")
	ObserveConsumeDuration(12 * time.Millisecond)
	var b strings.Builder
	writeWorkerProcessSeries(&b)
	got := b.String()
	for _, want := range []string{
		`# TYPE productflow_advisory_lock_wait_seconds histogram`,
		`productflow_advisory_lock_wait_seconds_bucket{le="0.01"}`,
		`productflow_advisory_lock_wait_seconds_count`,
		`productflow_generation_admission_denied_total{domain="graph"}`,
		`productflow_generation_admission_denied_total{domain="imagesession"}`,
		`productflow_consume_results_total{result="consumed"}`,
		`productflow_consume_results_total{result="busy"}`,
		`productflow_consume_results_total{result="later"}`,
		`productflow_consume_results_total{result="failed"}`,
		`productflow_consume_results_total{result="unknown"}`,
		`# TYPE productflow_consume_duration_seconds histogram`,
		`productflow_consume_duration_seconds_bucket{le="0.025"}`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing metric line %q in %s", want, got)
		}
	}
	if strings.Contains(got, "dispatch_id") || strings.Contains(got, "actor_name") {
		t.Fatal("worker process series must not use high-cardinality labels")
	}
}

func TestSnapshotGenerationLimitUsesSharedParser(t *testing.T) {
	_, gdb := testdb.Open(t)
	now := time.Now().UTC()
	if err := gdb.Where("key = ?", generation.MaxConcurrentSettingKey).Delete(&schema.AppSettings{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&schema.AppSettings{
		Key: generation.MaxConcurrentSettingKey, Value: "99", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = gdb.Where("key = ?", generation.MaxConcurrentSettingKey).Delete(&schema.AppSettings{}).Error
	})
	body, err := snapshot(gdb)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "productflow_generation_max_concurrent_tasks 20\n") {
		t.Fatalf("expected clamped generation limit 20 in %s", body)
	}
}

func TestSnapshotGenerationAdmissionRunningUsesSharedCount(t *testing.T) {
	_, gdb := testdb.Open(t)
	ctx := context.Background()
	before, err := generation.CountAdmissionRunning(ctx, gdb)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	productID, graphID, runID := clockid.New(), clockid.New(), clockid.New()
	if err := gdb.Exec(`INSERT INTO products (id, name, created_at, updated_at) VALUES (?, 'admission-metric', ?, ?)`, productID, now, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec(`INSERT INTO workflow_graphs (id, product_id, title, active, schema_version, revision, created_at, updated_at) VALUES (?, ?, 'admission-metric', TRUE, 3, 1, ?, ?)`, graphID, productID, now, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec(`INSERT INTO workflow_graph_runs (id, graph_id, status, run_scope, graph_revision, snapshot_json, is_retryable, started_at) VALUES (?, ?, 'running', 'graph', 1, '{}', TRUE, ?)`, runID, graphID, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec(`INSERT INTO workflow_graph_node_runs (id, graph_run_id, status, sort_order, started_at, attempt_count) VALUES (?, ?, 'running', 0, ?, 0)`, clockid.New(), runID, now).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = gdb.Exec(`DELETE FROM workflow_graph_node_runs WHERE graph_run_id = ?`, runID).Error
		_ = gdb.Exec(`DELETE FROM workflow_graph_runs WHERE id = ?`, runID).Error
		_ = gdb.Exec(`DELETE FROM workflow_graphs WHERE id = ?`, graphID).Error
		_ = gdb.Exec(`DELETE FROM products WHERE id = ?`, productID).Error
	})
	body, err := snapshot(gdb)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("productflow_generation_admission_running %d\n", before+1)
	if !strings.Contains(body, want) {
		t.Fatalf("expected %q in %s", want, body)
	}
}

func TestNewServerNilWhenDisabled(t *testing.T) {
	if NewServer("", nil, "token") != nil {
		t.Fatal("empty addr must disable the server")
	}
	if NewServer("127.0.0.1:29286", nil, "token") != nil {
		t.Fatal("nil db must disable the server")
	}
	if NewServer("127.0.0.1:29286", testdb.Gorm(t), "") != nil {
		t.Fatal("empty token must disable the server")
	}
}

func TestMetricsEmptyTokenDoesNotRegister(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	Register(engine, nil, "  ")
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	res := httptest.NewRecorder()
	engine.ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("empty token registered /metrics, got %d", res.Code)
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
		if res.Body.Len() != 0 {
			t.Fatalf("authorization %q leaked body %q", authorization, res.Body.String())
		}
	}
}
