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

	"secureai/backend/internal/platform/config"
	"secureai/backend/internal/platform/dependencies"
	"secureai/backend/internal/platform/httpapi"
)

func main() { os.Exit(run()) }

func run() int {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid_configuration", "reason", err.Error())
		return 1
	}
	logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 3*cfg.DependencyTimeout)
	deps, err := dependencies.Open(startup, cfg)
	cancel()
	if err != nil {
		logger.Error("startup_failed", "reason", err.Error())
		return 1
	}
	defer deps.Close()
	server := &http.Server{
		Addr: cfg.HTTPAddr, Handler: httpapi.NewRouter(cfg, logger, deps.Checks()),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024,
		ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	errs := make(chan error, 1)
	go func() { errs <- server.ListenAndServe() }()
	logger.Info("server_starting", "address", cfg.HTTPAddr, "environment", cfg.Environment)
	select {
	case err := <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http_server_failed")
			return 1
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			logger.Error("shutdown_timeout")
			return 1
		}
	}
	logger.Info("server_stopped")
	return 0
}
