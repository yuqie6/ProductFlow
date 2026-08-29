package agent

import (
	"context"
	"errors"
	"time"

	sqldb "database/sql"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/library"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"gorm.io/gorm"
)

var userOwnedTask = map[string]struct{}{
	"succeeded": {}, "canceled": {}, "paused": {},
}

// SyncGraphRunToTasks 把 GraphRun 状态投影到关联的 workflow request 和 Task。
// 商品 Goal 保持 waiting_user / goal_loop；用户完成、取消、暂停不被跑图终态改写。
func SyncGraphRunToTasks(ctx context.Context, pgxTx *gorm.DB, runID string) error {
	if runID == "" {
		return nil
	}
	var status string
	var failure *string
	var finished *time.Time
	err := pfdb.QueryRow(ctx, pgxTx, `
		SELECT status, failure_reason, finished_at FROM workflow_graph_runs WHERE id = $1
	`, runID).Scan(&status, &failure, &finished)
	if errors.Is(err, sqldb.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	rows, err := pfdb.Query(ctx, pgxTx, `
		SELECT id, task_id, conversation_id FROM agent_workflow_run_requests WHERE graph_run_id = $1
	`, runID)
	if err != nil {
		return err
	}
	defer rows.Close()
	type reqRef struct {
		id, conversationID string
		taskID             *string
	}
	var refs []reqRef
	for rows.Next() {
		var ref reqRef
		if err := rows.Scan(&ref.id, &ref.taskID, &ref.conversationID); err != nil {
			return err
		}
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, ref := range refs {
		if err := syncRequestRowFromRun(ctx, pgxTx, ref.id, status, failure, finished, now); err != nil {
			return err
		}
		if err := applyGraphRunStatusToTask(ctx, pgxTx, ref.taskID, status, failure, finished); err != nil {
			return err
		}
	}
	return nil
}

func syncRequestRowFromRun(ctx context.Context, pgxTx *gorm.DB, requestID, runStatus string, failure *string, finished *time.Time, now time.Time) error {
	finishedAt := now
	if finished != nil {
		finishedAt = *finished
	}
	switch runStatus {
	case graph.RunStatusRunning:
		_, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE agent_workflow_run_requests
			SET status = 'confirmed', updated_at = $2
			WHERE id = $1 AND status <> 'confirmed'
		`, requestID, now)
		return err
	case graph.RunStatusSucceeded:
		_, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE agent_workflow_run_requests
			SET status = 'succeeded', finished_at = COALESCE(finished_at, $2), updated_at = $3
			WHERE id = $1
		`, requestID, finishedAt, now)
		return err
	case graph.RunStatusFailed:
		_, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE agent_workflow_run_requests
			SET status = 'failed', failure_reason = $2, finished_at = COALESCE(finished_at, $3), updated_at = $4
			WHERE id = $1
		`, requestID, failure, finishedAt, now)
		return err
	case graph.RunStatusCancelled:
		reason := graph.GraphCancelledReason
		if failure != nil && *failure != "" {
			reason = *failure
		}
		_, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE agent_workflow_run_requests
			SET status = 'cancelled', failure_reason = $2, finished_at = COALESCE(finished_at, $3), updated_at = $4
			WHERE id = $1
		`, requestID, reason, finishedAt, now)
		return err
	default:
		return nil
	}
}

