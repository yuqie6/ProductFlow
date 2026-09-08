// Command productflow-worker 消费 asynq 信封 run_async_dispatch，执行已经 SENT 的 async_dispatches。
//
// 只处理 dispatcher 标过 SENT 的行。MaxRetry=0：业务失败不要靠 asynq 重试，unknown/Busy/Later 由队列语义处理。
// actor 名与 queue.Actor* 必须一致，否则任务会被丢掉。不要在 worker 里再 Stage 同一作业造成双跑。
package main

import (
	"context"
	"encoding/json"
	"errors"
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

	redisOpt, err := queue.ParseRedis(cfg.RedisURL)
	if err != nil {
		logger.Fatal("redis", zap.Error(err))
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

	mux := asynq.NewServeMux()
	mux.HandleFunc(queue.TaskRunAsyncDispatch, func(ctx context.Context, task *asynq.Task) error {
		var payload queue.TaskPayload
		if err := json.Unmarshal(task.Payload(), &payload); err != nil {
			return err
		}
		return queue.Consume(ctx, pool, payload.DispatchID, payload.AggregateID, actors)
	})

	server := asynq.NewServer(redisOpt, asynq.Config{Concurrency: 4})
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(stop)
	logger.Info("worker listen")
	if err := runWorker(server, mux, stop); err != nil {
		logger.Fatal("worker", zap.Error(err))
	}
}

func runWorker(server *asynq.Server, handler asynq.Handler, stop <-chan os.Signal) error {
	// Run also handles process signals. A second Shutdown caller can return
	// while the first caller is still draining workers, letting main exit early.
	if err := server.Start(handler); err != nil {
		return err
	}
	<-stop
	server.Shutdown()
	return nil
}
