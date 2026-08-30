package localedit

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

type RecoverySummary struct {
	QueuedTasks       int `json:"queued_tasks"`
	StaleRunningTasks int `json:"stale_running_tasks"`
	EnqueuedTasks     int `json:"enqueued_tasks"`
	UnknownTasks      int `json:"unknown_tasks"`
}

// RecoverUnfinished 把 queued 任务补回 PENDING；过期且已打 provider 的 running 标 unknown。
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
		var tasks []schema.LocalImageEditTasks
		if err := pgxTx.Where("status IN ?", []string{"queued", "running"}).Find(&tasks).Error; err != nil {
			return err
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
