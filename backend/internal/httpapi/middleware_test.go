package httpapi_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi"
	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
)

// logged serves h behind the production middleware chain and returns the
// access log lines as JSON objects.
func logged(t *testing.T, mount func(chi.Router), method, path, user string) (*httptest.ResponseRecorder, []map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r := chi.NewRouter()
	r.Use(httpapi.WithRequestID, httpapi.LogRequests(log), httpapi.Recover)
	mount(r)

	req := httptest.NewRequest(method, path, nil)
	if user != "" {
		req.Header.Set(httpapi.DevUserHeader, user)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	var lines []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("log line %q: %v", l, err)
		}
		lines = append(lines, m)
	}
	return rec, lines
}

func TestRecoverAnswersWithErrorResponse(t *testing.T) {
	rec, lines := logged(t, func(r chi.Router) {
		r.Get("/boom", func(http.ResponseWriter, *http.Request) { panic("nil map") })
	}, http.MethodGet, "/boom", "")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", rec.Code)
	}
	var body oapi.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != oapi.INTERNAL || body.RequestId == "" || body.RequestId != rec.Header().Get("X-Request-Id") {
		t.Fatalf("body %+v", body)
	}
	access := lines[len(lines)-1]
	if access["msg"] != "request" || access["status"] != float64(500) {
		t.Fatalf("access log %v", access)
	}
}

// http.ErrAbortHandler is how a handler deliberately aborts; net/http expects
// to see it and must not get a 500 body glued onto a half-sent response.
func TestRecoverPassesOnAbort(t *testing.T) {
	defer func() {
		if v := recover(); v != http.ErrAbortHandler {
			t.Fatalf("recovered %v, want http.ErrAbortHandler", v)
		}
	}()
	logged(t, func(r chi.Router) {
		r.Get("/abort", func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) })
	}, http.MethodGet, "/abort", "")
	t.Fatal("abort swallowed")
}

func TestAccessLog(t *testing.T) {
	h, _ := newAPI(t, httpapi.Options{DevAuth: true})
	mount := func(r chi.Router) {
		r.Get("/healthz", func(http.ResponseWriter, *http.Request) {})
		r.Mount("/", h)
	}

	_, lines := logged(t, mount, http.MethodGet, "/api/v1/organizations/org-acme", "usr-alice")
	got := lines[len(lines)-1]
	want := map[string]any{
		"level": "INFO", "msg": "request", "method": "GET", "status": float64(200),
		"route": "/api/v1/organizations/{orgId}", "user_id": "usr-alice",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v (line %v)", k, got[k], v, got)
		}
	}
	if got["request_id"] == "" || got["duration"] == nil || got["bytes"] == float64(0) {
		t.Errorf("line %v", got)
	}
	// The raw path can carry secrets (invitation tokens); only the pattern is logged.
	if strings.Contains(lineText(got), "org-acme") {
		t.Errorf("raw path leaked into the log: %v", got)
	}

	_, lines = logged(t, mount, http.MethodGet, "/api/v1/organizations/org-acme", "")
	if got := lines[len(lines)-1]; got["status"] != float64(401) || got["user_id"] != "" {
		t.Errorf("anonymous: %v", got)
	}

	_, lines = logged(t, mount, http.MethodGet, "/healthz", "")
	if got := lines[len(lines)-1]; got["level"] != "DEBUG" || got["status"] != float64(200) {
		t.Errorf("probe: %v", got)
	}
}

func lineText(m map[string]any) string {
	b, _ := json.Marshal(m)
	return string(b)
}
