package httpapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi"
	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
)

// activeOrg is an approved organization "zeta" owned by usr-zeta
// (usr-zeta@example.com), with its three default groups. dave and erin get
// an email; bob joins as OrgMember. It returns the org id and zeta's session.
func activeOrg(t *testing.T) (http.Handler, *pgxpool.Pool, string, string) {
	t.Helper()
	h, db, admin := newReviewAPI(t)
	orgID := pendingRequest(t, db, "zeta")
	if rec := asAdmin(h, admin, http.MethodPost, "/api/v1/admin/organizations/"+orgID+":approve", newKey(), proposalBody); rec.Code != 200 {
		t.Fatalf("approve: %d %s", rec.Code, rec.Body)
	}
	for _, q := range []string{
		`UPDATE users SET email = 'dave@example.com', display_name = 'Dave' WHERE id = 'usr-dave'`,
		`UPDATE users SET email = 'erin@example.com', display_name = 'Erin' WHERE id = 'usr-erin'`,
		`INSERT INTO organization_members (org_id, user_id, role) SELECT id, 'usr-bob', 'OrgMember' FROM organizations WHERE slug = 'zeta'`,
	} {
		if _, err := db.Exec(context.Background(), q); err != nil {
			t.Fatal(err)
		}
	}
	return h, db, orgID, newSession(t, db, "usr-zeta")
}

