// Command tenantry-worker delivers outbox events to clusters. It ships in the
// same image as tenantry-api but runs as its own Deployment, so it restarts
// and scales independently of the API (one replica is enough; more are safe).
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/outbox"
)

func main() {
	log := slog.Default()

	pool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err == nil {
		err = pool.Ping(context.Background())
	}
	if err != nil {
		log.Error("database unreachable", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	// SIGTERM finishes the batch in hand, then exits.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	log.Info("outbox worker started", "poll", outbox.PollInterval, "batch", outbox.BatchSize)
	outbox.NewWorker(pool, outbox.LogHandler(log), log).Run(ctx)
	log.Info("outbox worker stopped")
}
