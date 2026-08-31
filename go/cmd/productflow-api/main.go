package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	"github.com/yuqie6/productflow/internal/recipe"
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
	registerAPI(engine, apiHandlers{
		Auth:     auth.HTTP{AdminAccessKey: cfg.AdminAccessKey, Store: settingsStore},
		Settings: settings.HTTP{Store: settingsStore, DB: settingsStore, SettingsAccessToken: cfg.SettingsAccessToken},
		Product: product.HTTP{
			Service:  product.Service{DB: gdb, Media: mediaStore, Canvas: agent.WriteProductCanvas},
			Settings: settingsStore,
		},
		Library: library.HTTP{
			Service:  library.Service{DB: gdb, Media: mediaStore},
			Settings: settingsStore,
		},
		Graph: graph.HTTP{
			Service:  graphService,
			Settings: settingsStore,
		},
		Recipe: recipe.HTTP{
			Service:  recipe.Service{DB: gdb, Products: product.GraphGuard{}},
			Settings: settingsStore,
		},
		ImageSession: imagesession.HTTP{
			Service: imagesession.Service{
				DB: gdb, Media: mediaStore, Settings: settingsStore,
				Reconciler: liveImage,
			},
			Settings: settingsStore,
		},
		Delivery: delivery.HTTP{
			Service:  delivery.Service{DB: gdb, Media: mediaStore},
			Settings: settingsStore,
		},
		LocalEdit: localedit.HTTP{
			Service:  localedit.Service{DB: gdb, Media: mediaStore, Provider: liveImage},
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
