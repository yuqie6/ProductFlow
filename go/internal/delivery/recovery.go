package delivery

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

type RecoverySummary struct {
	QueuedJobs       int `json:"queued_jobs"`
	StaleRunningJobs int `json:"stale_running_jobs"`
	EnqueuedJobs     int `json:"enqueued_jobs"`
}

// RecoverUnfinished 把 queued / 过期 running 的交付任务补回 PENDING dispatch。交付没有 unknown。
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
		rows, err := pfdb.Query(ctx, pgxTx, `
			SELECT id, status FROM delivery_rendition_jobs
			WHERE is_retryable = TRUE
			  AND (
			    status = 'queued'
			    OR (status = 'running' AND started_at IS NOT NULL AND started_at <= $1)
			  )
		`, cutoff)
		if err != nil {
			return err
		}
		type item struct {
			id, status string
		}
		var jobs []item
		for rows.Next() {
			var it item
			if err := rows.Scan(&it.id, &it.status); err != nil {
				rows.Close()
				return err
			}
			jobs = append(jobs, it)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, job := range jobs {
			if job.status == "running" {
				n, err := pfdb.Exec(ctx, pgxTx, `
					UPDATE delivery_rendition_jobs SET
						status = 'queued', active_attempt_id = NULL, failure_reason = NULL,
						started_at = NULL, finished_at = NULL, updated_at = NOW()
					WHERE id = $1 AND status = 'running' AND started_at <= $2
				`, job.id, cutoff)
				if err != nil {
					return err
				}
				if n != 1 {
					continue
				}
				summary.StaleRunningJobs++
			} else {
				summary.QueuedJobs++
			}
			if _, err := queue.StageForActor(ctx, pgxTx, queue.ActorDelivery, job.id, 0); err != nil {
				return err
			}
			summary.EnqueuedJobs++
		}
		return nil
	})
	return summary, err
}
