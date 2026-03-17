package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"agentbox/internal/config"
	"agentbox/internal/httpserver"
	"agentbox/internal/runtime"
	"agentbox/internal/sandbox"
	"agentbox/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if err := os.MkdirAll(cfg.WorkspaceRoot, 0o755); err != nil {
		log.Fatalf("create workspace root: %v", err)
	}
	if err := os.MkdirAll(cfg.BuildRoot, 0o755); err != nil {
		log.Fatalf("create build root: %v", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	provider, err := runtime.NewAutoProvider(cfg.Engine)
	if err != nil {
		log.Fatalf("load runtime provider: %v", err)
	}
	builder, ok := provider.(runtime.Builder)
	if !ok {
		log.Fatalf("runtime provider %T does not support image builds", provider)
	}
	st, err := store.Open(cfg.StateDBPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	sessionService := sandbox.NewSessionService(cfg, st, provider, logger)
	environmentService := sandbox.NewEnvironmentService(st, logger)
	buildService := sandbox.NewBuildService(cfg, st, builder, provider, logger)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := sessionService.Reconcile(ctx); err != nil {
		logger.Error("reconcile failed", "error", err)
	}

	server := &http.Server{
		Addr:              cfg.BindAddr,
		Handler:           httpserver.New(sessionService, environmentService, buildService, cfg.AuthToken),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	logger.Info("sandbox daemon listening", "addr", cfg.BindAddr, "engine", provider.Name())
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}
