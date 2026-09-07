package graph

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/generation"
	"github.com/yuqie6/productflow/internal/platform/metrics"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

const generationCapacityLockKey = 42630001

// generationMaxConcurrent 读 app_settings.generation_max_concurrent_tasks。
// 空值或非正整数回落到 3；结果夹在 1–20。与连续生图共用同一把容量锁，改上限两边一起生效。
func generationMaxConcurrent(ctx context.Context, q *gorm.DB) int {
	n, _ := generation.LoadMaxConcurrent(ctx, q)
	return n
}

func runningGenerationCount(ctx context.Context, tx *gorm.DB) (int, error) {
	return generation.CountAdmissionRunning(ctx, tx)
}

// GenerationCapacityAvailable 与连续生图共用同一把容量锁。
// advisory lock 或计数查询失败原样返回；容量满返回 false, nil，不当错误。
// 导出入口供 ImageSession claim 使用，denied 记在 domain=imagesession。
func GenerationCapacityAvailable(ctx context.Context, tx *gorm.DB) (bool, error) {
	return generationCapacityAvailable(ctx, tx, "imagesession")
}

func generationCapacityAvailable(ctx context.Context, tx *gorm.DB, domain string) (bool, error) {
	lockStarted := time.Now()
	err := tx.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(?)", generationCapacityLockKey).Error
	metrics.ObserveAdvisoryLockWait(time.Since(lockStarted))
	if err != nil {
		return false, err
	}
	limit := generationMaxConcurrent(ctx, tx)
	count, err := runningGenerationCount(ctx, tx)
	if err != nil {
		return false, err
	}
	if count >= limit {
		metrics.ObserveGenerationAdmissionDenied(domain)
		return false, nil
	}
	return true, nil
}

var (
	errNotClaimed      = errors.New("not claimed")
	errWaitingCapacity = errors.New("waiting_for_capacity")
)

// claimQueuedNodeRun 把 queued 节点标 running 并分配 attempt_id。
// 先查容量（pg_advisory_xact_lock），再按 run → node 加 FOR UPDATE，避免与取消死锁。
// 未抢到返回 false, ""（不当失败）；容量满返回 errWaitingCapacity。
// 副作用：workflow_graph_node_runs + node.claimed / node.started 事件。不要先锁 node 再锁 run。
func claimQueuedNodeRun(ctx context.Context, gdb *gorm.DB, nodeRunID string) (bool, string, error) {
	claimed := false
	claimedAttemptID := ""
	err := tx.WithGorm(ctx, gdb, func(dbTx *gorm.DB) error {
		ok, err := generationCapacityAvailable(ctx, dbTx, "graph")
		if err != nil {
			return err
		}
		if !ok {
			return errWaitingCapacity
		}
		var ref schema.WorkflowGraphNodeRuns
		if err := dbTx.WithContext(ctx).Select("graph_run_id").Where("id = ?", nodeRunID).Take(&ref).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errNotClaimed
			}
			return err
		}
		// 所有会写 run event 的节点状态迁移都按 run → node 加锁。
		// claim 必须同一顺序，避免取消与 worker 形成 run/node 死锁。
		var run schema.WorkflowGraphRuns
		if err := dbTx.WithContext(ctx).Clauses(pfdb.ForUpdate()).
			Select("id", "status", "execution_lease_token", "execution_lease_expires_at").Where("id = ?", ref.GraphRunID).Take(&run).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errNotClaimed
			}
			return err
		}
		if run.Status != RunStatusRunning {
			return errNotClaimed
		}
		if err := graphRunLeaseSchemaOwned(ctx, run); err != nil {
			return err
		}
		now := time.Now().UTC()
		attemptID := clockid.New()
		res := dbTx.WithContext(ctx).Model(&schema.WorkflowGraphNodeRuns{}).
			Where("id = ? AND status = ?", nodeRunID, "queued").
			Updates(map[string]any{
				"status":              "running",
				"active_attempt_id":   attemptID,
				"attempt_count":       gorm.Expr("attempt_count + 1"),
				"progress_phase":      "claimed",
				"progress_updated_at": now,
				"started_at":          now,
				"failure_reason":      nil,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errNotClaimed
		}
		var node schema.WorkflowGraphNodeRuns
		if err := dbTx.WithContext(ctx).Select("graph_run_id", "node_id", "attempt_count").Where("id = ?", nodeRunID).Take(&node).Error; err != nil {
			return err
		}
		if err := appendGraphRunEventLocked(ctx, dbTx, node.GraphRunID, "node.claimed", &nodeRunID, map[string]any{
			"status": NodeRunRunning, "node_id": node.NodeID, "attempt_id": attemptID, "attempt_count": node.AttemptCount,
		}); err != nil {
			return err
		}
		if err := appendGraphRunEventLocked(ctx, dbTx, node.GraphRunID, "node.started", &nodeRunID, map[string]any{
			"status": NodeRunRunning, "node_id": node.NodeID, "attempt_id": attemptID, "attempt_count": node.AttemptCount,
		}); err != nil {
			return err
		}
		claimed = true
		claimedAttemptID = attemptID
		return nil
	})
	if errors.Is(err, errNotClaimed) {
		return false, "", nil
	}
	if errors.Is(err, errWaitingCapacity) {
		return false, "", errWaitingCapacity
	}
	return claimed, claimedAttemptID, err
}