func groupID(t *testing.T, db *pgxpool.Pool, orgID, name string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(context.Background(), `SELECT id FROM groups WHERE org_id = $1 AND display_name = $2`, orgID, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// invite creates an invitation as token; it returns the response and the token from the link.
func invite(t *testing.T, h http.Handler, token, orgID, key, body string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	rec := asAdmin(h, token, http.MethodPost, "/api/v1/organizations/"+orgID+"/invitations", key, body)
	if rec.Code != 201 {
		return rec, ""
	}
	var inv oapi.Invitation
	if err := json.Unmarshal(rec.Body.Bytes(), &inv); err != nil || inv.Link == nil {
		t.Fatalf("created invitation without a link: %s", rec.Body)
	}
	return rec, strings.TrimPrefix(*inv.Link, publicURL+"/invitations/")
}

func TestInvitationLifecycle(t *testing.T) {
	h, db, orgID, owner := activeOrg(t)
	dev := groupID(t, db, orgID, "developers")
	key := newKey()
	body := `{"email":"  Dave@Example.com ","role":"OrgMember","groupIds":["` + dev + `"]}`

	rec, token := invite(t, h, owner, orgID, key, body)
	if rec.Code != 201 || len(token) != 43 {
		t.Fatalf("create: %d %s (token %q)", rec.Code, rec.Body, token)
	}
	var created oapi.Invitation
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.Email != "dave@example.com" || created.Role != oapi.InvitationRoleOrgMember || created.State != oapi.InvitationStatePending ||
		created.InvitedBy.Id != "usr-zeta" || len(created.GroupIds) != 1 || created.GroupIds[0] != dev {
		t.Fatalf("created %+v", created)
	}

	// The token exists only in that response: the table keeps its sha256.
	raw, _ := base64.RawURLEncoding.DecodeString(token)
	sum := sha256.Sum256(raw)
	if n := count(t, db, `SELECT count(*) FROM invitations WHERE id = $1 AND token_hash = $2`, created.Id, sum[:]); n != 1 {
		t.Fatal("the stored hash is not the token's")
	}
	// A repeat gets the invitation without its link, and the stored response never held it.
	again := asAdmin(h, owner, http.MethodPost, "/api/v1/organizations/"+orgID+"/invitations", key, body)
	if again.Code != 201 || again.Header().Get(httpapi.ReplayedHeader) != "true" || strings.Contains(again.Body.String(), "link") ||
		!strings.Contains(again.Body.String(), created.Id) {
		t.Fatalf("replay: %d %s", again.Code, again.Body)
	}
	if n := count(t, db, `SELECT count(*) FROM idempotency_keys WHERE response_body::text LIKE '%' || $1 || '%'`, token); n != 0 {
		t.Fatal("the token was stored with the idempotency key")
	}
	list := send(h, request(http.MethodGet, "/api/v1/organizations/"+orgID+"/invitations", owner, ""))
	var l oapi.InvitationList
	if list.Code != 200 || json.Unmarshal(list.Body.Bytes(), &l) != nil || len(l.Items) != 1 || l.Items[0].Link != nil {
		t.Fatalf("list: %d %s", list.Code, list.Body)
	}

	// The invited person sees the offer; someone else sees the email hidden.
	preview := func(user string) (int, oapi.InvitationPreview) {
		rec := call(h, http.MethodGet, "/api/v1/invitations/"+token, user)
		var p oapi.InvitationPreview
		_ = json.Unmarshal(rec.Body.Bytes(), &p)
		return rec.Code, p
	}
	if code, p := preview("usr-dave"); code != 200 || p.OrgSlug != "zeta" || !p.EmailMatches || p.Email != "dave@example.com" ||
		p.InvitedBy != "Requester usr-zeta" || len(p.GroupNames) != 1 || p.GroupNames[0] != "developers" || p.AlreadyMember {
		t.Fatalf("preview as dave: %d %+v", code, p)
	}
	if code, p := preview("usr-erin"); code != 200 || p.EmailMatches || p.Email != "d•••@example.com" {
		t.Fatalf("preview as erin: %d %+v", code, p)
	}

	// The wrong account is refused, the link keeps working, the refusal is audited.
	if rec := call(h, http.MethodPost, "/api/v1/invitations/"+token+":accept", "usr-erin"); rec.Code != 403 ||
		errorBody(t, rec).Code != oapi.INVITATIONEMAILMISMATCH {
		t.Fatalf("accept as erin: %d %s", rec.Code, rec.Body)
	}
	if n := count(t, db, `SELECT count(*) FROM audit_events WHERE action = 'invitation.accept' AND result = 'Refused' AND actor_user_id = 'usr-erin'`); n != 1 {
		t.Fatal("refused accept not audited")
	}

	rec = call(h, http.MethodPost, "/api/v1/invitations/"+token+":accept", "usr-dave")
	var joined oapi.Organization
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &joined) != nil || joined.Id != orgID || joined.MyRole == nil || *joined.MyRole != oapi.OrgRoleOrgMember {
		t.Fatalf("accept as dave: %d %s", rec.Code, rec.Body)
	}
	if n := count(t, db, `SELECT count(*) FROM group_members WHERE group_id = $1 AND user_id = 'usr-dave' AND sync_status = 'Pending' AND deleted_at IS NULL`, dev); n != 1 {
		t.Fatal("dave did not join the invitation's group")
	}
	if n := count(t, db, `SELECT count(*) FROM invitations WHERE id = $1 AND state = 'Accepted' AND accepted_by = 'usr-dave'`, created.Id); n != 1 {
		t.Fatal("invitation not marked accepted")
	}
	if n := count(t, db, `SELECT count(*) FROM audit_events WHERE action = 'invitation.accept' AND result = 'Succeeded'`); n != 1 {
		t.Fatal("accept not audited")
	}
	// Once only.
	for _, user := range []string{"usr-dave", "usr-erin"} {
		if rec := call(h, http.MethodPost, "/api/v1/invitations/"+token+":accept", user); rec.Code != 404 || errorBody(t, rec).Code != oapi.INVITATIONINVALID {
			t.Fatalf("second accept by %s: %d %s", user, rec.Code, rec.Body)
		}
	}
	if code, _ := preview("usr-dave"); code != 404 {
		t.Fatalf("preview of a used link: %d", code)
	}
}

func TestInvitationRefusals(t *testing.T) {
	h, db, orgID, owner := activeOrg(t)
	other := pendingRequest(t, db, "other")
	cases := []struct {
		name, token, org, key, body string
		status                      int
		code                        oapi.ErrorCode
		field                       string
	}{
		{"OrgMember", sessionOf("usr-bob"), orgID, newKey(), `{"email":"x@example.com","role":"OrgMember"}`, 403, oapi.FORBIDDEN, ""},
		{"not a member", sessionOf("usr-erin"), orgID, newKey(), `{"email":"x@example.com","role":"OrgMember"}`, 404, oapi.NOTFOUND, ""},
		{"organization not approved", newSession(t, db, "usr-other"), other, newKey(), `{"email":"x@example.com","role":"OrgMember"}`, 409, oapi.ORGNOTACTIVE, ""},
		{"invite as owner", owner, orgID, newKey(), `{"email":"x@example.com","role":"OrgOwner"}`, 400, oapi.VALIDATIONFAILED, ""},
		{"not an email", owner, orgID, newKey(), `{"email":"not an email","role":"OrgMember"}`, 400, oapi.VALIDATIONFAILED, "email"},
		{"two addresses", owner, orgID, newKey(), `{"email":"a@example.com, b@example.com","role":"OrgMember"}`, 400, oapi.VALIDATIONFAILED, "email"},
		{"group of another organization", owner, orgID, newKey(), `{"email":"x@example.com","role":"OrgMember","groupIds":["grp-nope"]}`, 400, oapi.VALIDATIONFAILED, "groupIds"},
		{"already a member", owner, orgID, newKey(), `{"email":"USR-ZETA@example.com","role":"OrgAdmin"}`, 409, oapi.CONFLICT, ""},
		{"no Idempotency-Key", owner, orgID, "", `{"email":"x@example.com","role":"OrgMember"}`, 400, oapi.VALIDATIONFAILED, ""},
		{"unknown field", owner, orgID, newKey(), `{"email":"x@example.com","role":"OrgMember","expiresAt":"2030-01-01T00:00:00Z"}`, 400, oapi.VALIDATIONFAILED, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := asAdmin(h, c.token, http.MethodPost, "/api/v1/organizations/"+c.org+"/invitations", c.key, c.body)
			if rec.Code != c.status {
				t.Fatalf("status %d, want %d: %s", rec.Code, c.status, rec.Body)
			}
			e := errorBody(t, rec)
			if e.Code != c.code || (c.field != "" && (e.Details == nil || (*e.Details)["field"] != c.field)) {
				t.Fatalf("got %s %v, want %s field %q", e.Code, e.Details, c.code, c.field)
			}
		})
	}
	if n := count(t, db, `SELECT count(*) FROM invitations`); n != 0 {
		t.Fatalf("%d invitations after refusals", n)
	}
}

