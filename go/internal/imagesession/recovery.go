package imagesession

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

// DefaultStaleRunningAfter 是 dispatcher 未传入正数阈值时，按最后一次 progress heartbeat
// （没有则 started_at）判断 running 闲置的默认等待。进程崩溃后商家仍看到 running 的上限
// 约为该值加上 recovery 扫描间隔；asynq 墙钟到期走 worker 落 unknown，不等待本阈值。
const DefaultStaleRunningAfter = 90 * time.Minute

// RecoverySummary 统计 dispatcher 本轮补回或标 unknown 的连续生图任务。
type RecoverySummary struct {
	QueuedTasks       int  `json:"queued_tasks"`        // 本轮看到的 queued 任务数
	StaleRunningTasks int  `json:"stale_running_tasks"` // 过期且未打 provider 的 running 被重排队
	EnqueuedTasks     int  `json:"enqueued_tasks"`      // 成功补回 PENDING dispatch 的数量
	UnknownTasks      int  `json:"unknown_tasks"`       // 已过 provider 边界、标 unknown 且不可自动重试
	HasMore           bool `json:"has_more"`            // 跳过锁定行后仍有超过本批额度的候选
}

type imageTaskRecoverResult struct {
	outcome   string
	sessionID string
}

// RecoverUnfinished 把 queued 任务补回 PENDING；过期 running 若已打 provider 则 unknown。
// pool 为 nil 或写库失败时返回 error；已过 provider 边界标 unknown，不得当失败自动重试。
// 单条任务失败计入返回 error，不回滚本轮已提交的其它任务。
func RecoverUnfinished(ctx context.Context, pool *pgxpool.Pool, staleAfter time.Duration) (RecoverySummary, error) {
	return recoverUnfinished(ctx, pool, staleAfter, recoveryBatchLimit)
}

func recoverUnfinished(ctx context.Context, pool *pgxpool.Pool, staleAfter time.Duration, limit int) (RecoverySummary, error) {
	if staleAfter <= 0 {
		staleAfter = DefaultStaleRunningAfter
	}
	if limit <= 0 {
		limit = recoveryBatchLimit
	}
	gdb, err := pfdb.OpenGorm(pool)
	if err != nil {
		return RecoverySummary{}, err
	}
	cutoff := time.Now().UTC().Add(-staleAfter)
	ids, hasMore, err := discoverImageSessionCandidates(ctx, gdb, cutoff, limit)
	if err != nil {
		return RecoverySummary{}, err
	}
	summary := RecoverySummary{HasMore: hasMore}
	var errs []error
	for _, taskID := range ids {
		result, recErr := recoverImageTaskState(ctx, gdb, taskID, cutoff)
		if recErr != nil {
			errs = append(errs, fmt.Errorf("image session task %s: %w", taskID, recErr))
			continue
		}
		switch result.outcome {
		case "queued":
			summary.QueuedTasks++
			changed, restageErr := restageImageTask(ctx, gdb, taskID, result.sessionID, false)
			if restageErr != nil {
				errs = append(errs, fmt.Errorf("image session task %s restage: %w", taskID, restageErr))
				continue
			}
			if changed {
				summary.EnqueuedTasks++
			}
		case "requeued":
			summary.StaleRunningTasks++
			changed, restageErr := restageImageTask(ctx, gdb, taskID, result.sessionID, true)
			if restageErr != nil {
				errs = append(errs, fmt.Errorf("image session task %s restage: %w", taskID, restageErr))
				continue
			}
			if changed {
				summary.EnqueuedTasks++
			}
		case "unknown":
			summary.UnknownTasks++
			if pubErr := publishImageTask(ctx, gdb, result.sessionID); pubErr != nil {
				errs = append(errs, fmt.Errorf("image session task %s notify: %w", taskID, pubErr))
			}
		}
	}
	return summary, errors.Join(errs...)
}

