package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// How an operation is followed as server-sent events (PRODUCT-PLAN §16.5).
var (
	eventPoll      = time.Second
	eventKeepalive = 15 * time.Second
	eventMaxAge    = 10 * time.Minute
)

func (s server) GetOperationEvents(ctx context.Context, req oapi.GetOperationEventsRequestObject) (oapi.GetOperationEventsResponseObject, error) {
	c := caller(ctx)
	// The first read answers who may follow it: 404 like GET otherwise.
	op, err := s.ops.Get(ctx, c.user.ID, c.platformAdmin, req.OperationId)
	if err != nil {
		return nil, err
	}
	return operationEvents{ctx: ctx, first: op, next: func(ctx context.Context) (store.Operation, error) {
		return s.ops.Get(ctx, c.user.ID, c.platformAdmin, req.OperationId)
	}}, nil
}

// operationEvents writes the stream itself: the generated response would
// copy a body without flushing between events.
type operationEvents struct {
	ctx   context.Context
	first store.Operation
	next  func(context.Context) (store.Operation, error)
}

func (e operationEvents) VisitGetOperationEventsResponse(w http.ResponseWriter) error {
	rc := http.NewResponseController(w)
	// The server's write timeout is for short answers; this one lasts.
	if err := rc.SetWriteDeadline(time.Now().Add(eventMaxAge + time.Minute)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	send := func(op store.Operation) error {
		data, err := json.Marshal(toOperation(op))
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: operation\ndata: %s\n\n", data); err != nil {
			return err
		}
		return rc.Flush()
	}
	done := func(op store.Operation) bool {
		return op.State == "Succeeded" || op.State == "Failed" || op.State == "Blocked"
	}

	last := e.first
	if err := send(last); err != nil || done(last) {
		return err
	}
	poll, keep := time.NewTicker(eventPoll), time.NewTicker(eventKeepalive)
	defer poll.Stop()
	defer keep.Stop()
	end := time.After(eventMaxAge)
	for {
		select {
		case <-e.ctx.Done():
			return nil // the client went away
		case <-end:
			return nil
		case <-keep.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return err
			}
			if err := rc.Flush(); err != nil {
				return err
			}
		case <-poll.C:
			op, err := e.next(e.ctx)
			if err != nil {
				return nil // gone, or the database: the client reconnects or polls
			}
			if op.UpdatedAt.Equal(last.UpdatedAt) && op.State == last.State && op.Message == last.Message {
				continue
			}
			last = op
			if err := send(op); err != nil || done(op) {
				return err
			}
		}
	}
}
