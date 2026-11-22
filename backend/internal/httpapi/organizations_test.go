package httpapi_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/auth"
	"github.com/thaihoc1310/tenantry/backend/internal/config"
	"github.com/thaihoc1310/tenantry/backend/internal/httpapi"
	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
	"github.com/thaihoc1310/tenantry/backend/internal/testdb"
	"github.com/thaihoc1310/tenantry/backend/migrations"
)

// Seed: alice owns acme (Active), erin is its OrgAdmin and bob a member. bob's request "beta"
// was Rejected and he asked again under the same slug (now Pending). erin's
// request "oldco" was Rejected. carol is disabled. "gone" is a Deleted org alice
// still belongs to. dave belongs to nothing.
const seed = `
INSERT INTO users (id, issuer, subject, status) VALUES
  ('usr-alice', 'iss', 'a', 'Active'), ('usr-bob', 'iss', 'b', 'Active'),
  ('usr-carol', 'iss', 'c', 'Disabled'), ('usr-dave', 'iss', 'd', 'Active'),
  ('usr-erin', 'iss', 'e', 'Active');
INSERT INTO organizations (id, slug, display_name, owner_user_id, status, review_note, created_at) VALUES
  ('org-acme', 'acme', 'ACME', 'usr-alice', 'Active', '', now()),
  ('org-gone', 'gone', 'Gone', 'usr-alice', 'Deleted', '', now()),
  ('org-beta1', 'beta', 'Beta', 'usr-bob', 'Rejected', 'no reason given', now() - interval '1 day'),
  ('org-beta2', 'beta', 'Beta', 'usr-bob', 'Pending', '', now()),
  ('org-old', 'oldco', 'Old Co', 'usr-erin', 'Rejected', 'duplicate', now());
INSERT INTO organization_members (org_id, user_id, role) VALUES
  ('org-acme', 'usr-alice', 'OrgOwner'), ('org-acme', 'usr-bob', 'OrgMember'), ('org-acme', 'usr-erin', 'OrgAdmin'),
  ('org-gone', 'usr-alice', 'OrgOwner'),
  ('org-beta1', 'usr-bob', 'OrgOwner'), ('org-beta2', 'usr-bob', 'OrgOwner'),
  ('org-old', 'usr-erin', 'OrgOwner');
` + loginEveryone

// loginEveryone gives every user without one the session sessionOf names.
const loginEveryone = `
INSERT INTO sessions (id_hash, user_id, refresh_token_enc, access_expires_at, expires_at)
SELECT sha256(sha256(convert_to('session:' || id, 'UTF8'))), id, '\x00', now() + interval '5 minutes', now() + interval '8 hours'
FROM users
ON CONFLICT DO NOTHING;
`

// sessionOf is the cookie value of the session the seed gives user.
func sessionOf(user string) string {
	h := sha256.Sum256([]byte("session:" + user))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// hashOf is the sessions.id_hash of a cookie value.
func hashOf(t *testing.T, token string) []byte {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(b)
	return h[:]
}

// newSession logs user in with the given realm roles and returns the cookie value.
func newSession(t *testing.T, db *pgxpool.Pool, user string, roles ...string) string {
	t.Helper()
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	h := sha256.Sum256(b)
	if roles == nil {
		roles = []string{}
	}
	if _, err := db.Exec(context.Background(), `
INSERT INTO sessions (id_hash, user_id, realm_roles, refresh_token_enc, access_expires_at, expires_at)
VALUES ($1, $2, $3, '\x00', now() + interval '5 minutes', now() + interval '8 hours')`, h[:], user, roles); err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// testKey is the session key of every test API.
var testKey = bytes.Repeat([]byte{7}, 32)

const publicURL = "https://portal.test"

// trustedProxies stands in for Traefik's address range.
var trustedProxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}

// newAPI serves the API on a fresh, migrated and seeded database; the pool is
// returned for tests that need extra rows. Its OIDC issuer does not exist:
// only tests that log in through Keycloak need one (newAPIWithIssuer).
func newAPI(t *testing.T) (http.Handler, *pgxpool.Pool) {
	t.Helper()
	return newAPIWithIssuer(t, "http://127.0.0.1:1/realms/none")
}

func newAPIWithIssuer(t *testing.T, issuer string) (http.Handler, *pgxpool.Pool) {
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

	pool := testdb.Pool(t, url)
	a, err := auth.New(pool, config.Auth{
		Issuer: issuer, ClientID: "portal", ClientSecret: "portal-secret", PublicURL: publicURL, SessionKey: testKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	httpapi.Register(r, pool, a, trustedProxies)
	return httpapi.WithRequestID(r), pool
}

func call(h http.Handler, method, path, user string) *httptest.ResponseRecorder {
	return callBody(h, method, path, user, "")
}

// callBody sends a request as user (the seed's session; "" for anonymous),
// with the CSRF cookie and header a portal page would send.
func callBody(h http.Handler, method, path, user, body string) *httptest.ResponseRecorder {
	token := ""
	if user != "" {
		token = sessionOf(user)
	}
	return send(h, request(method, path, token, body))
}

// request builds a portal request carrying session token (if any) and the
// CSRF double submit.
func request(method, path, token, body string) *http.Request {
	req := bareRequest(method, path, token, body)
	req.AddCookie(&http.Cookie{Name: httpapi.CSRFCookie, Value: "csrf-1"})
	req.Header.Set(httpapi.CSRFHeader, "csrf-1")
	return req
}

// bareRequest is request without the CSRF cookie and header.
func bareRequest(method, path, token, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: token})
	}
	return req
}

