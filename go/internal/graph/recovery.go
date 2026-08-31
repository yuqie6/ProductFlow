package graph

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

const defaultStaleRunningAfter = 30 * time.Minute

// RecoverySummary 统计补回 dispatch、标 unknown 与仍 queued 的 GraphRun。
type RecoverySummary struct {
	QueuedRuns       int `json:"queued_runs"`        // 仍 active、需补 dispatch 的 run 数
	StaleRunningRuns int `json:"stale_running_runs"` // 过期 running 节点被重新 queued
	EnqueuedRuns     int `json:"enqueued_runs"`      // RestageIfIdle 实际补回 PENDING 的次数
	// UnknownRuns 是过期且已打 provider、被标 unknown 的 run 数；unknown 不可经 RetryRun 重试。
	UnknownRuns int `json:"unknown_runs"`
}

// RecoverUnfinishedGraphRuns 把仍 active 的图运行补回 PENDING dispatch。过期且已打 provider 的节点标 unknown。
// GORM 打开或写库失败原样返回。无法证明的供应商结果标 unknown，不得当失败自动重试。
func RecoverUnfinishedGraphRuns(ctx context.Context, pool *pgxpool.Pool, staleAfter time.Duration, products ProductGuard) (RecoverySummary, error) {
	if staleAfter <= 0 {
		staleAfter = defaultStaleRunningAfter
	}
	ctx = WithProductGuard(ctx, products)
	gdb, err := pfdb.OpenGorm(pool)
	if err != nil {
		return RecoverySummary{}, err
	}
	var summary RecoverySummary
	err = tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		var running []schema.WorkflowGraphRuns
		if err := pgxTx.WithContext(ctx).Select("id").Where("status = ?", "running").Find(&running).Error; err != nil {
			return err
		}
		cutoff := time.Now().UTC().Add(-staleAfter)
		for _, item := range running {
			runID := item.ID
			run, err := loadGraphRunByIDLocked(ctx, pgxTx, runID)
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					continue
				}
				return err
			}
			if run.Status != RunStatusRunning {
				continue
			}
			state := classifyDelivery(run)
			if state == "queued" {
				summary.QueuedRuns++
				changed, err := queue.RestageIfIdle(ctx, pgxTx, queue.ActorGraphRun, runID, nil)
				if err != nil {
					return err
				}
				if changed {
					summary.EnqueuedRuns++
				}
				continue
			}
			if state != "running" {
				continue
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
				continue
			}
			markedUnknown := false
			requeued := false
			now := time.Now().UTC()
			for _, node := range stale {
				if nodeSafeToRequeue(node) {
					result := pgxTx.WithContext(ctx).Model(&schema.WorkflowGraphNodeRuns{}).
						Where("id = ? AND status = ?", node.ID, "running").
						Updates(map[string]any{
							"status":              "queued",
							"active_attempt_id":   nil,
							"failure_reason":      nil,
							"finished_at":         nil,
							"progress_phase":      "requeued_after_idle",
							"progress_updated_at": now,
						})
					if result.Error != nil {
						return result.Error
					}
					if result.RowsAffected == 1 {
						if err := appendGraphRunEvent(ctx, pgxTx, run.ID, "node.progress", &node.ID, map[string]any{
							"status": "queued", "phase": "requeued_after_idle", "reason": "stale_worker",
						}); err != nil {
							return err
						}
						requeued = true
					}
					_ = pgxTx.WithContext(ctx).Where("node_run_id = ? AND effect_result = ?", node.ID, "pending").
						Delete(&schema.WorkflowGraphProviderEffects{}).Error
					continue
				}
				if err := markNodeUnknown(ctx, pgxTx, run.ID, node.ID, node.ActiveAttemptID, ProviderUnknownDetail); err != nil {
					return err
				}
				markedUnknown = true
			}
			if _, err := completeGraphRunIfNodesTerminal(ctx, pgxTx, run.ID); err != nil {
				return err
			}
			var statusRec schema.WorkflowGraphRuns
			if err := pgxTx.WithContext(ctx).Select("status").Where("id = ?", run.ID).Take(&statusRec).Error; err != nil {
				return err
			}
			if isTerminalRun(statusRec.Status) {
				if markedUnknown {
					summary.UnknownRuns++
				}
				continue
			}
			changed, err := queue.RestageIfIdle(ctx, pgxTx, queue.ActorGraphRun, run.ID, nil)
			if err != nil {
				return err
			}
			if changed {
				summary.EnqueuedRuns++
			}
			if markedUnknown {
				summary.UnknownRuns++
			}
			if requeued {
				summary.StaleRunningRuns++
			}
		}
		// 终态迁移与队列晋升是两次独立写。进程若死在中间，不会再有 running 行把 recovery 领到 queued run。
		// 因此对每个仍有 queued 的 graph 显式 promote 一条，而不是等 running 行来带头。
		var queuedGraphIDs []string
		if err := pgxTx.WithContext(ctx).Model(&schema.WorkflowGraphRuns{}).
			Where("status = ?", RunStatusQueued).
			Distinct("graph_id").
			Pluck("graph_id", &queuedGraphIDs).Error; err != nil {
			return err
		}
		for _, graphID := range queuedGraphIDs {
			if err := promoteNextQueuedRun(ctx, pgxTx, graphID); err != nil {
				return err
			}
		}
		return nil
	})
	return summary, err
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