// Inviting an email again replaces its open invitation; only the new link works.
func TestInviteAgainReplacesTheOpenInvitation(t *testing.T) {
	h, db, orgID, owner := activeOrg(t)
	_, first := invite(t, h, owner, orgID, newKey(), `{"email":"dave@example.com","role":"OrgMember"}`)
	_, second := invite(t, h, owner, orgID, newKey(), `{"email":"DAVE@example.com","role":"OrgAdmin"}`)
	if rec := call(h, http.MethodGet, "/api/v1/invitations/"+first, "usr-dave"); rec.Code != 404 {
		t.Fatalf("replaced link still works: %d", rec.Code)
	}
	if n := count(t, db, `SELECT count(*) FROM invitations WHERE state = 'Revoked'`); n != 1 {
		t.Fatal("older invitation not revoked")
	}
	rec := call(h, http.MethodPost, "/api/v1/invitations/"+second+":accept", "usr-dave")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"myRole":"OrgAdmin"`) {
		t.Fatalf("accept the new link: %d %s", rec.Code, rec.Body)
	}
}

// maxMembers is checked when inviting and again when accepting: an
// invitation holds no place until it is accepted.
func TestInvitationsRespectMaxMembers(t *testing.T) {
	h, db, orgID, owner := activeOrg(t)
	// zeta and bob are members: room for one more.
	if _, err := db.Exec(context.Background(), `UPDATE organization_entitlements SET max_members = 3 WHERE org_id = $1`, orgID); err != nil {
		t.Fatal(err)
	}
	_, toDave := invite(t, h, owner, orgID, newKey(), `{"email":"dave@example.com","role":"OrgMember"}`)
	_, toErin := invite(t, h, owner, orgID, newKey(), `{"email":"erin@example.com","role":"OrgMember"}`)
	if toDave == "" || toErin == "" {
		t.Fatal("inviting within the limit failed")
	}
	if rec := call(h, http.MethodPost, "/api/v1/invitations/"+toDave+":accept", "usr-dave"); rec.Code != 200 {
		t.Fatalf("dave: %d %s", rec.Code, rec.Body)
	}
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"accept over the limit": call(h, http.MethodPost, "/api/v1/invitations/"+toErin+":accept", "usr-erin"),
		"invite over the limit": asAdmin(h, owner, http.MethodPost, "/api/v1/organizations/"+orgID+"/invitations", newKey(), `{"email":"x@example.com","role":"OrgMember"}`),
	} {
		e := errorBody(t, rec)
		if rec.Code != 409 || e.Code != oapi.ENTITLEMENTEXCEEDED || e.Details == nil || (*e.Details)["limit"] != "maxMembers" {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	if n := count(t, db, `SELECT count(*) FROM organization_members WHERE org_id = $1`, orgID); n != 3 {
		t.Fatalf("%d members, want 3", n)
	}
}

// Two accepts of one link at the same moment: one joins, the other finds it used.
func TestAcceptConcurrently(t *testing.T) {
	h, db, orgID, owner := activeOrg(t)
	_, token := invite(t, h, owner, orgID, newKey(), `{"email":"dave@example.com","role":"OrgMember"}`)
	tabs := []string{newSession(t, db, "usr-dave"), newSession(t, db, "usr-dave"), newSession(t, db, "usr-dave")}
	codes := make([]int, len(tabs))
	var wg sync.WaitGroup
	for i, tab := range tabs {
		wg.Go(func() {
			codes[i] = send(h, request(http.MethodPost, "/api/v1/invitations/"+token+":accept", tab, "")).Code
		})
	}
	wg.Wait()
	ok := 0
	for _, c := range codes {
		switch c {
		case 200:
			ok++
		case 404:
		default:
			t.Fatalf("statuses %v", codes)
		}
	}
	if ok != 1 {
		t.Fatalf("statuses %v, want exactly one 200", codes)
	}
	if n := count(t, db, `SELECT count(*) FROM organization_members WHERE org_id = $1 AND user_id = 'usr-dave'`, orgID); n != 1 {
		t.Fatalf("%d memberships", n)
	}
}

func TestExpiredInvitation(t *testing.T) {
	h, db, orgID, owner := activeOrg(t)
	_, token := invite(t, h, owner, orgID, newKey(), `{"email":"dave@example.com","role":"OrgMember"}`)
	if _, err := db.Exec(context.Background(), `UPDATE invitations SET expires_at = now() - interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	for _, rec := range []*httptest.ResponseRecorder{
		call(h, http.MethodGet, "/api/v1/invitations/"+token, "usr-dave"),
		call(h, http.MethodPost, "/api/v1/invitations/"+token+":accept", "usr-dave"),
	} {
		if rec.Code != 404 || errorBody(t, rec).Code != oapi.INVITATIONINVALID {
			t.Fatalf("expired link: %d %s", rec.Code, rec.Body)
		}
	}
	rec := send(h, request(http.MethodGet, "/api/v1/organizations/"+orgID+"/invitations", owner, ""))
	if !strings.Contains(rec.Body.String(), `"state":"Expired"`) {
		t.Fatalf("list shows %s", rec.Body)
	}
	if rec := call(h, http.MethodGet, "/api/v1/invitations/not-a-token", "usr-dave"); rec.Code != 404 {
		t.Fatalf("garbage token: %d", rec.Code)
	}
}

