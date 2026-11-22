package keycloak

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/store"
	"github.com/thaihoc1310/tenantry/backend/internal/testdb"
	"github.com/thaihoc1310/tenantry/backend/migrations"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

const (
	groupA = "org_01:group_a"
	groupB = "org_01:group_b"
)

// setup gives a database with two users (Keycloak ids kc-ann, kc-bob), two
// groups of one organization and Ann in group A, all Pending; and a fake
// Keycloak that knows both users.
func setup(t *testing.T) (*pgxpool.Pool, *fakeKeycloak, *Worker) {
	t.Helper()
	url := testdb.New(t)
	ctx := context.Background()
	sqlDB, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sqlDB.Close() }()
	p, err := migrations.NewProvider(sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`
		INSERT INTO users (id, issuer, subject) VALUES ('usr-ann', 'iss', 'kc-ann'), ('usr-bob', 'iss', 'kc-bob');
		INSERT INTO organizations (id, slug, display_name, owner_user_id, status) VALUES ('org-01', 'acme', 'ACME', 'usr-ann', 'Active');
		INSERT INTO groups (id, org_id, display_name, machine_name) VALUES
			('grp-a', 'org-01', 'a', '` + groupA + `'), ('grp-b', 'org-01', 'b', '` + groupB + `');
		INSERT INTO group_members (group_id, user_id) VALUES ('grp-a', 'usr-ann');`); err != nil {
		t.Fatal(err)
	}
	pool := testdb.Pool(t, url)
	kc := newFakeKeycloak(t, "kc-ann", "kc-bob")
	return pool, kc, NewWorker(pool, kc.client(), quiet)
}

