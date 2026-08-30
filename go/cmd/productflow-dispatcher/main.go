package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/yuqie6/productflow/internal/agent"
	"github.com/yuqie6/productflow/internal/delivery"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/imagesession"
	"github.com/yuqie6/productflow/internal/localedit"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/db"
	applog "github.com/yuqie6/productflow/internal/platform/log"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/settings"
	"go.uber.org/zap"
)

func main() {
	watch := flag.Bool("watch", false, "持续运行 recovery 和 dispatch loop")
	interval := flag.Float64("interval", 1, "watch 模式两轮之间的等待秒数")
	limit := flag.Int("limit", 100, "每轮最多处理多少条待投递记录")
	flag.Parse()
	if *interval <= 0 || *interval > 3600 {
		fmt.Fprintln(os.Stderr, "--interval 必须大于 0 且不超过 3600 秒")
		os.Exit(2)
	}

	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	logger, err := applog.New(applog.Options{
		Level:         cfg.LogLevel,
		Format:        cfg.LogFormat,
		Dir:           cfg.LogDir,
		Process:       applog.ProcessDispatcher,
		MaxBytes:      cfg.LogMaxBytes,
		BackupCount:   cfg.LogBackupCount,
		RetentionDays: cfg.LogRetentionDays,
	})
	if err != nil {
		panic(err)
	}
	defer func() { _ = logger.Sync() }()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	cancel()
	if err != nil {
		logger.Fatal("postgres", zap.Error(err))
	}
	defer pool.Close()

	redisOpt, err := queue.ParseRedis(cfg.RedisURL)
	if err != nil {
		logger.Fatal("redis", zap.Error(err))
	}
	client := asynq.NewClient(redisOpt)
	defer client.Close()
	enqueue := queue.EnqueueWith(client)

	runOnce := func() error {
		bg := context.Background()
		settingsStore := settings.NewStore(pool, cfg)
		imageStale := time.Duration(settingsStore.IntSetting(bg, "image_session_stale_running_after_minutes", 90)) * time.Minute
		workflow, err := graph.RecoverUnfinishedGraphRuns(bg, pool, 0, product.GraphGuard{})
		if err != nil {
			return err
		}
		imageSession, err := imagesession.RecoverUnfinished(bg, pool, imageStale)
		if err != nil {
			return err
		}
		rendition, err := delivery.RecoverUnfinished(bg, pool, 0)
		if err != nil {
			return err
		}
		localImageEdit, err := localedit.RecoverUnfinished(bg, pool, 0)
		if err != nil {
			return err
		}
		agentTurns, err := agent.RecoverUnfinished(bg, pool, 0)
		if err != nil {
			return err
		}
		summary, err := queue.RunDispatcherOnce(bg, pool, enqueue, *limit)
		if err != nil {
			return err
		}
		fields := []zap.Field{
			zap.Int("pending", summary.Pending),
			zap.Int("sent", summary.Sent),
			zap.Int("reconciled", summary.Reconciled),
			zap.Int("dead", summary.Dead),
			zap.Int("workflow", workflow.EnqueuedRuns),
			zap.Int("workflow_unknown", workflow.UnknownRuns),
			zap.Int("image_session", imageSession.EnqueuedTasks),
			zap.Int("image_session_unknown", imageSession.UnknownTasks),
			zap.Int("agent", agentTurns.EnqueuedTurns),
			zap.Int("rendition", rendition.EnqueuedJobs),
			zap.Int("local_image_edit", localImageEdit.EnqueuedTasks),
			zap.Int("local_image_edit_unknown", localImageEdit.UnknownTasks),
		}
		recoveryWork := []int{
			workflow.EnqueuedRuns, workflow.UnknownRuns,
			imageSession.EnqueuedTasks, imageSession.UnknownTasks,
			agentTurns.EnqueuedTurns, rendition.EnqueuedJobs,
			localImageEdit.EnqueuedTasks, localImageEdit.UnknownTasks,
		}
		if dispatcherCycleIdle(summary, recoveryWork...) {
			logger.Debug("dispatcher cycle", fields...)
		} else {
			logger.Info("dispatcher cycle", fields...)
		}
		return nil
	}

	if !*watch {
		if err := runOnce(); err != nil {
			logger.Fatal("dispatcher", zap.Error(err))
		}
		return
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	for {
		started := time.Now()
		if err := runOnce(); err != nil {
			logger.Error("dispatcher cycle", zap.Error(err))
		}
		remaining := time.Duration(*interval*float64(time.Second)) - time.Since(started)
		if remaining < 0 {
			remaining = 0
		}
		timer := time.NewTimer(remaining)
		select {
		case <-stop:
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func dispatcherCycleIdle(summary queue.Summary, recovery ...int) bool {
	if summary.Pending != 0 || summary.Sent != 0 || summary.Reconciled != 0 {
		return false
	}
	for _, n := range recovery {
		if n != 0 {
			return false
		}
	}
	return true
}
