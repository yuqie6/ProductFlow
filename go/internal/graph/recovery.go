package graph

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

const defaultStaleRunningAfter = 30 * time.Minute

type RecoverySummary struct {
	QueuedRuns       int `json:"queued_runs"`
	StaleRunningRuns int `json:"stale_running_runs"`
	EnqueuedRuns     int `json:"enqueued_runs"`
	UnknownRuns      int `json:"unknown_runs"`
}

// RecoverUnfinishedGraphRuns 把仍 active 的图运行补回 PENDING dispatch。过期且已打 provider 的节点标 unknown。
func RecoverUnfinishedGraphRuns(ctx context.Context, pool *pgxpool.Pool, staleAfter time.Duration) (RecoverySummary, error) {
	if staleAfter <= 0 {
		staleAfter = defaultStaleRunningAfter
	}
	gdb, err := pfdb.OpenGorm(pool)
	if err != nil {
		return RecoverySummary{}, err
	}
	var summary RecoverySummary
	err = tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		rows, err := pfdb.Query(ctx, pgxTx, `
			SELECT id FROM workflow_graph_runs WHERE status = 'running'
		`)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		cutoff := time.Now().UTC().Add(-staleAfter)
		for _, runID := range ids {
			run, err := loadGraphRunByID(ctx, pgxTx, runID)
			if err != nil {
				return err
			}
			state := classifyDelivery(run)
			if state == "queued" {
				if _, err := queue.StageForActor(ctx, pgxTx, queue.ActorGraphRun, runID, 0); err != nil {
					return err
				}
				summary.QueuedRuns++
				summary.EnqueuedRuns++
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
			for _, node := range stale {
				if nodeSafeToRequeue(node) {
					if _, err := pfdb.Exec(ctx, pgxTx, `
						UPDATE workflow_graph_node_runs SET
							status = 'queued', active_attempt_id = NULL, failure_reason = NULL,
							finished_at = NULL, progress_phase = 'requeued_after_idle', progress_updated_at = NOW()
						WHERE id = $1
					`, node.ID); err != nil {
						return err
					}
					_, _ = pfdb.Exec(ctx, pgxTx, `
						DELETE FROM workflow_graph_provider_effects
						WHERE node_run_id = $1 AND effect_result = 'pending'
					`, node.ID)
					requeued = true
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
			var status string
			if err := pfdb.QueryRow(ctx, pgxTx, `SELECT status FROM workflow_graph_runs WHERE id = $1`, run.ID).Scan(&status); err != nil {
				return err
			}
			if isTerminalRun(status) {
				if markedUnknown {
					summary.UnknownRuns++
				}
				continue
			}
			if _, err := queue.StageForActor(ctx, pgxTx, queue.ActorGraphRun, run.ID, 0); err != nil {
				return err
			}
			summary.EnqueuedRuns++
			if markedUnknown {
				summary.UnknownRuns++
			}
			if requeued {
				summary.StaleRunningRuns++
			}
		}
		return nil
	})
	return summary, err
}

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
		case NodeRunSucceeded, NodeRunFailed, NodeRunUnknown:
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
