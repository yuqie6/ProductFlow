package localedit

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

// RecoverySummary 统计 dispatcher 本轮补回或标 unknown 的局部编辑任务。
type RecoverySummary struct {
	QueuedTasks       int  `json:"queued_tasks"`        // 本轮 queued 并补回 PENDING 的数量
	StaleRunningTasks int  `json:"stale_running_tasks"` // 过期 claimed 被重排队
	EnqueuedTasks     int  `json:"enqueued_tasks"`      // 成功补回 PENDING dispatch 的数量
	UnknownTasks      int  `json:"unknown_tasks"`       // 已过 provider 边界、标 unknown
	HasMore           bool `json:"has_more"`            // 跳过锁定行后仍有超过本批额度的候选
}

// RecoverUnfinished 把 queued 任务补回 PENDING；过期且已打 provider 的 running 标 unknown。
// pool 为 nil 或写库失败时返回 error；已过 provider 边界标 unknown，不得当失败自动重试。
// 单条任务失败计入返回 error，不回滚本轮已提交的其它任务。
func RecoverUnfinished(ctx context.Context, pool *pgxpool.Pool, staleAfter time.Duration) (RecoverySummary, error) {
	return recoverUnfinished(ctx, pool, staleAfter, recoveryBatchLimit)
}

func recoverUnfinished(ctx context.Context, pool *pgxpool.Pool, staleAfter time.Duration, limit int) (RecoverySummary, error) {
	if staleAfter <= 0 {
		staleAfter = 10 * time.Minute
	}
	if limit <= 0 {
		limit = recoveryBatchLimit
	}
	gdb, err := pfdb.OpenGorm(pool)
	if err != nil {
		return RecoverySummary{}, err
	}
	cutoff := time.Now().UTC().Add(-staleAfter)
	ids, hasMore, err := discoverLocalEditCandidates(ctx, gdb, cutoff, limit)
	if err != nil {
		return RecoverySummary{}, err
	}
	summary := RecoverySummary{HasMore: hasMore}
	var errs []error
	now := time.Now().UTC()
	for _, taskID := range ids {
		outcome, recErr := recoverLocalEditState(ctx, gdb, taskID, staleAfter, now)
		if recErr != nil {
			errs = append(errs, fmt.Errorf("local edit task %s: %w", taskID, recErr))
			continue
		}
		switch outcome {
		case "queued":
			changed, restageErr := restageLocalEditTask(ctx, gdb, taskID)
			if restageErr != nil {
				errs = append(errs, fmt.Errorf("local edit task %s restage: %w", taskID, restageErr))
				continue
			}
			if changed {
				summary.QueuedTasks++
				summary.EnqueuedTasks++
			}
		case "requeued":
			summary.StaleRunningTasks++
			if _, restageErr := restageLocalEditTask(ctx, gdb, taskID); restageErr != nil {
				errs = append(errs, fmt.Errorf("local edit task %s restage: %w", taskID, restageErr))
				continue
			}
			summary.EnqueuedTasks++
		case "unknown":
			summary.UnknownTasks++
		}
	}
	return summary, errors.Join(errs...)
}

func discoverLocalEditCandidates(ctx context.Context, gdb *gorm.DB, cutoff time.Time, limit int) ([]string, bool, error) {
	var ids []string
	err := tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		candidateScanStarted := time.Now()
		scanErr := localEditRecoveryScope(pgxTx.WithContext(ctx), cutoff).
			Clauses(pfdb.SkipLocked()).
			Order("updated_at ASC, id ASC").
			Limit(limit+1).
			Pluck("id", &ids).Error
		metrics.ObserveRecoveryLock("local_image_edit", time.Since(candidateScanStarted))
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

func localEditRecoveryScope(tx *gorm.DB, cutoff time.Time) *gorm.DB {
	return tx.Model(&schema.LocalImageEditTasks{}).Where(`
			is_retryable = ? AND (
				(status = ? AND NOT EXISTS (
					SELECT 1 FROM async_dispatches d
					WHERE d.delivery_key = ? || ':' || local_image_edit_tasks.id
					  AND d.status IN ?
				))
				OR (status = ? AND (started_at IS NULL OR started_at <= ?))
			)`, true, "queued", queue.ActorLocalEdit, []string{queue.StatusPending, queue.StatusSent, queue.StatusDead}, "running", cutoff)
}

func recoverLocalEditState(ctx context.Context, gdb *gorm.DB, taskID string, staleAfter time.Duration, now time.Time) (string, error) {
	var outcome string
	err := tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		var recErr error
		outcome, recErr = recoverOne(ctx, pgxTx, taskID, true, staleAfter, now)
		return recErr
	})
	return outcome, err
}

func restageLocalEditTask(ctx context.Context, gdb *gorm.DB, taskID string) (bool, error) {
	var changed bool
	err := tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		var task schema.LocalImageEditTasks
		err := pgxTx.WithContext(ctx).Where("id = ?", taskID).Take(&task).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if task.Status != "queued" {
			return nil
		}
		var restageErr error
		changed, restageErr = queue.RestageIfIdle(ctx, pgxTx, queue.ActorLocalEdit, task.ID, payloadFor(taskFromModel(task)))
		return restageErr
	})
	return changed, err
}

// recoverOne 处理一条 queued/running 任务的状态。queued 只确认仍 queued；running 过期且尚未打 provider 可重排队；
// 已过 provider 边界只能标 unknown。RestageIfIdle 由调用方在状态事务提交后另开 outbox 事务。
// resetStale=false 时只观察不改行。SKIP LOCKED 跳过仍被 live worker 持有的行。
func recoverOne(ctx context.Context, pgxTx *gorm.DB, taskID string, resetStale bool, staleAfter time.Duration, now time.Time) (string, error) {
	var row schema.LocalImageEditTasks
	err := pgxTx.Clauses(pfdb.SkipLocked()).Where("id = ?", taskID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "skipped", nil
	}
	if err != nil {
		return "", err
	}
	task := taskFromModel(row)
	if task.Status == "queued" {
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
