package metrics

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// HTTP counts and times each request by its chi route pattern
// (/api/v1/workspaces/{workspaceId}), never by its path: ids would make a
// series per object. A request no route matched counts as "unmatched".
func HTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(sw, r)
		route := "unmatched"
		if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
			route = rc.RoutePattern()
		}
		Requests.WithLabelValues(route, r.Method, strconv.Itoa(sw.code)).Inc()
		// An event stream lasts minutes by design; its time says nothing.
		if !strings.HasPrefix(sw.Header().Get("Content-Type"), "text/event-stream") {
			RequestDuration.WithLabelValues(route, r.Method).Observe(time.Since(start).Seconds())
		}
	})
}

type statusWriter struct {
	http.ResponseWriter
	code        int
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.code, w.wroteHeader = code, true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	w.wroteHeader = true
	return w.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the connection (event streams flush).
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
