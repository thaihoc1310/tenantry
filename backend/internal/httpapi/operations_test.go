package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
)

// op-failed gave up delivering after 10 attempts; op-running is under way.
const opSeed = `
INSERT INTO operations (id, org_id, resource_type, resource_id, type, state, error_code, error_message, correlation_id, created_by, finished_at) VALUES
  ('op-failed', 'org-acme', 'Workspace', 'ws-1', 'workspace.create', 'Failed', 'APPLY_FAILED', 'cluster unreachable', 'req-1', 'usr-alice', now()),
  ('op-running', 'org-acme', 'Workspace', 'ws-2', 'workspace.create', 'Running', '', '', 'req-2', NULL, NULL);
INSERT INTO outbox_events (operation_id, ordering_key, target_cluster_id, type, payload, version, attempts, next_attempt_at, last_error) VALUES
  ('op-failed', 'Workspace/ws-1', 'cluster-a', 'workspace.apply', '{}', 1, 10, now() + interval '5 minutes', 'cluster unreachable');
`

func TestOperations(t *testing.T) {
	h, db := newAPI(t)
	if _, err := db.Exec(context.Background(), opSeed); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, method, path, user string
		status                   int
		wantState                oapi.OperationState
		wantCode                 oapi.ErrorCode
	}{
		{"member reads", http.MethodGet, "/api/v1/operations/op-failed", "usr-bob", 200, "Failed", ""},
		{"non-member gets 404", http.MethodGet, "/api/v1/operations/op-failed", "usr-dave", 404, "", oapi.NOTFOUND},
		{"missing", http.MethodGet, "/api/v1/operations/op-nope", "usr-alice", 404, "", oapi.NOTFOUND},
		{"no login", http.MethodGet, "/api/v1/operations/op-failed", "", 401, "", oapi.UNAUTHENTICATED},
		{"member cannot retry", http.MethodPost, "/api/v1/operations/op-failed:retry", "usr-bob", 403, "", oapi.FORBIDDEN},
		{"non-member retry gets 404", http.MethodPost, "/api/v1/operations/op-failed:retry", "usr-dave", 404, "", oapi.NOTFOUND},
		{"running cannot be retried", http.MethodPost, "/api/v1/operations/op-running:retry", "usr-alice", 409, "", oapi.CONFLICT},
		{"admin retries a failed one", http.MethodPost, "/api/v1/operations/op-failed:retry", "usr-erin", 200, "Pending", ""},
		{"a second retry is refused", http.MethodPost, "/api/v1/operations/op-failed:retry", "usr-alice", 409, "", oapi.CONFLICT},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := call(h, c.method, c.path, c.user)
			if rec.Code != c.status {
				t.Fatalf("status %d, want %d: %s", rec.Code, c.status, rec.Body)
			}
			if c.status != 200 {
				if e := errorBody(t, rec); e.Code != c.wantCode {
					t.Fatalf("code %s, want %s", e.Code, c.wantCode)
				}
				return
			}
			var op oapi.Operation
			if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil {
				t.Fatal(err)
			}
			if op.State != c.wantState || op.OrgId != "org-acme" || op.CorrelationId != "req-1" ||
				op.CreatedBy == nil || *op.CreatedBy != "usr-alice" {
				t.Fatalf("got %+v", op)
			}
			switch op.State {
			case "Failed":
				if op.ErrorCode == nil || *op.ErrorCode != "APPLY_FAILED" || op.FinishedAt == nil {
					t.Fatalf("failed operation without its error: %+v", op)
				}
			case "Pending":
				if op.ErrorCode != nil || op.FinishedAt != nil {
					t.Fatalf("retried operation keeps its old error: %+v", op)
				}
			}
		})
	}

	// The retry put the undelivered event back in line at once.
	var attempts int
	var due bool
	if err := db.QueryRow(context.Background(),
		`SELECT attempts, next_attempt_at <= now() FROM outbox_events WHERE operation_id = 'op-failed'`).Scan(&attempts, &due); err != nil {
		t.Fatal(err)
	}
	if attempts != 0 || !due {
		t.Fatalf("event not reset: attempts=%d due=%v", attempts, due)
	}

	// Requests no route takes still get an ErrorResponse. An unknown action
	// matches the GET-only /operations/{operationId}, hence 405.
	for _, c := range []struct {
		method, path string
		status       int
		code         oapi.ErrorCode
	}{
		{http.MethodPost, "/api/v1/operations/op-failed:explode", 405, oapi.METHODNOTALLOWED},
		{http.MethodDelete, "/api/v1/operations/op-failed", 405, oapi.METHODNOTALLOWED},
		{http.MethodGet, "/api/v1/nothing-here", 404, oapi.NOTFOUND},
	} {
		rec := call(h, c.method, c.path, "usr-alice")
		if rec.Code != c.status {
			t.Fatalf("%s %s: %d %s", c.method, c.path, rec.Code, rec.Body)
		}
		if e := errorBody(t, rec); e.Code != c.code {
			t.Fatalf("%s %s: code %s", c.method, c.path, e.Code)
		}
	}
}

// A Platform Admin reads and retries operations of any organization, member
// or not (06-security-and-identity §8). The realm role alone grants it.
func TestPlatformAdminOperations(t *testing.T) {
	h, db := newAPI(t)
	if _, err := db.Exec(context.Background(), `
INSERT INTO operations (id, org_id, resource_type, resource_id, type, state, error_code, finished_at) VALUES
  ('op-a', 'org-acme', 'Workspace', 'ws-1', 'workspace.create', 'Failed', 'APPLY_FAILED', now()),
  ('op-b', 'org-acme', 'Workspace', 'ws-2', 'workspace.create', 'Failed', 'APPLY_FAILED', now());`); err != nil {
		t.Fatal(err)
	}
	as := func(method, path, user string, roles ...string) *httptest.ResponseRecorder {
		return send(h, request(method, path, newSession(t, db, user, roles...), ""))
	}

	for _, c := range []struct {
		name, method, path, user string
		roles                    []string
		status                   int
		wantState                oapi.OperationState
	}{
		{"non-member admin reads", http.MethodGet, "/api/v1/operations/op-a", "usr-dave", []string{"platform-admin"}, 200, "Failed"},
		{"non-member admin retries", http.MethodPost, "/api/v1/operations/op-a:retry", "usr-dave", []string{"platform-admin"}, 200, "Pending"},
		{"admin who is only OrgMember retries", http.MethodPost, "/api/v1/operations/op-b:retry", "usr-bob", []string{"offline_access", "platform-admin"}, 200, "Pending"},
		{"other realm roles grant nothing", http.MethodGet, "/api/v1/operations/op-a", "usr-dave", []string{"offline_access"}, 404, ""},
		{"admin still gets 404 for a missing one", http.MethodGet, "/api/v1/operations/op-nope", "usr-dave", []string{"platform-admin"}, 404, ""},
		{"admin cannot retry what is not Failed", http.MethodPost, "/api/v1/operations/op-a:retry", "usr-dave", []string{"platform-admin"}, 409, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := as(c.method, c.path, c.user, c.roles...)
			if rec.Code != c.status {
				t.Fatalf("status %d, want %d: %s", rec.Code, c.status, rec.Body)
			}
			if c.status != 200 {
				return
			}
			var op oapi.Operation
			if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil {
				t.Fatal(err)
			}
			if op.State != c.wantState {
				t.Fatalf("state %s, want %s", op.State, c.wantState)
			}
		})
	}
}
