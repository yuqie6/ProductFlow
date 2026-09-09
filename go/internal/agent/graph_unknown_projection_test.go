package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

func TestGraphUnknownProjectsRequestAndTask(t *testing.T) {
	for _, global := range []bool{false, true} {
		t.Run(fmt.Sprintf("global_%t", global), func(t *testing.T) {
			pool, gdb := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_unknown_projection_%d", time.Now().UnixNano()))
			as := newAgentServerOnDB(t, mockGateway{}, "", pool, gdb)
			task := seedProductGoalTask(t, as)
			runID := attachSucceededGraphRun(t, as, task)
			ctx := context.Background()
			if global {
				resp := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
				as.mustStatus(t, resp, http.StatusCreated)
				var session SessionResponse
				as.decode(t, resp, &session)
				resp = as.doJSON(t, http.MethodPost, "/api/v2/agent-tasks", map[string]any{"session_id": session.ID, "title": "全局生成", "goal": "生成商品图", "conversation_id": session.Conversations[0].ConversationID})
				as.mustStatus(t, resp, http.StatusCreated)
				as.decode(t, resp, &task)
				if err := gdb.Model(&schema.AgentWorkflowRunRequests{}).Where("graph_run_id = ?", runID).Updates(map[string]any{"task_id": task.ID, "conversation_id": task.ConversationID}).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := gdb.Model(&schema.WorkflowGraphRuns{}).Where("id = ?", runID).Updates(map[string]any{"status": "unknown", "failure_reason": "provider result unproven", "is_retryable": false}).Error; err != nil {
				t.Fatal(err)
			}
			if err := gdb.Model(&schema.AgentTasks{}).Where("id = ?", task.ID).Updates(map[string]any{"status": "running", "waiting_reason": "workflow_run_running"}).Error; err != nil {
				t.Fatal(err)
			}
			sync := func() error {
				return tx.WithGorm(ctx, gdb, func(db *gorm.DB) error { return SyncGraphRunToTasks(ctx, db, runID) })
			}
			for i := 0; i < 2; i++ {
				if err := sync(); err != nil {
					t.Fatal(err)
				}
			}
			var request schema.AgentWorkflowRunRequests
			if err := gdb.Where("graph_run_id = ?", runID).Take(&request).Error; err != nil {
				t.Fatal(err)
			}
			if request.Status != "unknown" || request.FailureReason == nil || *request.FailureReason != "provider result unproven" || request.FinishedAt == nil {
				t.Fatalf("request status=%s failure=%v finished=%v", request.Status, request.FailureReason, request.FinishedAt)
			}
			path := "/api/v2/agent-conversations/" + *task.ConversationID + "/workflow-run-request"
			if !global {
				path = "/api/v2/products/" + *task.ProductID + "/agent-conversations/" + *task.ConversationID + "/workflow-run-request"
			}
			var dispatchBefore int64
			if err := gdb.Table("river_job").Count(&dispatchBefore).Error; err != nil {
				t.Fatal(err)
			}
			for _, command := range []bool{false, true} {
				method, target := http.MethodGet, path
				if command {
					method, target = http.MethodPost, path+"/"+request.ID+"/confirm"
				}
				resp := as.do(t, method, target, nil, "", nil)
				as.mustStatus(t, resp, http.StatusOK)
				var wire WorkflowRunRequestResponse
				as.decode(t, resp, &wire)
				if wire.Status != "unknown" || wire.WorkflowRunStatus == nil || *wire.WorkflowRunStatus != "unknown" || wire.WorkflowRunID == nil || *wire.WorkflowRunID != runID {
					t.Fatalf("wire projection: %+v", wire)
				}
			}
			var dispatchAfter int64
			if err := gdb.Table("river_job").Count(&dispatchAfter).Error; err != nil {
				t.Fatal(err)
			}
			if dispatchBefore != dispatchAfter {
				t.Fatal("confirm replay enqueued unknown run")
			}
			got := mustGetTask(t, as, task.ID)
			if got.Summary == nil || !strings.Contains(*got.Summary, "结果未知") {
				t.Fatalf("summary=%v", got.Summary)
			}
			if global {
				if got.Status != "unknown" || got.FinishedAt == nil || got.FailureReason == nil {
					t.Fatalf("global task=%+v", got)
				}
			} else {
				if got.Status != "waiting_user" || got.WaitingReason == nil || *got.WaitingReason != "goal_loop" || got.FinishedAt != nil || got.FailureReason != nil {
					t.Fatalf("product task=%+v", got)
				}
			}
			for _, owned := range []string{"succeeded", "canceled", "paused"} {
				if err := gdb.Model(&schema.AgentTasks{}).Where("id = ?", task.ID).Updates(map[string]any{"status": owned, "summary": "用户决定"}).Error; err != nil {
					t.Fatal(err)
				}
				if err := sync(); err != nil {
					t.Fatal(err)
				}
				var row schema.AgentTasks
				if err := gdb.Where("id = ?", task.ID).Take(&row).Error; err != nil {
					t.Fatal(err)
				}
				if row.Status != owned || row.Summary == nil || *row.Summary != "用户决定" {
					t.Fatalf("overwrote user-owned task: %+v", row)
				}
			}
		})
	}
}

func TestGraphUnknownProjectionFailureRollsBackRequest(t *testing.T) {
	pool, gdb := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_unknown_rollback_%d", time.Now().UnixNano()))
	as := newAgentServerOnDB(t, mockGateway{}, "", pool, gdb)
	task := seedProductGoalTask(t, as)
	runID := attachSucceededGraphRun(t, as, task)
	if err := gdb.Exec("UPDATE workflow_graph_runs SET status='unknown' WHERE id=?", runID).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec("UPDATE agent_tasks SET status='running' WHERE id=?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec("ALTER TABLE agent_tasks ADD CONSTRAINT test_unknown_projection CHECK (status <> 'waiting_user')").Error; err != nil {
		t.Fatal(err)
	}
	sync := func() error {
		return tx.WithGorm(context.Background(), gdb, func(db *gorm.DB) error { return SyncGraphRunToTasks(context.Background(), db, runID) })
	}
	err := sync()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.ConstraintName != "test_unknown_projection" {
		t.Fatalf("expected task write error: %v", err)
	}
	var status string
	if err := gdb.Model(&schema.AgentWorkflowRunRequests{}).Select("status").Where("graph_run_id=?", runID).Scan(&status).Error; err != nil {
		t.Fatal(err)
	}
	if status != "confirmed" {
		t.Fatalf("partial request write: %s", status)
	}
	if err := gdb.Exec("ALTER TABLE agent_tasks DROP CONSTRAINT test_unknown_projection").Error; err != nil {
		t.Fatal(err)
	}
	if err := sync(); err != nil {
		t.Fatal(err)
	}
	if err := gdb.Model(&schema.AgentWorkflowRunRequests{}).Select("status").Where("graph_run_id=?", runID).Scan(&status).Error; err != nil {
		t.Fatal(err)
	}
	if status != "unknown" {
		t.Fatalf("recovery status: %s", status)
	}
}