func send(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestGetOrganization(t *testing.T) {
	h, _ := newAPI(t)

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

func TestPatchOrganization(t *testing.T) {
	h, _ := newAPI(t)

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

// errorBody decodes an ErrorResponse and checks the fields every error carries.
func errorBody(t *testing.T, rec *httptest.ResponseRecorder) oapi.ErrorResponse {
	t.Helper()
	var e oapi.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("%v: %s", err, rec.Body)
	}
	if e.RequestId == "" || strings.Contains(e.Message, "Schema:") {
		t.Fatalf("malformed error body: %+v", e)
	}
	return e
}

func TestListOrganizations(t *testing.T) {
	h, _ := newAPI(t)

	cases := []struct {
		name, user, query string
		status            int
		wantIDs           []string // newest first
		wantNext          string
	}{
		{"deleted org left out", "usr-alice", "", 200, []string{"org-acme"}, ""},
		{"own pending and rejected requests included", "usr-bob", "", 200, []string{"org-beta2", "org-beta1", "org-acme"}, ""},
		{"first page", "usr-bob", "?limit=2", 200, []string{"org-beta2", "org-beta1"}, "org-beta1"},
		{"last page", "usr-bob", "?limit=2&cursor=org-beta1", 200, []string{"org-acme"}, ""},
		{"page exactly full has no next", "usr-bob", "?limit=3", 200, []string{"org-beta2", "org-beta1", "org-acme"}, ""},
		{"member of nothing", "usr-dave", "", 200, []string{}, ""},
		{"limit 0", "usr-bob", "?limit=0", 400, nil, ""},
		{"limit over 200", "usr-bob", "?limit=201", 400, nil, ""},
		{"limit not a number", "usr-bob", "?limit=ten", 400, nil, ""},
		{"no login", "", "", 401, nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := call(h, http.MethodGet, "/api/v1/organizations"+c.query, c.user)
			if rec.Code != c.status {
				t.Fatalf("status %d, want %d: %s", rec.Code, c.status, rec.Body)
			}
			if c.status != 200 {
				errorBody(t, rec)
				return
			}
			if !strings.Contains(rec.Body.String(), `"items":[`) {
				t.Fatalf("items must be an array, even when empty: %s", rec.Body)
			}
			var l oapi.OrganizationList
			if err := json.Unmarshal(rec.Body.Bytes(), &l); err != nil {
				t.Fatal(err)
			}
			ids := []string{}
			for _, o := range l.Items {
				ids = append(ids, o.Id)
			}
			next := ""
			if l.NextCursor != nil {
				next = *l.NextCursor
			}
			if strings.Join(ids, ",") != strings.Join(c.wantIDs, ",") || next != c.wantNext {
				t.Fatalf("got %v next=%q, want %v next=%q", ids, next, c.wantIDs, c.wantNext)
			}
		})
	}
}

func TestCreateOrganization(t *testing.T) {
	h, _ := newAPI(t)
	const path = "/api/v1/organizations"

	// The happy path, checked in full.
	rec := callBody(h, http.MethodPost, path, "usr-dave", `{"slug":"daveco","displayName":"  Dave Co  "}`)
	if rec.Code != 201 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var o oapi.Organization
	if err := json.Unmarshal(rec.Body.Bytes(), &o); err != nil {
		t.Fatal(err)
	}
	if len(o.Id) != 30 || !strings.HasPrefix(o.Id, "org-") || o.Slug != "daveco" || o.DisplayName != "Dave Co" ||
		o.Description != "" || o.Status != "Pending" || o.OwnerUserId != "usr-dave" || o.MyRole == nil || *o.MyRole != "OrgOwner" {
		t.Fatalf("unexpected organization: %+v", o)
	}
	if loc := rec.Header().Get("Location"); loc != path+"/"+o.Id {
		t.Fatalf("Location %q", loc)
	}
	if rec := call(h, http.MethodGet, path+"/daveco", "usr-dave"); rec.Code != 200 {
		t.Fatalf("new request not readable by its owner: %d %s", rec.Code, rec.Body)
	}

	long := strings.Repeat("a", 501)
	cases := []struct {
		name, user, body string
		status           int
		wantCode         oapi.ErrorCode
		wantField        string
	}{
		{"one pending request per user", "usr-dave", `{"slug":"davetwo","displayName":"Dave Two"}`, 409, oapi.ORGREQUESTPENDING, ""},
		{"slug of an active org", "usr-alice", `{"slug":"acme","displayName":"x"}`, 409, oapi.SLUGTAKEN, "slug"},
		{"slug of a deleted org stays taken", "usr-alice", `{"slug":"gone","displayName":"x"}`, 409, oapi.SLUGTAKEN, "slug"},
		{"slug of a pending request", "usr-alice", `{"slug":"daveco","displayName":"x"}`, 409, oapi.SLUGTAKEN, "slug"},
		{"reserved slug", "usr-alice", `{"slug":"admin","displayName":"x"}`, 400, oapi.VALIDATIONFAILED, "slug"},
		{"hyphen", "usr-alice", `{"slug":"alice-team","displayName":"x"}`, 400, oapi.VALIDATIONFAILED, "slug"},
		{"slug with uppercase", "usr-alice", `{"slug":"Acme2","displayName":"x"}`, 400, oapi.VALIDATIONFAILED, "slug"},
		{"slug too short", "usr-alice", `{"slug":"ab","displayName":"x"}`, 400, oapi.VALIDATIONFAILED, "slug"},
		{"blank display name", "usr-alice", `{"slug":"aliceco","displayName":"   "}`, 400, oapi.VALIDATIONFAILED, "displayName"},
		{"missing display name", "usr-alice", `{"slug":"aliceco"}`, 400, oapi.VALIDATIONFAILED, ""},
		{"description too long", "usr-alice", `{"slug":"aliceco","displayName":"x","description":"` + long + `"}`, 400, oapi.VALIDATIONFAILED, "description"},
		{"unknown field", "usr-alice", `{"slug":"aliceco","displayName":"x","status":"Active"}`, 400, oapi.VALIDATIONFAILED, ""},
		{"no login", "", `{"slug":"anonco","displayName":"x"}`, 401, oapi.UNAUTHENTICATED, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := callBody(h, http.MethodPost, path, c.user, c.body)
			if rec.Code != c.status {
				t.Fatalf("status %d, want %d: %s", rec.Code, c.status, rec.Body)
			}
			e := errorBody(t, rec)
			if e.Code != c.wantCode {
				t.Fatalf("code %s, want %s", e.Code, c.wantCode)
			}
			if c.wantField != "" && (e.Details == nil || (*e.Details)["field"] != c.wantField) {
				t.Fatalf("details %v, want field %q", e.Details, c.wantField)
			}
		})
	}

	// A rejected request gives its slug back, so erin can ask again under it.
	rec = callBody(h, http.MethodPost, path, "usr-erin", `{"slug":"oldco","displayName":"Old Co","description":"second try"}`)
	if rec.Code != 201 {
		t.Fatalf("re-request of a rejected slug: %d %s", rec.Code, rec.Body)
	}
}

