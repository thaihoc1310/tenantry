package migrations_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/thaihoc1310/tenantry/backend/migrations"
)

// freshDB creates a throwaway database on TEST_DATABASE_URL's server, so the
// test never touches the dev database, and drops it when the test ends.
func freshDB(t *testing.T) *sql.DB {
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
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestUpDownUp(t *testing.T) {
	db := freshDB(t)
	ctx := context.Background()
	for _, cmd := range []string{"up", "reset", "up"} {
		if err := migrations.Run(ctx, db, cmd); err != nil {
			t.Fatalf("%s: %v", cmd, err)
		}
	}
}

func TestConstraints(t *testing.T) {
	db := freshDB(t)
	if err := migrations.Run(context.Background(), db, "up"); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `INSERT INTO users (id, issuer, subject) VALUES
		('usr-alice', 'https://id', 'sub-alice'), ('usr-bob', 'https://id', 'sub-bob')`)
	mustExec(t, db, `INSERT INTO organizations (id, slug, display_name, owner_user_id, status) VALUES
		('org-acme', 'acme', 'ACME', 'usr-alice', 'Active')`)
	mustExec(t, db, `INSERT INTO organization_members (org_id, user_id, role) VALUES ('org-acme', 'usr-alice', 'OrgOwner')`)

	const unique, check = "23505", "23514"
	cases := []struct {
		name, sql, code string
	}{
		{"same issuer+subject twice", `INSERT INTO users (id, issuer, subject) VALUES ('usr-x', 'https://id', 'sub-alice')`, unique},
		{"user status outside enum", `INSERT INTO users (id, issuer, subject, status) VALUES ('usr-x', 'https://id', 'sub-x', 'Banned')`, check},
		{"slug with uppercase", orgInsert("org-x", "Acme2", "usr-bob", "Pending"), check},
		{"slug too short", orgInsert("org-x", "ab", "usr-bob", "Pending"), check},
		{"slug with underscore", orgInsert("org-x", "ac_me", "usr-bob", "Pending"), check},
		{"slug ends with dash", orgInsert("org-x", "acme-", "usr-bob", "Pending"), check},
		{"slug longer than 20", orgInsert("org-x", "abcdefghijklmnopqrstu", "usr-bob", "Pending"), check},
		{"slug taken by active org", orgInsert("org-x", "acme", "usr-bob", "Pending"), unique},
		{"org status outside enum", orgInsert("org-x", "beta", "usr-bob", "Suspended"), check},
		{"second owner in one org", `INSERT INTO organization_members (org_id, user_id, role) VALUES ('org-acme', 'usr-bob', 'OrgOwner')`, unique},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { expectCode(t, db, c.sql, c.code) })
	}

	t.Run("second pending request from one user", func(t *testing.T) {
		mustExec(t, db, orgInsert("org-p1", "first-req", "usr-bob", "Pending"))
		expectCode(t, db, orgInsert("org-p2", "second-req", "usr-bob", "Pending"), unique)
	})
	t.Run("rejected request releases its slug", func(t *testing.T) {
		mustExec(t, db, `UPDATE organizations SET status = 'Rejected' WHERE id = 'org-p1'`)
		mustExec(t, db, orgInsert("org-p3", "first-req", "usr-bob", "Pending"))
	})
}

func orgInsert(id, slug, owner, status string) string {
	return "INSERT INTO organizations (id, slug, display_name, owner_user_id, status) VALUES ('" +
		id + "', '" + slug + "', 'x', '" + owner + "', '" + status + "')"
}

func mustExec(t *testing.T, db *sql.DB, q string) {
	t.Helper()
	if _, err := db.Exec(q); err != nil {
		t.Fatalf("%v\n%s", err, q)
	}
}

func expectCode(t *testing.T, db *sql.DB, q, code string) {
	t.Helper()
	_, err := db.Exec(q)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != code {
		t.Fatalf("want SQLSTATE %s, got %v\n%s", code, err, q)
	}
}
