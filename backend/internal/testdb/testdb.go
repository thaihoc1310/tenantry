// Package testdb gives each test its own throwaway PostgreSQL database on the
// server TEST_DATABASE_URL points at, so tests never touch the dev data and
// can run in parallel. Only _test.go files import it.
package testdb

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
)

// New creates an empty database, drops it when the test ends and returns its URL.
// Without TEST_DATABASE_URL the test is skipped.
func New(t *testing.T) string {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })

	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "tenantry_test_" + hex.EncodeToString(b)
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec("DROP DATABASE " + name + " WITH (FORCE)") })

	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	return u.String()
}

// Open is New plus a database/sql handle closed at the end of the test.
func Open(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", New(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