// Concurrent requests must not slip past the rules; the database decides.
func TestCreateOrganizationRace(t *testing.T) {
	h, db := newAPI(t)
	const n = 10
	if _, err := db.Exec(context.Background(),
		`INSERT INTO users (id, issuer, subject) SELECT 'usr-r' || i, 'iss', 'r' || i FROM generate_series(1, $1) i`, n); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(context.Background(), loginEveryone); err != nil {
		t.Fatal(err)
	}

	race := func(user func(i int) string, body func(i int) string) map[int]int {
		codes := make(chan int, n)
		var wg sync.WaitGroup
		for i := 1; i <= n; i++ {
			wg.Go(func() {
				codes <- callBody(h, http.MethodPost, "/api/v1/organizations", user(i), body(i)).Code
			})
		}
		wg.Wait()
		close(codes)
		count := map[int]int{}
		for c := range codes {
			count[c]++
		}
		return count
	}

	// Ten users, one slug.
	got := race(func(i int) string { return fmt.Sprint("usr-r", i) },
		func(int) string { return `{"slug":"race","displayName":"Race"}` })
	if got[201] != 1 || got[409] != n-1 {
		t.Fatalf("same slug: %v, want one 201 and %d 409", got, n-1)
	}

	// One user, ten slugs. usr-r1..r10 may already own "race", so use alice.
	got = race(func(int) string { return "usr-alice" },
		func(i int) string { return fmt.Sprintf(`{"slug":"alice%d","displayName":"A"}`, i) })
	if got[201] != 1 || got[409] != n-1 {
		t.Fatalf("same user: %v, want one 201 and %d 409", got, n-1)
	}
}
