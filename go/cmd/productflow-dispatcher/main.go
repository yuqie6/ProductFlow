// Command productflow-dispatcher recovers unfinished business effects and quota holds.
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

	"github.com/yuqie6/productflow/internal/agent"
	"github.com/yuqie6/productflow/internal/delivery"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/imagesession"
	"github.com/yuqie6/productflow/internal/localedit"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/db"
	applog "github.com/yuqie6/productflow/internal/platform/log"
	pfmetrics "github.com/yuqie6/productflow/internal/platform/metrics"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/quota"
	"github.com/yuqie6/productflow/internal/settings"
	"go.uber.org/zap"
)

func main() {
	watch := flag.Bool("watch", false, "持续运行各业务域恢复")
	recoveryInterval := flag.Float64("recovery-interval", 10, "watch 模式两轮 recovery 之间的等待秒数")
	flag.Parse()
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

	settingsStore := settings.NewStore(pool, cfg)

	recoverySteps := []recoveryStep{
		{
			domain: "graph", errorContext: "workflow recovery",
			run: func(ctx context.Context) (err error) {
				workflow, err := graph.RecoverUnfinishedGraphRuns(ctx, pool, 0, product.GraphGuard{})
				logRecoveryWork(logger, "graph", workflow.EnqueuedRuns, workflow.UnknownRuns, workflow.HasMore)
				return err
			},
		},
		{
			domain: "image_session", errorContext: "image session recovery",
			run: func(ctx context.Context) (err error) {
				imageStale := time.Duration(settingsStore.IntSetting(ctx, "image_session_stale_running_after_minutes", int(imagesession.DefaultStaleRunningAfter/time.Minute))) * time.Minute
				imageSession, err := imagesession.RecoverUnfinished(ctx, pool, imageStale)
				logRecoveryWork(logger, "image_session", imageSession.EnqueuedTasks, imageSession.UnknownTasks, imageSession.HasMore)
				return err
			},
		},
		{
			domain: "delivery", errorContext: "delivery recovery",
			run: func(ctx context.Context) (err error) {
				rendition, err := delivery.RecoverUnfinished(ctx, pool, 0)
				logRecoveryWork(logger, "delivery", rendition.EnqueuedJobs, 0, rendition.HasMore)
				return err
			},
		},
		{
			domain: "local_image_edit", errorContext: "local image edit recovery",
			run: func(ctx context.Context) (err error) {
				localImageEdit, err := localedit.RecoverUnfinished(ctx, pool, 0)
				logRecoveryWork(logger, "local_image_edit", localImageEdit.EnqueuedTasks, localImageEdit.UnknownTasks, localImageEdit.HasMore)
				return err
			},
		},
		{
			domain: "agent", errorContext: "agent recovery",
			run: func(ctx context.Context) (err error) {
				agentTurns, err := agent.RecoverUnfinished(ctx, pool, 0)
				logRecoveryWork(logger, "agent", agentTurns.EnqueuedTurns, 0, agentTurns.HasMore)
				return err
			},
		},
		{
			domain: "quota_unknown", errorContext: "quota unknown expiry",
			run: func(ctx context.Context) (err error) {
				expired, hasMore, err := (&quota.Service{DB: gdb}).ExpireUnknownHolds(ctx, time.Now().UTC(), 0)
				logRecoveryWork(logger, "quota_unknown", expired, 0, hasMore)
				return err
			},
		},
	}
	reportRecovery := func(result recoveryStepResult) {
		pfmetrics.ObserveRecovery(result.domain, result.duration, result.err != nil)
		if result.err != nil {
			logger.Error("dispatcher recovery domain",
				zap.String("domain", result.domain),
				zap.Int64("duration_ms", result.duration.Milliseconds()),
				zap.Error(result.err),
			)
		}
	}
	if !*watch {
		if err := runRecoverySteps(context.Background(), recoverySteps, reportRecovery); err != nil {
			logger.Fatal("business recovery", zap.Error(err))
		}
		return
	}
	watchCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	runRecoveryLoops(watchCtx, time.Duration(*recoveryInterval*float64(time.Second)), recoverySteps, reportRecovery)
}

func logRecoveryWork(logger *zap.Logger, domain string, enqueued, unknown int, hasMore bool) {
	fields := []zap.Field{zap.String("domain", domain), zap.Int("enqueued", enqueued), zap.Int("unknown", unknown), zap.Bool("has_more", hasMore)}
	if enqueued == 0 && unknown == 0 && !hasMore {
		logger.Debug("dispatcher recovery batch", fields...)
	} else {
		logger.Info("dispatcher recovery batch", fields...)
	}
}
