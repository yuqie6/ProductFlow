// Command productflow-worker 使用 River 消费与业务事务同时写入的 PostgreSQL 作业。
// 业务状态决定外部调用是否允许重试；River 负责投递、容量等待和基础设施错误重试。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yuqie6/productflow/internal/agent"
	"github.com/yuqie6/productflow/internal/delivery"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/imagesession"
	"github.com/yuqie6/productflow/internal/library"
	"github.com/yuqie6/productflow/internal/localedit"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/db"
	applog "github.com/yuqie6/productflow/internal/platform/log"
	pfmetrics "github.com/yuqie6/productflow/internal/platform/metrics"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/providers"
	"github.com/yuqie6/productflow/internal/providers/adapt"
	"github.com/yuqie6/productflow/internal/settings"
	"go.uber.org/zap"
	"go.uber.org/zap/exp/zapslog"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	logger, err := applog.New(applog.Options{
		Level:         cfg.LogLevel,
		Format:        cfg.LogFormat,
		Dir:           cfg.LogDir,
		Process:       applog.ProcessWorker,
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
	metricsServer := pfmetrics.NewServer(cfg.WorkerMetricsAddr, gdb, cfg.MetricsBearerToken)
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

	mediaStore := media.Store{Files: storage.Local{Root: cfg.StorageRoot}}
	productService := product.Service{DB: gdb, Media: mediaStore, Canvas: agent.WriteProductCanvas}
	deliveryService := delivery.Service{DB: gdb, Media: mediaStore}
	settingsStore := settings.NewStore(pool, cfg)
	liveImage := providers.LiveImage{Store: settingsStore}
	graphService := graph.Service{DB: gdb, AfterRunStatus: agent.SyncGraphRunToTasks, Products: product.GraphGuard{}}
	executor := graph.Executor{
		DB: gdb,
		Deps: graph.Dependencies{
			Prompt:   providers.LivePrompt{Store: settingsStore},
			Image:    adapt.GraphImage(liveImage),
			Assets:   productService,
			Delivery: deliveryService,
		},
		Log:            logger,
		AfterRunStatus: agent.SyncGraphRunToTasks,
		Products:       product.GraphGuard{},
	}
	imageExecutor := imagesession.Executor{DB: gdb, Media: mediaStore, Provider: adapt.Chat(liveImage, settingsStore)}
	deliveryExecutor := delivery.Executor{DB: gdb, Media: mediaStore}
	localExecutor := localedit.Executor{DB: gdb, Media: mediaStore, Provider: adapt.LocalEdit(liveImage)}
	poll := time.Duration(int(cfg.AgentTurnSyncPollSeconds*1000)) * time.Millisecond
	if poll < time.Millisecond {
		poll = time.Millisecond
	}
	agentExecutor := agent.Executor{Service: agent.Service{
		DB: gdb, Graph: graphService,
		Product: productService,
		Library: library.Service{DB: gdb, Media: mediaStore},
		Media:   mediaStore,
		Gateway: agent.HTTPGateway{
			BaseURL:     cfg.AgentServiceBaseURL,
			Token:       cfg.AgentServiceInternalToken,
			ReadTimeout: time.Duration(cfg.AgentServiceReadTimeoutSeconds * float64(time.Second)),
		},
		Poll: poll,
	}}
	actors := map[string]queue.ActorFunc{
		queue.ActorGraphRun: func(ctx context.Context, aggregateID string) error {
			logger.Info("consume", zap.String("actor", queue.ActorGraphRun), zap.String("workflow_run_id", aggregateID))
			return executor.ExecuteRun(ctx, aggregateID)
		},
		queue.ActorImageSession: func(ctx context.Context, aggregateID string) error {
			logger.Info("consume", zap.String("actor", queue.ActorImageSession), zap.String("image_session_generation_task_id", aggregateID))
			return imageExecutor.Execute(ctx, aggregateID)
		},
		queue.ActorDelivery: func(ctx context.Context, aggregateID string) error {
			logger.Info("consume", zap.String("actor", queue.ActorDelivery), zap.String("delivery_rendition_job_id", aggregateID))
			return deliveryExecutor.Execute(ctx, aggregateID)
		},
		queue.ActorLocalEdit: func(ctx context.Context, aggregateID string) error {
			logger.Info("consume", zap.String("actor", queue.ActorLocalEdit), zap.String("local_image_edit_task_id", aggregateID))
			return localExecutor.Execute(ctx, aggregateID)
		},
		queue.ActorAgentTurnSync: func(ctx context.Context, aggregateID string) error {
			logger.Info("consume", zap.String("actor", queue.ActorAgentTurnSync), zap.String("agent_turn_projection_id", aggregateID))
			return agentExecutor.Execute(ctx, aggregateID)
		},
	}

	worker, err := queue.NewClient(pool, actors, queue.WorkerConfig{
		Logger: slog.New(zapslog.NewHandler(logger.Core())).With("component", "river"),
	})
	if err != nil {
		logger.Fatal("worker setup", zap.Error(err))
	}
	workerCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// The signal initiates graceful Stop below; cancelling Start's context
	// immediately would interrupt paid provider calls before the grace period.
	if err := worker.Start(context.Background()); err != nil {
		logger.Fatal("worker start", zap.Error(err))
	}
	logger.Info("worker listen", zap.String("queue", "postgresql"))
	<-workerCtx.Done()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelShutdown()
	if err := worker.Stop(shutdownCtx); err != nil {
		logger.Error("worker shutdown", zap.Error(err))
	}
}
