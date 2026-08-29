package agent

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
	PendingTurns       int
	EnqueuedTurns      int
	RecoveredTaskTurns int
	UnknownExecutions  int
}

func RecoverUnfinished(ctx context.Context, pool *pgxpool.Pool, _ int) (RecoverySummary, error) {
	gdb, err := pfdb.OpenGorm(pool)
	if err != nil {
		return RecoverySummary{}, err
	}
	return RecoverUnfinishedTurns(ctx, Service{DB: gdb})
}

func RecoverUnfinishedTurns(ctx context.Context, s Service) (RecoverySummary, error) {
	var out RecoverySummary
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if _, err := recoverExpiredExecutions(ctx, pgxTx); err != nil {
			return err
		}
		rows, err := pfdb.Query(ctx, pgxTx, `
			SELECT id FROM agent_turn_projections
			WHERE resume_required = FALSE AND (
				status IN ('queued','running','cancel_requested')
				OR (status = 'awaiting_confirmation' AND library_organization_draft_revision_id IS NULL AND workflow_run_request_id IS NULL)
			)
			ORDER BY created_at, id
		`)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		taskIDs, recovered, err := recoverQueuedTaskTurns(ctx, pgxTx)
		if err != nil {
			return err
		}
		ids = append(ids, taskIDs...)
		out.PendingTurns = len(ids)
		out.RecoveredTaskTurns = recovered
		for _, id := range ids {
			var status string
			err := pfdb.QueryRow(ctx, pgxTx, `
				SELECT status FROM async_dispatches
				WHERE actor_name = $1 AND aggregate_id = $2
				ORDER BY created_at DESC LIMIT 1
			`, queue.ActorAgentTurnSync, id).Scan(&status)
			if err == nil && (status == queue.StatusPending || status == queue.StatusSent) {
				continue
			}
			if _, err := queue.StageForActor(ctx, pgxTx, queue.ActorAgentTurnSync, id, 0); err != nil {
				return err
			}
			out.EnqueuedTurns++
		}
		return nil
	})
	return out, err
}

func recoverQueuedTaskTurns(ctx context.Context, pgxTx *gorm.DB) ([]string, int, error) {
	rows, err := pfdb.Query(ctx, pgxTx, `
		SELECT t.id, t.goal, t.conversation_id, c.scope_type, c.product_id
		FROM agent_tasks t
		JOIN agent_conversations c ON c.id = t.conversation_id
		WHERE t.status = 'queued' AND t.current_turn_id IS NULL AND t.conversation_id IS NOT NULL
		ORDER BY t.created_at, t.id
	`)
	if err != nil {
		return nil, 0, err
	}
	type item struct {
		id, goal, convID, scope string
		productID               *string
	}
	var tasks []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.goal, &it.convID, &it.scope, &it.productID); err != nil {
			rows.Close()
			return nil, 0, err
		}
		tasks = append(tasks, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var ids []string
	created := 0
	for _, task := range tasks {
		var productID *string
		if task.scope == "product_workflow" {
			productID = task.productID
		}
		key := "initial:" + task.convID + ":" + task.id
		row, wasCreated, err := reserveTurn(ctx, pgxTx, productID, task.convID, task.goal, nil, key, &task.id, nil)
		if err != nil {
			continue
		}
		ids = append(ids, row.ID)
		if wasCreated {
			created++
		}
	}
	return ids, created, nil
}

func recoverExpiredExecutions(ctx context.Context, pgxTx *gorm.DB) (int, error) {
	rows, err := pfdb.Query(ctx, pgxTx, `
		SELECT e.id, e.turn_projection_id, e.phase, t.status
		FROM agent_turn_executions e
		JOIN agent_turn_projections t ON t.id = e.turn_projection_id
		WHERE e.owner_id IS NOT NULL AND e.lease_expires_at IS NOT NULL AND e.lease_expires_at <= NOW()
		FOR UPDATE OF e
	`)
	if err != nil {
		return 0, err
	}
	type exec struct {
		id, projectionID, phase, status string
	}
	var items []exec
	for rows.Next() {
		var it exec
		if err := rows.Scan(&it.id, &it.projectionID, &it.phase, &it.status); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	unknown := 0
	now := time.Now().UTC()
	_ = now
	for _, item := range items {
		if _, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE agent_turn_executions
			SET fencing_token = fencing_token + 1, owner_id = NULL, lease_token = NULL, lease_expires_at = NULL, released_at = NOW()
			WHERE id = $1
		`, item.id); err != nil {
			return 0, err
		}
		if inSet(activeTurn, item.status) && !(item.status == "queued" && item.phase == "claimed") {
			if _, err := pfdb.Exec(ctx, pgxTx, `
				UPDATE agent_turn_projections
				SET status = 'unknown', error_text = $2, finished_at = NOW(), updated_at = NOW(), resume_required = FALSE
				WHERE id = $1
			`, item.projectionID, "Agent execution lease expired before this Turn reached a provable terminal state"); err != nil {
				return 0, err
			}
			if _, err := pfdb.Exec(ctx, pgxTx, `UPDATE agent_turn_executions SET phase = 'terminal' WHERE id = $1`, item.id); err != nil {
				return 0, err
			}
			unknown++
		}
	}
	return unknown, nil
}
