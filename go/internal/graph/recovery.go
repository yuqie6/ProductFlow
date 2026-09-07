package graph

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/auth"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/metrics"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"github.com/yuqie6/productflow/internal/quota"
	"gorm.io/gorm"
)

const (
	defaultStaleRunningAfter = 30 * time.Minute
	graphRecoveryBatchLimit  = 25
)

// RecoverySummary 统计补回 dispatch、标 unknown 与仍 queued 的 GraphRun。
type RecoverySummary struct {
	QueuedRuns       int `json:"queued_runs"`        // 仍 active、需补 dispatch 的 run 数
	StaleRunningRuns int `json:"stale_running_runs"` // 过期 running 节点被重新 queued
	EnqueuedRuns     int `json:"enqueued_runs"`      // RestageIfIdle 实际补回 PENDING 的次数
	// UnknownRuns 是过期且已打 provider、被标 unknown 的 run 数；unknown 不可经 RetryRun 重试。
	UnknownRuns int  `json:"unknown_runs"`
	HasMore     bool `json:"has_more"` // unlocked snapshot 仍有未处理候选
}

type graphRunRecoverResult struct {
	restage bool
	queued  bool
	stale   bool
	unknown bool
}

// RecoverUnfinishedGraphRuns 把仍 active 的图运行补回 PENDING dispatch。过期且已打 provider 的节点标 unknown。
// GORM 打开或写库失败原样返回。无法证明的供应商结果标 unknown，不得当失败自动重试。
// 单条聚合失败计入返回 error，不回滚本轮已提交的其它 run。
func RecoverUnfinishedGraphRuns(ctx context.Context, pool *pgxpool.Pool, staleAfter time.Duration, products ProductGuard) (RecoverySummary, error) {
	return recoverUnfinishedGraphRuns(ctx, pool, staleAfter, products, graphRecoveryBatchLimit)
}

func recoverUnfinishedGraphRuns(ctx context.Context, pool *pgxpool.Pool, staleAfter time.Duration, products ProductGuard, limit int) (RecoverySummary, error) {
	if staleAfter <= 0 {
		staleAfter = defaultStaleRunningAfter
	}
	if limit <= 0 {
		limit = graphRecoveryBatchLimit
	}
	ctx = WithProductGuard(ctx, products)
	gdb, err := pfdb.OpenGorm(pool)
	if err != nil {
		return RecoverySummary{}, err
	}
	cutoff := time.Now().UTC().Add(-staleAfter)
	runningIDs, queuedGraphIDs, hasMore, err := discoverGraphRecoveryCandidates(ctx, gdb, cutoff, limit)
	if err != nil {
		return RecoverySummary{}, err
	}
	summary := RecoverySummary{HasMore: hasMore}
	var errs []error
	for _, runID := range runningIDs {
		result, recErr := recoverGraphRunState(ctx, gdb, runID, cutoff)
		if recErr != nil {
			errs = append(errs, fmt.Errorf("graph run %s: %w", runID, recErr))
			continue
		}
		if result.queued {
			summary.QueuedRuns++
		}
		if result.stale {
			summary.StaleRunningRuns++
		}
		if result.unknown {
			summary.UnknownRuns++
		}
		if !result.restage {
			continue
		}
		changed, restageErr := restageGraphRun(ctx, gdb, runID)
		if restageErr != nil {
			errs = append(errs, fmt.Errorf("graph run %s restage: %w", runID, restageErr))
			continue
		}
		if changed {
			summary.EnqueuedRuns++
		}
	}
	for _, graphID := range queuedGraphIDs {
		if recErr := tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
			return promoteNextQueuedRun(ctx, pgxTx, graphID)
		}); recErr != nil {
			errs = append(errs, fmt.Errorf("graph %s promote: %w", graphID, recErr))
		}
	}
	return summary, errors.Join(errs...)
}

func discoverGraphRecoveryCandidates(ctx context.Context, gdb *gorm.DB, cutoff time.Time, limit int) (runningIDs, queuedGraphIDs []string, hasMore bool, err error) {
	err = tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		candidateScanStarted := time.Now()
		scanErr := graphRunningRecoveryScope(pgxTx.WithContext(ctx), cutoff).
			Select("workflow_graph_runs.id").
			Order("workflow_graph_runs.started_at ASC, workflow_graph_runs.id ASC").
			Limit(limit+1).
			Pluck("id", &runningIDs).Error
		metrics.ObserveRecoveryLock("graph", time.Since(candidateScanStarted))
		if scanErr != nil {
			return scanErr
		}
		if len(runningIDs) > limit {
			hasMore = true
			runningIDs = runningIDs[:limit]
			return nil
		}
		remaining := limit - len(runningIDs)
		if remaining == 0 {
			var probe []string
			if err := graphQueuedPromotionScope(pgxTx.WithContext(ctx)).Limit(1).Pluck("graph_id", &probe).Error; err != nil {
				return err
			}
			hasMore = len(probe) > 0
			return nil
		}
		if err := graphQueuedPromotionScope(pgxTx.WithContext(ctx)).Limit(remaining+1).Pluck("graph_id", &queuedGraphIDs).Error; err != nil {
			return err
		}
		if len(queuedGraphIDs) > remaining {
			hasMore = true
			queuedGraphIDs = queuedGraphIDs[:remaining]
		}
		return nil
	})
	return runningIDs, queuedGraphIDs, hasMore, err
}