func TestRevokeInvitation(t *testing.T) {
	h, db, orgID, owner := activeOrg(t)
	rec, token := invite(t, h, owner, orgID, newKey(), `{"email":"dave@example.com","role":"OrgMember"}`)
	var inv oapi.Invitation
	_ = json.Unmarshal(rec.Body.Bytes(), &inv)
	path := "/api/v1/invitations/" + inv.Id

	for name, c := range map[string]struct {
		user   string
		status int
	}{"OrgMember": {"usr-bob", 403}, "not a member": {"usr-erin", 404}} {
		if rec := call(h, http.MethodDelete, path, c.user); rec.Code != c.status {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	for i := range 2 { // revoking twice changes nothing
		if rec := send(h, request(http.MethodDelete, path, owner, "")); rec.Code != 204 {
			t.Fatalf("revoke %d: %d %s", i, rec.Code, rec.Body)
		}
	}
	if rec := call(h, http.MethodPost, "/api/v1/invitations/"+token+":accept", "usr-dave"); rec.Code != 404 {
		t.Fatalf("revoked link accepted: %d", rec.Code)
	}
	if n := count(t, db, `SELECT count(*) FROM audit_events WHERE action = 'invitation.revoke'`); n != 1 {
		t.Fatalf("%d revocations audited", n)
	}
	if rec := send(h, request(http.MethodDelete, "/api/v1/invitations/inv-nope", owner, "")); rec.Code != 404 {
		t.Fatalf("unknown: %d", rec.Code)
	}

	// An accepted invitation cannot be revoked.
	rec, token = invite(t, h, owner, orgID, newKey(), `{"email":"erin@example.com","role":"OrgMember"}`)
	_ = json.Unmarshal(rec.Body.Bytes(), &inv)
	call(h, http.MethodPost, "/api/v1/invitations/"+token+":accept", "usr-erin")
	if rec := send(h, request(http.MethodDelete, "/api/v1/invitations/"+inv.Id, owner, "")); rec.Code != 409 {
		t.Fatalf("revoke accepted: %d %s", rec.Code, rec.Body)
	}
}

// The token never reaches the access log or the idempotency table: both keep
// the route pattern, not the path.
func TestInvitationTokenStaysOutOfLogs(t *testing.T) {
	h, db, orgID, owner := activeOrg(t)
	_, token := invite(t, h, owner, orgID, newKey(), `{"email":"dave@example.com","role":"OrgMember"}`)

	// The production chain (cmd/tenantry-api routes): the log middleware runs inside the router.
	var buf bytes.Buffer
	logged := chi.NewRouter()
	logged.Use(httpapi.WithRequestID, httpapi.LogRequests(slog.New(slog.NewJSONHandler(&buf, nil))), httpapi.Recover)
	logged.Mount("/", h)
	for _, req := range []*http.Request{
		request(http.MethodGet, "/api/v1/invitations/"+token, sessionOf("usr-dave"), ""),
		func() *http.Request {
			r := request(http.MethodPost, "/api/v1/invitations/"+token+":accept", sessionOf("usr-dave"), "")
			r.Header.Set(httpapi.IdempotencyHeader, newKey())
			return r
		}(),
	} {
		rec := httptest.NewRecorder()
		logged.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s %s: %d %s", req.Method, req.URL.Path, rec.Code, rec.Body)
		}
	}
	out, _ := io.ReadAll(&buf)
	if bytes.Contains(out, []byte(token)) || !bytes.Contains(out, []byte(`/api/v1/invitations/{invitation}`)) {
		t.Fatalf("access log: %s", out)
	}
	if n := count(t, db, `SELECT count(*) FROM idempotency_keys WHERE path LIKE '%' || $1 || '%'`, token); n != 0 {
		t.Fatal("the token is in the idempotency table")
	}
	if n := count(t, db, `SELECT count(*) FROM idempotency_keys WHERE path = '/api/v1/invitations/{token}:accept'`); n != 1 {
		t.Fatal("the accept's key does not keep the route pattern")
	}
}
