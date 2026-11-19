package httpapi

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
)

// A code the OpenAPI enum does not list would reach the UI as a value its
// generated client has no case for.
func TestEveryErrorCodeIsInTheContract(t *testing.T) {
	for code := range statusOf {
		if !oapi.ErrorCode(code).Valid() {
			t.Errorf("%s has an HTTP status but is missing from ErrorCode in openapi/tenantry.v1.yaml", code)
		}
	}
}

// Anything that is not an *apperr.Error is our fault: 500, and the cause
// (SQL text, hostnames...) stays in the log, never in the response.
func TestUnexpectedErrorIsHidden(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, httptest.NewRequest("GET", "/", nil), errors.New("dial tcp 10.0.0.5:5432: connection refused"))
	if rec.Code != 500 || strings.Contains(rec.Body.String(), "10.0.0.5") || !strings.Contains(rec.Body.String(), `"INTERNAL"`) {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}