func graphQueuedPromotionScope(tx *gorm.DB) *gorm.DB {
	return tx.Model(&schema.WorkflowGraphRuns{}).
		Where("status = ?", RunStatusQueued).
		Where("NOT EXISTS (SELECT 1 FROM workflow_graph_runs active WHERE active.graph_id = workflow_graph_runs.graph_id AND active.status = ?)", RunStatusRunning).
		Distinct("graph_id").
		Order("graph_id ASC")
}

func graphRunningRecoveryScope(tx *gorm.DB, cutoff time.Time) *gorm.DB {
	return tx.Model(&schema.WorkflowGraphRuns{}).Where(`workflow_graph_runs.status = ?
			  AND (workflow_graph_runs.execution_lease_expires_at IS NULL OR workflow_graph_runs.execution_lease_expires_at <= NOW())
			  AND (
				EXISTS (
					SELECT 1 FROM workflow_graph_node_runs n
					WHERE n.graph_run_id = workflow_graph_runs.id
					  AND n.status = ?
					  AND COALESCE(n.progress_updated_at, n.started_at) <= ?
				)
				OR (
					NOT EXISTS (
						SELECT 1 FROM workflow_graph_node_runs n
						WHERE n.graph_run_id = workflow_graph_runs.id AND n.status = ?
					)
					AND (
						EXISTS (
							SELECT 1 FROM workflow_graph_node_runs n
							WHERE n.graph_run_id = workflow_graph_runs.id AND n.status = ?
						)
						OR (
							EXISTS (
								SELECT 1 FROM workflow_graph_node_runs n
								WHERE n.graph_run_id = workflow_graph_runs.id
							)
							AND NOT EXISTS (
								SELECT 1 FROM workflow_graph_node_runs n
								WHERE n.graph_run_id = workflow_graph_runs.id AND n.status IN ?
							)
						)
					)
					AND NOT EXISTS (
						SELECT 1 FROM async_dispatches d
						WHERE d.delivery_key = ? || ':' || workflow_graph_runs.id
						  AND d.status IN ?
					)
				)
			)`, RunStatusRunning, NodeRunRunning, cutoff, NodeRunRunning, NodeRunQueued,
		[]string{NodeRunQueued, NodeRunRunning}, queue.ActorGraphRun, []string{queue.StatusPending, queue.StatusSent, queue.StatusDead})
}

func recoverGraphRunState(ctx context.Context, gdb *gorm.DB, runID string, cutoff time.Time) (graphRunRecoverResult, error) {
	var result graphRunRecoverResult
	type quotaAction struct {
		nodeRunID string
		attemptID string
		unknown   bool
	}
	var actions []quotaAction
	var merchantID string
	err := tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		run, locked, err := loadGraphRunSkipLocked(ctx, pgxTx, runID)
		if err != nil {
			return err
		}
		if !locked {
			return nil
		}
		if run.Status != RunStatusRunning {
			return nil
		}
		if run.ExecutionLeaseExpiresAt != nil && run.ExecutionLeaseExpiresAt.After(time.Now().UTC()) {
			return nil
		}
		state := classifyDelivery(run)
		if state == "queued" {
			result.queued = true
			result.restage = true
			return nil
		}
		if state != "running" {
			return nil
		}
		var stale []graphNodeRunRow
		for _, node := range run.NodeRuns {
			if node.Status != NodeRunRunning {
				continue
			}
			stamp := node.StartedAt
			if node.ProgressUpdated != nil {
				stamp = *node.ProgressUpdated
			}
			if !stamp.After(cutoff) {
				stale = append(stale, node)
			}
		}
		if len(stale) == 0 {
			return nil
		}
		if scanErr := pgxTx.WithContext(ctx).Raw(`
			SELECT p.merchant_id
			FROM workflow_graph_runs r
			JOIN workflow_graphs g ON g.id = r.graph_id
			JOIN products p ON p.id = g.product_id
			WHERE r.id = ?`, runID).Scan(&merchantID).Error; scanErr != nil {
			return scanErr
		}
		markedUnknown := false
		requeued := false
		now := time.Now().UTC()
		for _, node := range stale {
			attemptID := ""
			if node.ActiveAttemptID != nil {
				attemptID = *node.ActiveAttemptID
			}
			if nodeSafeToRequeue(node) {
				update := pgxTx.WithContext(ctx).Model(&schema.WorkflowGraphNodeRuns{}).
					Where("id = ? AND status = ?", node.ID, "running").
					Updates(map[string]any{
						"status":              "queued",
						"active_attempt_id":   nil,
						"failure_reason":      nil,
						"finished_at":         nil,
						"progress_phase":      "requeued_after_idle",
						"progress_updated_at": now,
					})
				if update.Error != nil {
					return update.Error
				}
				if update.RowsAffected == 1 {
					if err := appendGraphRunEventLocked(ctx, pgxTx, run.ID, "node.progress", &node.ID, map[string]any{
						"status": "queued", "phase": "requeued_after_idle", "reason": "stale_worker",
					}); err != nil {
						return err
					}
					requeued = true
					actions = append(actions, quotaAction{nodeRunID: node.ID, attemptID: attemptID, unknown: false})
				}
				_ = pgxTx.WithContext(ctx).Where("node_run_id = ? AND effect_result = ?", node.ID, "pending").
					Delete(&schema.WorkflowGraphProviderEffects{}).Error
				continue
			}
			if err := markNodeUnknown(ctx, pgxTx, run.ID, node.ID, node.ActiveAttemptID, ProviderUnknownDetail); err != nil {
				return err
			}
			markedUnknown = true
			actions = append(actions, quotaAction{nodeRunID: node.ID, attemptID: attemptID, unknown: true})
		}
		if _, err := completeGraphRunIfNodesTerminal(ctx, pgxTx, run.ID); err != nil {
			return err
		}
		var statusRec schema.WorkflowGraphRuns
		if err := pgxTx.WithContext(ctx).Select("status").Where("id = ?", run.ID).Take(&statusRec).Error; err != nil {
			return err
		}
		if isTerminalRun(statusRec.Status) {
			result.unknown = markedUnknown
			return nil
		}
		result.restage = true
		result.unknown = markedUnknown
		result.stale = requeued
		return nil
	})
	if err != nil {
		return result, err
	}
	svc := &quota.Service{DB: gdb}
	for _, action := range actions {
		key := imageNodeQuotaKey(action.nodeRunID, action.attemptID)
		if action.unknown {
			_ = finalizeQuotaIgnoreMissing(svc.MarkUnknown(ctx, merchantID, key))
			continue
		}
		_ = finalizeQuotaIgnoreMissing(svc.Release(ctx, merchantID, key))
	}
	return result, nil
}

