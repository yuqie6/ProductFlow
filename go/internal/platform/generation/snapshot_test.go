package generation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"gorm.io/gorm"
)

func TestLoadSnapshotEmptyDefaults(t *testing.T) {
	_, gdb := testdb.Open(t)
	resetGenerationRows(t, gdb)
	snap, err := LoadSnapshot(context.Background(), gdb)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Max != DefaultMaxConcurrent || snap.AdmissionRunning != 0 || snap.OverviewRunning != 0 || snap.OverviewQueued != 0 || snap.OverviewActive() != 0 {
		t.Fatalf("empty snapshot: %+v", snap)
	}
}

func TestQueueOverviewUsesOneStatementSnapshot(t *testing.T) {
	for _, domain := range []string{"session", "graph"} {
		for _, initial := range []string{"running", "queued"} {
			t.Run(domain+"_from_"+initial, func(t *testing.T) {
				_, gdb := testdb.Open(t)
				resetGenerationRows(t, gdb)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				now := time.Now().UTC()
				if domain == "session" {
					id := insertImageSession(t, gdb, now)
					insertSessionTask(t, gdb, id, initial, now)
				} else {
					insertGraphRun(t, gdb, now, []string{initial})
				}
				reads, changed := 0, false
				callback := "test:queue_snapshot_transition"
				if err := gdb.Callback().Query().After("gorm:query").Register(callback, func(db *gorm.DB) {
					if db.DryRun || db.Statement.Table == "app_settings" {
						return
					}
					reads++
					trigger := strings.Contains(db.Statement.SQL.String(), "AS session_counts")
					if domain == "session" && db.Statement.Table == "image_session_generation_tasks" {
						trigger = true
					}
					if domain == "graph" && db.Statement.Table == "workflow_graph_runs" {
						trigger = true
					}
					if !trigger || changed {
						return
					}
					changed = true
					next := "running"
					if initial == "running" {
						next = "queued"
					}
					// A separate connection commits after the first domain count (old) or combined read (new).
					var err error
					if domain == "session" {
						updates := map[string]any{"status": next, "active_attempt_id": nil}
						if next == "running" {
							updates["active_attempt_id"], updates["started_at"] = clockid.New(), now
						}
						err = gdb.WithContext(ctx).Model(&schema.ImageSessionGenerationTasks{}).
							Where("status = ?", initial).Updates(updates).Error
					} else {
						err = gdb.WithContext(ctx).Model(&schema.WorkflowGraphNodeRuns{}).
							Where("status = ?", initial).Update("status", next).Error
					}
					if err != nil {
						db.AddError(err)
					}
				}); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = gdb.Callback().Query().Remove(callback) })
				snap, err := LoadQueueOverview(ctx, gdb)
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("QUEUE_SNAPSHOT domain=%s initial=%s statements=%d running=%d queued=%d", domain, initial, reads, snap.OverviewRunning, snap.OverviewQueued)
				wantRunning, wantQueued := 0, 1
				if initial == "running" {
					wantRunning, wantQueued = 1, 0
				}
				if !changed || reads != 1 || snap.OverviewRunning != wantRunning || snap.OverviewQueued != wantQueued {
					t.Fatal("overview did not preserve a single pre-transition statement snapshot")
				}
			})
		}
	}
}