func loadNodeRunStatuses(ctx context.Context, tx *gorm.DB, runID string) ([]string, []string, error) {
	var recs []schema.WorkflowGraphNodeRuns
	if err := tx.WithContext(ctx).Select("status", "failure_reason").Where("graph_run_id = ?", runID).Find(&recs).Error; err != nil {
		return nil, nil, err
	}
	var statuses []string
	var reasons []string
	for _, rec := range recs {
		statuses = append(statuses, rec.Status)
		if rec.FailureReason != nil {
			reasons = append(reasons, *rec.FailureReason)
		} else {
			reasons = append(reasons, "")
		}
	}
	return statuses, reasons, nil
}

func terminalNodeRunUpdates(status string, now time.Time) map[string]any {
	return map[string]any{
		"status":              status,
		"finished_at":         now,
		"active_attempt_id":   nil,
		"progress_phase":      nil,
		"progress_updated_at": now,
	}
}

// aggregateGraphRunTerminalStatus 优先 unknown（不可重试），其次 failed（可重试），再 cancelled。
func aggregateGraphRunTerminalStatus(statuses, reasons []string) (string, *string, bool) {
	for i, status := range statuses {
		if status != NodeRunUnknown {
			continue
		}
		msg := reasons[i]
		if msg == "" {
			msg = ProviderUnknownDetail
		}
		if len(msg) > 1000 {
			msg = msg[:1000]
		}
		return RunStatusUnknown, &msg, false
	}
	for i, status := range statuses {
		if status != NodeRunFailed {
			continue
		}
		msg := reasons[i]
		if msg == "" {
			msg = "节点运行失败"
		}
		if len(msg) > 1000 {
			msg = msg[:1000]
		}
		return RunStatusFailed, &msg, true
	}
	for _, status := range statuses {
		if status == NodeRunCancelled {
			msg := GraphCancelledReason
			return RunStatusCancelled, &msg, false
		}
	}
	return RunStatusSucceeded, nil, false
}

// completeGraphRunIfNodesTerminal 在所有节点离开 queued/running 后收口 run。
// 聚合顺序：unknown（不可重试）> failed（可重试）> cancelled > succeeded。
// 写 workflow_graph_runs 终态、run.* 事件，并 promote 下一条 queued。仍有 queued/running 返回 false。
// 调用方须已持事务；不要在这里把 unknown 改成 failed。
func completeGraphRunIfNodesTerminal(ctx context.Context, tx *gorm.DB, runID string) (bool, error) {
	var run schema.WorkflowGraphRuns
	err := tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Select("id", "graph_id", "status", "execution_lease_token", "execution_lease_expires_at").Where("id = ?", runID).Take(&run).Error
	if err != nil || run.Status != RunStatusRunning {
		return false, err
	}
	if err := graphRunLeaseSchemaOwned(ctx, run); err != nil {
		return false, err
	}
	statuses, reasons, err := loadNodeRunStatuses(ctx, tx, runID)
	if err != nil {
		return false, err
	}
	if len(statuses) == 0 {
		return false, nil
	}
	for _, st := range statuses {
		if st == NodeRunQueued || st == NodeRunRunning {
			return false, nil
		}
	}
	now := time.Now().UTC()
	runStatus, failure, retryable := aggregateGraphRunTerminalStatus(statuses, reasons)
	result := tx.WithContext(ctx).Model(&schema.WorkflowGraphRuns{}).
		Where("id = ? AND status = ?", runID, "running").
		Updates(map[string]any{
			"status":         runStatus,
			"failure_reason": failure,
			"is_retryable":   retryable,
			"finished_at":    now,
		})
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected != 1 {
		return false, nil
	}
	runEventKind := "run.completed"
	switch runStatus {
	case RunStatusFailed:
		runEventKind = "run.failed"
	case RunStatusCancelled:
		runEventKind = "run.cancelled"
	case RunStatusUnknown:
		runEventKind = "run.unknown"
	}
	if err := appendGraphRunEventLocked(ctx, tx, runID, runEventKind, nil, map[string]any{
		"status": runStatus, "failure_reason": failure,
	}); err != nil {
		return false, err
	}
	if err := promoteNextQueuedRun(ctx, tx, run.GraphID); err != nil {
		return false, err
	}
	return true, nil
}

