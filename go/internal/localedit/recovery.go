package localedit

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/metrics"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

const recoveryBatchLimit = 25

// RecoverySummary 统计 dispatcher 本轮补回或标 unknown 的局部编辑任务。
type RecoverySummary struct {
	QueuedTasks       int  `json:"queued_tasks"`        // 本轮 queued 并补回 PENDING 的数量
	StaleRunningTasks int  `json:"stale_running_tasks"` // 过期 claimed 被重排队
	EnqueuedTasks     int  `json:"enqueued_tasks"`      // 成功补回 PENDING dispatch 的数量
	UnknownTasks      int  `json:"unknown_tasks"`       // 已过 provider 边界、标 unknown
	HasMore           bool `json:"has_more"`            // 本轮批次已填满，下一轮继续探测
}

// RecoverUnfinished 把 queued 任务补回 PENDING；过期且已打 provider 的 running 标 unknown。
// pool 为 nil 或写库失败时返回 error；已过 provider 边界标 unknown，不得当失败自动重试。
func RecoverUnfinished(ctx context.Context, pool *pgxpool.Pool, staleAfter time.Duration) (RecoverySummary, error) {
	if staleAfter <= 0 {
		staleAfter = 10 * time.Minute
	}
	gdb, err := pfdb.OpenGorm(pool)
	if err != nil {
		return RecoverySummary{}, err
	}
	var summary RecoverySummary
	err = tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		cutoff := time.Now().UTC().Add(-staleAfter)
		var tasks []schema.LocalImageEditTasks
		candidateScanStarted := time.Now()
		candidateScanErr := pgxTx.WithContext(ctx).Clauses(pfdb.SkipLocked()).Where(`
			is_retryable = ? AND (
				(status = ? AND NOT EXISTS (
					SELECT 1 FROM async_dispatches d
					WHERE d.delivery_key = ? || ':' || local_image_edit_tasks.id
					  AND d.status IN ?
				))
				OR (status = ? AND (started_at IS NULL OR started_at <= ?))
			)`, true, "queued", queue.ActorLocalEdit, []string{queue.StatusPending, queue.StatusSent, queue.StatusDead}, "running", cutoff).
			Order("updated_at ASC, id ASC").
			Limit(recoveryBatchLimit).
			Find(&tasks).Error
		metrics.ObserveRecoveryLock("local_image_edit", time.Since(candidateScanStarted))
		if candidateScanErr != nil {
			return candidateScanErr
		}
		if len(tasks) >= recoveryBatchLimit {
			summary.HasMore = true
		}
		now := time.Now().UTC()
		for _, task := range tasks {
			outcome, err := recoverOne(ctx, pgxTx, task.ID, true, staleAfter, now)
			if err != nil {
				return err
			}
			switch outcome {
			case "queued":
				summary.QueuedTasks++
				summary.EnqueuedTasks++
			case "requeued":
				summary.StaleRunningTasks++
				summary.EnqueuedTasks++
			case "unknown":
				summary.UnknownTasks++
			}
		}
		return nil
	})
	return summary, err
}

// recoverOne 处理一条 queued/running 任务。queued 补 PENDING；running 过期且尚未打 provider 可重排队；
// 已过 provider 边界只能标 unknown。resetStale=false 时只观察不改行。
func recoverOne(ctx context.Context, pgxTx *gorm.DB, taskID string, resetStale bool, staleAfter time.Duration, now time.Time) (string, error) {
	task, err := loadTaskByID(ctx, pgxTx, taskID)
	if err != nil {
		return "", err
	}
	if task.Status == "queued" {
		changed, err := queue.RestageIfIdle(ctx, pgxTx, queue.ActorLocalEdit, task.ID, payloadFor(task))
		if err != nil {
			return "", err
		}
		if !changed {
			return "idle", nil
		}
		return "queued", nil
	}
	if task.Status != "running" {
		return "ignored", nil
	}
	stale := task.StartedAt == nil || now.Sub(task.StartedAt.UTC()) >= staleAfter
	if !resetStale || !stale {
		return "fresh", nil
	}
	phase := ""
	if task.ProgressPhase != nil {
		phase = *task.ProgressPhase
	}
	if phase == "claimed" {
		if err := markStaleClaimed(ctx, pgxTx, task); err != nil {
			return "", err
		}
		if _, err := queue.RestageIfIdle(ctx, pgxTx, queue.ActorLocalEdit, task.ID, payloadFor(task)); err != nil {
			return "", err
		}
		return "requeued", nil
	}
	detail := "局部编辑运行阶段无法识别，已停止自动重投"
	if phase == "provider_pending" || phase == "provider_call" || phase == "provider_result_received" {
		detail = "provider boundary 已开始，滞留运行不能自动重投"
	}
	if err := markUnknownLocked(ctx, pgxTx, task, detail); err != nil {
		return "", err
	}
	return "unknown", nil
}

func payloadFor(task taskRow) map[string]any {
	hash := ""
	if task.RequestHash != nil {
		hash = *task.RequestHash
	}
	return map[string]any{"task_id": task.ID, "request_hash": hash}
}
