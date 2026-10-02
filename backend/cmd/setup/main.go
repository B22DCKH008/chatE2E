// setup provisions the local database schema and JetStream before API startup.
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"secureai/backend/internal/broker"
	"secureai/backend/internal/platform/config"
	"secureai/backend/internal/platform/dependencies"
	"secureai/backend/migrations"
)

func main() { os.Exit(run()) }
func run() int {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid_configuration", "reason", err.Error())
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	d, err := dependencies.Open(ctx, cfg)
	if err != nil {
		logger.Error("setup_connection_failed", "reason", err.Error())
		return 1
	}
	defer d.Close()
	if err := migrations.Up(ctx, d.Postgres); err != nil {
		logger.Error("migration_failed")
		return 1
	}
	if err := broker.Provision(ctx, d.JetStream); err != nil {
		logger.Error("stream_setup_failed")
		return 1
	}
	logger.Info("setup_completed")
	return 0
}