// failGraphRunLocked 假定调用方已 FOR UPDATE 住 run：把仍 queued/running 的节点标 failed，
// run 标 failed + is_retryable=true，再 promote queued。不检查 provider 边界——已打过 provider
// 的节点必须先走 markNodeUnknown，否则会把无法证明的结果当可重试失败。
func failGraphRunLocked(ctx context.Context, tx *gorm.DB, runID, reason string) error {
	now := time.Now().UTC()
	if len(reason) > 1000 {
		reason = reason[:1000]
	}
	var nodeRuns []schema.WorkflowGraphNodeRuns
	if err := tx.WithContext(ctx).Select("id", "node_id", "active_attempt_id").Clauses(pfdb.ForUpdate()).Where("graph_run_id = ? AND status IN ?", runID, []string{"queued", "running"}).Find(&nodeRuns).Error; err != nil {
		return err
	}
	merchantID, err := merchantIDForGraphRun(ctx, tx, runID)
	if err != nil {
		return err
	}
	for _, node := range nodeRuns {
		updates := terminalNodeRunUpdates(NodeRunFailed, now)
		updates["failure_reason"] = reason
		result := tx.WithContext(ctx).Model(&schema.WorkflowGraphNodeRuns{}).
			Where("id = ? AND status IN ?", node.ID, []string{"queued", "running"}).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			continue
		}
		attemptID := ""
		if node.ActiveAttemptID != nil {
			attemptID = *node.ActiveAttemptID
		}
		if err := (Executor{DB: tx}).releaseImageQuota(ctx, merchantID, node.ID, attemptID); err != nil {
			return err
		}
		if err := appendGraphRunEventLocked(ctx, tx, runID, "node.failed", &node.ID, map[string]any{
			"status": NodeRunFailed, "node_id": node.NodeID, "reason": reason,
		}); err != nil {
			return err
		}
	}
	result := tx.WithContext(ctx).Model(&schema.WorkflowGraphRuns{}).
		Where("id = ? AND status = ?", runID, "running").
		Updates(map[string]any{
			"status":         "failed",
			"failure_reason": reason,
			"finished_at":    now,
			"is_retryable":   true,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return nil
	}
	if err := appendGraphRunEventLocked(ctx, tx, runID, "run.failed", nil, map[string]any{
		"status": RunStatusFailed, "reason": reason,
	}); err != nil {
		return err
	}
	var rec schema.WorkflowGraphRuns
	if err := tx.WithContext(ctx).Select("graph_id").Where("id = ?", runID).Take(&rec).Error; err != nil {
		return err
	}
	return promoteNextQueuedRun(ctx, tx, rec.GraphID)
}

func nodePastProviderBoundary(phase *string) bool {
	if phase == nil {
		return false
	}
	return *phase == "provider_call" || *phase == "provider_result_received"
}

