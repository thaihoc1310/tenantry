package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi"
)

func TestHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	routes(nil, httpapi.Options{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("got %d %q, want 200 \"ok\"", rec.Code, rec.Body.String())
	}
}
