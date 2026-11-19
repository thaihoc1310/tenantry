package httpapi_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi"
	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
	"github.com/thaihoc1310/tenantry/backend/internal/testdb"
	"github.com/thaihoc1310/tenantry/backend/migrations"
)

// Seed: alice owns acme (Active), erin is its OrgAdmin and bob a member. bob's request "beta"
// was Rejected and he asked again under the same slug (now Pending).
// carol is disabled. "gone" is a Deleted org alice still belongs to.
const seed = `
INSERT INTO users (id, issuer, subject, status) VALUES
  ('usr-alice', 'iss', 'a', 'Active'), ('usr-bob', 'iss', 'b', 'Active'),
  ('usr-carol', 'iss', 'c', 'Disabled'), ('usr-dave', 'iss', 'd', 'Active'),
  ('usr-erin', 'iss', 'e', 'Active');
INSERT INTO organizations (id, slug, display_name, owner_user_id, status, review_note, created_at) VALUES
  ('org-acme', 'acme', 'ACME', 'usr-alice', 'Active', '', now()),
  ('org-gone', 'gone', 'Gone', 'usr-alice', 'Deleted', '', now()),
  ('org-beta1', 'beta', 'Beta', 'usr-bob', 'Rejected', 'no reason given', now() - interval '1 day'),
  ('org-beta2', 'beta', 'Beta', 'usr-bob', 'Pending', '', now());
INSERT INTO organization_members (org_id, user_id, role) VALUES
  ('org-acme', 'usr-alice', 'OrgOwner'), ('org-acme', 'usr-bob', 'OrgMember'), ('org-acme', 'usr-erin', 'OrgAdmin'),
  ('org-gone', 'usr-alice', 'OrgOwner'),
  ('org-beta1', 'usr-bob', 'OrgOwner'), ('org-beta2', 'usr-bob', 'OrgOwner');
`

func newAPI(t *testing.T, opts httpapi.Options) http.Handler {
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
	if _, err := sqlDB.Exec(seed); err != nil {
		t.Fatal(err)
	}

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	mux := http.NewServeMux()
	httpapi.Register(mux, pool, opts)
	return httpapi.WithRequestID(mux)
}

func call(h http.Handler, method, path, user string) *httptest.ResponseRecorder {
	return callBody(h, method, path, user, "")
}

func callBody(h http.Handler, method, path, user, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if user != "" {
		req.Header.Set(httpapi.DevUserHeader, user)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestGetOrganization(t *testing.T) {
	h := newAPI(t, httpapi.Options{DevAuth: true})

	cases := []struct {
		name, user, path string
		status           int
		wantID           string
		wantRole         oapi.OrgRole
		wantCode         oapi.ErrorCode
	}{
		{"owner by id", "usr-alice", "/api/v1/organizations/org-acme", 200, "org-acme", "OrgOwner", ""},
		{"member by slug", "usr-bob", "/api/v1/organizations/acme", 200, "org-acme", "OrgMember", ""},
		{"live request wins over a rejected one with the same slug", "usr-bob", "/api/v1/organizations/beta", 200, "org-beta2", "OrgOwner", ""},
		{"rejected request still readable by id", "usr-bob", "/api/v1/organizations/org-beta1", 200, "org-beta1", "OrgOwner", ""},
		{"non-member gets 404, not 403", "usr-dave", "/api/v1/organizations/org-acme", 404, "", "", oapi.NOTFOUND},
		{"missing org", "usr-alice", "/api/v1/organizations/nope", 404, "", "", oapi.NOTFOUND},
		{"deleted org", "usr-alice", "/api/v1/organizations/org-gone", 404, "", "", oapi.NOTFOUND},
		{"no login", "", "/api/v1/organizations/org-acme", 401, "", "", oapi.UNAUTHENTICATED},
		{"unknown user", "usr-ghost", "/api/v1/organizations/org-acme", 401, "", "", oapi.UNAUTHENTICATED},
		{"disabled user", "usr-carol", "/api/v1/organizations/org-acme", 401, "", "", oapi.UNAUTHENTICATED},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := call(h, http.MethodGet, c.path, c.user)
			if rec.Code != c.status {
				t.Fatalf("status %d, want %d: %s", rec.Code, c.status, rec.Body)
			}
			if c.status == 200 {
				var o oapi.Organization
				if err := json.Unmarshal(rec.Body.Bytes(), &o); err != nil {
					t.Fatal(err)
				}
				if o.Id != c.wantID || o.MyRole == nil || *o.MyRole != c.wantRole {
					t.Fatalf("got id=%s role=%v, want %s %s", o.Id, o.MyRole, c.wantID, c.wantRole)
				}
				return
			}
			var e oapi.ErrorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
				t.Fatal(err)
			}
			if e.Code != c.wantCode || e.RequestId == "" || e.RequestId != rec.Header().Get("X-Request-Id") {
				t.Fatalf("got %+v (X-Request-Id %q)", e, rec.Header().Get("X-Request-Id"))
			}
		})
	}
}