func runOnce(t *testing.T, w *Worker) int {
	t.Helper()
	n, err := w.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func exec(t *testing.T, db *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := db.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

// rowState is a group or membership row as "live|tombstone:<sync_status>",
// or "absent".
func rowState(t *testing.T, db *pgxpool.Pool, group, user string) string {
	t.Helper()
	q := `SELECT coalesce((SELECT CASE WHEN deleted_at IS NULL THEN 'live' ELSE 'tombstone' END || ':' || sync_status
		FROM groups WHERE id = $1), 'absent')`
	args := []any{group}
	if user != "" {
		q = `SELECT coalesce((SELECT CASE WHEN deleted_at IS NULL THEN 'live' ELSE 'tombstone' END || ':' || sync_status
			FROM group_members WHERE group_id = $1 AND user_id = $2), 'absent')`
		args = append(args, user)
	}
	var s string
	if err := db.QueryRow(context.Background(), q, args...).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSync(t *testing.T) {
	db, kc, w := setup(t)
	if n := runOnce(t, w); n != 2 {
		t.Fatalf("handled %d rows, want 2 groups", n)
	}
	var kcID string
	if err := db.QueryRow(context.Background(), `SELECT keycloak_group_id FROM groups WHERE id = 'grp-a'`).Scan(&kcID); err != nil ||
		kcID != kc.groupNamed(groupA) || kcID == "" {
		t.Fatalf("keycloak_group_id %q, Keycloak has %q (%v)", kcID, kc.groupNamed(groupA), err)
	}
	if rowState(t, db, "grp-a", "") != "live:Synced" || rowState(t, db, "grp-b", "") != "live:Synced" {
		t.Fatal("groups not Synced")
	}
	if !kc.isMember("kc-ann", groupA) || rowState(t, db, "grp-a", "usr-ann") != "live:Synced" {
		t.Fatalf("Ann not synced into group A: %s", rowState(t, db, "grp-a", "usr-ann"))
	}
	// Nobody has a Workspace binding before M6: TenantUsers exists, empty.
	if kc.groupNamed(TenantUsers) == "" || kc.isMember("kc-ann", TenantUsers) {
		t.Fatal("TenantUsers missing, or Ann in it without a binding")
	}

	// Nothing due: nothing done.
	calls := kc.count("PUT /admin/realms/tenantry/users/{user}/groups/{group}") + kc.count("GET /admin/realms/tenantry/groups")
	if n := runOnce(t, w); n != 0 || kc.count("PUT /admin/realms/tenantry/users/{user}/groups/{group}")+kc.count("GET /admin/realms/tenantry/groups") != calls {
		t.Fatalf("an idle run handled %d rows or called Keycloak", n)
	}

	// A removed member leaves Keycloak, then the database.
	exec(t, db, `UPDATE group_members SET deleted_at = now(), sync_status = 'Pending' WHERE group_id = 'grp-a'`)
	runOnce(t, w)
	if kc.isMember("kc-ann", groupA) || rowState(t, db, "grp-a", "usr-ann") != "absent" {
		t.Fatalf("removal not synced: %s", rowState(t, db, "grp-a", "usr-ann"))
	}

	// So does a deleted group, with its memberships.
	exec(t, db, `INSERT INTO group_members (group_id, user_id) VALUES ('grp-b', 'usr-bob')`)
	runOnce(t, w)
	exec(t, db, `UPDATE groups SET deleted_at = now(), sync_status = 'Pending' WHERE id = 'grp-b'`)
	exec(t, db, `UPDATE group_members SET deleted_at = now(), sync_status = 'Pending' WHERE group_id = 'grp-b'`)
	runOnce(t, w)
	if kc.groupNamed(groupB) != "" || rowState(t, db, "grp-b", "") != "absent" || rowState(t, db, "grp-b", "usr-bob") != "absent" {
		t.Fatal("deleted group still in Keycloak or in the database")
	}
}

func TestSyncRetriesWithBackoff(t *testing.T) {
	db, kc, w := setup(t)
	kc.set(func(f *fakeKeycloak) {
		f.before = func(r *http.Request) int {
			if r.Method == http.MethodPut {
				return http.StatusServiceUnavailable
			}
			return 0
		}
	})
	runOnce(t, w)
	var attempts int
	var lastError string
	var wait time.Duration
	read := func() {
		t.Helper()
		var secs float64
		if err := db.QueryRow(context.Background(), `SELECT sync_attempts, last_error, extract(epoch FROM next_sync_at - now())
			FROM group_members WHERE group_id = 'grp-a' AND user_id = 'usr-ann'`).Scan(&attempts, &lastError, &secs); err != nil {
			t.Fatal(err)
		}
		wait = time.Duration(secs * float64(time.Second))
	}
	read()
	if rowState(t, db, "grp-a", "usr-ann") != "live:Error" || attempts != 1 || !strings.Contains(lastError, "503") ||
		wait < 500*time.Millisecond || wait > time.Second {
		t.Fatalf("after one failure: %s attempts %d wait %v error %q", rowState(t, db, "grp-a", "usr-ann"), attempts, wait, lastError)
	}
	if rowState(t, db, "grp-a", "") != "live:Synced" {
		t.Fatal("the member's failure touched its group")
	}
	// Not due yet: not tried.
	puts := kc.count("PUT /admin/realms/tenantry/users/{user}/groups/{group}")
	if runOnce(t, w); kc.count("PUT /admin/realms/tenantry/users/{user}/groups/{group}") != puts {
		t.Fatal("retried before its backoff ran out")
	}
	exec(t, db, `UPDATE group_members SET next_sync_at = now()`)
	runOnce(t, w)
	if read(); attempts != 2 || wait < 1500*time.Millisecond || wait > 2*time.Second {
		t.Fatalf("after two failures: attempts %d wait %v", attempts, wait)
	}

	// Keycloak recovers.
	kc.set(func(f *fakeKeycloak) { f.before = nil })
	exec(t, db, `UPDATE group_members SET next_sync_at = now()`)
	runOnce(t, w)
	if read(); rowState(t, db, "grp-a", "usr-ann") != "live:Synced" || attempts != 0 || lastError != "" || !kc.isMember("kc-ann", groupA) {
		t.Fatalf("after recovery: %s attempts %d error %q", rowState(t, db, "grp-a", "usr-ann"), attempts, lastError)
	}

	// A person Keycloak does not know stays in Error, retried.
	exec(t, db, `INSERT INTO users (id, issuer, subject) VALUES ('usr-gone', 'iss', 'kc-gone');
		INSERT INTO group_members (group_id, user_id) VALUES ('grp-a', 'usr-gone')`)
	runOnce(t, w)
	if rowState(t, db, "grp-a", "usr-gone") != "live:Error" {
		t.Fatalf("unknown Keycloak user: %s", rowState(t, db, "grp-a", "usr-gone"))
	}
}

// A worker that dies half-way (here: its batch context ends right after a
// Keycloak call) leaves its rows due; the next run finishes the job without
// creating anything twice.
func TestSyncAfterCrash(t *testing.T) {
	db, kc, w := setup(t)
	crashAfter := func(method string) context.Context {
		ctx, cancel := context.WithCancel(context.Background())
		kc.set(func(f *fakeKeycloak) {
			f.after = func(r *http.Request) {
				if r.Method == method {
					cancel()
				}
			}
		})
		return ctx
	}

	// Dies after creating group B in Keycloak (first in line), before recording it.
	exec(t, db, `UPDATE groups SET next_sync_at = now() - interval '1 minute' WHERE id = 'grp-b'`)
	if _, err := w.RunOnce(crashAfter(http.MethodPost)); err == nil {
		t.Fatal("the batch survived its context")
	}
	kc.set(func(f *fakeKeycloak) { f.after = nil })
	if kc.groupNamed(groupB) == "" {
		t.Fatal("the crash came before Keycloak created group B")
	}
	if rowState(t, db, "grp-a", "") != "live:Pending" || rowState(t, db, "grp-b", "") != "live:Pending" {
		t.Fatal("a crashed batch left a trace in the database")
	}
	// Group B is deleted meanwhile: its Keycloak id was never recorded.
	exec(t, db, `UPDATE groups SET deleted_at = now() WHERE id = 'grp-b'`)
	runOnce(t, w)
	if rowState(t, db, "grp-a", "") != "live:Synced" || kc.groupNamed(groupA) == "" { // groupNamed panics on a duplicate
		t.Fatal("group A not synced after the crash")
	}
	if kc.groupNamed(groupB) != "" || rowState(t, db, "grp-b", "") != "absent" {
		t.Fatal("group B left behind in Keycloak")
	}

	// Dies after adding Bob in Keycloak, before recording it.
	exec(t, db, `INSERT INTO group_members (group_id, user_id) VALUES ('grp-a', 'usr-bob')`)
	if _, err := w.RunOnce(crashAfter(http.MethodPut)); err == nil {
		t.Fatal("the batch survived its context")
	}
	kc.set(func(f *fakeKeycloak) { f.after = nil })
	if !kc.isMember("kc-bob", groupA) || rowState(t, db, "grp-a", "usr-bob") != "live:Pending" {
		t.Fatalf("crash state: Keycloak member %v, row %s", kc.isMember("kc-bob", groupA), rowState(t, db, "grp-a", "usr-bob"))
	}
	runOnce(t, w)
	if !kc.isMember("kc-bob", groupA) || rowState(t, db, "grp-a", "usr-bob") != "live:Synced" {
		t.Fatal("membership not synced after the crash")
	}
}

// Adding a member back while the worker is removing them waits for the
// worker, then wins: the person ends up a member, in Keycloak too.
func TestSyncReAddWhileRemoving(t *testing.T) {
	db, kc, w := setup(t)
	runOnce(t, w)
	exec(t, db, `UPDATE group_members SET deleted_at = now(), sync_status = 'Pending' WHERE group_id = 'grp-a'`)

	var readd sync.WaitGroup
	kc.set(func(f *fakeKeycloak) {
		f.before = func(r *http.Request) int {
			if r.Method != http.MethodDelete || !strings.Contains(r.URL.Path, "/kc-ann/") {
				return 0
			}
			// The worker holds the row: the API's JoinGroup has to wait.
			readd.Go(func() {
				if _, err := store.New(db).JoinGroup(context.Background(), store.JoinGroupParams{GroupID: "grp-a", UserID: "usr-ann"}); err != nil {
					t.Error(err)
				}
			})
			waitForLockWait(t, db)
			return 0
		}
	})
	runOnce(t, w)
	readd.Wait()
	kc.set(func(f *fakeKeycloak) { f.before = nil })
	if s := rowState(t, db, "grp-a", "usr-ann"); s != "live:Pending" {
		t.Fatalf("after the race: %s, want a live member waiting for sync", s)
	}
	runOnce(t, w)
	if !kc.isMember("kc-ann", groupA) || rowState(t, db, "grp-a", "usr-ann") != "live:Synced" {
		t.Fatal("did not converge to a member")
	}
}

// waitForLockWait returns once some session of the database waits for a lock.
func waitForLockWait(t *testing.T, db *pgxpool.Pool) {
	t.Helper()
	for range 500 {
		var n int
		if err := db.QueryRow(context.Background(), `SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&n); err != nil {
			t.Error(err)
			return
		}
		if n > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("nobody waited for the worker's lock")
}

// Two workers never take the same row.
func TestSyncWorkersShareTheQueue(t *testing.T) {
	db, kc, w := setup(t)
	for i := range 30 {
		exec(t, db, `INSERT INTO groups (id, org_id, display_name, machine_name) VALUES ($1, 'org-01', $1, 'org_01:' || $1)`,
			fmt.Sprintf("grp-%d", i))
	}
	other := NewWorker(db, kc.client(), quiet)
	var wg sync.WaitGroup
	for _, worker := range []*Worker{w, other} {
		wg.Go(func() {
			for range 5 {
				if _, err := worker.RunOnce(context.Background()); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
	if n := kc.count("POST /admin/realms/tenantry/groups"); n != 32+1 { // 32 groups and TenantUsers
		t.Fatalf("%d groups created, want 33", n)
	}
	var due int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM groups WHERE sync_status <> 'Synced'`).Scan(&due); err != nil || due != 0 {
		t.Fatalf("%d groups left unsynced (%v)", due, err)
	}
}

// The client fetches one token and reuses it; when Keycloak stops accepting
// it, the client fetches a new one and retries once.
func TestClientToken(t *testing.T) {
	kc := newFakeKeycloak(t)
	c := kc.client()
	ctx := context.Background()
	for range 3 {
		if _, err := c.EnsureGroup(ctx, "g"); err != nil {
			t.Fatal(err)
		}
	}
	if kc.tokenCount() != 1 {
		t.Fatalf("%d token requests, want 1", kc.tokenCount())
	}
	kc.set(func(f *fakeKeycloak) { f.token = "token-2" })
	if _, _, err := c.FindGroup(ctx, "g"); err != nil || kc.tokenCount() != 2 {
		t.Fatalf("after rotation: %v, %d token requests", err, kc.tokenCount())
	}
	bad := NewClient(c.cfg)
	bad.cfg.ClientSecret = "wrong"
	if _, _, err := bad.FindGroup(ctx, "g"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("wrong secret: %v", err)
	}
}

// A Platform Admin disables an account, then enables it again; Keycloak
// follows, and a failure is retried with backoff.
func TestSyncUserEnabled(t *testing.T) {
	db, kc, w := setup(t)
	runOnce(t, w) // the groups and Ann's membership
	userState := func(id string) string {
		t.Helper()
		var s string
		if err := db.QueryRow(context.Background(), `SELECT status || ':' || sync_status FROM users WHERE id = $1`, id).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}

	exec(t, db, `UPDATE users SET status = 'Disabled', sync_status = 'Pending' WHERE id = 'usr-bob'`)
	runOnce(t, w)
	if !kc.isDisabled("kc-bob") || userState("usr-bob") != "Disabled:Synced" || kc.isDisabled("kc-ann") {
		t.Fatalf("disable: keycloak %v, row %s", kc.isDisabled("kc-bob"), userState("usr-bob"))
	}

	kc.set(func(f *fakeKeycloak) {
		f.before = func(r *http.Request) int {
			if r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/users/kc-bob") {
				return http.StatusServiceUnavailable
			}
			return 0
		}
	})
	exec(t, db, `UPDATE users SET status = 'Active', sync_status = 'Pending' WHERE id = 'usr-bob'`)
	runOnce(t, w)
	var attempts int
	if err := db.QueryRow(context.Background(), `SELECT sync_attempts FROM users WHERE id = 'usr-bob'`).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if userState("usr-bob") != "Active:Error" || attempts != 1 || !kc.isDisabled("kc-bob") {
		t.Fatalf("failed enable: row %s attempts %d", userState("usr-bob"), attempts)
	}
	kc.set(func(f *fakeKeycloak) { f.before = nil })
	exec(t, db, `UPDATE users SET next_sync_at = now() WHERE id = 'usr-bob'`)
	runOnce(t, w)
	if kc.isDisabled("kc-bob") || userState("usr-bob") != "Active:Synced" {
		t.Fatalf("enable after recovery: keycloak %v, row %s", kc.isDisabled("kc-bob"), userState("usr-bob"))
	}

	// Disabling an account Keycloak no longer has succeeds; enabling it does not.
	exec(t, db, `INSERT INTO users (id, issuer, subject, status, sync_status) VALUES ('usr-gone', 'iss', 'kc-gone', 'Disabled', 'Pending')`)
	runOnce(t, w)
	if userState("usr-gone") != "Disabled:Synced" {
		t.Fatalf("disable a user Keycloak lost: %s", userState("usr-gone"))
	}
	exec(t, db, `UPDATE users SET status = 'Active', sync_status = 'Pending' WHERE id = 'usr-gone'`)
	runOnce(t, w)
	if userState("usr-gone") != "Active:Error" {
		t.Fatalf("enable a user Keycloak lost: %s", userState("usr-gone"))
	}
}
