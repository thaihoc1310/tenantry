// Package housekeeping deletes what has outlived its use (IMPLEMENTATION-PLAN
// §5.3). It only touches PostgreSQL. Expired sessions are already ignored
// when read, so a late or failed round costs disk space, never correctness.
package housekeeping

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/audit"
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
	q := store.New(db)
	for _, job := range []struct {
		what string
		run  func(context.Context) (int64, error)
	}{
		{"expired sessions", q.DeleteExpiredSessions},
		{"idempotency keys older than 24 hours", q.DeleteOldIdempotencyKeys},
	} {
		n, err := job.run(ctx)
		if err != nil {
			return fmt.Errorf("delete %s: %w", job.what, err)
		}
		if n > 0 {
			log.Info("deleted "+job.what, "count", n)
		}
	}
	return releaseQuarantined(ctx, db, log)
}

// releaseQuarantined frees the address blocks past their quarantine and
// deletes the Workspaces that held them, in one transaction, with an audit
// event each (04-state-machines §4).
func releaseQuarantined(ctx context.Context, db *pgxpool.Pool, log *slog.Logger) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		q := store.New(tx)
		rows, err := q.ReleaseQuarantined(ctx)
		if err != nil || len(rows) == 0 {
			return err
		}
		ids := make([]string, len(rows))
		for i, r := range rows {
			ids[i] = r.WorkspaceID
			if err := audit.Record(ctx, q, audit.Event{
				OrgID: r.OrgID, Action: audit.NetworkAllocationRelease, TargetType: "Workspace", TargetID: r.WorkspaceID,
				Detail: map[string]any{"cidr": r.Cidr.String(), "k8sName": r.K8sName},
			}); err != nil {
				return err
			}
		}
		if err := q.DeleteWorkspaceRows(ctx, ids); err != nil {
			return err
		}
		log.Info("released address blocks past their quarantine", "count", len(rows))
		return nil
	})
}
