package queueprobe

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/riverqueue/river/rivermigrate"
	"gorm.io/gorm"
)

func crashClient(t *testing.T, sqlDB *sql.DB, pool *pgxpool.Pool, work func(context.Context, *river.Job[probeArgs]) error) *river.Client[*sql.Tx] {
	t.Helper()
	workers := river.NewWorkers()
	river.AddWorker(workers, river.WorkFunc(work))
	client, err := river.NewClient(riverdatabasesql.NewWithPgxListener(sqlDB, pool), &river.Config{
		Workers: workers, Queues: map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 1}},
		JobTimeout: 100 * time.Millisecond, RescueStuckJobsAfter: time.Second,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// 只作为独立子进程入口；正常测试调用不建立资源。
func TestRiverCrashChild(t *testing.T) {
	raw := os.Getenv("PF_QUEUE_CRASH_DATABASE")
	if raw == "" {
		return
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, raw)
	if err != nil {
		t.Fatal("child database config")
	}
	defer pool.Close()
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()
	client := crashClient(t, sqlDB, pool, func(ctx context.Context, j *river.Job[probeArgs]) error {
		if _, err := pool.Exec(ctx, "UPDATE businesses SET state = 'provider_pending' WHERE id = $1", j.Args.ID); err != nil {
			return err
		}
		// HTTP provider 是父进程，SIGKILL 不会撤销它已经记录的效果。
		response, err := http.Get(os.Getenv("PF_QUEUE_CRASH_PROVIDER"))
		if err != nil {
			return err
		}
		response.Body.Close()
		select {} // 模拟无法通过 context 取消的失联执行；父进程随后发送 SIGKILL。
	})
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	select {}
}

func TestRiverSIGKILLUnknownDoesNotReplayProvider(t *testing.T) {
	db, sqlDB, pool := isolated(t)
	ctx := context.Background()
	driver := riverdatabasesql.NewWithPgxListener(sqlDB, pool)
	migrator, err := rivermigrate.New(driver, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		t.Fatal(err)
	}
	var effects atomic.Int32
	called := make(chan struct{}, 2)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		effects.Add(1)
		called <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer provider.Close()
	producer, err := river.NewClient(driver, &river.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&business{ID: "crash", State: "accepted"}).Error; err != nil {
			return err
		}
		_, err := producer.InsertTx(ctx, tx.Statement.ConnPool.(*sql.Tx), probeArgs{ID: "crash"}, nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestRiverCrashChild$", "-test.timeout=60s")
	child.Env = append(os.Environ(), "PF_QUEUE_CRASH_DATABASE="+pool.Config().ConnString(), "PF_QUEUE_CRASH_PROVIDER="+provider.URL)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		if !waited {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	})
	wait(t, called)
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err == nil {
		t.Fatal("child should be killed")
	}
	waited = true
	// 不修改 attempted_at 或业务行时间，实际等待测试配置的一秒过期。
	time.Sleep(1100 * time.Millisecond)
	restored := make(chan struct{}, 1)
	consumer := crashClient(t, sqlDB, pool, func(ctx context.Context, j *river.Job[probeArgs]) error {
		if j.Attempt < 2 {
			return fmt.Errorf("expected rescued attempt, got %d", j.Attempt)
		}
		result := db.WithContext(ctx).Model(&business{}).Where("id = ? AND state = ?", j.Args.ID, "provider_pending").Update("state", "unknown")
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("lost pending state")
		}
		restored <- struct{}{}
		return nil
	})
	events, unsub := consumer.Subscribe(river.EventKindJobCompleted)
	defer unsub()
	if err := consumer.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := consumer.Stop(stop); err != nil {
			t.Error(err)
		}
	})
	select {
	case <-restored:
	case <-time.After(45 * time.Second):
		t.Fatal("crashed job not rescued within probe window")
	}
	select {
	case <-events:
	case <-time.After(5 * time.Second):
		t.Fatal("rescued job not completed")
	}
	var row business
	if err := db.First(&row, "id = ?", "crash").Error; err != nil {
		t.Fatal(err)
	}
	if row.State != "unknown" || effects.Load() != 1 {
		t.Fatalf("state=%s provider_calls=%d", row.State, effects.Load())
	}
	t.Log("SIGKILL after HTTP provider effect; real rescue attempt>=2; unknown retained; provider_calls=1: PASS")
}
