package imagesession

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
	QueuedTasks       int `json:"queued_tasks"`
	StaleRunningTasks int `json:"stale_running_tasks"`
	EnqueuedTasks     int `json:"enqueued_tasks"`
	UnknownTasks      int `json:"unknown_tasks"`
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
	err = tx.WithGorm(ctx, gdb, func(pgxTx *gorm.DB) error {
		cutoff := time.Now().UTC().Add(-staleAfter)
		rows, err := pfdb.Query(ctx, pgxTx, `
			SELECT id, status, active_attempt_id, progress_phase, completed_candidates,
			       active_candidate_index,
			       COALESCE(progress_updated_at, started_at)
			FROM image_session_generation_tasks
			WHERE is_retryable = TRUE
			  AND (
			    status = 'queued'
			    OR (status = 'running' AND COALESCE(progress_updated_at, started_at) <= $1)
			  )
		`, cutoff)
		if err != nil {
			return err
		}
		type item struct {
			id, status string
			attempt    *string
			phase      *string
			completed  int
			activeIdx  *int
			stamp      *time.Time
		}
		var tasks []item
		for rows.Next() {
			var it item
			if err := rows.Scan(&it.id, &it.status, &it.attempt, &it.phase, &it.completed, &it.activeIdx, &it.stamp); err != nil {
				rows.Close()
				return err
			}
			tasks = append(tasks, it)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, task := range tasks {
			if task.status == "queued" {
				summary.QueuedTasks++
				changed, err := queue.RestageIfIdle(ctx, pgxTx, queue.ActorImageSession, task.id, nil)
				if err != nil {
					return err
				}
				if changed {
					summary.EnqueuedTasks++
				}
				continue
			}
			if task.attempt == nil {
				continue
			}
			phase := ""
			if task.phase != nil {
				phase = *task.phase
			}
			safe := phase == "running" || phase == "candidate_saved"
			nextCandidate := task.completed + 1
			var coveringEffect bool
			_ = pfdb.QueryRow(ctx, pgxTx, `
				SELECT EXISTS (
					SELECT 1 FROM image_session_provider_effects
					WHERE generation_task_id = $1
					  AND effect_result IN ('pending', 'applied', 'unknown')
					  AND candidate_start_index <= $2
					  AND (candidate_start_index + candidate_count - 1) >= $2
				)
			`, task.id, nextCandidate).Scan(&coveringEffect)
			unknown := task.activeIdx != nil || !safe || coveringEffect
			if unknown {
				_, err := pfdb.Exec(ctx, pgxTx, `
					UPDATE image_session_generation_tasks SET
						status = 'unknown', active_attempt_id = NULL, finished_at = NOW(),
						is_retryable = FALSE, failure_reason = $2, progress_phase = $3,
						progress_updated_at = NOW(), active_candidate_index = NULL
					WHERE id = $1 AND status = 'running'
				`, task.id, unknownDetail, unknownPhase)
				if err != nil {
					return err
				}
				_, _ = pfdb.Exec(ctx, pgxTx, `
					UPDATE image_session_provider_effects SET
						effect_result = 'unknown', reconciliation_state = 'unknown', detail = $2, updated_at = NOW()
					WHERE generation_task_id = $1 AND attempt_id = $3 AND effect_result = 'pending'
				`, task.id, unknownDetail, *task.attempt)
				summary.UnknownTasks++
				continue
			}
			n, err := pfdb.Exec(ctx, pgxTx, `
				UPDATE image_session_generation_tasks SET
					status = 'queued', active_attempt_id = NULL, started_at = NULL, finished_at = NULL,
					active_candidate_index = NULL, provider_response_id = NULL, provider_response_status = NULL,
					progress_phase = 'requeued_after_idle', progress_updated_at = NOW()
				WHERE id = $1 AND status = 'running'
			`, task.id)
			if err != nil {
				return err
			}
			if n != 1 {
				continue
			}
			_, _ = pfdb.Exec(ctx, pgxTx, `
				DELETE FROM image_session_provider_effects WHERE generation_task_id = $1 AND effect_result = 'pending'
			`, task.id)
			changed, err := queue.RestageIfIdle(ctx, pgxTx, queue.ActorImageSession, task.id, nil)
			if err != nil {
				return err
			}
			summary.StaleRunningTasks++
			if changed {
				summary.EnqueuedTasks++
			}
		}
		return nil
	})
	return summary, err
}
