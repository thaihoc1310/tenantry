package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
)

// staffedOrg is activeOrg plus erin as OrgAdmin and bob in developers.
func staffedOrg(t *testing.T) (http.Handler, *pgxpool.Pool, string, string) {
	t.Helper()
	h, db, orgID, owner := activeOrg(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `INSERT INTO organization_members (org_id, user_id, role) VALUES ($1, 'usr-erin', 'OrgAdmin')`, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO group_members (group_id, user_id) VALUES ($1, 'usr-bob')`, groupID(t, db, orgID, "developers")); err != nil {
		t.Fatal(err)
	}
	return h, db, orgID, owner
}

func role(t *testing.T, db *pgxpool.Pool, orgID, user string) string {
	t.Helper()
	var r string
	if err := db.QueryRow(context.Background(), `SELECT coalesce((SELECT role FROM organization_members WHERE org_id = $1 AND user_id = $2), '')`, orgID, user).Scan(&r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestListMembers(t *testing.T) {
	h, db, orgID, owner := staffedOrg(t)
	rec := send(h, request(http.MethodGet, "/api/v1/organizations/zeta/members", owner, ""))
	var l oapi.MemberList
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &l) != nil {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	got := map[string]oapi.Member{}
	for _, m := range l.Items {
		got[m.UserId] = m
	}
	if len(got) != 3 || got["usr-zeta"].Role != oapi.OrgRoleOrgOwner || got["usr-erin"].Role != oapi.OrgRoleOrgAdmin ||
		len(got["usr-bob"].Groups) != 1 || got["usr-bob"].Groups[0].Name != "developers" || len(got["usr-zeta"].Groups) != 1 {
		t.Fatalf("members %+v", l.Items)
	}
	// Pages by user id.
	rec = send(h, request(http.MethodGet, "/api/v1/organizations/zeta/members?limit=2", owner, ""))
	_ = json.Unmarshal(rec.Body.Bytes(), &l)
	if len(l.Items) != 2 || l.NextCursor == nil {
		t.Fatalf("first page %s", rec.Body)
	}

	pending := pendingRequest(t, db, "other")
	for name, c := range map[string]struct {
		token, org string
		status     int
	}{
		"OrgMember":                 {sessionOf("usr-bob"), orgID, 403},
		"not a member":              {sessionOf("usr-dave"), orgID, 404},
		"organization not approved": {newSession(t, db, "usr-other"), pending, 409},
	} {
		if rec := send(h, request(http.MethodGet, "/api/v1/organizations/"+c.org+"/members", c.token, "")); rec.Code != c.status {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
}

func TestPatchMember(t *testing.T) {
	h, db, orgID, owner := staffedOrg(t)
	patch := func(token, user, body string) (int, oapi.Member) {
		rec := send(h, request(http.MethodPatch, "/api/v1/organizations/zeta/members/"+user, token, body))
		var m oapi.Member
		_ = json.Unmarshal(rec.Body.Bytes(), &m)
		return rec.Code, m
	}
	if code, m := patch(owner, "usr-bob", `{"role":"OrgAdmin"}`); code != 200 || m.Role != oapi.OrgRoleOrgAdmin || len(m.Groups) != 1 {
		t.Fatalf("promote: %d %+v", code, m)
	}
	if code, _ := patch(sessionOf("usr-erin"), "usr-bob", `{"role":"OrgMember"}`); code != 200 || role(t, db, orgID, "usr-bob") != "OrgMember" {
		t.Fatalf("an OrgAdmin demotes: %d", code)
	}
	if code, _ := patch(owner, "usr-bob", `{"role":"OrgMember"}`); code != 200 {
		t.Fatalf("same role: %d", code)
	}
	if n := count(t, db, `SELECT count(*) FROM audit_events WHERE action = 'organization_member.role_change'`); n != 2 {
		t.Fatalf("%d role changes audited, want 2 (the no-op is not)", n)
	}
	for name, c := range map[string]struct {
		token, user, body string
		status            int
	}{
		"the owner's role, by an OrgAdmin": {sessionOf("usr-erin"), "usr-zeta", `{"role":"OrgMember"}`, 403},
		"the owner's own role":             {owner, "usr-zeta", `{"role":"OrgAdmin"}`, 403},
		"by an OrgMember":                  {sessionOf("usr-bob"), "usr-erin", `{"role":"OrgMember"}`, 403},
		"make someone owner":               {owner, "usr-bob", `{"role":"OrgOwner"}`, 400},
		"not a member":                     {owner, "usr-dave", `{"role":"OrgAdmin"}`, 404},
	} {
		if code, _ := patch(c.token, c.user, c.body); code != c.status {
			t.Errorf("%s: %d, want %d", name, code, c.status)
		}
	}
	if role(t, db, orgID, "usr-zeta") != "OrgOwner" {
		t.Fatal("the owner's role changed")
	}
}

// Removing a member ends portal access at once and turns their group
// memberships into tombstones for the sync worker; being added back revives them.
func TestRemoveMember(t *testing.T) {
	h, db, orgID, owner := staffedOrg(t)
	dev := groupID(t, db, orgID, "developers")
	if rec := call(h, http.MethodGet, "/api/v1/organizations/zeta", "usr-bob"); rec.Code != 200 {
		t.Fatalf("bob before: %d", rec.Code)
	}
	if rec := send(h, request(http.MethodDelete, "/api/v1/organizations/zeta/members/usr-bob", sessionOf("usr-erin"), "")); rec.Code != 204 {
		t.Fatalf("remove: %d %s", rec.Code, rec.Body)
	}
	if rec := call(h, http.MethodGet, "/api/v1/organizations/zeta", "usr-bob"); rec.Code != 404 {
		t.Fatalf("bob still sees the organization: %d", rec.Code)
	}
	if n := count(t, db, `SELECT count(*) FROM group_members WHERE group_id = $1 AND user_id = 'usr-bob' AND deleted_at IS NOT NULL AND sync_status = 'Pending'`, dev); n != 1 {
		t.Fatal("the group membership is not a tombstone waiting for sync")
	}
	if n := count(t, db, `SELECT count(*) FROM audit_events WHERE action = 'organization_member.remove' AND target_id = 'usr-bob' AND actor_user_id = 'usr-erin'`); n != 1 {
		t.Fatal("removal not audited")
	}

	// Invited back into developers before the worker ran: the tombstone comes
	// back to life, and the sync converges on "is a member".
	_, token := invite(t, h, owner, orgID, newKey(), `{"email":"bob@example.com","role":"OrgMember","groupIds":["`+dev+`"]}`)
	if _, err := db.Exec(context.Background(), `UPDATE users SET email = 'bob@example.com' WHERE id = 'usr-bob'`); err != nil {
		t.Fatal(err)
	}
	if rec := call(h, http.MethodPost, "/api/v1/invitations/"+token+":accept", "usr-bob"); rec.Code != 200 {
		t.Fatalf("accept: %d %s", rec.Code, rec.Body)
	}
	if n := count(t, db, `SELECT count(*) FROM group_members WHERE group_id = $1 AND user_id = 'usr-bob' AND deleted_at IS NULL AND sync_status = 'Pending'`, dev); n != 1 {
		t.Fatal("the tombstone did not come back to life")
	}

	for name, c := range map[string]struct {
		token, user string
		status      int
	}{
		"the owner":       {sessionOf("usr-erin"), "usr-zeta", 403},
		"by an OrgMember": {sessionOf("usr-bob"), "usr-erin", 403},
		"not a member":    {owner, "usr-dave", 404},
	} {
		if rec := send(h, request(http.MethodDelete, "/api/v1/organizations/zeta/members/"+c.user, c.token, "")); rec.Code != c.status {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
}

func TestLeaveOrganization(t *testing.T) {
	h, db, orgID, owner := staffedOrg(t)
	if rec := call(h, http.MethodPost, "/api/v1/organizations/zeta/members/me:leave", "usr-bob"); rec.Code != 204 {
		t.Fatalf("leave: %d %s", rec.Code, rec.Body)
	}
	if role(t, db, orgID, "usr-bob") != "" || count(t, db, `SELECT count(*) FROM group_members WHERE user_id = 'usr-bob' AND deleted_at IS NULL`) != 0 {
		t.Fatal("bob is still a member or in a group")
	}
	rec := send(h, request(http.MethodPost, "/api/v1/organizations/zeta/members/me:leave", owner, ""))
	if rec.Code != 409 || errorBody(t, rec).Code != oapi.LEAVENOTALLOWED {
		t.Fatalf("owner leaves: %d %s", rec.Code, rec.Body)
	}
	if rec := call(h, http.MethodPost, "/api/v1/organizations/zeta/members/me:leave", "usr-bob"); rec.Code != 404 {
		t.Fatalf("leave twice: %d", rec.Code)
	}
}

func propose(h http.Handler, token, to string) (int, oapi.OwnershipTransfer) {
	rec := send(h, request(http.MethodPost, "/api/v1/organizations/zeta:transfer-ownership", token, `{"toUserId":"`+to+`"}`))
	var tr oapi.OwnershipTransfer
	_ = json.Unmarshal(rec.Body.Bytes(), &tr)
	return rec.Code, tr
}

func TestTransferOwnership(t *testing.T) {
	h, db, orgID, owner := staffedOrg(t)
	code, tr := propose(h, owner, "usr-erin")
	if code != 201 || tr.From.Id != "usr-zeta" || tr.To.Id != "usr-erin" || tr.State != oapi.Proposed {
		t.Fatalf("propose: %d %+v", code, tr)
	}
	for user, want := range map[string]int{"usr-erin": 200, "usr-bob": 404, "usr-dave": 404} {
		if rec := call(h, http.MethodGet, "/api/v1/organizations/zeta/ownership-transfer", user); rec.Code != want {
			t.Errorf("%s sees the transfer: %d, want %d", user, rec.Code, want)
		}
	}
	if rec := send(h, request(http.MethodGet, "/api/v1/organizations/zeta/ownership-transfer", owner, "")); rec.Code != 200 {
		t.Fatalf("owner sees the transfer: %d", rec.Code)
	}

	// Someone else cannot accept it.
	if rec := call(h, http.MethodPost, "/api/v1/ownership-transfers/"+tr.Id+":accept", "usr-bob"); rec.Code != 403 || errorBody(t, rec).Code != oapi.TRANSFERINVALID {
		t.Fatalf("bob accepts: %d %s", rec.Code, rec.Body)
	}
	rec := call(h, http.MethodPost, "/api/v1/ownership-transfers/"+tr.Id+":accept", "usr-erin")
	var o oapi.Organization
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &o) != nil || o.MyRole == nil || *o.MyRole != oapi.OrgRoleOrgOwner || o.OwnerUserId != "usr-erin" {
		t.Fatalf("accept: %d %s", rec.Code, rec.Body)
	}
	if role(t, db, orgID, "usr-zeta") != "OrgAdmin" || role(t, db, orgID, "usr-erin") != "OrgOwner" {
		t.Fatal("roles not swapped")
	}
	if n := count(t, db, `SELECT count(*) FROM audit_events WHERE action IN ('ownership_transfer.propose', 'ownership_transfer.accept')`); n != 2 {
		t.Fatalf("%d transfer events audited", n)
	}
	// The old owner is an OrgAdmin now: no more transfers, and the transfer is done.
	if code, _ := propose(h, owner, "usr-erin"); code != 403 {
		t.Fatalf("old owner proposes: %d", code)
	}
	if rec := call(h, http.MethodPost, "/api/v1/ownership-transfers/"+tr.Id+":accept", "usr-erin"); rec.Code != 403 {
		t.Fatalf("accept twice: %d", rec.Code)
	}
}

func TestTransferRefusals(t *testing.T) {
	h, db, orgID, owner := staffedOrg(t)
	cases := []struct {
		name, token, to string
		status          int
		code            oapi.ErrorCode
	}{
		{"by an OrgAdmin", sessionOf("usr-erin"), "usr-erin", 403, oapi.TRANSFERNOTALLOWED},
		{"to an OrgMember", owner, "usr-bob", 422, oapi.RECIPIENTNOTORGADMIN},
		{"to a non-member", owner, "usr-dave", 422, oapi.RECIPIENTNOTORGADMIN},
		{"to oneself", owner, "usr-zeta", 422, oapi.RECIPIENTNOTORGADMIN},
		{"by a non-member", sessionOf("usr-dave"), "usr-erin", 404, oapi.NOTFOUND},
	}
	for _, c := range cases {
		rec := send(h, request(http.MethodPost, "/api/v1/organizations/zeta:transfer-ownership", c.token, `{"toUserId":"`+c.to+`"}`))
		if rec.Code != c.status || errorBody(t, rec).Code != c.code {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body)
		}
	}

	// A recipient who stops being an OrgAdmin can no longer accept.
	_, tr := propose(h, owner, "usr-erin")
	send(h, request(http.MethodPatch, "/api/v1/organizations/zeta/members/usr-erin", owner, `{"role":"OrgMember"}`))
	if rec := call(h, http.MethodPost, "/api/v1/ownership-transfers/"+tr.Id+":accept", "usr-erin"); rec.Code != 403 {
		t.Fatalf("demoted recipient accepts: %d", rec.Code)
	}
	if n := count(t, db, `SELECT count(*) FROM ownership_transfers WHERE id = $1 AND state = 'Cancelled'`, tr.Id); n != 1 {
		t.Fatal("transfer to a demoted recipient not cancelled")
	}

	// A new offer replaces the one waiting.
	send(h, request(http.MethodPatch, "/api/v1/organizations/zeta/members/usr-erin", owner, `{"role":"OrgAdmin"}`))
	send(h, request(http.MethodPatch, "/api/v1/organizations/zeta/members/usr-bob", owner, `{"role":"OrgAdmin"}`))
	_, first := propose(h, owner, "usr-erin")
	_, second := propose(h, owner, "usr-bob")
	if rec := call(h, http.MethodPost, "/api/v1/ownership-transfers/"+first.Id+":accept", "usr-erin"); rec.Code != 403 {
		t.Fatalf("replaced offer accepted: %d", rec.Code)
	}

	// An expired offer cannot be accepted and is no longer shown.
	if _, err := db.Exec(context.Background(), `UPDATE ownership_transfers SET expires_at = now() - interval '1 second' WHERE id = $1`, second.Id); err != nil {
		t.Fatal(err)
	}
	if rec := call(h, http.MethodGet, "/api/v1/organizations/zeta/ownership-transfer", "usr-bob"); rec.Code != 404 {
		t.Fatalf("expired offer shown: %d", rec.Code)
	}
	if rec := call(h, http.MethodPost, "/api/v1/ownership-transfers/"+second.Id+":accept", "usr-bob"); rec.Code != 403 {
		t.Fatalf("expired offer accepted: %d", rec.Code)
	}
	if n := count(t, db, `SELECT count(*) FROM ownership_transfers WHERE id = $1 AND state = 'Expired'`, second.Id); n != 1 {
		t.Fatal("expired offer not marked Expired")
	}
	if role(t, db, orgID, "usr-zeta") != "OrgOwner" {
		t.Fatal("ownership moved")
	}
	if rec := call(h, http.MethodPost, "/api/v1/ownership-transfers/transfer-nope:accept", "usr-bob"); rec.Code != 404 {
		t.Fatalf("unknown transfer: %d", rec.Code)
	}
}

func TestCancelTransfer(t *testing.T) {
	h, db, _, owner := staffedOrg(t)
	cancel := func(token, id string) int {
		return send(h, request(http.MethodDelete, "/api/v1/ownership-transfers/"+id, token, "")).Code
	}
	_, tr := propose(h, owner, "usr-erin")
	if code := cancel(sessionOf("usr-bob"), tr.Id); code != 403 {
		t.Fatalf("bob cancels: %d", code)
	}
	if code := cancel(sessionOf("usr-dave"), tr.Id); code != 404 {
		t.Fatalf("outsider cancels: %d", code)
	}
	if code := cancel(owner, tr.Id); code != 204 {
		t.Fatalf("owner withdraws: %d", code)
	}
	if code := cancel(owner, tr.Id); code != 204 {
		t.Fatalf("withdraw twice: %d", code)
	}
	_, tr = propose(h, owner, "usr-erin")
	if code := cancel(sessionOf("usr-erin"), tr.Id); code != 204 {
		t.Fatalf("recipient declines: %d", code)
	}
	if n := count(t, db, `SELECT count(*) FROM audit_events WHERE action = 'ownership_transfer.cancel' AND detail->>'reason' = 'declined by the recipient'`); n != 1 {
		t.Fatal("decline not audited")
	}
	_, tr = propose(h, owner, "usr-erin")
	call(h, http.MethodPost, "/api/v1/ownership-transfers/"+tr.Id+":accept", "usr-erin")
	if code := cancel(sessionOf("usr-erin"), tr.Id); code != 409 {
		t.Fatalf("cancel an accepted transfer: %d", code)
	}
}

// The recipient accepting while leaving at the same moment: both lock the
// organization's row, so one goes first; whatever the order, the
// organization ends with exactly one owner, and it is a member.
func TestAcceptTransferWhileLeaving(t *testing.T) {
	for range 5 {
		h, db, orgID, owner := staffedOrg(t)
		_, tr := propose(h, owner, "usr-erin")
		var wg sync.WaitGroup
		wg.Go(func() { call(h, http.MethodPost, "/api/v1/ownership-transfers/"+tr.Id+":accept", "usr-erin") })
		wg.Go(func() { call(h, http.MethodPost, "/api/v1/organizations/zeta/members/me:leave", "usr-erin") })
		wg.Wait()
		if n := count(t, db, `SELECT count(*) FROM organization_members m JOIN organizations o ON o.id = m.org_id
			WHERE m.org_id = $1 AND m.role = 'OrgOwner' AND o.owner_user_id = m.user_id`, orgID); n != 1 {
			t.Fatalf("%d owners after accept and leave raced", n)
		}
	}
}
