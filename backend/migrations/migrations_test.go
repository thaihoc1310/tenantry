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
	mustExec(t, db, `INSERT INTO operations (id, org_id, resource_type, resource_id, type, correlation_id)
		VALUES ('op-1', 'org-acme', 'Workspace', 'ws-1', 'workspace.create', 'req-1')`)
	mustExec(t, db, invite("inv-1", "dave@example.com", "OrgMember", "Pending"))
	mustExec(t, db, `INSERT INTO ownership_transfers (id, org_id, from_user_id, to_user_id, expires_at)
		VALUES ('ot-1', 'org-acme', 'usr-alice', 'usr-bob', now() + interval '7 days')`)
	mustExec(t, db, `INSERT INTO clusters (id, display_name, proxy_endpoint, console_url, apps_domain) VALUES
		('cluster-a', 'A', 'https://k', 'https://c', 'apps.test'), ('cluster-b', 'B', 'https://k', 'https://c', 'apps.test')`)
	mustExec(t, db, `INSERT INTO shared_compute_pools (id, cluster_id, name, allocation_overcommit_ratio) VALUES
		('pool-a', 'cluster-a', 'A', 2), ('pool-b', 'cluster-b', 'B', 2)`)
	mustExec(t, db, `INSERT INTO cluster_nodes (cluster_id, node_name, kind, shared_pool_id) VALUES
		('cluster-a', 'wk1', 'Worker', 'pool-a'), ('cluster-a', 'wk2', 'Worker', NULL), ('cluster-a', 'cp1', 'ControlPlane', NULL)`)
	mustExec(t, db, `INSERT INTO organization_cluster_grants (id, org_id, cluster_id, max_workspaces, max_namespaces_total,
		requests_cpu_milli, requests_memory_bytes, limits_cpu_milli, limits_memory_bytes, pods, requests_storage_bytes)
		VALUES ('grant-1', 'org-acme', 'cluster-a', 1, 1, 0, 0, 0, 0, 0, 0)`)
	mustExec(t, db, `INSERT INTO dedicated_node_grants (id, org_id, cluster_id, node_name) VALUES ('dng-1', 'org-acme', 'cluster-a', 'wk2')`)

	const unique, check, foreignKey = "23505", "23514", "23503"
	cases := []struct {
		name, sql, code string
	}{
		{"same issuer+subject twice", `INSERT INTO users (id, issuer, subject) VALUES ('usr-x', 'https://id', 'sub-alice')`, unique},
		{"user status outside enum", `INSERT INTO users (id, issuer, subject, status) VALUES ('usr-x', 'https://id', 'sub-x', 'Banned')`, check},
		{"slug with uppercase", orgInsert("org-x", "Acme2", "usr-bob", "Pending"), check},
		{"slug too short", orgInsert("org-x", "ab", "usr-bob", "Pending"), check},
		{"slug with underscore", orgInsert("org-x", "ac_me", "usr-bob", "Pending"), check},
		{"slug ends with dash", orgInsert("org-x", "acme-", "usr-bob", "Pending"), check},
		{"slug with a hyphen", orgInsert("org-x", "ac-me", "usr-bob", "Pending"), check},
		{"slug longer than 20", orgInsert("org-x", "abcdefghijklmnopqrstu", "usr-bob", "Pending"), check},
		{"slug taken by active org", orgInsert("org-x", "acme", "usr-bob", "Pending"), unique},
		{"org status outside enum", orgInsert("org-x", "beta", "usr-bob", "Suspended"), check},
		{"second owner in one org", `INSERT INTO organization_members (org_id, user_id, role) VALUES ('org-acme', 'usr-bob', 'OrgOwner')`, unique},
		{"event for a cluster that is not registered", `INSERT INTO outbox_events (operation_id, ordering_key, target_cluster_id, type, payload, version)
			VALUES ('op-1', 'Workspace/ws-1', 'cluster-x', 'workspace.apply', '{}', 1)`, foreignKey},
		{"fewer groups than the three default ones", `INSERT INTO organization_entitlements (org_id, max_members, max_groups, max_workspaces_total)
			VALUES ('org-acme', 20, 2, 8)`, check},
		{"invitation to become owner", invite("inv-x", "carol@example.com", "OrgOwner", "Pending"), check},
		{"invitation email not lowercase", invite("inv-x", "Carol@example.com", "OrgMember", "Pending"), check},
		{"accepted invitation without its acceptor", invite("inv-x", "carol@example.com", "OrgMember", "Accepted"), check},
		{"second pending invitation for one email", invite("inv-x", "dave@example.com", "OrgMember", "Pending"), unique},
		{"transfer to oneself", `INSERT INTO ownership_transfers (id, org_id, from_user_id, to_user_id, expires_at)
			VALUES ('ot-x', 'org-acme', 'usr-alice', 'usr-alice', now() + interval '7 days')`, check},
		{"second waiting transfer in one org", `INSERT INTO ownership_transfers (id, org_id, from_user_id, to_user_id, expires_at)
			VALUES ('ot-x', 'org-acme', 'usr-alice', 'usr-bob', now() + interval '7 days')`, unique},
		{"node in a pool of another cluster", `UPDATE cluster_nodes SET shared_pool_id = 'pool-b' WHERE node_name = 'wk2'`, foreignKey},
		{"control-plane node in a pool", `UPDATE cluster_nodes SET shared_pool_id = 'pool-a' WHERE node_name = 'cp1'`, check},
		{"node kind outside enum", `INSERT INTO cluster_nodes (cluster_id, node_name, kind) VALUES ('cluster-a', 'x', 'Edge')`, check},
		{"node dedicated twice", `INSERT INTO dedicated_node_grants (id, org_id, cluster_id, node_name) VALUES ('dng-x', 'org-acme', 'cluster-a', 'wk2')`, unique},
		{"negative system reserve", `UPDATE shared_compute_pools SET system_reserve_cpu_milli = -1 WHERE id = 'pool-a'`, check},
		{"user sync status outside enum", `UPDATE users SET sync_status = 'Done' WHERE id = 'usr-bob'`, check},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { expectCode(t, db, c.sql, c.code) })
	}

	// One subtest, so running it alone with -run still sets up what it checks.
	t.Run("one pending request per user, rejection releases the slug", func(t *testing.T) {
		mustExec(t, db, orgInsert("org-p1", "firstreq", "usr-bob", "Pending"))
		expectCode(t, db, orgInsert("org-p2", "secondreq", "usr-bob", "Pending"), unique)
		mustExec(t, db, `UPDATE organizations SET status = 'Rejected' WHERE id = 'org-p1'`)
		mustExec(t, db, orgInsert("org-p3", "firstreq", "usr-bob", "Pending"))
	})
	t.Run("a revoked node grant frees the node", func(t *testing.T) {
		mustExec(t, db, `UPDATE dedicated_node_grants SET revoked_at = now() WHERE id = 'dng-1'`)
		mustExec(t, db, `INSERT INTO dedicated_node_grants (id, org_id, cluster_id, node_name) VALUES ('dng-2', 'org-acme', 'cluster-a', 'wk2')`)
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

// invite is an invitation of org-acme by usr-alice; token hashes are derived from the id.
func invite(id, email, role, state string) string {
	return "INSERT INTO invitations (id, org_id, email, token_hash, role, invited_by, expires_at, state) VALUES ('" +
		id + "', 'org-acme', '" + email + "', sha256(convert_to('" + id + "', 'UTF8')), '" + role + "', 'usr-alice', now() + interval '7 days', '" + state + "')"
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

// 0006 forbids hyphens in organization slugs from then on, without failing on
// organizations made before, which may have one. 0016 then checks every row:
// it refuses a database that still has such a slug, until that row is fixed.
func TestSlugRuleKeepsOlderOrganizations(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	p := provider(t, db)
	if _, err := p.UpTo(ctx, 5); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `INSERT INTO users (id, issuer, subject) VALUES ('usr-a', 'https://id', 'a')`)
	mustExec(t, db, orgInsert("org-old", "old-style", "usr-a", "Active"))
	if _, err := p.UpTo(ctx, 15); err != nil {
		t.Fatalf("0006 on a database with a hyphenated slug: %v", err)
	}
	expectCode(t, db, orgInsert("org-new", "new-style", "usr-a", "Pending"), "23514")

	var pgErr *pgconn.PgError
	if _, err := p.Up(ctx); !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != "organizations_slug_check" {
		t.Fatalf("0016 with a hyphenated slug left: %v, want 23514 on organizations_slug_check", err)
	}
	mustExec(t, db, `UPDATE organizations SET slug = 'oldstyle' WHERE id = 'org-old'`)
	if _, err := p.Up(ctx); err != nil {
		t.Fatalf("0016 once the slug is fixed: %v", err)
	}
}
