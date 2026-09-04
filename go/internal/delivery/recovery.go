package delivery

import (
	"context"
	"errors"
	"fmt"
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

// RecoverySummary 统计 dispatcher 本轮补回的交付任务。
type RecoverySummary struct {
	QueuedJobs       int  `json:"queued_jobs"`        // 本轮看到的 queued 任务数
	StaleRunningJobs int  `json:"stale_running_jobs"` // 过期 running 被重置为 queued 的数量
	EnqueuedJobs     int  `json:"enqueued_jobs"`      // 成功补回 PENDING dispatch 的数量
	HasMore          bool `json:"has_more"`           // unlocked snapshot 仍有未处理候选
}

// RecoverUnfinished 把 queued / 过期 running 的交付任务补回 PENDING dispatch。交付没有 unknown。
// pool 为 nil 或写库失败、ctx 取消时返回 error。
// 单条任务失败计入返回 error，不回滚本轮已提交的其它任务。
func RecoverUnfinished(ctx context.Context, pool *pgxpool.Pool, staleAfter time.Duration) (RecoverySummary, error) {
	return recoverUnfinished(ctx, pool, staleAfter, recoveryBatchLimit)
}

func recoverUnfinished(ctx context.Context, pool *pgxpool.Pool, staleAfter time.Duration, limit int) (RecoverySummary, error) {
	if staleAfter <= 0 {
		staleAfter = 30 * time.Minute
	}
	if limit <= 0 {
		limit = recoveryBatchLimit
	}
	gdb, err := pfdb.OpenGorm(pool)
	if err != nil {
		return RecoverySummary{}, err
	}
	cutoff := time.Now().UTC().Add(-staleAfter)
	ids, hasMore, err := discoverDeliveryCandidates(ctx, gdb, cutoff, limit)
	if err != nil {
		return RecoverySummary{}, err
	}
	summary := RecoverySummary{HasMore: hasMore}
	var errs []error
	for _, jobID := range ids {
		outcome, recErr := recoverDeliveryJobState(ctx, gdb, jobID, cutoff)
		if recErr != nil {
			errs = append(errs, fmt.Errorf("delivery job %s: %w", jobID, recErr))
			continue
		}
		switch outcome {
		case "queued":
			summary.QueuedJobs++
		case "requeued":
			summary.StaleRunningJobs++
		default:
			continue
		}
		changed, restageErr := restageDeliveryJob(ctx, gdb, jobID)
		if restageErr != nil {
			errs = append(errs, fmt.Errorf("delivery job %s restage: %w", jobID, restageErr))
			continue
		}
		if changed {
			summary.EnqueuedJobs++
		}
	}
	return summary, errors.Join(errs...)
}

func discoverDeliveryCandidates(ctx context.Context, gdb *gorm.DB, cutoff time.Time, limit int) ([]string, bool, error) {
	var ids []string
	err := tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		candidateScanStarted := time.Now()
		scanErr := deliveryRecoveryScope(pgxTx.WithContext(ctx), cutoff).
			Order("updated_at ASC, id ASC").
			Limit(limit+1).
			Pluck("id", &ids).Error
		metrics.ObserveRecoveryLock("delivery", time.Since(candidateScanStarted))
		return scanErr
	})
	if err != nil {
		return nil, false, err
	}
	hasMore := len(ids) > limit
	if hasMore {
		ids = ids[:limit]
	}
	return ids, hasMore, nil
}

func deliveryRecoveryScope(tx *gorm.DB, cutoff time.Time) *gorm.DB {
	return tx.Model(&schema.DeliveryRenditionJobs{}).Where(`
			is_retryable = ? AND (
				(status = ? AND NOT EXISTS (
					SELECT 1 FROM async_dispatches d
					WHERE d.delivery_key = ? || ':' || delivery_rendition_jobs.id
					  AND d.status IN ?
				))
				OR (status = ? AND started_at IS NOT NULL AND started_at <= ?)
			)`, true, "queued", queue.ActorDelivery, []string{queue.StatusPending, queue.StatusSent, queue.StatusDead}, "running", cutoff)
}

func recoverDeliveryJobState(ctx context.Context, gdb *gorm.DB, jobID string, cutoff time.Time) (string, error) {
	var outcome string
	err := tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		var job schema.DeliveryRenditionJobs
		err := pgxTx.WithContext(ctx).Clauses(pfdb.SkipLocked()).Where("id = ?", jobID).Take(&job).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if !job.IsRetryable {
			return nil
		}
		if job.Status == "queued" {
			outcome = "queued"
			return nil
		}
		if job.Status != "running" {
			return nil
		}
		now := time.Now().UTC()
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
			return nil
		}
		outcome = "requeued"
		return nil
	})
	return outcome, err
}

func restageDeliveryJob(ctx context.Context, gdb *gorm.DB, jobID string) (bool, error) {
	var changed bool
	err := tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		var job schema.DeliveryRenditionJobs
		err := pgxTx.WithContext(ctx).Select("id", "status").Where("id = ?", jobID).Take(&job).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if job.Status != "queued" {
			return nil
		}
		var restageErr error
		changed, restageErr = queue.RestageIfIdle(ctx, pgxTx, queue.ActorDelivery, jobID, nil)
		return restageErr
	})
	return changed, err
}