// failClaimedNode 收口一个已 claim 节点的证明失败。锁序 run → node。
// 已过 provider_call / provider_result_received 边界则标 unknown，禁止当 failed 重试。
// attempt 不匹配、节点已终态、run 已终态：只尝试 complete，不改状态。
// 写 node.failed 事件后调用 completeGraphRunIfNodesTerminal。
func failClaimedNode(ctx context.Context, gdb *gorm.DB, runID, nodeRunID, expectedAttemptID, reason string) error {
	return tx.WithGorm(ctx, gdb, func(dbTx *gorm.DB) error {
		var run schema.WorkflowGraphRuns
		err := dbTx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Where("id = ?", runID).Take(&run).Error
		if err != nil {
			return err
		}
		if isTerminalRun(run.Status) {
			return nil
		}
		if err := graphRunLeaseSchemaOwned(ctx, run); err != nil {
			return err
		}
		var node schema.WorkflowGraphNodeRuns
		err = dbTx.WithContext(ctx).Clauses(pfdb.ForUpdate()).
			Where("id = ? AND graph_run_id = ?", nodeRunID, runID).
			Take(&node).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			_, err = completeGraphRunIfNodesTerminal(ctx, dbTx, runID)
			return err
		}
		if err != nil {
			return err
		}
		if node.Status == NodeRunFailed || node.Status == NodeRunUnknown || node.Status == NodeRunSucceeded || node.Status == NodeRunSkipped || node.Status == NodeRunCancelled {
			_, err = completeGraphRunIfNodesTerminal(ctx, dbTx, runID)
			return err
		}
		if expectedAttemptID == "" || node.ActiveAttemptID == nil || *node.ActiveAttemptID != expectedAttemptID {
			_, err = completeGraphRunIfNodesTerminal(ctx, dbTx, runID)
			return err
		}
		if nodePastProviderBoundary(node.ProgressPhase) {
			detail := strings.TrimSpace(reason)
			if detail == "" {
				detail = ProviderUnknownDetail
			}
			attemptID := expectedAttemptID
			if err := markNodeUnknown(ctx, dbTx, runID, nodeRunID, &attemptID, detail); err != nil {
				return err
			}
			_, err = completeGraphRunIfNodesTerminal(ctx, dbTx, runID)
			return err
		}
		if node.Status != NodeRunQueued && node.Status != NodeRunRunning {
			_, err = completeGraphRunIfNodesTerminal(ctx, dbTx, runID)
			return err
		}
		now := time.Now().UTC()
		if len(reason) > 1000 {
			reason = reason[:1000]
		}
		updates := terminalNodeRunUpdates(NodeRunFailed, now)
		updates["failure_reason"] = reason
		result := dbTx.WithContext(ctx).Model(&schema.WorkflowGraphNodeRuns{}).Where("id = ? AND status IN ? AND active_attempt_id = ?", nodeRunID, []string{"queued", "running"}, expectedAttemptID).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			_, err = completeGraphRunIfNodesTerminal(ctx, dbTx, runID)
			return err
		}
		merchantID, err := merchantIDForGraphRun(ctx, dbTx, runID)
		if err != nil {
			return err
		}
		if err := (Executor{DB: dbTx}).releaseImageQuota(ctx, merchantID, nodeRunID, expectedAttemptID); err != nil {
			return err
		}
		if err := appendGraphRunEventLocked(ctx, dbTx, runID, "node.failed", &nodeRunID, map[string]any{
			"status": NodeRunFailed, "node_id": node.NodeID, "reason": reason,
		}); err != nil {
			return err
		}
		_, err = completeGraphRunIfNodesTerminal(ctx, dbTx, runID)
		return err
	})
}

// failGraphRun 是 ExecuteRun 在已证明失败时的收口。先锁 run：若有 running 且已过 provider 边界的节点，
// 标 unknown 再 complete；否则 failGraphRunLocked。run 已终态直接返回 nil。
func failGraphRun(ctx context.Context, gdb *gorm.DB, runID, reason string) error {
	return tx.WithGorm(ctx, gdb, func(dbTx *gorm.DB) error {
		var run schema.WorkflowGraphRuns
		err := dbTx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Where("id = ?", runID).Take(&run).Error
		if err != nil {
			return err
		}
		if isTerminalRun(run.Status) {
			return nil
		}
		if err := graphRunLeaseSchemaOwned(ctx, run); err != nil {
			return err
		}
		var nodes []schema.WorkflowGraphNodeRuns
		if err := dbTx.WithContext(ctx).
			Select("id", "status", "progress_phase", "active_attempt_id").
			Where("graph_run_id = ?", runID).
			Find(&nodes).Error; err != nil {
			return err
		}
		var boundaryID string
		var boundaryAttempt *string
		found := false
		for _, node := range nodes {
			if node.Status == NodeRunRunning && nodePastProviderBoundary(node.ProgressPhase) {
				boundaryID = node.ID
				boundaryAttempt = node.ActiveAttemptID
				found = true
				break
			}
		}
		if found {
			if err := markNodeUnknown(ctx, dbTx, runID, boundaryID, boundaryAttempt, ProviderUnknownDetail); err != nil {
				return err
			}
			_, err = completeGraphRunIfNodesTerminal(ctx, dbTx, runID)
			return err
		}
		return failGraphRunLocked(ctx, dbTx, runID, reason)
	})
}
