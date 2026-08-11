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

	"github.com/yuqie6/productflow-agent-service/internal/app"
	"github.com/yuqie6/productflow-agent-service/internal/config"
	"github.com/yuqie6/productflow-agent-service/internal/productflow"
)

func main() {
	configValue, err := config.Load()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	productFlowClient, err := productflow.NewClient(
		configValue.ProductFlowBaseURL,
		configValue.InternalToken,
		&http.Client{Timeout: configValue.ProductFlowRequestTimeout},
	)
	if err != nil {
		slog.Error("create ProductFlow client", "error", err)
		os.Exit(1)
	}
	manager, err := app.NewManager(app.ManagerConfigFrom(configValue, productFlowClient))
	if err != nil {
		slog.Error("create conversation manager", "error", err)
		os.Exit(1)
	}
	serverHandler, err := app.NewServer(manager, configValue.InternalToken, configValue.MaxBodyBytes)
	if err != nil {
		slog.Error("create HTTP server", "error", err)
		os.Exit(1)
	}
	server := &http.Server{
		Addr: configValue.ListenAddress, Handler: serverHandler.Handler(),
		ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 75 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("HTTP shutdown failed", "error", err)
		}
	}()
	slog.Info("productflow agent service listening", "address", configValue.ListenAddress, "harness_commit", config.HarnessCommit)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("HTTP server failed", "error", err)
		os.Exit(1)
	}
	if err := manager.Close(); err != nil {
		slog.Error("close conversation services", "error", err)
		os.Exit(1)
	}
}
