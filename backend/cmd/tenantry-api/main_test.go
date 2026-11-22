package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/auth"
	"github.com/thaihoc1310/tenantry/backend/internal/config"
	"github.com/thaihoc1310/tenantry/backend/internal/httpapi"
	"github.com/thaihoc1310/tenantry/backend/internal/testdb"
	"github.com/thaihoc1310/tenantry/backend/migrations"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	routes(nil, nil, quiet).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("got %d %q, want 200 \"ok\"", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatal("no X-Request-Id: the middleware chain is not mounted")
	}
}

// migrated returns the URL of a fresh, migrated database holding user usr-a,
// logged in with session sessionA.
func migrated(t *testing.T) string {
	t.Helper()
	url := testdb.New(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	p, err := migrations.NewProvider(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users (id, issuer, subject) VALUES ('usr-a', 'iss', 'a')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sessions (id_hash, user_id, refresh_token_enc, access_expires_at, expires_at)
VALUES (sha256(decode($1, 'hex')), 'usr-a', '\x00', now() + interval '5 minutes', now() + interval '1 hour')`, hex.EncodeToString(sessionA)); err != nil {
		t.Fatal(err)
	}
	return url
}

// sessionA is the raw session value of usr-a; the cookie carries it in base64url.
var sessionA = bytes.Repeat([]byte{1}, 32)

// testAuth is an auth service whose Keycloak is never contacted.
func testAuth(t *testing.T, pool *pgxpool.Pool) *auth.Service {
	t.Helper()
	a, err := auth.New(pool, config.Auth{
		Issuer: "http://127.0.0.1:1/realms/none", ClientID: "portal", ClientSecret: "x",
		PublicURL: "https://portal.test", SessionKey: bytes.Repeat([]byte{7}, 32),
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// A port already in use must fail the process, not look like a clean stop.
func TestRunFailsWhenPortTaken(t *testing.T) {
	url := migrated(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	t.Setenv("DATABASE_URL", url)
	t.Setenv("TENANTRY_ADDR", l.Addr().String())
	t.Setenv("TENANTRY_OIDC_ISSUER", "https://auth.tenantry.site/realms/tenantry")
	t.Setenv("TENANTRY_OIDC_CLIENT_SECRET", "x")
	t.Setenv("TENANTRY_PUBLIC_URL", "https://portal.tenantry.site")
	t.Setenv("TENANTRY_SESSION_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))

	err = run()
	if err == nil || !strings.Contains(err.Error(), "address already in use") {
		t.Fatalf("run() = %v, want an address-in-use error", err)
	}
}

// Even a logged-in caller cannot make the API read an unbounded body.
func TestBodyIsCapped(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), migrated(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	h := routes(pool, testAuth(t, pool), quiet)

	send := func(size int) *httptest.ResponseRecorder {
		body := `{"slug":"acme","displayName":"` + strings.Repeat("x", size) + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/organizations", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: base64.RawURLEncoding.EncodeToString(sessionA)})
		req.AddCookie(&http.Cookie{Name: httpapi.CSRFCookie, Value: "c"})
		req.Header.Set(httpapi.CSRFHeader, "c")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	if rec := send(maxBodyBytes); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "too large") {
		t.Fatalf("oversized body: %d %s", rec.Code, rec.Body)
	}
	// Under the cap the request reaches validation as usual (displayName max 100).
	if rec := send(200); rec.Code != http.StatusBadRequest || strings.Contains(rec.Body.String(), "too large") {
		t.Fatalf("small body: %d %s", rec.Code, rec.Body)
	}
}

// /readyz follows the database: ready while it answers, 503 once it is gone.
func TestReadyz(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), migrated(t))
	if err != nil {
		t.Fatal(err)
	}
	h := routes(pool, testAuth(t, pool), quiet)
	probe := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		return rec
	}
	if rec := probe(); rec.Code != http.StatusOK {
		t.Fatalf("with a database: %d %s", rec.Code, rec.Body)
	}
	pool.Close() // stands in for losing the database
	if rec := probe(); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("without a database: %d %s", rec.Code, rec.Body)
	}
}
