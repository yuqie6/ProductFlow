package queue

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/riverqueue/river/rivertype"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"gorm.io/gorm"
)

func queueFixture(t *testing.T) (*pgxpool.Pool, *gorm.DB, context.Context, schema.ImageSessionGenerationTasks) {
	t.Helper()
	pool, db := testdb.Open(t)
	merchant := auth.MustDevMerchantID(t, db)
	ctx := auth.WithMerchantID(context.Background(), merchant)
	now := time.Now().UTC()
	session := schema.ImageSessions{ID: clockid.New(), MerchantID: merchant, Title: "queue integration", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	task := schema.ImageSessionGenerationTasks{ID: clockid.New(), SessionID: session.ID, Status: "queued", Prompt: "test", Size: "1024x1024", GenerationCount: 1, CreatedAt: now, IsRetryable: true}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM river_job WHERE args ->> 'aggregate_id' = ?", task.ID)
		db.Delete(&task)
		db.Delete(&session)
	})
	return pool, db, ctx, task
}

func enqueueFixture(t *testing.T, ctx context.Context, db *gorm.DB, id string) RiverJob {
	t.Helper()
	var job RiverJob
	if err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		job, err = StageTaskForActor(ctx, tx, ActorImageSession, id, 0)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return job
}

func TestRiverSubmissionSharesBusinessTransaction(t *testing.T) {
	_, db, ctx, task := queueFixture(t)
	sentinel := errors.New("rollback")
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&schema.ImageSessionGenerationTasks{}).Where("id=?", task.ID).Update("prompt", "not committed").Error; err != nil {
			return err
		}
		if _, err := StageTaskForActor(ctx, tx, ActorImageSession, task.ID, 0); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	var count int64
	if err := db.Table("river_job").Where("args ->> 'aggregate_id'=?", task.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rollback left %d jobs", count)
	}
	var stored schema.ImageSessionGenerationTasks
	if err := db.First(&stored, "id=?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Prompt != "test" {
		t.Fatalf("business write survived rollback: %q", stored.Prompt)
	}
	var committed RiverJob
	if err := db.Transaction(func(tx *gorm.DB) error {
		nestedErr := tx.Transaction(func(inner *gorm.DB) error {
			if _, err := StageTaskForActor(ctx, inner, ActorImageSession, task.ID, 0); err != nil {
				return err
			}
			return sentinel
		})
		if !errors.Is(nestedErr, sentinel) {
			return nestedErr
		}
		var err error
		committed, err = StageTaskForActor(ctx, tx, ActorImageSession, task.ID, 0)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	again := enqueueFixture(t, ctx, db, task.ID)
	if committed.ID != again.ID {
		t.Fatalf("duplicate active invocation: %d != %d", committed.ID, again.ID)
	}
	wrong := auth.WithMerchantID(ctx, "wrong-merchant")
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, err := StageTaskForActor(wrong, tx, ActorImageSession, task.ID, 0)
		return err
	}); !errors.Is(err, ErrSuperseded) {
		t.Fatalf("foreign submission: %v", err)
	}
}

func TestRiverStoppedExecutionCannotRestageAndOldInvocationCannotClaimRetry(t *testing.T) {
	_, db, ctx, task := queueFixture(t)
	old := enqueueFixture(t, ctx, db, task.ID)
	if err := db.Table("river_job").Where("id=?", old.ID).Updates(map[string]any{"state": rivertype.JobStateDiscarded, "finalized_at": time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		changed, err := RestageTaskIfIdle(ctx, tx, ActorImageSession, task.ID, nil)
		if changed {
			t.Error("discarded execution revived")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var next RiverJob
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&task).Update("queue_execution_id", clockid.New()).Error; err != nil {
			return err
		}
		var err error
		next, err = StageTaskForActor(ctx, tx, ActorImageSession, task.ID, 0)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if old.ID == next.ID || old.Args.ExecutionID == next.Args.ExecutionID {
		t.Fatal("retry reused old execution")
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return AssertExecution(withTaskArgs(ctx, old.Args), tx, ActorImageSession, task.ID)
	}); !errors.Is(err, ErrSuperseded) {
		t.Fatalf("old claim: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return AssertExecution(withTaskArgs(ctx, next.Args), tx, ActorImageSession, task.ID)
	}); err != nil {
		t.Fatal(err)
	}
	// Queue history cleanup cannot erase the identity stored on the business row.
	if err := db.Exec("DELETE FROM river_job WHERE id=?", old.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return AssertExecution(withTaskArgs(ctx, old.Args), tx, ActorImageSession, task.ID)
	}); !errors.Is(err, ErrSuperseded) {
		t.Fatal(err)
	}
}

