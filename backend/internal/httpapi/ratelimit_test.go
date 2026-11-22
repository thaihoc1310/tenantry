package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
)

// Each user gets 120 writes a minute; reads and other users are not affected.
func TestWriteRateLimit(t *testing.T) {
	h, _ := newAPI(t)
	const path = "/api/v1/operations/op-nope:retry" // a cheap write: 404
	for i := range 120 {
		if rec := call(h, http.MethodPost, path, "usr-dave"); rec.Code != 404 {
			t.Fatalf("write %d: %d %s", i+1, rec.Code, rec.Body)
		}
	}
	rec := call(h, http.MethodPost, path, "usr-dave")
	if rec.Code != 429 || errorBody(t, rec).Code != oapi.RATELIMITED {
		t.Fatalf("write 121: %d %s", rec.Code, rec.Body)
	}
	if s, err := strconv.Atoi(rec.Header().Get("Retry-After")); err != nil || s < 1 || s > 60 {
		t.Fatalf("Retry-After %q", rec.Header().Get("Retry-After"))
	}
	if rec := call(h, http.MethodGet, "/api/v1/me", "usr-dave"); rec.Code != 200 {
		t.Fatalf("read over the write limit: %d", rec.Code)
	}
	if rec := call(h, http.MethodPost, path, "usr-erin"); rec.Code != 404 {
		t.Fatalf("another user: %d", rec.Code)
	}
}

// Each client address gets 20 login starts a minute. Behind the trusted
// proxy the address comes from X-Forwarded-For; from anyone else that header
// is ignored, so it cannot be used to get a fresh budget.
func TestLoginRateLimit(t *testing.T) {
	h, _ := newAPI(t) // its issuer is unreachable: every start fails fast with 500
	start := func(remote, xff string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
		req.RemoteAddr = remote
		if xff != "" {
			req.Header.Set("X-Forwarded-For", xff)
		}
		return send(h, req)
	}
	for i := range 20 {
		// A different spoofed header each time changes nothing.
		if rec := start("203.0.113.5:4000", "198.51.100."+strconv.Itoa(i)); rec.Code == 429 {
			t.Fatalf("start %d refused", i+1)
		}
	}
	rec := start("203.0.113.5:4000", "198.51.100.99")
	if rec.Code != 429 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") ||
		!strings.Contains(rec.Body.String(), "too many requests") || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("start 21: %d %v %s", rec.Code, rec.Header(), rec.Body)
	}
	// The callback has its own budget, and other addresses theirs.
	if rec := send(h, func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/auth/callback", nil)
		r.RemoteAddr = "203.0.113.5:4000"
		return r
	}()); rec.Code == 429 {
		t.Fatal("callback shares the start budget")
	}
	if rec := start("203.0.113.6:4000", ""); rec.Code == 429 {
		t.Fatal("another address refused")
	}

	// Through the proxy (10.0.0.0/8 in tests), each client counts apart.
	for i := range 20 {
		if rec := start("10.1.1.1:5000", "198.51.100.7"); rec.Code == 429 {
			t.Fatalf("proxied start %d refused", i+1)
		}
	}
	if rec := start("10.1.1.1:5000", "198.51.100.7"); rec.Code != 429 {
		t.Fatalf("proxied client over the limit: %d", rec.Code)
	}
	if rec := start("10.1.1.1:5000", "198.51.100.8"); rec.Code == 429 {
		t.Fatal("another client behind the same proxy refused")
	}
}
