package delivery

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/yuqie6/productflow/internal/platform/queue"
)

func runDeliveryRiverWorker(t *testing.T, ctx context.Context, pool *pgxpool.Pool, jobID string, executor Executor) error {
	t.Helper()
	args, err := deliveryRiverArgs(ctx, pool, jobID)
	if err != nil {
		return err
	}
	return runDeliveryRiverWorkerArgs(ctx, args, executor)
}

func deliveryRiverArgs(ctx context.Context, pool *pgxpool.Pool, jobID string) (queue.TaskArgs, error) {
	var raw []byte
	if err := pool.QueryRow(ctx, `
		SELECT j.args
		FROM river_job j
		JOIN delivery_rendition_jobs b ON b.id = j.args ->> 'aggregate_id'
		WHERE j.kind = 'productflow_task'
		  AND j.args ->> 'actor' = $1
		  AND j.args ->> 'aggregate_id' = $2
		  AND j.args ->> 'execution_id' = b.queue_execution_id
		ORDER BY j.id DESC
		LIMIT 1
	`, queue.ActorDelivery, jobID).Scan(&raw); err != nil {
		return queue.TaskArgs{}, err
	}
	var args queue.TaskArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return queue.TaskArgs{}, err
	}
	return args, nil
}

func runDeliveryRiverWorkerArgs(ctx context.Context, args queue.TaskArgs, executor Executor) error {
	worker := queue.NewWorker(map[string]queue.ActorFunc{queue.ActorDelivery: executor.Execute})
	return worker.Work(ctx, &river.Job[queue.TaskArgs]{Args: args})
}
