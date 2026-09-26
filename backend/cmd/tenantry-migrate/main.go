// Command tenantry-migrate applies the embedded SQL migrations.
//
//	tenantry-migrate [up|down|status]   (default: up)
//
// up applies every pending migration, down rolls back the latest one.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/thaihoc1310/tenantry/backend/migrations"
)

func main() {
	if err := run(); err != nil {
		slog.Error("migrate failed", "err", err)
		os.Exit(1)
	}
}

var commands = map[string]func(context.Context, *goose.Provider) error{
	"up": func(ctx context.Context, p *goose.Provider) error {
		// On failure Up still returns the migrations applied before it.
		results, err := p.Up(ctx)
		logResults(results)
		if err == nil && len(results) == 0 {
			slog.Info("nothing to migrate")
		}
		return err
	},
	"down": func(ctx context.Context, p *goose.Provider) error {
		result, err := p.Down(ctx)
		if result != nil {
			logResults([]*goose.MigrationResult{result})
		}
		return err
	},
	"status": func(ctx context.Context, p *goose.Provider) error {
		statuses, err := p.Status(ctx)
		for _, s := range statuses {
			slog.Info("migration", "version", s.Source.Version, "file", s.Source.Path, "state", s.State)
		}
		return err
	},
}

func run() error {
	name := "up"
	if len(os.Args) > 1 {
		name = os.Args[1]
	}
	command, ok := commands[name]
	if !ok {
		return fmt.Errorf("unknown command %q, want up, down or status", name)
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is required")
	}

	// Cancel on SIGTERM so a job killed by helm rolls back its transaction
	// instead of dying mid-statement.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	db, err := sql.Open("pgx", url)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	p, err := migrations.NewProvider(db)
	if err != nil {
		return err
	}
	return command(ctx, p)
}

func logResults(results []*goose.MigrationResult) {
	for _, r := range results {
		slog.Info("migrated", "direction", r.Direction, "file", r.Source.Path, "duration", r.Duration)
	}
}