func TestLoadSnapshotAdmissionRunningDiffersFromOverviewRunning(t *testing.T) {
	_, gdb := testdb.Open(t)
	resetGenerationRows(t, gdb)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := gdb.Create(&schema.AppSettings{
		Key: MaxConcurrentSettingKey, Value: "5", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	// 同一 running run 上两个 running 节点：admission 计 2，总览 running 计 1 个 run。
	insertGraphRun(t, gdb, now, []string{"running", "running"})
	// running run 上只有 queued 节点：admission 不计，总览 queued 计 1 个 run。
	insertGraphRun(t, gdb, now, []string{"queued"})
	// running run 节点都已终态：两边都不计（总览 queued 需要 EXISTS queued，不是「run 还活着」）。
	insertGraphRun(t, gdb, now, []string{"succeeded"})

	sessionID := insertImageSession(t, gdb, now)
	insertSessionTask(t, gdb, sessionID, "running", now)
	insertSessionTask(t, gdb, sessionID, "queued", now)

	snap, err := LoadSnapshot(ctx, gdb)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Max != 5 {
		t.Fatalf("Max=%d want 5", snap.Max)
	}
	if snap.AdmissionRunning != 3 {
		t.Fatalf("AdmissionRunning=%d want 3 (2 graph nodes + 1 session task)", snap.AdmissionRunning)
	}
	if snap.OverviewRunning != 2 {
		t.Fatalf("OverviewRunning=%d want 2 (1 graph run + 1 session task)", snap.OverviewRunning)
	}
	if snap.OverviewQueued != 2 {
		t.Fatalf("OverviewQueued=%d want 2 (1 queued-only graph run + 1 session task)", snap.OverviewQueued)
	}
	if snap.OverviewActive() != 4 {
		t.Fatalf("OverviewActive=%d want 4", snap.OverviewActive())
	}
	if snap.AdmissionRunning == snap.OverviewRunning {
		t.Fatal("admission running must stay distinct from overview running")
	}

	overview, err := LoadQueueOverview(ctx, gdb)
	if err != nil {
		t.Fatal(err)
	}
	if overview.AdmissionRunning != 0 {
		t.Fatalf("queue overview must not load admission count, got %d", overview.AdmissionRunning)
	}
	if overview.Max != snap.Max || overview.OverviewRunning != snap.OverviewRunning || overview.OverviewQueued != snap.OverviewQueued {
		t.Fatalf("queue overview=%+v snapshot=%+v", overview, snap)
	}
}

func TestQueueOverviewGraphClassification(t *testing.T) {
	for _, tc := range []struct {
		name     string
		nodes    []string
		terminal bool
		running  int
		queued   int
	}{
		{"running_precedes_queued", []string{"running", "queued", "running"}, false, 1, 0},
		{"queued_once_per_run", []string{"queued", "queued"}, false, 0, 1},
		{"terminal_nodes", []string{"succeeded", "failed"}, false, 0, 0},
		{"no_nodes", nil, false, 0, 0},
		{"terminal_run", []string{"running", "queued"}, true, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, gdb := testdb.Open(t)
			resetGenerationRows(t, gdb)
			now := time.Now().UTC()
			insertGraphRun(t, gdb, now, tc.nodes)
			if tc.terminal {
				if err := gdb.Model(&schema.WorkflowGraphRuns{}).Where("status = ?", "running").
					Updates(map[string]any{"status": "succeeded", "finished_at": now}).Error; err != nil {
					t.Fatal(err)
				}
			}
			id := insertImageSession(t, gdb, now)
			insertSessionTask(t, gdb, id, "cancelled", now)
			insertSessionTask(t, gdb, id, "failed", now)
			snap, err := LoadQueueOverview(context.Background(), gdb)
			if err != nil {
				t.Fatal(err)
			}
			if snap.OverviewRunning != tc.running || snap.OverviewQueued != tc.queued {
				t.Fatalf("classification=%+v want running=%d queued=%d", snap, tc.running, tc.queued)
			}
		})
	}
}

func resetGenerationRows(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, stmt := range []string{
		"DELETE FROM image_session_generation_tasks",
		"DELETE FROM image_sessions",
		"DELETE FROM workflow_graph_node_runs",
		"DELETE FROM workflow_graph_runs",
		"DELETE FROM workflow_graphs",
		"DELETE FROM products",
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Where("key = ?", MaxConcurrentSettingKey).Delete(&schema.AppSettings{}).Error; err != nil {
		t.Fatal(err)
	}
}

func insertGraphRun(t *testing.T, db *gorm.DB, now time.Time, nodeStatuses []string) {
	t.Helper()
	_ = auth.MustDevMerchantID(t, db)
	productID := clockid.New()
	graphID := clockid.New()
	runID := clockid.New()
	if err := db.Exec(`
		INSERT INTO products (id, name, created_at, updated_at) VALUES (?, 'capacity', ?, ?)
	`, productID, now, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`
		INSERT INTO workflow_graphs (id, product_id, title, active, schema_version, revision, created_at, updated_at)
		VALUES (?, ?, 'capacity', TRUE, 3, 1, ?, ?)
	`, graphID, productID, now, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`
		INSERT INTO workflow_graph_runs (
			id, graph_id, status, run_scope, graph_revision, snapshot_json, is_retryable, started_at
		) VALUES (?, ?, 'running', 'graph', 1, '{}', TRUE, ?)
	`, runID, graphID, now).Error; err != nil {
		t.Fatal(err)
	}
	for i, status := range nodeStatuses {
		if err := db.Exec(`
			INSERT INTO workflow_graph_node_runs (
				id, graph_run_id, status, sort_order, started_at, attempt_count
			) VALUES (?, ?, ?, ?, ?, 0)
		`, clockid.New(), runID, status, i, now).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func insertImageSession(t *testing.T, db *gorm.DB, now time.Time) string {
	t.Helper()
	_ = auth.MustDevMerchantID(t, db)
	id := clockid.New()
	if err := db.Exec(`
		INSERT INTO image_sessions (id, title, created_at, updated_at) VALUES (?, 'capacity', ?, ?)
	`, id, now, now).Error; err != nil {
		t.Fatal(err)
	}
	return id
}

func insertSessionTask(t *testing.T, db *gorm.DB, sessionID, status string, now time.Time) {
	t.Helper()
	id := clockid.New()
	if status == "running" {
		attempt := clockid.New()
		if err := db.Exec(`
			INSERT INTO image_session_generation_tasks (
				id, session_id, status, prompt, size, generation_count, created_at,
				started_at, attempts, is_retryable, completed_candidates, active_attempt_id
			) VALUES (?, ?, 'running', 'p', '1024x1024', 1, ?, ?, 0, TRUE, 0, ?)
		`, id, sessionID, now, now, attempt).Error; err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := db.Exec(`
		INSERT INTO image_session_generation_tasks (
			id, session_id, status, prompt, size, generation_count, created_at,
			attempts, is_retryable, completed_candidates
		) VALUES (?, ?, ?, 'p', '1024x1024', 1, ?, 0, TRUE, 0)
	`, id, sessionID, status, now).Error; err != nil {
		t.Fatal(err)
	}
}