func TestRiverWorkerRetriesInfrastructureWithoutRepeatingBusinessEffect(t *testing.T) {
	pool, db, ctx, task := queueFixture(t)
	job := enqueueFixture(t, ctx, db, task.ID)
	var attempts atomic.Int32
	var effects atomic.Int32
	worker, err := NewClient(pool, map[string]ActorFunc{ActorImageSession: func(ctx context.Context, id string) error {
		if id != task.ID {
			return nil
		}
		n := attempts.Add(1)
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := AssertExecution(ctx, tx, ActorImageSession, id); err != nil {
				return err
			}
			var row schema.ImageSessionGenerationTasks
			if err := tx.First(&row, "id=?", id).Error; err != nil {
				return err
			}
			if row.Status == "unknown" {
				return nil
			}
			effects.Add(1)
			return tx.Model(&row).Update("status", "unknown").Error
		}); err != nil {
			return err
		}
		if n == 1 {
			return errors.New("infrastructure error after persisted unknown")
		}
		return nil
	}}, WorkerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := worker.Stop(stop); err != nil {
			t.Error(err)
		}
	})
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var state string
		if err := db.Table("river_job").Select("state").Where("id=?", job.ID).Scan(&state).Error; err != nil {
			t.Fatal(err)
		}
		if state == string(rivertype.JobStateCompleted) {
			if attempts.Load() != 2 || effects.Load() != 1 {
				t.Fatalf("attempts=%d effects=%d", attempts.Load(), effects.Load())
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("job did not complete, attempts=%d effects=%d", attempts.Load(), effects.Load())
}

func TestWorkerSnoozesCapacityButPropagatesInfrastructureFailure(t *testing.T) {
	for _, cause := range []error{ErrBusy, ErrLater, errors.New("database unavailable"), ErrSuperseded} {
		w := taskWorker{actors: map[string]ActorFunc{ActorImageSession: func(context.Context, string) error { return cause }}}
		err := w.Work(context.Background(), &river.Job[TaskArgs]{Args: TaskArgs{Actor: ActorImageSession}})
		var snooze *river.JobSnoozeError
		switch cause {
		case ErrBusy, ErrLater:
			if !errors.As(err, &snooze) {
				t.Fatalf("want snooze, got %v", err)
			}
		case ErrSuperseded:
			if err != nil {
				t.Fatal(err)
			}
		default:
			if !errors.Is(err, cause) {
				t.Fatal(err)
			}
		}
	}
}

func TestRiverStopTimeoutCancelsWorkBeforeClosingSQL(t *testing.T) {
	pool, db, ctx, task := queueFixture(t)
	enqueueFixture(t, ctx, db, task.ID)
	started := make(chan struct{})
	checkpoint := make(chan error, 1)
	worker, err := NewClient(pool, map[string]ActorFunc{ActorImageSession: func(ctx context.Context, id string) error {
		if id != task.ID {
			return nil
		}
		close(started)
		<-ctx.Done()
		// The running handler must retain database access after cancellation.
		persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		err := db.WithContext(persist).Model(&schema.ImageSessionGenerationTasks{}).Where("id=?", id).Update("status", "unknown").Error
		checkpoint <- err
		return err
	}}, WorkerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stop, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = worker.Stop(stop)
	})
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not start")
	}
	stop, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := worker.Stop(stop); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stop=%v", err)
	}
	select {
	case err := <-checkpoint:
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("stop returned before checkpoint")
	}
	select {
	case <-worker.client.Stopped():
	default:
		t.Fatal("worker remains alive")
	}
}

func TestRiverReplicasConsumeWithoutNotifications(t *testing.T) {
	pool, db, ctx, first := queueFixture(t)
	tasks := []schema.ImageSessionGenerationTasks{first}
	for i := 1; i < 12; i++ {
		_, _, _, task := queueFixture(t)
		tasks = append(tasks, task)
	}
	var calls sync.Map
	actor := func(ctx context.Context, id string) error {
		counter, _ := calls.LoadOrStore(id, &atomic.Int32{})
		counter.(*atomic.Int32).Add(1)
		return db.Transaction(func(tx *gorm.DB) error {
			if err := AssertExecution(ctx, tx, ActorImageSession, id); err != nil {
				return err
			}
			return tx.Model(&schema.ImageSessionGenerationTasks{}).Where("id=?", id).Update("status", "succeeded").Error
		})
	}
	// River's documented polling mode makes delivery independent of NOTIFY.
	// Both native clients use the same typed handler and SQL transaction adapter.
	for i := 0; i < 2; i++ {
		sqlDB := stdlib.OpenDBFromPool(pool)
		workers := river.NewWorkers()
		river.AddWorker(workers, NewWorker(map[string]ActorFunc{ActorImageSession: actor}))
		client, err := river.NewClient(riverdatabasesql.New(sqlDB), &river.Config{
			Workers: workers, Queues: map[string]river.QueueConfig{"generation": {MaxWorkers: 3}}, PollOnly: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := client.Start(ctx); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := client.Stop(stop); err != nil {
				t.Error(err)
			}
			_ = sqlDB.Close()
		})
	}
	jobs := make([]int64, 0, len(tasks))
	for _, task := range tasks {
		jobs = append(jobs, enqueueFixture(t, ctx, db, task.ID).ID)
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var completed int64
		if err := db.Table("river_job").Where("id IN ? AND state='completed'", jobs).Count(&completed).Error; err != nil {
			t.Fatal(err)
		}
		if completed == int64(len(tasks)) {
			for _, task := range tasks {
				value, ok := calls.Load(task.ID)
				if !ok || value.(*atomic.Int32).Load() != 1 {
					t.Fatalf("task %s duplicate or missing invocation", task.ID)
				}
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("polling replicas did not consume all committed jobs")
}
