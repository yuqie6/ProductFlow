package queueprobe

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/riverqueue/river/rivermigrate"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type probeArgs struct {
	ID string `json:"id"`
}

func (probeArgs) Kind() string { return "probe" }

type business struct {
	ID    string `gorm:"primaryKey"`
	State string
}

// 使用项目相同的 pgxpool -> database/sql -> GORM 路径；仅访问新建的独立库。
func isolated(t *testing.T) (*gorm.DB, *sql.DB, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	raw := os.Getenv("DATABASE_URL")
	if raw == "" {
		t.Fatal("DATABASE_URL required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	u.Scheme = strings.Split(u.Scheme, "+")[0]
	admin, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatal("open admin")
	}
	name := fmt.Sprintf("pf_queue_probe_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		admin.Close()
		t.Fatal("create isolated database", err)
	}
	t.Cleanup(func() {
		_, err := admin.Exec(context.Background(), "DROP DATABASE "+name+" WITH (FORCE)")
		admin.Close()
		if err != nil {
			t.Error("cleanup", err)
		}
	})
	u.Path = "/" + name
	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	sqlDB := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { sqlDB.Close(); pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().CreateTable(&business{}); err != nil {
		t.Fatal(err)
	}
	return db, sqlDB, pool
}

func wait(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("completion timeout")
	}
}

func TestRiverGormAtomicityAndWorker(t *testing.T) {
	db, sqlDB, pool := isolated(t)
	ctx := context.Background()
	driver := riverdatabasesql.NewWithPgxListener(sqlDB, pool)
	migrator, err := rivermigrate.New(driver, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		t.Fatal(err)
	}
	workers := river.NewWorkers()
	completed := make(chan struct{}, 2)
	river.AddWorker(workers, river.WorkFunc(func(ctx context.Context, j *river.Job[probeArgs]) error {
		var row business
		if err := db.WithContext(ctx).First(&row, "id = ?", j.Args.ID).Error; err != nil {
			return err
		}
		if row.State != "accepted" {
			return errors.New("business row not committed")
		}
		completed <- struct{}{}
		return nil
	}))
	config := &river.Config{Workers: workers, Queues: map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 2}}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	client, err := river.NewClient(driver, config)
	if err != nil {
		t.Fatal(err)
	}
	insert := func(tx *gorm.DB, id string) error {
		if err := tx.Create(&business{ID: id, State: "accepted"}).Error; err != nil {
			return err
		}
		sqlTx, ok := tx.Statement.ConnPool.(*sql.Tx)
		if !ok {
			return errors.New("GORM transaction cannot unwrap")
		}
		_, err := client.InsertTx(ctx, sqlTx, probeArgs{ID: id}, nil)
		return err
	}
	sentinel := errors.New("rollback")
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := insert(tx, "rolled_back"); err != nil {
			return err
		}
		return sentinel
	}); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	var jobs, rows int64
	if err := db.Table("river_job").Count(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&business{}).Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if jobs != 0 || rows != 0 {
		t.Fatalf("rollback leaked jobs=%d business=%d", jobs, rows)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Transaction(func(nested *gorm.DB) error {
			if err := insert(nested, "savepoint_rollback"); err != nil {
				return err
			}
			return sentinel
		}); !errors.Is(err, sentinel) {
			return err
		}
		return insert(tx, "committed")
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Table("river_job").Count(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&business{}).Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if jobs != 1 || rows != 1 {
		t.Fatalf("savepoint mismatch jobs=%d business=%d", jobs, rows)
	}
	// 生产者未启动消费；换一个客户端读取此前提交的持久任务。
	consumer, err := river.NewClient(driver, config)
	if err != nil {
		t.Fatal(err)
	}
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
	wait(t, completed)
	select {
	case <-events:
	case <-time.After(5 * time.Second):
		t.Fatal("job not acknowledged")
	}
	t.Log("outer rollback + nested savepoint rollback + committed business visibility + fresh consumer: PASS")
}

// 验证 asynq 重试可由持久业务阶段保护；这里是合成执行器，不能替代五类生产执行器验收。
func TestAsynqRetryDoesNotRepeatUnknownEffect(t *testing.T) {
	db, _, _ := isolated(t)
	if err := db.Create(&business{ID: "job", State: "accepted"}).Error; err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(t.TempDir(), "redis.sock")
	cmd := exec.Command("redis-server", "--port", "0", "--unixsocket", socket, "--save", "", "--appendonly", "no")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	opt := asynq.RedisClientOpt{Network: "unix", Addr: socket}
	client := asynq.NewClient(opt)
	defer client.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := client.Ping(); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("private Redis unavailable")
		}
		time.Sleep(20 * time.Millisecond)
	}
	var calls, attempts atomic.Int32
	done := make(chan struct{}, 1)
	server := asynq.NewServer(opt, asynq.Config{Concurrency: 1, TaskCheckInterval: 20 * time.Millisecond, DelayedTaskCheckInterval: 100 * time.Millisecond, RetryDelayFunc: func(int, error, *asynq.Task) time.Duration { return 0 }})
	mux := asynq.NewServeMux()
	mux.HandleFunc("probe", func(ctx context.Context, _ *asynq.Task) error {
		n := attempts.Add(1)
		claimed := db.WithContext(ctx).Model(&business{}).Where("id = ? AND state = ?", "job", "accepted").Update("state", "provider_pending")
		if claimed.Error != nil {
			return claimed.Error
		}
		if claimed.RowsAffected == 0 {
			var row business
			if err := db.First(&row, "id = ?", "job").Error; err != nil {
				return err
			}
			if row.State != "unknown" {
				return fmt.Errorf("unexpected state %s", row.State)
			}
			done <- struct{}{}
			return nil
		}
		calls.Add(1)
		if err := db.Model(&business{}).Where("id = ?", "job").Update("state", "unknown").Error; err != nil {
			return err
		}
		if n != 1 {
			return errors.New("effect replayed")
		}
		return errors.New("simulated failure after persisted unknown")
	})
	if err := server.Start(mux); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Shutdown)
	if _, err := client.Enqueue(asynq.NewTask("probe", nil), asynq.MaxRetry(1)); err != nil {
		t.Fatal(err)
	}
	wait(t, done)
	if attempts.Load() != 2 || calls.Load() != 1 {
		t.Fatalf("attempts=%d effects=%d", attempts.Load(), calls.Load())
	}
	t.Log("asynq actual retry attempts=2, fake external effects=1, PG unknown preserved: PASS")
}
