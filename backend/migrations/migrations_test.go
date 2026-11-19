package migrations_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/thaihoc1310/tenantry/backend/internal/testdb"
	"github.com/thaihoc1310/tenantry/backend/migrations"
)

// Every Down must undo its Up, or rollbacks in production break.
func TestUpDownUp(t *testing.T) {
	p := provider(t, testdb.Open(t))
	ctx := context.Background()
	if _, err := p.Up(ctx); err != nil {
		t.Fatalf("up: %v", err)
	}
	if _, err := p.DownTo(ctx, 0); err != nil {
		t.Fatalf("down to 0: %v", err)
	}
	if _, err := p.Up(ctx); err != nil {
		t.Fatalf("up again: %v", err)
	}
}

// Two migrate jobs can overlap (concurrent helm upgrades, a retry pod started
// before the old one died). While one holds the migration lock, the other must
// wait instead of running the same DDL.
func TestUpWaitsForMigrationLock(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()

	holder, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Close() }()
	mustExecOn(t, holder, "SELECT pg_advisory_lock($1)", lock.DefaultLockID)

	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, err := provider(t, db).Up(waitCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("up should have waited for the lock until the deadline, got err=%v", err)
	}
	var users sql.NullString
	if err := db.QueryRow("SELECT to_regclass('users')::text").Scan(&users); err != nil {
		t.Fatal(err)
	}
	if users.Valid {
		t.Fatal("up created tables while another run held the migration lock")
	}

	mustExecOn(t, holder, "SELECT pg_advisory_unlock($1)", lock.DefaultLockID)
	if _, err := provider(t, db).Up(ctx); err != nil {
		t.Fatalf("up after the lock was released: %v", err)
	}
}

func TestConstraints(t *testing.T) {
	db := testdb.Open(t)
	if _, err := provider(t, db).Up(context.Background()); err != nil {
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

	// One subtest, so running it alone with -run still sets up what it checks.
	t.Run("one pending request per user, rejection releases the slug", func(t *testing.T) {
		mustExec(t, db, orgInsert("org-p1", "first-req", "usr-bob", "Pending"))
		expectCode(t, db, orgInsert("org-p2", "second-req", "usr-bob", "Pending"), unique)
		mustExec(t, db, `UPDATE organizations SET status = 'Rejected' WHERE id = 'org-p1'`)
		mustExec(t, db, orgInsert("org-p3", "first-req", "usr-bob", "Pending"))
	})
}

func provider(t *testing.T, db *sql.DB) *goose.Provider {
	t.Helper()
	p, err := migrations.NewProvider(db)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func orgInsert(id, slug, owner, status string) string {
	return "INSERT INTO organizations (id, slug, display_name, owner_user_id, status) VALUES ('" +
		id + "', '" + slug + "', 'x', '" + owner + "', '" + status + "')"
}

func mustExecOn(t *testing.T, conn *sql.Conn, q string, args ...any) {
	t.Helper()
	if _, err := conn.ExecContext(context.Background(), q, args...); err != nil {
		t.Fatalf("%v\n%s", err, q)
	}
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
