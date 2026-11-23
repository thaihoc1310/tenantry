package httpapi

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// The middlewares here wrap the whole HTTP surface, /healthz included; mount
// them on the root router in this order:
//
//	r.Use(httpapi.SecurityHeaders, httpapi.WithRequestID, httpapi.LogRequests(log), httpapi.Recover)
//
// so the request id exists before anything logs and the access log sees the
// 500 that Recover writes.

// SecurityHeaders sets what every API answer carries (IMPLEMENTATION-PLAN
// M12): JSON and the login pages are never cached, sniffed, framed or sent
// on as a referrer; the only HTML (the login failure page) styles itself
// inline and loads nothing. A handler may set Cache-Control itself (the
// event stream).
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Strict-Transport-Security", "max-age=31536000")
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// WithRequestID tags every request with an id that is echoed in the
// X-Request-Id header and in error bodies, so a user report can be matched to
// logs. It also gives the request the mark store.AcquireDeadline sets when no
// database connection was free in time (503 SERVICE_BUSY).
func WithRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" || len(id) > 64 {
			id = rand.Text()
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(store.WithBusyMark(context.WithValue(r.Context(), requestIDKey, id))))
	})
}

// Recover turns a panic in a handler into a 500 ErrorResponse and logs it with
// its stack, so one bad request neither kills the connection silently nor
// goes unnoticed. http.ErrAbortHandler is the deliberate way to abort a
// response and is passed on.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(v)
			}
			slog.ErrorContext(r.Context(), "handler panicked", "panic", v, "request_id", requestID(r.Context()), "stack", string(debug.Stack()))
			// Headers already sent: the status cannot change any more; the
			// client sees a truncated body.
			if ww, ok := w.(middleware.WrapResponseWriter); !ok || ww.Status() == 0 {
				writeError(w, r, errors.New("panic"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// requestLog collects what inner layers learn about a request for the access
// log line: the user is only known after authenticate has run.
type requestLog struct{ userID string }

// LogRequests writes one line per request. It logs the route pattern
// (/api/v1/operations/{operationId}), never the raw path, which may carry
// tokens such as an invitation link. Probes go to Debug to keep the log readable.
func LogRequests(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rl := &requestLog{}
			ctx := context.WithValue(r.Context(), requestLogKey, rl)
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(ww, r.WithContext(ctx))

			level := slog.LevelInfo
			if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
				level = slog.LevelDebug
			}
			status := ww.Status()
			if status == 0 {
				status = http.StatusOK // handler wrote nothing
			}
			log.Log(ctx, level, "request",
				"method", r.Method,
				"route", routePattern(r),
				"status", status,
				"bytes", ww.BytesWritten(),
				"duration", time.Since(start),
				"request_id", requestID(ctx),
				"user_id", rl.userID,
			)
		})
	}
}

// noteUser records the authenticated user for the access log.
func noteUser(ctx context.Context, id string) {
	if rl, ok := ctx.Value(requestLogKey).(*requestLog); ok {
		rl.userID = id
	}
}

// routePattern is the full pattern chi matched, e.g.
// /api/v1/operations/{operationId}; empty when no route matched. chi fills its
// route context in place while routing, so it is complete once the handler returned.
func routePattern(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		return rctx.RoutePattern()
	}
	return ""
}
