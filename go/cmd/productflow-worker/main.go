package main

import (
	"context"
	"encoding/json"
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
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/providers"
	"github.com/yuqie6/productflow/internal/settings"
	"go.uber.org/zap"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	logger, err := applog.New(cfg.LogLevel)
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

	redisOpt, err := queue.ParseRedis(cfg.RedisURL)
	if err != nil {
		logger.Fatal("redis", zap.Error(err))
	}

	mediaStore := media.Store{Files: storage.Local{Root: cfg.StorageRoot}}
	productService := product.Service{DB: gdb, Media: mediaStore}
	deliveryService := delivery.Service{DB: gdb, Media: mediaStore}
	settingsStore := settings.NewStore(pool, cfg)
	liveImage := providers.LiveImage{Store: settingsStore}
	executor := graph.Executor{
		DB: gdb,
		Deps: graph.Dependencies{
			Prompt:   providers.LivePrompt{Store: settingsStore},
			Image:    liveImage,
			Assets:   productService,
			Delivery: deliveryService,
		},
	}
	imageExecutor := imagesession.Executor{DB: gdb, Media: mediaStore, Provider: liveImage}
	deliveryExecutor := delivery.Executor{DB: gdb, Media: mediaStore}
	localExecutor := localedit.Executor{DB: gdb, Media: mediaStore, Provider: liveImage}
	poll := time.Duration(int(cfg.AgentTurnSyncPollSeconds*1000)) * time.Millisecond
	if poll < time.Millisecond {
		poll = time.Millisecond
	}
	agentExecutor := agent.Executor{Service: agent.Service{
		DB: gdb, Graph: graph.Service{DB: gdb},
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
			return executor.ExecuteRun(ctx, aggregateID)
		},
		queue.ActorImageSession:  imageExecutor.Execute,
		queue.ActorDelivery:      deliveryExecutor.Execute,
		queue.ActorLocalEdit:     localExecutor.Execute,
		queue.ActorAgentTurnSync: agentExecutor.Execute,
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
	errCh := make(chan error, 1)
	go func() {
		logger.Info("worker listen")
		errCh <- server.Run(mux)
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-stop:
		server.Shutdown()
	case err := <-errCh:
		if err != nil {
			logger.Fatal("worker", zap.Error(err))
		}
	}
}
