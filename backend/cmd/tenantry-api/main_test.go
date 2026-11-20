package main

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi"
	"github.com/thaihoc1310/tenantry/backend/internal/testdb"
	"github.com/thaihoc1310/tenantry/backend/migrations"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	routes(nil, httpapi.Options{}, quiet).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("got %d %q, want 200 \"ok\"", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatal("no X-Request-Id: the middleware chain is not mounted")
	}
}

// migrated returns the URL of a fresh, migrated database holding user usr-a.
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
	return url
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
	h := routes(pool, httpapi.Options{DevAuth: true}, quiet)

	send := func(size int) *httptest.ResponseRecorder {
		body := `{"slug":"acme","displayName":"` + strings.Repeat("x", size) + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/organizations", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(httpapi.DevUserHeader, "usr-a")
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
	h := routes(pool, httpapi.Options{}, quiet)
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
