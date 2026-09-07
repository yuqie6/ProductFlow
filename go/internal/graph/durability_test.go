package graph

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/generation"
	"github.com/yuqie6/productflow/internal/platform/metrics"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"gorm.io/gorm"
)

func TestTerminalNodeRunUpdatesClearLiveProgress(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	for _, status := range []string{NodeRunSucceeded, NodeRunSkipped, NodeRunFailed, NodeRunCancelled, NodeRunUnknown} {
		t.Run(status, func(t *testing.T) {
			updates := terminalNodeRunUpdates(status, now)
			if updates["status"] != status || updates["finished_at"] != now || updates["progress_updated_at"] != now {
				t.Fatalf("unexpected terminal updates: %#v", updates)
			}
			if value, ok := updates["progress_phase"]; !ok || value != nil {
				t.Fatalf("terminal progress_phase must be explicitly cleared: %#v", updates)
			}
			if value, ok := updates["active_attempt_id"]; !ok || value != nil {
				t.Fatalf("terminal active_attempt_id must be explicitly cleared: %#v", updates)
			}
		})
	}
}

func TestAggregateGraphRunTerminalStatusPriority(t *testing.T) {
	longReason := strings.Repeat("x", 1001)
	tests := []struct {
		name       string
		statuses   []string
		reasons    []string
		want       string
		wantReason string
		retryable  bool
	}{
		{name: "success and skipped", statuses: []string{NodeRunSucceeded, NodeRunSkipped}, reasons: []string{"", ""}, want: RunStatusSucceeded},
		{name: "mixed cancellation", statuses: []string{NodeRunSucceeded, NodeRunCancelled, NodeRunSkipped}, reasons: []string{"", "", ""}, want: RunStatusCancelled, wantReason: GraphCancelledReason},
		{name: "failure beats cancellation", statuses: []string{NodeRunCancelled, NodeRunFailed}, reasons: []string{"", "provider rejected"}, want: RunStatusFailed, wantReason: "provider rejected", retryable: true},
		{name: "unknown beats failure", statuses: []string{NodeRunFailed, NodeRunUnknown}, reasons: []string{"failed", ""}, want: RunStatusUnknown, wantReason: ProviderUnknownDetail},
		{name: "reason is bounded", statuses: []string{NodeRunUnknown}, reasons: []string{longReason}, want: RunStatusUnknown, wantReason: longReason[:1000]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, reason, retryable := aggregateGraphRunTerminalStatus(tt.statuses, tt.reasons)
			if status != tt.want || retryable != tt.retryable {
				t.Fatalf("got status=%s retryable=%t, want status=%s retryable=%t", status, retryable, tt.want, tt.retryable)
			}
			if tt.wantReason == "" {
				if reason != nil {
					t.Fatalf("unexpected reason %q", *reason)
				}
				return
			}
			if reason == nil || *reason != tt.wantReason {
				t.Fatalf("got reason=%v, want %q", reason, tt.wantReason)
			}
		})
	}
}

