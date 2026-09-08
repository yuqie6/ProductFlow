package agent

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

func TestGraphProjectionCannotRestoreStaleRunningSnapshot(t *testing.T) {
	for _, terminal := range []string{"succeeded", "unknown", "cancelled"} {
		t.Run(terminal, func(t *testing.T) {
			pool, db := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_projection_snapshot_%d", time.Now().UnixNano()))
			as := newAgentServerOnDB(t, mockGateway{}, "", pool, db)
			task := seedProductGoalTask(t, as)
			runID := attachSucceededGraphRun(t, as, task)
			if err := db.Model(&schema.WorkflowGraphRuns{}).Where("id=?", runID).Updates(map[string]any{"status": "running", "finished_at": nil}).Error; err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			type staleKey struct{}
			read := make(chan struct{}, 1)
			release := make(chan struct{})
			const callback = "test:stale_graph_snapshot"
			if err := db.Callback().Query().After("gorm:query").Register(callback, func(q *gorm.DB) {
				if q.Statement.Table == "workflow_graph_runs" && q.Statement.Context.Value(staleKey{}) == true {
					select {
					case read <- struct{}{}:
					default:
					}
					select {
					case <-release:
					case <-ctx.Done():
					}
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer db.Callback().Query().Remove(callback)
			oldDone := make(chan error, 1)
			go func() {
				oldCtx := context.WithValue(ctx, staleKey{}, true)
				oldDone <- tx.WithGorm(oldCtx, db, func(tx *gorm.DB) error { return SyncGraphRunToTasks(oldCtx, tx, runID) })
			}()
			select {
			case <-read:
			case <-ctx.Done():
				t.Fatal("stale reader did not read run")
			}
			newDone := make(chan error, 1)
			pidReady := make(chan int, 1)
			go func() {
				newDone <- tx.WithGorm(ctx, db, func(tx *gorm.DB) error {
					var pid int
					if err := tx.Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
						return err
					}
					pidReady <- pid
					if terminal == "cancelled" {
						var run schema.WorkflowGraphRuns
						if err := tx.Where("id=?", runID).Take(&run).Error; err != nil {
							return err
						}
						_, err := as.svc.Graph.CancelRunTx(ctx, tx, *task.ProductID, run.GraphID, runID)
						return err
					}
					if err := tx.Model(&schema.WorkflowGraphRuns{}).Where("id=?", runID).Updates(map[string]any{"status": terminal, "failure_reason": "terminal evidence", "finished_at": time.Now().UTC()}).Error; err != nil {
						return err
					}
					return SyncGraphRunToTasks(ctx, tx, runID)
				})
			}()
			var pid int
			select {
			case pid = <-pidReady:
			case <-ctx.Done():
				t.Fatal("terminal writer not started")
			}
			// Release the old read after the terminal writer either commits (old defect)
			// or is observably waiting for its request lock (serialized implementation).
			completed := false
			observed := false
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for !observed {
				select {
				case err := <-newDone:
					if err != nil {
						t.Fatal(err)
					}
					completed = true
					observed = true
				case <-ticker.C:
					var blocked bool
					if err := pool.QueryRow(ctx, "SELECT cardinality(pg_blocking_pids($1)) > 0", pid).Scan(&blocked); err != nil {
						t.Fatal(err)
					}
					observed = blocked
				case <-ctx.Done():
					t.Fatal("terminal writer neither completed nor blocked")
				}
			}
			close(release)
			select {
			case err := <-oldDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("old reader did not finish")
			}
			if !completed {
				select {
				case err := <-newDone:
					if err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal("terminal writer did not finish")
				}
			}
			var request schema.AgentWorkflowRunRequests
			if err := db.Where("graph_run_id=?", runID).Take(&request).Error; err != nil {
				t.Fatal(err)
			}
			var got schema.AgentTasks
			if err := db.Where("id=?", task.ID).Take(&got).Error; err != nil {
				t.Fatal(err)
			}
			if request.Status != terminal || request.FinishedAt == nil || got.Status != "waiting_user" || got.WaitingReason == nil || *got.WaitingReason != "goal_loop" {
				t.Fatalf("terminal=%s request=%s task=%s waiting=%v", terminal, request.Status, got.Status, got.WaitingReason)
			}
		})
	}
}
