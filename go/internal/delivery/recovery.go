package delivery

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

// RecoverySummary 统计 dispatcher 本轮补回的交付任务。
type RecoverySummary struct {
	QueuedJobs       int `json:"queued_jobs"`        // 本轮看到的 queued 任务数
	StaleRunningJobs int `json:"stale_running_jobs"` // 过期 running 被重置为 queued 的数量
	EnqueuedJobs     int `json:"enqueued_jobs"`      // 成功补回 PENDING dispatch 的数量
}

// RecoverUnfinished 把 queued / 过期 running 的交付任务补回 PENDING dispatch。交付没有 unknown。
// pool 为 nil 或写库失败、ctx 取消时返回 error。
func RecoverUnfinished(ctx context.Context, pool *pgxpool.Pool, staleAfter time.Duration) (RecoverySummary, error) {
	if staleAfter <= 0 {
		staleAfter = 30 * time.Minute
	}
	gdb, err := pfdb.OpenGorm(pool)
	if err != nil {
		return RecoverySummary{}, err
	}
	var summary RecoverySummary
	err = tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		cutoff := time.Now().UTC().Add(-staleAfter)
		var jobs []schema.DeliveryRenditionJobs
		if err := pgxTx.Where(
			"is_retryable = ? AND (status = ? OR (status = ? AND started_at IS NOT NULL AND started_at <= ?))",
			true, "queued", "running", cutoff,
		).Find(&jobs).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		for _, job := range jobs {
			if job.Status == "running" {
				res := pgxTx.Model(&schema.DeliveryRenditionJobs{}).
					Where("id = ? AND status = ? AND started_at <= ?", job.ID, "running", cutoff).
					Updates(map[string]any{
						"status":            "queued",
						"active_attempt_id": nil,
						"failure_reason":    nil,
						"started_at":        nil,
						"finished_at":       nil,
						"updated_at":        now,
					})
				if res.Error != nil {
					return res.Error
				}
				if res.RowsAffected != 1 {
					continue
				}
				summary.StaleRunningJobs++
			} else {
				summary.QueuedJobs++
			}
			changed, err := queue.RestageIfIdle(ctx, pgxTx, queue.ActorDelivery, job.ID, nil)
			if err != nil {
				return err
			}
			if changed {
				summary.EnqueuedJobs++
			}
		}
		return nil
	})
	return summary, err
}
