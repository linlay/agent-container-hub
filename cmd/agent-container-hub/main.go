package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"agent-container-hub/internal/config"
	"agent-container-hub/internal/httpserver"
	"agent-container-hub/internal/runtime"
	"agent-container-hub/internal/sandbox"
	"agent-container-hub/internal/store"
)

var buildVersion = "dev"

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if err := os.MkdirAll(cfg.RootfsRoot, 0o755); err != nil {
		log.Fatalf("create rootfs root: %v", err)
	}
	if err := os.MkdirAll(cfg.BuildRoot, 0o755); err != nil {
		log.Fatalf("create build root: %v", err)
	}
	if err := os.MkdirAll(cfg.ConfigRoot, 0o755); err != nil {
		log.Fatalf("create config root: %v", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	provider, err := runtime.NewAutoProvider(cfg.Engine)
	if err != nil {
		log.Fatalf("load runtime provider: %v", err)
	}
	appStore, err := store.Open(cfg.StateDBPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer appStore.Close()
	environmentStore, err := store.OpenFileEnvironmentStore(filepath.Join(cfg.ConfigRoot, "environments"))
	if err != nil {
		log.Fatalf("open environment store: %v", err)
	}

	sessionService := sandbox.NewSessionService(cfg, appStore, environmentStore, provider, logger)
	buildService := sandbox.NewBuildService(cfg, appStore, environmentStore, provider, logger)
	environmentService := sandbox.NewEnvironmentService(cfg.ConfigRoot, environmentStore, buildService, logger)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := sessionService.Reconcile(ctx); err != nil {
		logger.Error("reconcile failed", "error", err)
	}
	if err := buildService.ReconcileExistingImages(ctx); err != nil {
		logger.Error("reconcile existing images failed", "error", err)
	}

	server := &http.Server{
		Addr: cfg.BindAddr,
		Handler: httpserver.New(sessionService, environmentService, buildService, cfg.AuthToken, httpserver.Options{
			Logger:           logger,
			AccessLogEnabled: cfg.HTTPAccessLogEnabled,
			ErrorLogEnabled:  cfg.HTTPErrorLogEnabled,
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	logger.Info("http logging configured",
		"api_access_log_enabled", cfg.HTTPAccessLogEnabled,
		"api_error_log_enabled", cfg.HTTPErrorLogEnabled,
		"version", buildVersion,
	)
	logger.Info("sandbox daemon listening", "addr", cfg.BindAddr, "engine", provider.Name(), "version", buildVersion)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}
