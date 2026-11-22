package metrics_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/thaihoc1310/tenantry/backend/internal/metrics"
)

// Requests count by route pattern, never by path, and an event stream is
// not timed.
func TestHTTPLabelsByRoutePattern(t *testing.T) {
	r := chi.NewRouter()
	r.Use(metrics.HTTP)
	r.Get("/api/v1/workspaces/{workspaceId}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	r.Get("/api/v1/operations/{operationId}/events", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: x\n\n")
	})
	for _, p := range []string{"/api/v1/workspaces/ws-1", "/api/v1/workspaces/ws-2", "/nope", "/api/v1/operations/op-1/events"} {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, p, nil))
	}
	if n := testutil.ToFloat64(metrics.Requests.WithLabelValues("/api/v1/workspaces/{workspaceId}", "GET", "418")); n != 2 {
		t.Errorf("by pattern: %v", n)
	}
	if n := testutil.ToFloat64(metrics.Requests.WithLabelValues("unmatched", "GET", "404")); n != 1 {
		t.Errorf("unmatched: %v", n)
	}
	if n := testutil.CollectAndCount(metrics.RequestDuration); n != 2 {
		t.Errorf("timed series %d, want 2 (the stream is not timed)", n)
	}

	// /metrics shows them, with the Go runtime's.
	srv := httptest.NewServer(metrics.Handler())
	defer srv.Close()
	res, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	for _, want := range []string{`tenantry_api_requests_total{code="418",method="GET",route="/api/v1/workspaces/{workspaceId}"} 2`, "go_goroutines"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("/metrics lacks %s", want)
		}
	}
}