func applyGraphRunStatusToTask(ctx context.Context, pgxTx *gorm.DB, taskID *string, runStatus string, failure *string, finished *time.Time) error {
	if taskID == nil || *taskID == "" || runStatus == "" {
		return nil
	}
	task, err := lockTask(ctx, pgxTx, *taskID)
	if err != nil {
		return err
	}
	if _, owned := userOwnedTask[task.Status]; owned {
		return nil
	}
	keepGoal := false
	if task.ConversationID != nil {
		var scope string
		err := pfdb.QueryRow(ctx, pgxTx, `SELECT scope_type FROM agent_conversations WHERE id = $1`, *task.ConversationID).Scan(&scope)
		if err != nil && !errors.Is(err, sqldb.ErrNoRows) {
			return err
		}
		keepGoal = scope == "product_workflow"
	}
	finishedAt := time.Now().UTC()
	if finished != nil {
		finishedAt = *finished
	}
	var (
		status, waiting, summary string
		failureOut               *string
		finishedOut              *time.Time
		clearCanceled            bool
		setCanceledAt            bool
		started                  = task.StartedAt
	)
	switch runStatus {
	case graph.RunStatusRunning:
		status = "running"
		waiting = "workflow_run_running"
		summary = "工作流运行中"
		clearCanceled = true
		if started == nil {
			now := time.Now().UTC()
			started = &now
		}
	case graph.RunStatusSucceeded:
		if keepGoal {
			status = "waiting_user"
			waiting = "goal_loop"
			summary = "工作流运行已完成，Goal 未结束"
		} else {
			status = "succeeded"
			summary = "工作流运行已完成"
			finishedOut = &finishedAt
		}
	case graph.RunStatusFailed:
		reason := "未知原因"
		if failure != nil && *failure != "" {
			reason = *failure
		}
		if keepGoal {
			status = "waiting_user"
			waiting = "goal_loop"
			summary = boundedSummary("工作流运行失败：" + reason + "。Goal 未结束")
		} else {
			status = "failed"
			failureOut = &reason
			summary = boundedSummary("工作流运行失败：" + reason)
			finishedOut = &finishedAt
		}
	case graph.RunStatusCancelled:
		reason := graph.GraphCancelledReason
		if failure != nil && *failure != "" {
			reason = *failure
		}
		if keepGoal {
			status = "waiting_user"
			waiting = "goal_loop"
			summary = "工作流运行已取消，Goal 未结束"
		} else {
			status = "canceled"
			failureOut = &reason
			summary = "工作流运行已取消"
			finishedOut = &finishedAt
			setCanceledAt = true
		}
	default:
		return nil
	}
	var waitingArg any
	if waiting != "" {
		waitingArg = waiting
	}
	if _, err := pfdb.Exec(ctx, pgxTx, `
		UPDATE agent_tasks SET
			status = $2, waiting_reason = $3, failure_reason = $4, summary = $5,
			started_at = $6, finished_at = $7, updated_at = NOW(),
			canceled_at = CASE
				WHEN $8 THEN NULL
				WHEN $9 THEN $7
				ELSE canceled_at
			END
		WHERE id = $1
	`, task.ID, status, waitingArg, failureOut, summary, started, finishedOut, clearCanceled, setCanceledAt); err != nil {
		return err
	}
	return refreshSessionSummary(ctx, pgxTx, task.SessionID)
}

func markTurnSucceededForRequest(ctx context.Context, pgxTx *gorm.DB, requestID, conversationID string) error {
	if _, err := pfdb.Exec(ctx, pgxTx, `
		UPDATE agent_turn_projections
		SET status = 'succeeded', finished_at = COALESCE(finished_at, NOW()), updated_at = NOW()
		WHERE workflow_run_request_id = $1
	`, requestID); err != nil {
		return err
	}
	_, err := pfdb.Exec(ctx, pgxTx, `
		UPDATE agent_conversations SET status = 'completed', updated_at = NOW() WHERE id = $1
	`, conversationID)
	return err
}

func completeOrganizationDraftTask(ctx context.Context, pgxTx *gorm.DB, draft library.OrganizationDraft) error {
	if draft.CurrentRevision == nil {
		return nil
	}
	var taskID *string
	err := pfdb.QueryRow(ctx, pgxTx, `
		SELECT task_id FROM agent_turn_projections
		WHERE library_organization_draft_revision_id = $1
		ORDER BY created_at DESC, id DESC LIMIT 1
	`, draft.CurrentRevision.ID).Scan(&taskID)
	if errors.Is(err, sqldb.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if taskID == nil || *taskID == "" {
		return nil
	}
	task, err := lockTask(ctx, pgxTx, *taskID)
	if err != nil {
		return err
	}
	if task.Status != "awaiting_confirmation" {
		return nil
	}
	if _, err := pfdb.Exec(ctx, pgxTx, `
		UPDATE agent_tasks SET status = 'succeeded', waiting_reason = NULL, failure_reason = NULL,
			finished_at = NOW(), updated_at = NOW()
		WHERE id = $1
	`, task.ID); err != nil {
		return err
	}
	return refreshSessionSummary(ctx, pgxTx, task.SessionID)
}

func parkTaskAfterCancelledRunRequest(ctx context.Context, pgxTx *gorm.DB, taskID *string) error {
	if taskID == nil || *taskID == "" {
		return nil
	}
	task, err := lockTask(ctx, pgxTx, *taskID)
	if err != nil {
		return err
	}
	if _, owned := userOwnedTask[task.Status]; owned {
		return nil
	}
	keepGoal := false
	if task.ConversationID != nil {
		var scope string
		_ = pfdb.QueryRow(ctx, pgxTx, `SELECT scope_type FROM agent_conversations WHERE id = $1`, *task.ConversationID).Scan(&scope)
		keepGoal = scope == "product_workflow"
	}
	if keepGoal {
		_, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE agent_tasks SET status = 'waiting_user', waiting_reason = 'goal_loop',
				failure_reason = NULL, finished_at = NULL, updated_at = NOW()
			WHERE id = $1
		`, task.ID)
		if err != nil {
			return err
		}
		return refreshSessionSummary(ctx, pgxTx, task.SessionID)
	}
	_, err = pfdb.Exec(ctx, pgxTx, `
		UPDATE agent_tasks SET status = 'canceled', waiting_reason = NULL,
			canceled_at = NOW(), finished_at = NOW(), updated_at = NOW()
		WHERE id = $1
	`, task.ID)
	if err != nil {
		return err
	}
	return refreshSessionSummary(ctx, pgxTx, task.SessionID)
}
