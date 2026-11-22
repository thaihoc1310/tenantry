// Package housekeeping deletes what has outlived its use (IMPLEMENTATION-PLAN
// §5.3). It only touches PostgreSQL. Expired sessions are already ignored
// when read, so a late or failed round costs disk space, never correctness.
package housekeeping

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

const Interval = time.Minute

// Run does a round now and then every Interval until ctx is cancelled.
func Run(ctx context.Context, db *pgxpool.Pool, log *slog.Logger) {
	for {
		if err := RunOnce(ctx, db, log); err != nil && ctx.Err() == nil {
			log.Error("housekeeping failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(Interval):
		}
	}
}

// RunOnce does one round.
func RunOnce(ctx context.Context, db *pgxpool.Pool, log *slog.Logger) error {
	n, err := store.New(db).DeleteExpiredSessions(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		log.Info("deleted expired sessions", "count", n)
	}
	return nil
}
