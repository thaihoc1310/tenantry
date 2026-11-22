package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi"
	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
)

const (
	keyA = "6f1c2b8e-3d4a-4c5b-9e7f-0a1b2c3d4e5f"
	keyB = "0b9e8d7c-6f5a-4b3c-8d2e-1f0a9b8c7d6e"
)

// keyed sends a portal request as user with an Idempotency-Key.
func keyed(h http.Handler, method, path, user, key, body string) *httptest.ResponseRecorder {
	req := request(method, path, sessionOf(user), body)
	req.Header.Set(httpapi.IdempotencyHeader, key)
	return send(h, req)
}

func orgsWithSlug(t *testing.T, db *pgxpool.Pool, slug string) int {
	t.Helper()
	return count(t, db, `SELECT count(*) FROM organizations WHERE slug = $1`, slug)
}

// A repeated request gets the first response back and changes nothing.
func TestIdempotentCreate(t *testing.T) {
	h, db := newAPI(t)
	const body = `{"slug":"dave","displayName":"Dave"}`

	first := keyed(h, http.MethodPost, "/api/v1/organizations", "usr-dave", keyA, body)
	if first.Code != 201 || first.Header().Get(httpapi.ReplayedHeader) != "" {
		t.Fatalf("first: %d %v %s", first.Code, first.Header(), first.Body)
	}
	again := keyed(h, http.MethodPost, "/api/v1/organizations", "usr-dave", keyA, body)
	if again.Code != 201 || again.Header().Get(httpapi.ReplayedHeader) != "true" ||
		again.Header().Get("Location") != first.Header().Get("Location") || again.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("repeat: %d %v", again.Code, again.Header())
	}
	if again.Body.String() != first.Body.String() {
		t.Fatalf("repeat answered %s, first %s", again.Body, first.Body)
	}
	var a oapi.Organization
	if err := json.Unmarshal(first.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	if n := orgsWithSlug(t, db, "dave"); n != 1 {
		t.Fatalf("%d organizations", n)
	}
	if n := count(t, db, `SELECT count(*) FROM audit_events WHERE action = 'organization.create' AND target_id = $1`, a.Id); n != 1 {
		t.Fatalf("%d audit events for one creation", n)
	}

	// The key belongs to the request it came with.
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"another body": keyed(h, http.MethodPost, "/api/v1/organizations", "usr-dave", keyA, `{"slug":"dave2","displayName":"Dave"}`),
		"another path": keyed(h, http.MethodPost, "/api/v1/operations/op-x:retry", "usr-dave", keyA, ""),
	} {
		if rec.Code != 422 || errorBody(t, rec).Code != oapi.IDEMPOTENCYKEYREUSED {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	if n := orgsWithSlug(t, db, "dave2"); n != 0 {
		t.Fatal("a reused key created an organization")
	}

	// Keys are per user: the same key from someone else is their own.
	if rec := keyed(h, http.MethodPost, "/api/v1/organizations", "usr-erin", keyA, `{"slug":"erin","displayName":"Erin"}`); rec.Code != 201 ||
		rec.Header().Get(httpapi.ReplayedHeader) != "" {
		t.Fatalf("other user: %d %s", rec.Code, rec.Body)
	}
}

// A failed request stores nothing: corrected, it goes through under the same key.
func TestIdempotentFailureIsNotKept(t *testing.T) {
	h, db := newAPI(t)
	if rec := keyed(h, http.MethodPost, "/api/v1/organizations", "usr-dave", keyA, `{"slug":"acme","displayName":"Dave"}`); rec.Code != 409 {
		t.Fatalf("taken slug: %d %s", rec.Code, rec.Body)
	}
	if n := count(t, db, `SELECT count(*) FROM idempotency_keys`); n != 0 {
		t.Fatalf("%d keys kept after a failure", n)
	}
	if rec := keyed(h, http.MethodPost, "/api/v1/organizations", "usr-dave", keyA, `{"slug":"dave","displayName":"Dave"}`); rec.Code != 201 {
		t.Fatalf("corrected: %d %s", rec.Code, rec.Body)
	}
	// The key and the change were committed together.
	if n := count(t, db, `SELECT count(*) FROM idempotency_keys WHERE user_id = 'usr-dave' AND response_status = 201`); n != 1 {
		t.Fatalf("%d stored responses", n)
	}
}

// Concurrent repeats wait for the first and get its response, not a 409
// from running into the slug it just took.
func TestIdempotentConcurrentRepeats(t *testing.T) {
	h, db := newAPI(t)
	const body = `{"slug":"dave","displayName":"Dave"}`
	ids := make([]string, 6)
	var wg sync.WaitGroup
	for i := range ids {
		wg.Go(func() {
			rec := keyed(h, http.MethodPost, "/api/v1/organizations", "usr-dave", keyA, body)
			var o oapi.Organization
			if rec.Code != 201 || json.Unmarshal(rec.Body.Bytes(), &o) != nil {
				t.Errorf("status %d: %s", rec.Code, rec.Body)
			}
			ids[i] = o.Id
		})
	}
	wg.Wait()
	for _, id := range ids {
		if id != ids[0] {
			t.Fatalf("different answers: %v", ids)
		}
	}
	if n := orgsWithSlug(t, db, "dave"); n != 1 {
		t.Fatalf("%d organizations", n)
	}
}

// Retrying an operation twice under one key retries it once: the repeat is
// answered 200 with the first result, not 409 "not Failed".
func TestIdempotentRetry(t *testing.T) {
	h, db := newAPI(t)
	if _, err := db.Exec(context.Background(), opSeed); err != nil {
		t.Fatal(err)
	}
	const path = "/api/v1/operations/op-failed:retry"
	for i, want := range []string{"", "true"} {
		rec := keyed(h, http.MethodPost, path, "usr-alice", keyB, "")
		if rec.Code != 200 || rec.Header().Get(httpapi.ReplayedHeader) != want {
			t.Fatalf("call %d: %d %v %s", i, rec.Code, rec.Header(), rec.Body)
		}
	}
	if rec := call(h, http.MethodPost, path, "usr-alice"); rec.Code != 409 {
		t.Fatalf("without the key: %d", rec.Code)
	}
	if n := count(t, db, `SELECT count(*) FROM audit_events WHERE action = 'operation.retry'`); n != 1 {
		t.Fatalf("%d retries audited", n)
	}
}

func TestIdempotencyKeyFormat(t *testing.T) {
	h, db := newAPI(t)
	for _, key := range []string{"abc", keyA + "0", "6f1c2b8e3d4a4c5b9e7f0a1b2c3d4e5f"} {
		rec := keyed(h, http.MethodPost, "/api/v1/organizations", "usr-dave", key, `{"slug":"dave","displayName":"Dave"}`)
		if rec.Code != 400 || errorBody(t, rec).Code != oapi.VALIDATIONFAILED {
			t.Errorf("key %q: %d %s", key, rec.Code, rec.Body)
		}
	}
	if n := orgsWithSlug(t, db, "dave"); n != 0 {
		t.Fatal("created with a malformed key")
	}
}