// The dev header must be inert unless explicitly enabled.
func TestDevHeaderIgnoredWhenDisabled(t *testing.T) {
	h := newAPI(t, httpapi.Options{DevAuth: false})
	if rec := call(h, http.MethodGet, "/api/v1/organizations/org-acme", "usr-alice"); rec.Code != 401 {
		t.Fatalf("status %d, want 401", rec.Code)
	}
}

func TestPatchOrganization(t *testing.T) {
	h := newAPI(t, httpapi.Options{DevAuth: true})

	cases := []struct {
		name, user, path, body string
		status                 int
		wantName, wantDesc     string // checked on 200
		wantCode               oapi.ErrorCode
	}{
		{"owner renames, value trimmed", "usr-alice", "/api/v1/organizations/acme", `{"displayName":"  ACME Inc  "}`, 200, "ACME Inc", "", ""},
		{"admin sets description, name kept", "usr-erin", "/api/v1/organizations/org-acme", `{"description":"robots"}`, 200, "ACME Inc", "robots", ""},
		{"empty body changes nothing", "usr-alice", "/api/v1/organizations/acme", `{}`, 200, "ACME Inc", "robots", ""},
		{"pending request is editable", "usr-bob", "/api/v1/organizations/beta", `{"displayName":"Beta 2"}`, 200, "Beta 2", "", ""},
		{"member cannot edit", "usr-bob", "/api/v1/organizations/acme", `{"displayName":"x"}`, 403, "", "", oapi.FORBIDDEN},
		{"non-member gets 404", "usr-dave", "/api/v1/organizations/acme", `{"displayName":"x"}`, 404, "", "", oapi.NOTFOUND},
		{"rejected request is read-only", "usr-bob", "/api/v1/organizations/org-beta1", `{"displayName":"x"}`, 409, "", "", oapi.CONFLICT},
		{"blank name", "usr-alice", "/api/v1/organizations/acme", `{"displayName":"   "}`, 400, "", "", oapi.VALIDATIONFAILED},
		{"name over 80 chars", "usr-alice", "/api/v1/organizations/acme", `{"displayName":"` + strings.Repeat("a", 81) + `"}`, 400, "", "", oapi.VALIDATIONFAILED},
		{"unknown field (slug is immutable)", "usr-alice", "/api/v1/organizations/acme", `{"slug":"other"}`, 400, "", "", oapi.VALIDATIONFAILED},
		{"missing body", "usr-alice", "/api/v1/organizations/acme", "", 400, "", "", oapi.VALIDATIONFAILED},
		{"anonymous gets 401 even with a bad body", "", "/api/v1/organizations/acme", `{"slug":"other"}`, 401, "", "", oapi.UNAUTHENTICATED},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := callBody(h, http.MethodPatch, c.path, c.user, c.body)
			if rec.Code != c.status {
				t.Fatalf("status %d, want %d: %s", rec.Code, c.status, rec.Body)
			}
			if c.status == 200 {
				var o oapi.Organization
				if err := json.Unmarshal(rec.Body.Bytes(), &o); err != nil {
					t.Fatal(err)
				}
				if o.DisplayName != c.wantName || o.Description != c.wantDesc {
					t.Fatalf("got name=%q desc=%q, want %q %q", o.DisplayName, o.Description, c.wantName, c.wantDesc)
				}
				return
			}
			var e oapi.ErrorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
				t.Fatalf("%v: %s", err, rec.Body)
			}
			if e.Code != c.wantCode || e.RequestId == "" {
				t.Fatalf("got %+v", e)
			}
			if strings.Contains(e.Message, "Schema:") {
				t.Fatalf("validation message dumps the schema: %q", e.Message)
			}
		})
	}

	// The 200s above must have been written, not just echoed.
	var o oapi.Organization
	_ = json.Unmarshal(call(h, http.MethodGet, "/api/v1/organizations/acme", "usr-bob").Body.Bytes(), &o)
	if o.DisplayName != "ACME Inc" || o.Description != "robots" {
		t.Fatalf("not persisted: %+v", o)
	}
}
