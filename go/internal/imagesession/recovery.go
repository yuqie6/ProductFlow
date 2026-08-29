package imagesession

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/tx"
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
	var summary RecoverySummary
	err := tx.With(ctx, pool, func(pgxTx pgx.Tx) error {
		cutoff := time.Now().UTC().Add(-staleAfter)
		rows, err := pgxTx.Query(ctx, `
			SELECT id, status, active_attempt_id, progress_phase, completed_candidates,
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
			stamp      *time.Time
		}
		var tasks []item
		for rows.Next() {
			var it item
			if err := rows.Scan(&it.id, &it.status, &it.attempt, &it.phase, &it.completed, &it.stamp); err != nil {
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
				if _, err := queue.StageForActor(ctx, pgxTx, queue.ActorImageSession, task.id, 0); err != nil {
					return err
				}
				summary.QueuedTasks++
				summary.EnqueuedTasks++
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
			var pendingEffect bool
			_ = pgxTx.QueryRow(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM image_session_provider_effects
					WHERE generation_task_id = $1
					  AND effect_result IN ('pending', 'applied', 'unknown')
					  AND candidate_start_index <= $2
					  AND (candidate_start_index + candidate_count - 1) >= $2
				)
			`, task.id, task.completed+1).Scan(&pendingEffect)
			unknown := task.completed >= 0 && (phase != "" && !safe || pendingEffect)
			if phase == "running" && !pendingEffect {
				unknown = false
			}
			if !safe || pendingEffect {
				unknown = true
			}
			if phase == "running" || phase == "candidate_saved" {
				if !pendingEffect {
					unknown = false
				}
			}
			if unknown {
				_, err := pgxTx.Exec(ctx, `
					UPDATE image_session_generation_tasks SET
						status = 'unknown', active_attempt_id = NULL, finished_at = NOW(),
						is_retryable = FALSE, failure_reason = $2, progress_phase = $3, progress_updated_at = NOW()
					WHERE id = $1 AND status = 'running'
				`, task.id, unknownDetail, unknownPhase)
				if err != nil {
					return err
				}
				_, _ = pgxTx.Exec(ctx, `
					UPDATE image_session_provider_effects SET
						effect_result = 'unknown', reconciliation_state = 'unknown', detail = $2, updated_at = NOW()
					WHERE generation_task_id = $1 AND attempt_id = $3 AND effect_result = 'pending'
				`, task.id, unknownDetail, *task.attempt)
				summary.UnknownTasks++
				continue
			}
			tag, err := pgxTx.Exec(ctx, `
				UPDATE image_session_generation_tasks SET
					status = 'queued', active_attempt_id = NULL, started_at = NULL, finished_at = NULL,
					active_candidate_index = NULL, provider_response_id = NULL, provider_response_status = NULL,
					progress_phase = 'requeued_after_idle', progress_updated_at = NOW()
				WHERE id = $1 AND status = 'running'
			`, task.id)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				continue
			}
			_, _ = pgxTx.Exec(ctx, `
				DELETE FROM image_session_provider_effects WHERE generation_task_id = $1 AND effect_result = 'pending'
			`, task.id)
			if _, err := queue.StageForActor(ctx, pgxTx, queue.ActorImageSession, task.id, 0); err != nil {
				return err
			}
			summary.StaleRunningTasks++
			summary.EnqueuedTasks++
		}
		return nil
	})
	return summary, err
}
