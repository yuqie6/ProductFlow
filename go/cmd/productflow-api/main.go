package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/library"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	applog "github.com/yuqie6/productflow/internal/platform/log"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/product"
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

	engine := httpx.NewEngine(logger)
	cookieStore := httpx.NewCookieStore(httpx.SessionConfig{
		Secret: cfg.SessionSecret,
		Secure: cfg.SessionCookieSecure,
	})
	engine.Use(httpx.Session(cookieStore))
	httpx.RegisterHealth(engine, pool)
	settingsStore := settings.NewStore(pool, cfg)
	auth.HTTP{AdminAccessKey: cfg.AdminAccessKey, Store: settingsStore}.Register(engine)
	settings.HTTP{Store: settingsStore, SettingsAccessToken: cfg.SettingsAccessToken}.Register(engine)
	mediaStore := media.Store{Files: storage.Local{Root: cfg.StorageRoot}}
	product.HTTP{
		Service:  product.Service{Pool: pool, Media: mediaStore},
		Settings: settingsStore,
	}.Register(engine)
	library.HTTP{
		Service:  library.Service{Pool: pool, Media: mediaStore},
		Settings: settingsStore,
	}.Register(engine)

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