func TestGenerationCapacityAvailableRecordsAdvisoryLockWait(t *testing.T) {
	_, gdb := testdb.Open(t)
	ctx := context.Background()
	before := metrics.AdvisoryLockWaitCount()
	if err := gdb.WithContext(ctx).Transaction(func(dbTx *gorm.DB) error {
		_, err := GenerationCapacityAvailable(ctx, dbTx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got := metrics.AdvisoryLockWaitCount(); got <= before {
		t.Fatalf("advisory lock wait count %d, want > %d", got, before)
	}
}

func TestGenerationCapacityUsesAdmissionNodeCount(t *testing.T) {
	_, gdb := testdb.Open(t)
	ctx := context.Background()
	beforeAdmission, err := generation.CountAdmissionRunning(ctx, gdb)
	if err != nil {
		t.Fatal(err)
	}
	beforeSnap, err := generation.LoadSnapshot(ctx, gdb)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	productID, graphID, runID := clockid.New(), clockid.New(), clockid.New()
	if err := gdb.Exec(`INSERT INTO products (id, name, created_at, updated_at, merchant_id) VALUES (?, 'capacity', ?, ?, ?)`, productID, now, now, auth.MustDevMerchantID(t, gdb)).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec(`INSERT INTO workflow_graphs (id, product_id, title, active, schema_version, revision, created_at, updated_at) VALUES (?, ?, 'capacity', TRUE, 3, 1, ?, ?)`, graphID, productID, now, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec(`INSERT INTO workflow_graph_runs (id, graph_id, status, run_scope, graph_revision, snapshot_json, is_retryable, started_at) VALUES (?, ?, 'running', 'graph', 1, '{}', TRUE, ?)`, runID, graphID, now).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := gdb.Exec(`INSERT INTO workflow_graph_node_runs (id, graph_run_id, status, sort_order, started_at, attempt_count) VALUES (?, ?, 'running', ?, ?, 0)`, clockid.New(), runID, i, now).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_ = gdb.Exec(`DELETE FROM workflow_graph_node_runs WHERE graph_run_id = ?`, runID).Error
		_ = gdb.Exec(`DELETE FROM workflow_graph_runs WHERE id = ?`, runID).Error
		_ = gdb.Exec(`DELETE FROM workflow_graphs WHERE id = ?`, graphID).Error
		_ = gdb.Exec(`DELETE FROM products WHERE id = ?`, productID).Error
	})
	afterAdmission, err := generation.CountAdmissionRunning(ctx, gdb)
	if err != nil {
		t.Fatal(err)
	}
	afterSnap, err := generation.LoadSnapshot(ctx, gdb)
	if err != nil {
		t.Fatal(err)
	}
	if afterAdmission-beforeAdmission != 2 {
		t.Fatalf("admission delta=%d want 2 (node count, not run count)", afterAdmission-beforeAdmission)
	}
	if afterSnap.OverviewRunning-beforeSnap.OverviewRunning != 1 {
		t.Fatalf("overview running delta=%d want 1 (run count)", afterSnap.OverviewRunning-beforeSnap.OverviewRunning)
	}

	prev := schema.AppSettings{}
	hadPrev := gdb.Where("key = ?", generation.MaxConcurrentSettingKey).Take(&prev).Error == nil
	if err := gdb.Where("key = ?", generation.MaxConcurrentSettingKey).Delete(&schema.AppSettings{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&schema.AppSettings{
		Key: generation.MaxConcurrentSettingKey, Value: "1", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = gdb.Where("key = ?", generation.MaxConcurrentSettingKey).Delete(&schema.AppSettings{}).Error
		if hadPrev {
			_ = gdb.Create(&prev).Error
		}
	})
	denied := false
	beforeGraphDenied := metrics.GenerationAdmissionDeniedCount("graph")
	beforeSessionDenied := metrics.GenerationAdmissionDeniedCount("imagesession")
	if err := gdb.WithContext(ctx).Transaction(func(dbTx *gorm.DB) error {
		ok, err := generationCapacityAvailable(ctx, dbTx, "graph")
		if err != nil {
			return err
		}
		denied = !ok
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !denied {
		t.Fatal("limit 1 with 2 running nodes must deny admission")
	}
	if got := metrics.GenerationAdmissionDeniedCount("graph"); got != beforeGraphDenied+1 {
		t.Fatalf("graph denied count %d, want %d", got, beforeGraphDenied+1)
	}
	if got := metrics.GenerationAdmissionDeniedCount("imagesession"); got != beforeSessionDenied {
		t.Fatalf("imagesession denied count %d, want unchanged %d", got, beforeSessionDenied)
	}
	if err := gdb.WithContext(ctx).Transaction(func(dbTx *gorm.DB) error {
		ok, err := GenerationCapacityAvailable(ctx, dbTx)
		if err != nil {
			return err
		}
		if ok {
			return fmt.Errorf("exported ImageSession admission must also deny")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := metrics.GenerationAdmissionDeniedCount("imagesession"); got != beforeSessionDenied+1 {
		t.Fatalf("imagesession denied count %d, want %d", got, beforeSessionDenied+1)
	}
}
