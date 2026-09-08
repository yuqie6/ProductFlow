package agent

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

func TestWorkflowRequestConfirmCancelRace(t *testing.T) {
	for _, first := range []string{"confirm", "cancel", "cancel_write", "confirm_twice", "cancel_twice"} {
		t.Run(first+"_reads_first", func(t *testing.T) {
			pool, db := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_request_decision_%d", time.Now().UnixNano()))
			as := newAgentServerOnDB(t, mockGateway{}, "tok", pool, db)
			task, req := createProductGoalRunRequest(t, as, false)
			attachAwaitingWorkflowTurn(t, as, task, req.ID)
			if err := db.Model(&schema.AgentTurnProjections{}).Where("workflow_run_request_id=?", req.ID).Update("harness_turn_id", clockid.New()).Error; err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, db)), 10*time.Second)
			defer cancel()
			type pauseKey struct{}
			read := make(chan struct{}, 1)
			release := make(chan struct{})
			const observer = "test:request_decision_read"
			observe := func(q *gorm.DB) {
				if q.Statement.Table == "agent_workflow_run_requests" && q.Statement.Context.Value(pauseKey{}) == true {
					select {
					case read <- struct{}{}:
					default:
					}
					select {
					case <-release:
					case <-ctx.Done():
					}
				}
			}
			if first == "cancel_write" {
				if err := db.Callback().Update().After("gorm:update").Register(observer, observe); err != nil {
					t.Fatal(err)
				}
				defer db.Callback().Update().Remove(observer)
			} else {
				if err := db.Callback().Query().After("gorm:query").Register(observer, observe); err != nil {
					t.Fatal(err)
				}
				defer db.Callback().Query().Remove(observer)
			}
			invoke := func(callCtx context.Context, action string, pids chan int) error {
				return tx.WithGorm(callCtx, db, func(db *gorm.DB) error {
					if pids != nil {
						var pid int
						if err := db.Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
							return err
						}
						pids <- pid
					}
					svc := as.svc
					svc.DB = db
					if action == "confirm" || action == "confirm_twice" {
						_, err := svc.ConfirmWorkflowRunRequest(callCtx, task.ProductID, *task.ConversationID, req.ID)
						return err
					}
					_, err := svc.CancelWorkflowRunRequestHTTP(callCtx, task.ProductID, *task.ConversationID, req.ID)
					return err
				})
			}
			firstDone := make(chan error, 1)
			go func() { firstDone <- invoke(context.WithValue(ctx, pauseKey{}, true), first, nil) }()
			select {
			case <-read:
			case <-ctx.Done():
				t.Fatal("first read did not arrive")
			}
			second := "confirm"
			if first == "confirm" || first == "cancel_twice" {
				second = "cancel"
			}
			secondDone := make(chan error, 1)
			pids := make(chan int, 1)
			go func() { secondDone <- invoke(ctx, second, pids) }()
			var pid int
			select {
			case pid = <-pids:
			case <-ctx.Done():
				t.Fatal("second transaction did not start")
			}
			var secondErr error
			completed, observed := false, false
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for !observed {
				select {
				case secondErr = <-secondDone:
					completed = true
					observed = true
				case <-ticker.C:
					var blocked bool
					if err := pool.QueryRow(ctx, "SELECT cardinality(pg_blocking_pids($1)) > 0", pid).Scan(&blocked); err != nil {
						t.Fatal(err)
					}
					observed = blocked
				case <-ctx.Done():
					t.Fatal("second decision neither completed nor blocked")
				}
			}
			close(release)
			var firstErr error
			select {
			case firstErr = <-firstDone:
			case <-ctx.Done():
				t.Fatal("first decision did not finish")
			}
			if !completed {
				select {
				case secondErr = <-secondDone:
				case <-ctx.Done():
					t.Fatal("second decision did not finish")
				}
			}
			for _, err := range []error{firstErr, secondErr} {
				if err != nil {
					var app apperr.Error
					if !errors.As(err, &app) || app.Status != 409 || app.Code != apperr.CodeNotPending {
						t.Fatalf("unexpected decision error: %v", err)
					}
				}
			}
			if (first == "confirm_twice" || first == "cancel_twice") && (firstErr != nil || secondErr != nil) {
				t.Fatalf("duplicate decision did not replay: %v / %v", firstErr, secondErr)
			}
			var approvals int64
			if err := db.Model(&schema.AgentTurnEvents{}).Where("kind='approval/resolved'").Count(&approvals).Error; err != nil {
				t.Fatal(err)
			}
			if approvals != 1 {
				t.Fatalf("decision journal count %d want 1", approvals)
			}
			var stored schema.AgentWorkflowRunRequests
			if err := db.Where("id=?", req.ID).Take(&stored).Error; err != nil {
				t.Fatal(err)
			}
			var runs, dispatches int64
			if err := db.Model(&schema.WorkflowGraphRuns{}).Where("graph_id=?", req.WorkflowID).Count(&runs).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&schema.AsyncDispatches{}).Where("actor_name=?", queue.ActorGraphRun).Count(&dispatches).Error; err != nil {
				t.Fatal(err)
			}
			if stored.Status == "cancelled" {
				if stored.GraphRunID != nil || runs != 0 || dispatches != 0 {
					t.Fatalf("canceled request retained submitted execution: run=%v runs=%d dispatches=%d", stored.GraphRunID, runs, dispatches)
				}
			} else if stored.Status == "confirmed" {
				if stored.GraphRunID == nil || runs != 1 || dispatches != 1 {
					t.Fatalf("confirmed request missing unique run: %+v runs=%d", stored, runs)
				}
				if first != "confirm_twice" && firstErr == nil && secondErr == nil {
					t.Fatal("both conflicting decisions reported success")
				}
			} else {
				t.Fatalf("unexpected request status %s", stored.Status)
			}
		})
	}
}
