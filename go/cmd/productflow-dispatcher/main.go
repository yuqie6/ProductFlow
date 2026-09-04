// Command productflow-dispatcher 把 PENDING 的 async_dispatches 标 SENT 并入 asynq，再跑各域未完成作业恢复。
//
// HTTP/API 禁止直接 enqueue。本进程是唯一把 PENDING→SENT 的地方。空闲轮询是 Debug 日志，不要改成 Info 刷屏。
// 恢复会调 graph/agent/imagesession/localedit/delivery 的 RecoverUnfinished；改恢复顺序要先想清楚锁与幂等键。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
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
	pfmetrics "github.com/yuqie6/productflow/internal/platform/metrics"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/settings"
	"go.uber.org/zap"
)

func main() {
	watch := flag.Bool("watch", false, "持续运行 recovery 和 dispatch loop")
	interval := flag.Float64("interval", 1, "watch 模式两轮 dispatch 之间的等待秒数")
	recoveryInterval := flag.Float64("recovery-interval", 10, "watch 模式两轮 recovery 之间的等待秒数")
	limit := flag.Int("limit", 100, "每轮最多处理多少条待投递记录")
	flag.Parse()
	if *interval <= 0 || *interval > 3600 {
		fmt.Fprintln(os.Stderr, "--interval 必须大于 0 且不超过 3600 秒")
		os.Exit(2)
	}
	if *recoveryInterval <= 0 || *recoveryInterval > 3600 {
		fmt.Fprintln(os.Stderr, "--recovery-interval 必须大于 0 且不超过 3600 秒")
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
	gdb, err := db.OpenGorm(pool)
	if err != nil {
		logger.Fatal("gorm", zap.Error(err))
	}
	metricsServer := pfmetrics.NewServer(cfg.DispatcherMetricsAddr, gdb, cfg.MetricsBearerToken)
	if metricsServer != nil {
		go func() {
			if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("metrics server", zap.Error(err))
			}
		}()
		defer func() {
			shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelShutdown()
			_ = metricsServer.Shutdown(shutdownCtx)
		}()
	}

	redisOpt, err := queue.ParseRedis(cfg.RedisURL)
	if err != nil {
		logger.Fatal("redis", zap.Error(err))
	}
	client := asynq.NewClient(redisOpt)
	defer client.Close()
	enqueue := queue.EnqueueWith(client)
	settingsStore := settings.NewStore(pool, cfg)

	runRecoveryCycle := func(ctx context.Context) error {
		recoveryStarted := time.Now()
		var workflow graph.RecoverySummary
		var imageSession imagesession.RecoverySummary
		var rendition delivery.RecoverySummary
		var localImageEdit localedit.RecoverySummary
		var agentTurns agent.RecoverySummary
		imageStale := time.Duration(settingsStore.IntSetting(ctx, "image_session_stale_running_after_minutes", 90)) * time.Minute
		recoveryErr := runRecoverySteps(ctx, []recoveryStep{
			{
				domain: "graph", errorContext: "workflow recovery",
				run: func(ctx context.Context) (err error) {
					workflow, err = graph.RecoverUnfinishedGraphRuns(ctx, pool, 0, product.GraphGuard{})
					return err
				},
			},
			{
				domain: "image_session", errorContext: "image session recovery",
				run: func(ctx context.Context) (err error) {
					imageSession, err = imagesession.RecoverUnfinished(ctx, pool, imageStale)
					return err
				},
			},
			{
				domain: "delivery", errorContext: "delivery recovery",
				run: func(ctx context.Context) (err error) {
					rendition, err = delivery.RecoverUnfinished(ctx, pool, 0)
					return err
				},
			},
			{
				domain: "local_image_edit", errorContext: "local image edit recovery",
				run: func(ctx context.Context) (err error) {
					localImageEdit, err = localedit.RecoverUnfinished(ctx, pool, 0)
					return err
				},
			},
			{
				domain: "agent", errorContext: "agent recovery",
				run: func(ctx context.Context) (err error) {
					agentTurns, err = agent.RecoverUnfinished(ctx, pool, 0)
					return err
				},
			},
		}, func(result recoveryStepResult) {
			pfmetrics.ObserveRecovery(result.domain, result.duration, result.err != nil)
			if result.err != nil {
				logger.Error("dispatcher recovery domain",
					zap.String("domain", result.domain),
					zap.Int64("duration_ms", result.duration.Milliseconds()),
					zap.Error(result.err),
				)
			}
		})
		recoveryDuration := time.Since(recoveryStarted)
		fields := []zap.Field{
			zap.Int64("recovery_duration_ms", recoveryDuration.Milliseconds()),
			zap.Int("workflow", workflow.EnqueuedRuns),
			zap.Int("workflow_unknown", workflow.UnknownRuns),
			zap.Int("image_session", imageSession.EnqueuedTasks),
			zap.Int("image_session_unknown", imageSession.UnknownTasks),
			zap.Int("agent", agentTurns.EnqueuedTurns),
			zap.Int("rendition", rendition.EnqueuedJobs),
			zap.Int("local_image_edit", localImageEdit.EnqueuedTasks),
			zap.Int("local_image_edit_unknown", localImageEdit.UnknownTasks),
			zap.Bool("workflow_more", workflow.HasMore),
			zap.Bool("image_session_more", imageSession.HasMore),
			zap.Bool("rendition_more", rendition.HasMore),
			zap.Bool("local_image_edit_more", localImageEdit.HasMore),
			zap.Bool("agent_more", agentTurns.HasMore),
		}
		recoveryWork := []int{
			workflow.EnqueuedRuns, workflow.UnknownRuns,
			imageSession.EnqueuedTasks, imageSession.UnknownTasks,
			agentTurns.EnqueuedTurns, rendition.EnqueuedJobs,
			localImageEdit.EnqueuedTasks, localImageEdit.UnknownTasks,
		}
		for _, hasMore := range []bool{
			workflow.HasMore, imageSession.HasMore, rendition.HasMore,
			localImageEdit.HasMore, agentTurns.HasMore,
		} {
			if hasMore {
				recoveryWork = append(recoveryWork, 1)
			}
		}
		if dispatcherCycleIdle(queue.Summary{}, recoveryWork...) {
			logger.Debug("dispatcher recovery cycle", fields...)
		} else {
			logger.Info("dispatcher recovery cycle", fields...)
		}
		return recoveryErr
	}
	runDispatchCycle := func(ctx context.Context) error {
		summary, err := queue.RunDispatcherOnce(ctx, pool, enqueue, *limit)
		if err != nil {
			return fmt.Errorf("dispatch: %w", err)
		}
		fields := []zap.Field{
			zap.Int("pending", summary.Pending),
			zap.Int("sent", summary.Sent),
			zap.Int("reconciled", summary.Reconciled),
			zap.Int("dead", summary.Dead),
		}
		if dispatcherCycleIdle(summary) {
			logger.Debug("dispatcher dispatch cycle", fields...)
		} else {
			logger.Info("dispatcher dispatch cycle", fields...)
		}
		return nil
	}

	if !*watch {
		if err := runOneShot(context.Background(), runRecoveryCycle, runDispatchCycle); err != nil {
			logger.Fatal("dispatcher", zap.Error(err))
		}
		return
	}

	watchCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	dispatchWake, waitListen := startDispatchWake(watchCtx, pool, logger)
	runWatchLoops(
		watchCtx,
		time.Duration(*interval*float64(time.Second)),
		time.Duration(*recoveryInterval*float64(time.Second)),
		dispatchWake,
		runDispatchCycle,
		runRecoveryCycle,
		func(err error) {
			if err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("dispatcher dispatch loop", zap.Error(err))
			}
		},
		func(err error) {
			if err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("dispatcher recovery loop", zap.Error(err))
			}
		},
	)
	waitListen()
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
