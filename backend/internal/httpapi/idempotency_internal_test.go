package httpapi

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/thaihoc1310/tenantry/backend/internal/store"
	"github.com/thaihoc1310/tenantry/backend/internal/testdb"
	"github.com/thaihoc1310/tenantry/backend/migrations"
)

// The handler's writes and the stored response commit together: when the
// response cannot be stored, the handler's change is rolled back with it.
func TestIdempotentChangeAndResponseCommitTogether(t *testing.T) {
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
	if _, err := sqlDB.Exec(`INSERT INTO users (id, issuer, subject) VALUES ('usr-a', 'iss', 'a')`); err != nil {
		t.Fatal(err)
	}
	db := testdb.Pool(t, url)
	defer db.Close()

	// The handler creates a user through store.InTx, as services do, then
	// answers 200 with the given body.
	handler := func(answer string) http.Handler {
		return idempotent(db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := store.InTx(r.Context(), db, func(tx pgx.Tx) error {
				_, err := tx.Exec(r.Context(), `INSERT INTO users (id, issuer, subject) VALUES ('usr-new', 'iss', 'new')`)
				return err
			}); err != nil {
				t.Error(err)
			}
			_, _ = w.Write([]byte(answer))
		}))
	}
	post := func(h http.Handler) int {
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("{}"))
		req.Header.Set(IdempotencyHeader, "6f1c2b8e-3d4a-4c5b-9e7f-0a1b2c3d4e5f")
		req = req.WithContext(context.WithValue(req.Context(), principalKey, principal{user: store.User{ID: "usr-a"}}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	users := func() int {
		var n int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM users WHERE id = 'usr-new'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// Not JSON: the response cannot be stored, so the user is not created.
	if code := post(handler("not json")); code != 500 || users() != 0 {
		t.Fatalf("unstorable response: status %d, %d users created", code, users())
	}
	if code := post(handler(`{"ok":true}`)); code != 200 || users() != 1 {
		t.Fatalf("storable response: status %d, %d users created", code, users())
	}
}
