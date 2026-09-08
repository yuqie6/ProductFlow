package agent

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

func TestOldGraphRunCannotOverwriteNewRequestTask(t *testing.T) {
	for _, pending := range []bool{true, false} {
		t.Run(fmt.Sprintf("pending_%t", pending), func(t *testing.T) {
			pool, db := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_request_authority_%d", time.Now().UnixNano()))
			as := newAgentServerOnDB(t, mockGateway{}, "", pool, db)
			task := seedProductGoalTask(t, as)
			oldRun := attachSucceededGraphRun(t, as, task)
			var old schema.AgentWorkflowRunRequests
			if err := db.Where("graph_run_id=?", oldRun).Take(&old).Error; err != nil {
				t.Fatal(err)
			}
			latest := old
			latest.ID = clockid.New()
			latest.IdempotencyKey = latest.ID
			latest.CreatedAt = old.CreatedAt.Add(time.Second)
			latest.GraphRunID = nil
			latest.Status = "awaiting_confirmation"
			wantStatus, wantWaiting := "awaiting_confirmation", "workflow_run_confirmation"
			if !pending {
				var run schema.WorkflowGraphRuns
				if err := db.Where("id=?", oldRun).Take(&run).Error; err != nil {
					t.Fatal(err)
				}
				run.ID = clockid.New()
				run.Status = "running"
				run.FinishedAt = nil
				if err := db.Create(&run).Error; err != nil {
					t.Fatal(err)
				}
				latest.GraphRunID = &run.ID
				latest.Status = "confirmed"
				wantStatus, wantWaiting = "running", "workflow_run_running"
			}
			if err := db.Create(&latest).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&schema.AgentTasks{}).Where("id=?", task.ID).Updates(map[string]any{"status": wantStatus, "waiting_reason": wantWaiting, "summary": "新请求", "finished_at": nil}).Error; err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			for _, status := range []string{"succeeded", "failed", "unknown", "cancelled", "running"} {
				if !pending && status == "running" {
					continue
				}
				if err := db.Model(&schema.WorkflowGraphRuns{}).Where("id=?", oldRun).Updates(map[string]any{"status": status, "failure_reason": "旧运行"}).Error; err != nil {
					t.Fatal(err)
				}
				if err := tx.WithGorm(ctx, db, func(tx *gorm.DB) error { return SyncGraphRunToTasks(ctx, tx, oldRun) }); err != nil {
					t.Fatal(err)
				}
				var got schema.AgentTasks
				if err := db.Where("id=?", task.ID).Take(&got).Error; err != nil {
					t.Fatal(err)
				}
				if got.Status != wantStatus || got.WaitingReason == nil || *got.WaitingReason != wantWaiting || got.Summary == nil || *got.Summary != "新请求" {
					t.Errorf("old %s overwrote task: status=%s waiting=%v summary=%v", status, got.Status, got.WaitingReason, got.Summary)
				}
				var request schema.AgentWorkflowRunRequests
				if err := db.Where("id=?", old.ID).Take(&request).Error; err != nil {
					t.Fatal(err)
				}
				wantRequest := status
				if status == "running" {
					wantRequest = "confirmed"
				}
				if request.Status != wantRequest {
					t.Fatalf("old request not synchronized: %s", request.Status)
				}
			}
			got := mustGetTask(t, as, task.ID)
			if got.Status != wantStatus || got.WaitingReason == nil || *got.WaitingReason != wantWaiting {
				t.Fatalf("read projection overwrote newest request: %+v", got)
			}
			if !pending {
				if err := db.Model(&schema.WorkflowGraphRuns{}).Where("id=?", *latest.GraphRunID).Update("status", "succeeded").Error; err != nil {
					t.Fatal(err)
				}
				if err := tx.WithGorm(ctx, db, func(tx *gorm.DB) error { return SyncGraphRunToTasks(ctx, tx, *latest.GraphRunID) }); err != nil {
					t.Fatal(err)
				}
				got = mustGetTask(t, as, task.ID)
				if got.Status != "waiting_user" || got.WaitingReason == nil || *got.WaitingReason != "goal_loop" {
					t.Fatalf("latest result not applied: %+v", got)
				}
			}
		})
	}
}

func TestOldGraphProjectionWaitsForTaskAuthorityCommit(t *testing.T) {
	pool, db := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_request_order_%d", time.Now().UnixNano()))
	as := newAgentServerOnDB(t, mockGateway{}, "", pool, db)
	task := seedProductGoalTask(t, as)
	oldRun := attachSucceededGraphRun(t, as, task)
	var old schema.AgentWorkflowRunRequests
	if err := db.Where("graph_run_id=?", oldRun).Take(&old).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	newer := db.WithContext(ctx).Begin()
	if newer.Error != nil {
		t.Fatal(newer.Error)
	}
	defer newer.Rollback()
	if _, err := lockTask(ctx, newer, task.ID); err != nil {
		t.Fatal(err)
	}
	type observerKey struct{}
	observed := make(chan struct{}, 1)
	const callback = "test:old_graph_task_lock"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(q *gorm.DB) {
		if q.Statement.Table == "agent_tasks" && q.Statement.Context.Value(observerKey{}) == true {
			select {
			case observed <- struct{}{}:
			default:
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove(callback)
	done := make(chan error, 1)
	go func() {
		oldCtx := context.WithValue(ctx, observerKey{}, true)
		done <- tx.WithGorm(oldCtx, db, func(tx *gorm.DB) error { return SyncGraphRunToTasks(oldCtx, tx, oldRun) })
	}()
	select {
	case <-observed:
	case <-ctx.Done():
		t.Fatal("old callback did not reach Task lock")
	}
	latest := old
	latest.ID = clockid.New()
	latest.IdempotencyKey = latest.ID
	latest.CreatedAt = old.CreatedAt.Add(time.Second)
	latest.GraphRunID = nil
	latest.Status = "awaiting_confirmation"
	if err := newer.Create(&latest).Error; err != nil {
		t.Fatal(err)
	}
	if err := markRequestWaiting(ctx, newer, latest.ConversationID, latest.TaskID); err != nil {
		t.Fatal(err)
	}
	if err := newer.Commit().Error; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("old projection did not finish")
	}
	got := mustGetTask(t, as, task.ID)
	if got.Status != "awaiting_confirmation" || got.WaitingReason == nil || *got.WaitingReason != "workflow_run_confirmation" {
		t.Fatalf("new authority overwritten: %+v", got)
	}
}