func restageGraphRun(ctx context.Context, gdb *gorm.DB, runID string) (bool, error) {
	var changed bool
	err := tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		var rec schema.WorkflowGraphRuns
		err := pgxTx.WithContext(ctx).Select("id", "status", "graph_id").Where("id = ?", runID).Take(&rec).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if rec.Status != RunStatusRunning {
			return nil
		}
		var merchantID string
		if scanErr := pgxTx.WithContext(ctx).Raw(`
			SELECT p.merchant_id
			FROM workflow_graphs g
			JOIN products p ON p.id = g.product_id
			WHERE g.id = ?`, rec.GraphID).Scan(&merchantID).Error; scanErr != nil {
			return scanErr
		}
		restageCtx := auth.WithMerchantID(ctx, merchantID)
		var restageErr error
		changed, restageErr = queue.RestageIfIdle(restageCtx, pgxTx, queue.ActorGraphRun, runID, nil)
		return restageErr
	})
	return changed, err
}

func loadGraphRunSkipLocked(ctx context.Context, tx *gorm.DB, runID string) (graphRunRow, bool, error) {
	var rec schema.WorkflowGraphRuns
	err := tx.WithContext(ctx).Clauses(pfdb.SkipLocked()).Where("id = ?", runID).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return graphRunRow{}, false, nil
	}
	if err != nil {
		return graphRunRow{}, false, err
	}
	run := graphRunFromSchema(rec)
	nodes, err := loadNodeRuns(ctx, tx, run.ID)
	if err != nil {
		return graphRunRow{}, false, err
	}
	run.NodeRuns = nodes
	return run, true, nil
}

// classifyDelivery 决定 recovery 对仍 running 的 run 补 dispatch 还是只 promote。
// 有 running 节点返回 running；只有 queued 或节点已全终态返回 queued；其余 none。
// 不要在这里标 unknown——过期且已打 provider 的节点由 RecoverUnfinishedGraphRuns 另标。
func classifyDelivery(run graphRunRow) string {
	if run.Status != RunStatusRunning {
		return "none"
	}
	hasRunning := false
	hasQueued := false
	allTerminal := len(run.NodeRuns) > 0
	for _, node := range run.NodeRuns {
		switch node.Status {
		case NodeRunRunning:
			hasRunning = true
			allTerminal = false
		case NodeRunQueued:
			hasQueued = true
			allTerminal = false
		case NodeRunSucceeded, NodeRunFailed, NodeRunUnknown, NodeRunSkipped, NodeRunCancelled:
		default:
			allTerminal = false
		}
	}
	if hasRunning {
		return "running"
	}
	if hasQueued || allTerminal {
		return "queued"
	}
	return "none"
}

func nodeSafeToRequeue(node graphNodeRunRow) bool {
	if node.Status != NodeRunRunning {
		return false
	}
	if node.ProgressPhase == nil {
		return true
	}
	phase := *node.ProgressPhase
	return phase == "claimed" || phase == "prepared"
}
