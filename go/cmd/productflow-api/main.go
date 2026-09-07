// Command productflow-api 是浏览器打到的 HTTP 进程（默认 :29280）。
//
// 启动顺序：读 env overlay → 日志 → PostgreSQL/GORM → 本地 storage → 注册各切片路由。
// 不跑 asynq 消费，也不把 PENDING dispatch 标 SENT。改路由看 register.go；密钥只来自 env。
// 收到 SIGINT/SIGTERM 会 Shutdown，不要在这里 panic 普通校验错误（仅 config.Load 失败才 panic）。
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/agent"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/delivery"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/imagesession"
	"github.com/yuqie6/productflow/internal/library"
	"github.com/yuqie6/productflow/internal/localedit"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	applog "github.com/yuqie6/productflow/internal/platform/log"
	pfmetrics "github.com/yuqie6/productflow/internal/platform/metrics"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/providers"
	"github.com/yuqie6/productflow/internal/providers/adapt"
	"github.com/yuqie6/productflow/internal/quota"
	"github.com/yuqie6/productflow/internal/recipe"
	"github.com/yuqie6/productflow/internal/settings"
	"github.com/yuqie6/productflow/internal/visualsystem"
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
		Process:       applog.ProcessAPI,
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

	engine := httpx.NewEngine(logger)
	cookieStore := httpx.NewCookieStore(httpx.SessionConfig{
		Secret: cfg.SessionSecret,
		Secure: cfg.SessionCookieSecure,
	})
	engine.Use(httpx.Session(cookieStore))
	httpx.RegisterHealth(engine, pool)
	pfmetrics.Register(engine, gdb, cfg.MetricsBearerToken)
	settingsStore := settings.NewStore(pool, cfg)
	mediaStore := media.Store{Files: storage.Local{Root: cfg.StorageRoot}}
	liveImage := providers.LiveImage{Store: settingsStore}
	graphService := graph.Service{
		DB: gdb, Pool: pool, AfterRunStatus: agent.SyncGraphRunToTasks,
		AfterProposalDecision: agent.SyncGraphProposalDecision, Products: product.GraphGuard{},
	}
	poll := time.Duration(int(cfg.AgentTurnSyncPollSeconds*1000)) * time.Millisecond
	if poll < time.Millisecond {
		poll = time.Millisecond
	}
	authHTTP := auth.HTTP{
		AdminAccessKey: cfg.AdminAccessKey,
		Store:          settingsStore,
		DB:             gdb,
		Service:        auth.Service{DB: gdb},
	}
	httpx.AuthenticatedFunc = auth.Authenticated
	engine.Use(authHTTP.LoadPrincipal())
	engine.Use(authHTTP.AttachWorkingMerchant())
	engine.Use(authHTTP.RejectSuspendedMerchantWrites())
	operatorOnly := auth.RequireOperatorIf(func(c *gin.Context) (bool, error) {
		runtime, err := settingsStore.Runtime(c.Request.Context())
		if err != nil {
			return false, err
		}
		return runtime.AdminAccessRequired, nil
	})
	registerAPI(engine, apiHandlers{
		Auth: authHTTP,
		Settings: settings.HTTP{
			Store: settingsStore, DB: settingsStore, SettingsAccessToken: cfg.SettingsAccessToken,
			OperatorOnly: operatorOnly,
		},
		Quota: quota.HTTP{DB: gdb, Auth: authHTTP},
		Product: product.HTTP{
			Service: product.Service{
				DB: gdb, Media: mediaStore, Canvas: agent.WriteProductCanvas,
				SourceNote: providers.LivePrompt{Store: settingsStore},
			},
			Settings: settingsStore,
		},
		Library: library.HTTP{
			Service:  library.Service{DB: gdb, Media: mediaStore},
			Settings: settingsStore,
		},
		Graph: graph.HTTP{
			Service:           graphService,
			GenerationOptions: liveImage.GenerationOptions,
			Settings:          settingsStore,
		},
		Recipe: recipe.HTTP{
			Service:  recipe.Service{DB: gdb, Products: product.GraphGuard{}},
			Settings: settingsStore,
		},
		ImageSession: imagesession.HTTP{
			Service: imagesession.Service{
				DB: gdb, Pool: pool, Media: mediaStore, Settings: settingsStore,
				Reconciler: adapt.Reconciler(liveImage),
			},
			Settings: settingsStore,
		},
		Delivery: delivery.HTTP{
			Service:  delivery.Service{DB: gdb, Media: mediaStore},
			Settings: settingsStore,
		},
		VisualSystem: visualsystem.HTTP{
			Service:  visualsystem.Service{DB: gdb},
			Settings: settingsStore,
		},
		LocalEdit: localedit.HTTP{
			Service:  localedit.Service{DB: gdb, Media: mediaStore, Provider: adapt.LocalEdit(liveImage)},
			Settings: settingsStore,
		},
		Agent: agent.HTTP{
			Service: agent.Service{
				DB: gdb, Pool: pool, Graph: graphService,
				Product:  product.Service{DB: gdb, Media: mediaStore, Canvas: agent.WriteProductCanvas},
				Library:  library.Service{DB: gdb, Media: mediaStore},
				Media:    mediaStore,
				Settings: settingsStore,
				Gateway: agent.HTTPGateway{
					BaseURL:        cfg.AgentServiceBaseURL,
					Token:          cfg.AgentServiceInternalToken,
					ConnectTimeout: time.Duration(cfg.AgentServiceConnectTimeoutSeconds * float64(time.Second)),
					ReadTimeout:    time.Duration(cfg.AgentServiceReadTimeoutSeconds * float64(time.Second)),
				},
				Poll: poll,
			},
			Settings:      settingsStore,
			InternalToken: cfg.AgentServiceInternalToken,
		},
	})

	server := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           engine,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("listen", zap.String("addr", cfg.Addr()))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("http", zap.Error(err))
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
}
