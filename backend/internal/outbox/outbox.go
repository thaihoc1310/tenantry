// Package outbox delivers changes to clusters (IMPLEMENTATION-PLAN §5.1). A
// service writes an event with Enqueue in the same transaction as the change
// itself; Worker later hands each event to a Handler, at least once, retrying
// with backoff until it succeeds.
package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/operation"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// Event is a change to push to one cluster.
type Event struct {
	TargetClusterID string
	Type            string // e.g. workspace.apply
	Payload         any    // marshalled to JSON
	Version         int64  // desired generation the payload describes
}

// Enqueue records e for delivery on behalf of op. Events of the same resource
// are delivered in the order they were enqueued.
func Enqueue(ctx context.Context, q *store.Queries, op store.Operation, e Event) (store.OutboxEvent, error) {
	payload, err := json.Marshal(e.Payload)
	if err != nil {
		return store.OutboxEvent{}, fmt.Errorf("outbox payload: %w", err)
	}
	return q.EnqueueEvent(ctx, store.EnqueueEventParams{
		OperationID:     op.ID,
		OrderingKey:     op.ResourceType + "/" + op.ResourceID,
		TargetClusterID: e.TargetClusterID,
		Type:            e.Type,
		Payload:         payload,
		Version:         e.Version,
	})
}

// Handler delivers one event. It must be idempotent: an event is delivered
// again if the worker stops before recording the result.
type Handler interface {
	Handle(ctx context.Context, e store.OutboxEvent) error
}

type HandlerFunc func(ctx context.Context, e store.OutboxEvent) error

func (f HandlerFunc) Handle(ctx context.Context, e store.OutboxEvent) error { return f(ctx, e) }

// Tuning from IMPLEMENTATION-PLAN §12, decision 8.
const (
	PollInterval = time.Second
	BatchSize    = 10
	// FailAfter consecutive failures marks the operation Failed so the UI
	// offers a retry. Delivery itself keeps going at the maximum backoff.
	FailAfter    = 10
	firstBackoff = time.Second
	maxBackoff   = 5 * time.Minute
	// batchTimeout bounds one batch; on shutdown the batch in hand is finished
	// within it rather than abandoned half-way.
	batchTimeout = 2 * time.Minute
)

// Backoff is the wait after the given number of consecutive failures:
// 1s, 2s, 4s, ... capped at 5 minutes.
func Backoff(failures int32) time.Duration {
	if failures < 1 {
		return 0
	}
	d := firstBackoff * time.Duration(math.Pow(2, float64(min(failures-1, 30))))
	return min(d, maxBackoff)
}

type Worker struct {
	db      *pgxpool.Pool
	q       *store.Queries
	handler Handler
	log     *slog.Logger
}

func NewWorker(db *pgxpool.Pool, h Handler, log *slog.Logger) *Worker {
	return &Worker{db: db, q: store.New(db), handler: h, log: log}
}

// Run delivers events until ctx is cancelled. A full batch is followed
// straight away by the next one; otherwise the worker sleeps PollInterval.
func (w *Worker) Run(ctx context.Context) {
	for {
		batchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), batchTimeout)
		n, err := w.RunOnce(batchCtx)
		cancel()
		if err != nil {
			w.log.Error("outbox batch failed", "err", err)
		}
		if n == BatchSize && err == nil && ctx.Err() == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(PollInterval):
		}
	}
}

// RunOnce claims one batch of due events, hands each to the handler and
// records the outcome, all in one transaction that holds the events' row
// locks. It returns how many events it handled.
func (w *Worker) RunOnce(ctx context.Context) (int, error) {
	n := 0
	err := pgx.BeginFunc(ctx, w.db, func(tx pgx.Tx) error {
		q := w.q.WithTx(tx)
		events, err := q.ClaimDueEvents(ctx, BatchSize)
		if err != nil {
			return err
		}
		n = len(events)
		for _, e := range events {
			if err := w.deliver(ctx, q, e); err != nil {
				return err
			}
		}
		return nil
	})
	return n, err
}

// deliver handles one event. Only database errors are returned; a handler
// failure is recorded on the event and its operation.
func (w *Worker) deliver(ctx context.Context, q *store.Queries, e store.OutboxEvent) error {
	log := w.log.With("event_id", e.ID, "event_type", e.Type, "operation_id", e.OperationID)

	if herr := w.handle(ctx, e); herr != nil {
		attempts, err := q.MarkEventFailed(ctx, store.MarkEventFailedParams{
			ID: e.ID, LastError: herr.Error(), RetryInSeconds: Backoff(e.Attempts + 1).Seconds(),
		})
		if err != nil {
			return err
		}
		log.Warn("outbox delivery failed", "err", herr, "attempts", attempts)
		if attempts >= FailAfter {
			if _, _, err := operation.Move(ctx, q, e.OperationID, operation.Failed, operation.ApplyFailed, herr.Error()); err != nil {
				return err
			}
		}
		return nil
	}

	if err := q.MarkEventDelivered(ctx, e.ID); err != nil {
		return err
	}
	// Not allowed (already Running, or further along) is fine: an operation
	// with several events moves on the first delivery.
	_, _, err := operation.Move(ctx, q, e.OperationID, operation.Running, "", "")
	return err
}

// handle calls the handler, turning a panic into an error so one bad event
// cannot crash the worker over and over.
func (w *Worker) handle(ctx context.Context, e store.OutboxEvent) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("handler panicked: %v", r)
		}
	}()
	return w.handler.Handle(ctx, e)
}

// LogHandler only logs events. It stands in until the provisioning worker
// applies CRs to clusters (M6).
func LogHandler(log *slog.Logger) Handler {
	return HandlerFunc(func(_ context.Context, e store.OutboxEvent) error {
		log.Info("outbox event delivered (log only)", "event_id", e.ID, "event_type", e.Type,
			"cluster_id", e.TargetClusterID, "version", e.Version, "payload", json.RawMessage(e.Payload))
		return nil
	})
}
