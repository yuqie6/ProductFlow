package imagesession

import (
	"context"
	"testing"

	"github.com/riverqueue/river"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"gorm.io/gorm"
)

func imageSessionQueueContext(t *testing.T, db *gorm.DB) context.Context {
	t.Helper()
	return auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, db))
}

func stageImageSessionJob(t *testing.T, db *gorm.DB, taskID string) queue.RiverJob {
	t.Helper()
	ctx := imageSessionQueueContext(t, db)
	var job queue.RiverJob
	if err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		job, err = queue.StageTaskForActor(ctx, tx, queue.ActorImageSession, taskID, 0)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return job
}

func executeImageSessionJob(ctx context.Context, job queue.RiverJob, executor Executor) error {
	worker := queue.NewWorker(map[string]queue.ActorFunc{
		queue.ActorImageSession: executor.Execute,
	})
	return worker.Work(ctx, &river.Job[queue.TaskArgs]{Args: job.Args})
}

func riverImageSessionJobCount(t *testing.T, ss *sessionServer, taskID string) int {
	t.Helper()
	var count int
	if err := ss.pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM river_job
		WHERE kind = 'productflow_task'
		  AND args ->> 'actor' = 'run_image_session_generation_task'
		  AND args ->> 'aggregate_id' = $1
	`, taskID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
