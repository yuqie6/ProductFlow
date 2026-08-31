package imagesession

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

// RecoverySummary 统计 dispatcher 本轮补回或标 unknown 的连续生图任务。
type RecoverySummary struct {
	QueuedTasks       int `json:"queued_tasks"`        // 本轮看到的 queued 任务数
	StaleRunningTasks int `json:"stale_running_tasks"` // 过期且未打 provider 的 running 被重排队
	EnqueuedTasks     int `json:"enqueued_tasks"`      // 成功补回 PENDING dispatch 的数量
	UnknownTasks      int `json:"unknown_tasks"`       // 已过 provider 边界、标 unknown 且不可自动重试
}

// RecoverUnfinished 把 queued 任务补回 PENDING；过期 running 若已打 provider 则 unknown。
func RecoverUnfinished(ctx context.Context, pool *pgxpool.Pool, staleAfter time.Duration) (RecoverySummary, error) {
	if staleAfter <= 0 {
		staleAfter = 90 * time.Minute
	}
	gdb, err := pfdb.OpenGorm(pool)
	if err != nil {
		return RecoverySummary{}, err
	}
	var summary RecoverySummary
	touched := map[string]struct{}{}
	err = tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		cutoff := time.Now().UTC().Add(-staleAfter)
		var tasks []schema.ImageSessionGenerationTasks
		if err := pgxTx.Where(
			"is_retryable = ? AND (status = ? OR (status = ? AND COALESCE(progress_updated_at, started_at) <= ?))",
			true, "queued", "running", cutoff,
		).Find(&tasks).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		for _, task := range tasks {
			if task.Status == "queued" {
				summary.QueuedTasks++
				changed, err := queue.RestageIfIdle(ctx, pgxTx, queue.ActorImageSession, task.ID, nil)
				if err != nil {
					return err
				}
				if changed {
					summary.EnqueuedTasks++
					touched[task.SessionID] = struct{}{}
				}
				continue
			}
			if task.ActiveAttemptID == nil {
				continue
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
				summary.UnknownTasks++
				touched[task.SessionID] = struct{}{}
				continue
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
				continue
			}
			_ = pgxTx.Where("generation_task_id = ? AND effect_result = ?", task.ID, "pending").
				Delete(&schema.ImageSessionProviderEffects{}).Error
			changed, err := queue.RestageIfIdle(ctx, pgxTx, queue.ActorImageSession, task.ID, nil)
			if err != nil {
				return err
			}
			summary.StaleRunningTasks++
			if changed {
				summary.EnqueuedTasks++
			}
			touched[task.SessionID] = struct{}{}
		}
		for sessionID := range touched {
			if err := publishSession(ctx, pgxTx, sessionID); err != nil {
				return err
			}
		}
		return nil
	})
	return summary, err
}
