package agent

import (
	"context"
	"errors"
	"time"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/library"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

var userOwnedTask = map[string]struct{}{
	"succeeded": {}, "canceled": {}, "paused": {},
}

// SyncGraphRunToTasks 把 GraphRun 状态投影到关联的 workflow request 和 Task。
// 商品 Goal 保持 waiting_user / goal_loop；用户完成、取消、暂停不被跑图终态改写。
//
// runID 空或 GraphRun 缺失时静默成功。锁 Task / 更新 request 的数据库错误会返回。未识别的 run 状态跳过 Task 写入。
func SyncGraphRunToTasks(ctx context.Context, pgxTx *gorm.DB, runID string) error {
	if runID == "" {
		return nil
	}
	var refs []schema.AgentWorkflowRunRequests
	if err := pgxTx.WithContext(ctx).Select("id").Where("graph_run_id = ?", runID).Order("id").Find(&refs).Error; err != nil {
		return err
	}
	for _, ref := range refs {
		// 同一确认单的同步先串行化，再读取 Graph 状态。Graph 取消已持 run 锁，
		// 此处只锁 Agent 自己的请求行，避免 request -> run 的反向取锁。
		err := pgxTx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Select("id, task_id").
			Where("id = ? AND graph_run_id = ?", ref.ID, runID).Take(&ref).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		var run schema.WorkflowGraphRuns
		err = pgxTx.WithContext(ctx).Select("status, failure_reason, finished_at").Where("id = ?", runID).Take(&run).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if err := syncRequestRowFromRun(ctx, pgxTx, ref.ID, run.Status, run.FailureReason, run.FinishedAt, now); err != nil {
			return err
		}
		if err := applyGraphRunStatusToTask(ctx, pgxTx, ref.TaskID, ref.ID, run.Status, run.FailureReason, run.FinishedAt); err != nil {
			return err
		}
	}
	return nil
}

// syncRequestRowFromRun 把 WorkflowGraphRun 状态投影到 agent_workflow_run_requests。running 升 confirmed；终态写 finished_at。不改 Task（由 applyGraphRunStatusToTask 负责 goal_loop）。
func syncRequestRowFromRun(ctx context.Context, pgxTx *gorm.DB, requestID, runStatus string, failure *string, finished *time.Time, now time.Time) error {
	finishedAt := now
	if finished != nil {
		finishedAt = *finished
	}
	q := pgxTx.WithContext(ctx).Model(&schema.AgentWorkflowRunRequests{}).Where("id = ?", requestID)
	switch runStatus {
	case graph.RunStatusRunning:
		return q.Where("status <> ?", "confirmed").Updates(map[string]any{
			"status":     "confirmed",
			"updated_at": now,
		}).Error
	case graph.RunStatusSucceeded:
		return q.Updates(map[string]any{
			"status":      "succeeded",
			"finished_at": gorm.Expr("COALESCE(finished_at, ?)", finishedAt),
			"updated_at":  now,
		}).Error
	case graph.RunStatusFailed, graph.RunStatusUnknown:
		return q.Updates(map[string]any{
			"status":         runStatus,
			"failure_reason": failure,
			"finished_at":    gorm.Expr("COALESCE(finished_at, ?)", finishedAt),
			"updated_at":     now,
		}).Error
	case graph.RunStatusCancelled:
		reason := graph.GraphCancelledReason
		if failure != nil && *failure != "" {
			reason = *failure
		}
		return q.Updates(map[string]any{
			"status":         "cancelled",
			"failure_reason": reason,
			"finished_at":    gorm.Expr("COALESCE(finished_at, ?)", finishedAt),
			"updated_at":     now,
		}).Error
	default:
		return nil
	}
}

// applyGraphRunStatusToTask 只允许 Task 最新确认单的 GraphRun 投影到 Task。用户已 succeeded/canceled/paused 立刻返回。
//
// 商品工作流 keepGoal：succeeded/failed/unknown/cancelled 都停在 waiting_user / goal_loop，摘要说明跑图结果但 Goal 未结束。全局 Task 才随跑图终态结束。
//
// SyncGraphRunToTasks 调用。禁区：不要在 GetTask 之外再写一套「跑图成功即完成 Goal」。
func applyGraphRunStatusToTask(ctx context.Context, pgxTx *gorm.DB, taskID *string, requestID, runStatus string, failure *string, finished *time.Time) error {
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
	// 请求创建会在同一事务持有 Task 锁并设置待确认；锁后读取最新请求，
	// 保证旧运行迟到只更新自己的确认单，不能覆盖更新请求的 Task 状态。
	var latest schema.AgentWorkflowRunRequests
	err = pgxTx.WithContext(ctx).Select("id").Where("task_id = ?", task.ID).
		Order("created_at DESC, id DESC").Take(&latest).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if latest.ID != requestID {
		return nil
	}
	keepGoal := false
	if task.ConversationID != nil {
		var conv schema.AgentConversations
		err := pgxTx.Select("scope_type").Where("id = ?", *task.ConversationID).Take(&conv).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		keepGoal = conv.ScopeType == "product_workflow"
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
	case graph.RunStatusUnknown:
		reason := "工作流执行结果无法确认"
		if failure != nil && *failure != "" {
			reason = *failure
		}
		if keepGoal {
			status = "waiting_user"
			waiting = "goal_loop"
			summary = boundedSummary("工作流运行结果未知：" + reason + "。Goal 未结束")
		} else {
			status = "unknown"
			failureOut = &reason
			summary = boundedSummary("工作流运行结果未知：" + reason)
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
	updates := map[string]any{
		"status":         status,
		"waiting_reason": waitingArg,
		"failure_reason": failureOut,
		"summary":        summary,
		"started_at":     started,
		"finished_at":    finishedOut,
		"updated_at":     time.Now().UTC(),
	}
	if clearCanceled {
		updates["canceled_at"] = nil
	} else if setCanceledAt {
		updates["canceled_at"] = finishedOut
	}
	if err := pgxTx.Model(&schema.AgentTasks{}).Where("id = ?", task.ID).Updates(updates).Error; err != nil {
		return err
	}
	publishTaskChanged(pgxTx, task.ID, task.SessionID)
	return refreshSessionSummary(ctx, pgxTx, task.SessionID)
}

func markTurnSucceededForRequest(ctx context.Context, pgxTx *gorm.DB, requestID, conversationID string) error {
	now := time.Now().UTC()
	if err := pgxTx.WithContext(ctx).Model(&schema.AgentTurnProjections{}).Where("workflow_run_request_id = ?", requestID).Updates(map[string]any{
		"status":      "succeeded",
		"finished_at": gorm.Expr("COALESCE(finished_at, ?)", now),
		"updated_at":  now,
	}).Error; err != nil {
		return err
	}
	if err := resolveWorkflowRequestApproval(ctx, pgxTx, requestID, "confirmed"); err != nil {
		return err
	}
	return pgxTx.WithContext(ctx).Model(&schema.AgentConversations{}).Where("id = ?", conversationID).Updates(map[string]any{
		"status":     "completed",
		"updated_at": now,
	}).Error
}

func markTurnCanceledForRequest(ctx context.Context, pgxTx *gorm.DB, requestID, conversationID string) error {
	now := time.Now().UTC()
	if err := pgxTx.WithContext(ctx).Model(&schema.AgentTurnProjections{}).Where("workflow_run_request_id = ?", requestID).Updates(map[string]any{
		"status":      "canceled",
		"finished_at": gorm.Expr("COALESCE(finished_at, ?)", now),
		"updated_at":  now,
	}).Error; err != nil {
		return err
	}
	return pgxTx.WithContext(ctx).Model(&schema.AgentConversations{}).Where("id = ?", conversationID).Updates(map[string]any{
		"status":     "canceled",
		"updated_at": now,
	}).Error
}

func resolveWorkflowRequestApproval(ctx context.Context, pgxTx *gorm.DB, requestID, decision string) error {
	projectionID, err := projectionIDForWorkflowRequest(ctx, pgxTx, requestID)
	if err != nil || projectionID == "" {
		return err
	}
	approvalID, kind, err := latestApprovalRequest(ctx, pgxTx, projectionID, "workflow_run")
	if err != nil {
		return err
	}
	if approvalID == "" {
		approvalID = requestID
		kind = "workflow_run"
	}
	return appendApprovalResolved(ctx, pgxTx, projectionID, approvalID, kind, decision, map[string]any{
		"request_id": requestID,
	})
}

// lockProjectionForLinkedDraft 在 Draft 确认写 conversation 之前锁住关联 projection。
// 与 AppendEvents 的 projection → conversation 顺序对齐，避免 conversation → projection 死锁。
func lockProjectionForLinkedDraft(ctx context.Context, pgxTx *gorm.DB, conversationID string) error {
	var draft schema.LibraryOrganizationDrafts
	err := pgxTx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Select("current_revision_id").
		Where("conversation_id = ?", conversationID).Take(&draft).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if draft.CurrentRevisionID == nil || *draft.CurrentRevisionID == "" {
		return nil
	}
	var proj schema.AgentTurnProjections
	err = pgxTx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Select("id").
		Where("library_organization_draft_revision_id = ?", *draft.CurrentRevisionID).
		Order("created_at DESC, id DESC").
		Take(&proj).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	return err
}

// completeOrganizationDraftTask 在用户确认图库整理 Draft 后，把仍 awaiting_confirmation 的全局 Task 标 succeeded，并向 journal 写 approval/resolved。
//
// 这是全局整理 Goal 的用户完成路径，不是商品 goal_loop。找不到关联 Turn 则只跳过。写 agent_tasks、agent_turn_events、conversation。
func completeOrganizationDraftTask(ctx context.Context, pgxTx *gorm.DB, draft library.OrganizationDraft) error {
	if draft.CurrentRevision == nil {
		return nil
	}
	// 确认路径与 turn/end 投影路径统一为 projection -> task。
	// 先锁 projection，避免持 task 等 projection 与 live writer 形成环。
	var proj schema.AgentTurnProjections
	err := pgxTx.Clauses(pfdb.ForUpdate()).Select("id, task_id").
		Where("library_organization_draft_revision_id = ?", draft.CurrentRevision.ID).
		Order("created_at DESC, id DESC").
		Take(&proj).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if proj.TaskID == nil || *proj.TaskID == "" {
		return nil
	}
	task, err := lockTask(ctx, pgxTx, *proj.TaskID)
	if err != nil {
		return err
	}
	if task.Status != "awaiting_confirmation" {
		return nil
	}
	now := time.Now().UTC()
	if err := pgxTx.Model(&schema.AgentTasks{}).Where("id = ?", task.ID).Updates(map[string]any{
		"status":         "succeeded",
		"waiting_reason": nil,
		"failure_reason": nil,
		"finished_at":    now,
		"updated_at":     now,
	}).Error; err != nil {
		return err
	}
	projectionID := proj.ID
	if projectionID != "" {
		approvalID, kind, err := latestApprovalRequest(ctx, pgxTx, projectionID, "artifact")
		if err != nil {
			return err
		}
		if approvalID == "" {
			approvalID = draft.CurrentRevision.ID
			kind = "artifact"
		}
		if err := appendApprovalResolved(ctx, pgxTx, projectionID, approvalID, kind, "confirmed", map[string]any{
			"revision_id": draft.CurrentRevision.ID,
		}); err != nil {
			return err
		}
		if err := pgxTx.Model(&schema.AgentTurnProjections{}).Where("id = ?", projectionID).Updates(map[string]any{
			"status":      "succeeded",
			"finished_at": gorm.Expr("COALESCE(finished_at, ?)", now),
			"updated_at":  now,
		}).Error; err != nil {
			return err
		}
	}
	if task.ConversationID != nil {
		if err := pgxTx.Model(&schema.AgentConversations{}).Where("id = ?", *task.ConversationID).Updates(map[string]any{
			"status":     "completed",
			"updated_at": now,
		}).Error; err != nil {
			return err
		}
	}
	publishTaskChanged(pgxTx, task.ID, task.SessionID)
	return refreshSessionSummary(ctx, pgxTx, task.SessionID)
}

// parkTaskAfterCancelledRunRequest 在用户取消尚未提交 GraphRun 的确认单后安置 Task：商品工作流停在 goal_loop，全局 Task 标 canceled。
//
// 用户已拥有的终态/暂停不覆盖。写 agent_tasks。
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
		var conv schema.AgentConversations
		_ = pgxTx.Select("scope_type").Where("id = ?", *task.ConversationID).Take(&conv).Error
		keepGoal = conv.ScopeType == "product_workflow"
	}
	now := time.Now().UTC()
	if keepGoal {
		if err := pgxTx.Model(&schema.AgentTasks{}).Where("id = ?", task.ID).Updates(map[string]any{
			"status":         "waiting_user",
			"waiting_reason": "goal_loop",
			"failure_reason": nil,
			"finished_at":    nil,
			"updated_at":     now,
		}).Error; err != nil {
			return err
		}
		return refreshSessionSummary(ctx, pgxTx, task.SessionID)
	}
	if err := pgxTx.Model(&schema.AgentTasks{}).Where("id = ?", task.ID).Updates(map[string]any{
		"status":         "canceled",
		"waiting_reason": nil,
		"canceled_at":    now,
		"finished_at":    now,
		"updated_at":     now,
	}).Error; err != nil {
		return err
	}
	return refreshSessionSummary(ctx, pgxTx, task.SessionID)
}
