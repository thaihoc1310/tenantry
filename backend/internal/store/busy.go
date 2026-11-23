package store

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/metrics"
)

// AcquireDeadline bounds how long a caller waits for a connection of the pool
// (set as the pool's tracer by config.Pool). A pool drained by something slow
// would otherwise keep every request waiting until its client gives up; with
// it, the request fails after Wait and the API answers 503 SERVICE_BUSY
// (AcquireTimedOut). Each such wait is counted
// (tenantry_db_pool_acquire_timeouts_total) and logged with the pool's state.
type AcquireDeadline struct{ Wait time.Duration }

type acquireKey struct{}

type acquiring struct {
	parent context.Context
	cancel context.CancelFunc
}

func (a AcquireDeadline) TraceAcquireStart(ctx context.Context, _ *pgxpool.Pool, _ pgxpool.TraceAcquireStartData) context.Context {
	// The pool builds a new connection on a context of its own, so this
	// deadline ends the wait, never a connection being made.
	wait, cancel := context.WithTimeout(ctx, a.Wait)
	return context.WithValue(wait, acquireKey{}, &acquiring{parent: ctx, cancel: cancel})
}

func (a AcquireDeadline) TraceAcquireEnd(ctx context.Context, pool *pgxpool.Pool, d pgxpool.TraceAcquireEndData) {
	w, ok := ctx.Value(acquireKey{}).(*acquiring)
	if !ok {
		return
	}
	w.cancel()
	// Only this deadline: a caller that gave up or ran out of its own time
	// is not the pool being busy.
	if !errors.Is(d.Err, context.DeadlineExceeded) || w.parent.Err() != nil {
		return
	}
	if m, ok := w.parent.Value(busyKey{}).(*atomic.Bool); ok {
		m.Store(true)
	}
	metrics.DBAcquireTimeouts.Inc()
	s := pool.Stat()
	slog.WarnContext(w.parent, "no database connection free in time", "wait", a.Wait.String(),
		"acquired", s.AcquiredConns(), "max", s.MaxConns())
}

// The pool looks for its tracer among the connections' query tracers.
func (AcquireDeadline) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return ctx
}

func (AcquireDeadline) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

type busyKey struct{}

// WithBusyMark gives ctx a mark that AcquireDeadline sets when a wait for a
// connection on behalf of ctx ran out; AcquireTimedOut reads it.
func WithBusyMark(ctx context.Context) context.Context {
	return context.WithValue(ctx, busyKey{}, new(atomic.Bool))
}

// AcquireTimedOut tells whether err comes from a wait for a connection that
// ran out (AcquireDeadline) for ctx, marked with WithBusyMark.
func AcquireTimedOut(ctx context.Context, err error) bool {
	m, ok := ctx.Value(busyKey{}).(*atomic.Bool)
	return ok && m.Load() && errors.Is(err, context.DeadlineExceeded)
}
