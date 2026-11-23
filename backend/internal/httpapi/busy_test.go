package httpapi_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/thaihoc1310/tenantry/backend/internal/auth"
	"github.com/thaihoc1310/tenantry/backend/internal/config"
	"github.com/thaihoc1310/tenantry/backend/internal/httpapi"
	"github.com/thaihoc1310/tenantry/backend/internal/metrics"
)

// A request that finds no database connection free within the pool's wait
// gets 503 SERVICE_BUSY with Retry-After, counted, instead of hanging; one
// whose own context ends first is not the pool being busy; once a
// connection is free again requests go through.
func TestServiceBusy(t *testing.T) {
	_, migrated := newAPI(t)
	ctx := context.Background()
	pool, err := config.Config{DatabaseURL: migrated.Config().ConnString()}.Pool(ctx, 1, 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	a, err := auth.New(pool, config.Auth{
		Issuer: "http://127.0.0.1:1/realms/none", ClientID: "portal", ClientSecret: "portal-secret", PublicURL: publicURL, SessionKey: testKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	httpapi.Register(r, pool, a, trustedProxies)
	h := httpapi.WithRequestID(r)
	// A well-formed session token: authenticating it reads the database.
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	token := base64.RawURLEncoding.EncodeToString(b)

	held, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	before := testutil.ToFloat64(metrics.DBAcquireTimeouts)
	start := time.Now()
	rec := send(h, bareRequest(http.MethodGet, "/api/v1/me", token, ""))
	var body struct{ Code, Message, RequestId string }
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusServiceUnavailable || body.Code != "SERVICE_BUSY" || rec.Header().Get("Retry-After") != "2" || body.RequestId == "" {
		t.Fatalf("busy: %d %q Retry-After=%q", rec.Code, rec.Body, rec.Header().Get("Retry-After"))
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("answered after %v, want about the 200ms wait", took)
	}
	if got := testutil.ToFloat64(metrics.DBAcquireTimeouts) - before; got != 1 {
		t.Errorf("acquire timeouts counted: %v, want 1", got)
	}

	// The request's own deadline runs out first: not SERVICE_BUSY, not counted.
	rctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	rec = send(h, bareRequest(http.MethodGet, "/api/v1/me", token, "").WithContext(rctx))
	if rec.Code == http.StatusServiceUnavailable {
		t.Errorf("own deadline: %d %s, want no SERVICE_BUSY", rec.Code, rec.Body)
	}
	if got := testutil.ToFloat64(metrics.DBAcquireTimeouts) - before; got != 1 {
		t.Errorf("acquire timeouts counted: %v, want still 1", got)
	}

	held.Release()
	if rec := send(h, bareRequest(http.MethodGet, "/api/v1/me", token, "")); rec.Code != http.StatusUnauthorized {
		t.Errorf("connection free again: %d %s, want 401 for an unknown session", rec.Code, rec.Body)
	}
}
