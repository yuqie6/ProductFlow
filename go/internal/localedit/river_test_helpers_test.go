package localedit

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/yuqie6/productflow/internal/platform/queue"
)

func runLocalEditRiverWorker(t *testing.T, ctx context.Context, pool *pgxpool.Pool, taskID string, executor Executor) error {
	t.Helper()
	args, err := localEditRiverArgs(ctx, pool, taskID)
	if err != nil {
		return err
	}
	return runLocalEditRiverWorkerArgs(ctx, args, executor)
}

func localEditRiverArgs(ctx context.Context, pool *pgxpool.Pool, taskID string) (queue.TaskArgs, error) {
	var raw []byte
	if err := pool.QueryRow(ctx, `
		SELECT j.args
		FROM river_job j
		JOIN local_image_edit_tasks b ON b.id = j.args ->> 'aggregate_id'
		WHERE j.kind = 'productflow_task'
		  AND j.args ->> 'actor' = $1
		  AND j.args ->> 'aggregate_id' = $2
		  AND j.args ->> 'execution_id' = b.queue_execution_id
		ORDER BY j.id DESC
		LIMIT 1
	`, queue.ActorLocalEdit, taskID).Scan(&raw); err != nil {
		return queue.TaskArgs{}, err
	}
	var args queue.TaskArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return queue.TaskArgs{}, err
	}
	return args, nil
}

func runLocalEditRiverWorkerArgs(ctx context.Context, args queue.TaskArgs, executor Executor) error {
	worker := queue.NewWorker(map[string]queue.ActorFunc{queue.ActorLocalEdit: executor.Execute})
	return worker.Work(ctx, &river.Job[queue.TaskArgs]{Args: args})
}
