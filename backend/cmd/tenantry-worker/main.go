// Command tenantry-worker delivers outbox events to clusters. It ships in the
// same image as tenantry-api but runs as its own Deployment, so it restarts
// and scales independently of the API (one replica is enough; more are safe).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/config"
	"github.com/thaihoc1310/tenantry/backend/internal/outbox"
)

func main() {
	if err := run(); err != nil {
		slog.Error("tenantry-worker stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := cfg.Logger()
	slog.SetDefault(log)

	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err == nil {
		err = pool.Ping(context.Background())
	}
	if err != nil {
		return fmt.Errorf("database unreachable: %w", err)
	}
	defer pool.Close()

	// SIGTERM finishes the batch in hand, then exits.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	log.Info("outbox worker started", "poll", outbox.PollInterval, "batch", outbox.BatchSize)
	outbox.NewWorker(pool, outbox.LogHandler(log), log).Run(ctx)
	log.Info("outbox worker stopped")
	return nil
}