func discoverImageSessionCandidates(ctx context.Context, gdb *gorm.DB, cutoff time.Time, limit int) ([]string, bool, error) {
	var ids []string
	err := tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		candidateScanStarted := time.Now()
		scanErr := imageSessionRecoveryScope(pgxTx.WithContext(ctx), cutoff).
			Clauses(pfdb.SkipLocked()).
			Order("created_at ASC, id ASC").
			Limit(limit+1).
			Pluck("id", &ids).Error
		metrics.ObserveRecoveryLock("image_session", time.Since(candidateScanStarted))
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

func imageSessionRecoveryScope(tx *gorm.DB, cutoff time.Time) *gorm.DB {
	return tx.Model(&schema.ImageSessionGenerationTasks{}).Where(`
			is_retryable = ? AND (
				(status = ? AND NOT EXISTS (
					SELECT 1 FROM async_dispatches d
					WHERE d.delivery_key = ? || ':' || image_session_generation_tasks.id
					  AND d.status IN ?
				))
				OR (status = ? AND COALESCE(progress_updated_at, started_at) <= ?)
			)`, true, "queued", queue.ActorImageSession, []string{queue.StatusPending, queue.StatusSent, queue.StatusDead}, "running", cutoff)
}

func recoverImageTaskState(ctx context.Context, gdb *gorm.DB, taskID string, cutoff time.Time) (imageTaskRecoverResult, error) {
	var result imageTaskRecoverResult
	err := tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		var task schema.ImageSessionGenerationTasks
		err := pgxTx.WithContext(ctx).Clauses(pfdb.SkipLocked()).Where("id = ?", taskID).Take(&task).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		result.sessionID = task.SessionID
		if !task.IsRetryable {
			return nil
		}
		if task.Status == "queued" {
			result.outcome = "queued"
			return nil
		}
		if task.Status != "running" {
			return nil
		}
		stamp := task.StartedAt
		if task.ProgressUpdatedAt != nil {
			stamp = task.ProgressUpdatedAt
		}
		if stamp == nil || stamp.After(cutoff) {
			return nil
		}
		if task.ActiveAttemptID == nil {
			return nil
		}
		phase := ""
		if task.ProgressPhase != nil {
			phase = *task.ProgressPhase
		}
		safe := phase == "running" || phase == "candidate_saved"
		nextCandidate := task.CompletedCandidates + 1
		var covering int64
		if err := pgxTx.Model(&schema.ImageSessionProviderEffects{}).
			Where("generation_task_id = ? AND effect_result IN ? AND candidate_start_index <= ? AND (candidate_start_index + candidate_count - 1) >= ?",
				task.ID, []string{"pending", "applied", "unknown"}, nextCandidate, nextCandidate).
			Count(&covering).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		unknown := task.ActiveCandidateIndex != nil || !safe || covering > 0
		if unknown {
			if err := pgxTx.Model(&schema.ImageSessionGenerationTasks{}).
				Where("id = ? AND status = ?", task.ID, "running").
				Updates(map[string]any{
					"status":                 "unknown",
					"active_attempt_id":      nil,
					"finished_at":            now,
					"is_retryable":           false,
					"failure_reason":         unknownDetail,
					"progress_phase":         unknownPhase,
					"progress_updated_at":    now,
					"active_candidate_index": nil,
				}).Error; err != nil {
				return err
			}
			_ = pgxTx.Model(&schema.ImageSessionProviderEffects{}).
				Where("generation_task_id = ? AND attempt_id = ? AND effect_result = ?", task.ID, *task.ActiveAttemptID, "pending").
				Updates(map[string]any{
					"effect_result":        "unknown",
					"reconciliation_state": "unknown",
					"detail":               unknownDetail,
					"updated_at":           now,
				}).Error
			result.outcome = "unknown"
			return nil
		}
		res := pgxTx.Model(&schema.ImageSessionGenerationTasks{}).
			Where("id = ? AND status = ?", task.ID, "running").
			Updates(map[string]any{
				"status":                   "queued",
				"active_attempt_id":        nil,
				"started_at":               nil,
				"finished_at":              nil,
				"active_candidate_index":   nil,
				"provider_response_id":     nil,
				"provider_response_status": nil,
				"progress_phase":           "requeued_after_idle",
				"progress_updated_at":      now,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return nil
		}
		_ = pgxTx.Where("generation_task_id = ? AND effect_result = ?", task.ID, "pending").
			Delete(&schema.ImageSessionProviderEffects{}).Error
		result.outcome = "requeued"
		return nil
	})
	return result, err
}

func restageImageTask(ctx context.Context, gdb *gorm.DB, taskID, sessionID string, publishAlways bool) (bool, error) {
	var changed bool
	err := tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		var task schema.ImageSessionGenerationTasks
		err := pgxTx.WithContext(ctx).Select("id", "status").Where("id = ?", taskID).Take(&task).Error
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
		changed, restageErr = queue.RestageIfIdle(ctx, pgxTx, queue.ActorImageSession, taskID, nil)
		if restageErr != nil {
			return restageErr
		}
		if publishAlways || changed {
			return publishSession(ctx, pgxTx, sessionID)
		}
		return nil
	})
	return changed, err
}

func publishImageTask(ctx context.Context, gdb *gorm.DB, sessionID string) error {
	return tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		return publishSession(ctx, pgxTx, sessionID)
	})
}
