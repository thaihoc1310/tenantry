package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
)

// createBody is an organization for erin@example.com; ownerEmail and slug vary.
func createBody(slug, owner string) string {
	return `{"slug":"` + slug + `","displayName":"Kappa","description":"A customer","ownerEmail":"` + owner + `",` +
		strings.TrimPrefix(proposalBody, "{")
}

func TestCreateAdminOrganization(t *testing.T) {
	h, db, admin := newReviewAPI(t)
	if _, err := db.Exec(context.Background(), `UPDATE users SET email = 'erin@example.com', display_name = 'Erin' WHERE id = 'usr-erin'`); err != nil {
		t.Fatal(err)
	}
	key := newKey()
	rec := asAdmin(h, admin, http.MethodPost, "/api/v1/admin/organizations", key, createBody("kappa", " Erin@Example.com "))
	var got oapi.ApprovedOrganization
	if rec.Code != 201 || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	o := got.Organization
	if o.Status != "Active" || o.Slug != "kappa" || o.Owner.Id != "usr-erin" || got.ClusterGrant.ClusterId != "cluster-a" ||
		!got.ClusterGrant.IsDefault || got.Entitlement.MaxMembers != 20 {
		t.Fatalf("created %+v", got)
	}
	// Everything an approval gives: owner, default groups with the owner in admins, entitlement, audit.
	if role(t, db, o.Id, "usr-erin") != "OrgOwner" {
		t.Fatal("the owner is not OrgOwner")
	}
	if n := count(t, db, `SELECT count(*) FROM groups WHERE org_id = $1 AND sync_status = 'Pending'`, o.Id); n != 3 {
		t.Fatalf("%d default groups", n)
	}
	if n := count(t, db, `SELECT count(*) FROM group_members gm JOIN groups g ON g.id = gm.group_id
		WHERE g.org_id = $1 AND g.display_name = 'admins' AND gm.user_id = 'usr-erin'`, o.Id); n != 1 {
		t.Fatal("the owner is not in admins")
	}
	if n := count(t, db, `SELECT count(*) FROM audit_events WHERE action = 'organization.create' AND org_id = $1 AND actor_user_id = 'usr-dave'`, o.Id); n != 1 {
		t.Fatal("not audited")
	}
	// The owner sees it at once.
	if rec := call(h, http.MethodGet, "/api/v1/organizations/kappa", "usr-erin"); rec.Code != 200 {
		t.Fatalf("owner reads it: %d", rec.Code)
	}
	// A retry replays the answer instead of creating a second one.
	if rec := asAdmin(h, admin, http.MethodPost, "/api/v1/admin/organizations", key, createBody("kappa", " Erin@Example.com ")); rec.Code != 201 {
		t.Fatalf("replay: %d %s", rec.Code, rec.Body)
	}
	if n := count(t, db, `SELECT count(*) FROM organizations WHERE slug = 'kappa'`); n != 1 {
		t.Fatalf("%d organizations after a replay", n)
	}

	refusals := []struct {
		name, body string
		status     int
		code       oapi.ErrorCode
	}{
		{"owner never logged in", createBody("lambda", "nobody@example.com"), 422, oapi.OWNERUSERNOTFOUND},
		{"slug taken", createBody("kappa", "erin@example.com"), 409, oapi.SLUGTAKEN},
		{"slug with a hyphen", createBody("la-mbda", "erin@example.com"), 400, oapi.VALIDATIONFAILED},
		{"not an email", createBody("lambda", "erin"), 400, oapi.VALIDATIONFAILED},
		{"cluster in maintenance", strings.Replace(createBody("lambda", "erin@example.com"), `"clusterId":"cluster-a"`, `"clusterId":"cluster-b"`, 1), 409, oapi.CLUSTERNOTREADY},
		{"no network pool", strings.Replace(createBody("lambda", "erin@example.com"), `"networkPoolIds":["onboarding-pod-network"]`, `"networkPoolIds":[]`, 1), 400, oapi.VALIDATIONFAILED},
	}
	for _, c := range refusals {
		rec := asAdmin(h, admin, http.MethodPost, "/api/v1/admin/organizations", newKey(), c.body)
		if e := errorBody(t, rec); rec.Code != c.status || e.Code != c.code {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body)
		}
	}
	if n := count(t, db, `SELECT count(*) FROM organizations WHERE slug = 'lambda'`); n != 0 {
		t.Fatal("a refused creation left an organization")
	}
	// A disabled owner cannot be given an organization.
	if _, err := db.Exec(context.Background(), `UPDATE users SET email = 'carol@example.com' WHERE id = 'usr-carol'`); err != nil {
		t.Fatal(err)
	}
	if rec := asAdmin(h, admin, http.MethodPost, "/api/v1/admin/organizations", newKey(), createBody("lambda", "carol@example.com")); rec.Code != 400 {
		t.Fatalf("disabled owner: %d %s", rec.Code, rec.Body)
	}
}

func TestGetAdminOrganization(t *testing.T) {
	h, db, orgID, _ := activeOrg(t)
	admin := newSession(t, db, "usr-dave", "platform-admin")
	rec := asAdmin(h, admin, http.MethodGet, "/api/v1/admin/organizations/"+orgID, "", "")
	var d oapi.AdminOrganizationDetail
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &d) != nil {
		t.Fatalf("detail: %d %s", rec.Code, rec.Body)
	}
	if d.Entitlement == nil || d.Entitlement.MaxGroups != 10 || len(d.ClusterGrants) != 1 || d.Usage == nil ||
		d.Usage.Organization.Members.Used != 2 || d.Usage.Organization.Groups.Used != 3 || d.Usage.ClusterGrants[0].GrantId != d.ClusterGrants[0].Id {
		t.Fatalf("detail %+v", d)
	}
	// The same numbers as the organization's own /usage.
	owner := newSession(t, db, "usr-zeta")
	var own oapi.OrganizationUsage
	if rec := asAdmin(h, owner, http.MethodGet, "/api/v1/organizations/zeta/usage", "", ""); rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &own) != nil {
		t.Fatalf("own usage: %d", rec.Code)
	}
	a, _ := json.Marshal(own)
	b, _ := json.Marshal(*d.Usage)
	if string(a) != string(b) {
		t.Fatalf("usage differs:\nadmin %s\nowner %s", b, a)
	}

	pending := pendingRequest(t, db, "pending")
	rec = asAdmin(h, admin, http.MethodGet, "/api/v1/admin/organizations/"+pending, "", "")
	d = oapi.AdminOrganizationDetail{}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &d) != nil || d.Entitlement != nil || d.Usage != nil || len(d.ClusterGrants) != 0 {
		t.Fatalf("pending: %d %s", rec.Code, rec.Body)
	}
	if rec := asAdmin(h, admin, http.MethodGet, "/api/v1/admin/organizations/org-nope", "", ""); rec.Code != 404 {
		t.Fatalf("unknown: %d", rec.Code)
	}
}

func TestPutAdminEntitlement(t *testing.T) {
	h, db, orgID, _ := staffedOrg(t) // 3 members, 3 groups
	admin := newSession(t, db, "usr-dave", "platform-admin")
	put := func(body string) (int, oapi.ErrorResponse) {
		rec := asAdmin(h, admin, http.MethodPut, "/api/v1/admin/organizations/"+orgID+"/entitlement", "", body)
		if rec.Code == 200 {
			return 200, oapi.ErrorResponse{}
		}
		return rec.Code, errorBody(t, rec)
	}
	if code, _ := put(`{"maxMembers":3,"maxGroups":3,"maxWorkspacesTotal":0}`); code != 200 {
		t.Fatalf("down to usage: %d", code)
	}
	var trail string
	if err := db.QueryRow(context.Background(), `SELECT detail::text FROM audit_events WHERE action = 'organization_entitlement.update'`).Scan(&trail); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(trail, `"before": {"maxGroups": 10, "maxMembers": 20, "maxWorkspacesTotal": 8}`) ||
		!strings.Contains(trail, `"after": {"maxGroups": 3, "maxMembers": 3, "maxWorkspacesTotal": 0}`) {
		t.Fatalf("audit detail %s", trail)
	}
	for _, c := range []struct {
		body, limit string
		used        float64
	}{
		{`{"maxMembers":2,"maxGroups":10,"maxWorkspacesTotal":8}`, "maxMembers", 3},
		{`{"maxMembers":20,"maxGroups":2,"maxWorkspacesTotal":8}`, "maxGroups", 3},
	} {
		code, e := put(c.body)
		if code != 409 || e.Code != oapi.ENTITLEMENTBELOWUSAGE || (*e.Details)["limit"] != c.limit || (*e.Details)["used"] != c.used {
			t.Errorf("%s: %d %+v", c.body, code, e)
		}
	}
	// A fourth group counts.
	if code, _ := put(`{"maxMembers":20,"maxGroups":10,"maxWorkspacesTotal":8}`); code != 200 {
		t.Fatal(code)
	}
	if code, _ := createGroup(h, sessionOf("usr-erin"), "backend"); code != 201 {
		t.Fatal(code)
	}
	if code, e := put(`{"maxMembers":20,"maxGroups":3,"maxWorkspacesTotal":8}`); code != 409 || (*e.Details)["used"] != float64(4) {
		t.Fatalf("below four groups: %d %+v", code, e)
	}
	pending := pendingRequest(t, db, "pending")
	rec := asAdmin(h, admin, http.MethodPut, "/api/v1/admin/organizations/"+pending+"/entitlement", "", `{"maxMembers":20,"maxGroups":10,"maxWorkspacesTotal":8}`)
	if e := errorBody(t, rec); rec.Code != 409 || e.Code != oapi.ORGNOTACTIVE {
		t.Fatalf("pending: %d %s", rec.Code, rec.Body)
	}
}

func TestDisableEnableUser(t *testing.T) {
	h, db, admin := newReviewAPI(t)
	if _, err := db.Exec(context.Background(), `UPDATE users SET email = id || '@example.com', display_name = upper(id)`); err != nil {
		t.Fatal(err)
	}
	// Bob has a second session, on another device.
	other := newSession(t, db, "usr-bob")

	rec := asAdmin(h, admin, http.MethodPost, "/api/v1/admin/users/usr-bob:disable", "", "")
	var u oapi.AdminUser
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &u) != nil || u.Status != "Disabled" || u.SyncStatus != oapi.SyncStatusPending {
		t.Fatalf("disable: %d %s", rec.Code, rec.Body)
	}
	for _, s := range []string{sessionOf("usr-bob"), other} {
		if rec := asAdmin(h, s, http.MethodGet, "/api/v1/me", "", ""); rec.Code != 401 {
			t.Fatalf("a session of the disabled user still works: %d", rec.Code)
		}
	}
	if n := count(t, db, `SELECT count(*) FROM sessions WHERE user_id = 'usr-bob'`); n != 0 {
		t.Fatalf("%d sessions left", n)
	}
	if n := count(t, db, `SELECT count(*) FROM audit_events WHERE action = 'user.disable' AND target_id = 'usr-bob'`); n != 1 {
		t.Fatal("not audited")
	}
	// Twice changes nothing.
	if rec := asAdmin(h, admin, http.MethodPost, "/api/v1/admin/users/usr-bob:disable", "", ""); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	if n := count(t, db, `SELECT count(*) FROM audit_events WHERE action = 'user.disable'`); n != 1 {
		t.Fatalf("%d disable events", n)
	}

	rec = asAdmin(h, admin, http.MethodPost, "/api/v1/admin/users/usr-bob:enable", "", "")
	u = oapi.AdminUser{}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &u) != nil || u.Status != "Active" || u.SyncStatus != oapi.SyncStatusPending {
		t.Fatalf("enable: %d %s", rec.Code, rec.Body)
	}
	if rec := asAdmin(h, admin, http.MethodPost, "/api/v1/admin/users/usr-dave:disable", "", ""); rec.Code != 403 {
		t.Fatalf("disable oneself: %d", rec.Code)
	}
	if rec := asAdmin(h, admin, http.MethodPost, "/api/v1/admin/users/usr-nope:disable", "", ""); rec.Code != 404 {
		t.Fatalf("unknown user: %d", rec.Code)
	}
}

func TestListAdminUsers(t *testing.T) {
	h, db, admin := newReviewAPI(t)
	if _, err := db.Exec(context.Background(), `UPDATE users SET email = id || '@example.com', display_name = upper(id)`); err != nil {
		t.Fatal(err)
	}
	list := func(query string) oapi.AdminUserList {
		t.Helper()
		rec := asAdmin(h, admin, http.MethodGet, "/api/v1/admin/users"+query, "", "")
		var l oapi.AdminUserList
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &l) != nil {
			t.Fatalf("%s: %d %s", query, rec.Code, rec.Body)
		}
		return l
	}
	all := list("")
	if len(all.Items) != 5 || *all.Items[0].Email != "usr-alice@example.com" {
		t.Fatalf("all %+v", all.Items)
	}
	if l := list("?q=CAROL"); len(l.Items) != 1 || l.Items[0].Status != "Disabled" {
		t.Fatalf("q=CAROL %+v", l.Items)
	}
	if l := list("?q=%25"); len(l.Items) != 0 {
		t.Fatal("% matched as a wildcard")
	}
	// Pages follow each other without gaps or repeats.
	var seen []string
	cursor := ""
	for range 5 {
		q := "?limit=2"
		if cursor != "" {
			q += "&cursor=" + cursor
		}
		p := list(q)
		for _, u := range p.Items {
			seen = append(seen, u.Id)
		}
		if p.NextCursor == nil {
			break
		}
		cursor = *p.NextCursor
	}
	if strings.Join(seen, ",") != "usr-alice,usr-bob,usr-carol,usr-dave,usr-erin" {
		t.Fatalf("paged %v", seen)
	}
	if rec := asAdmin(h, admin, http.MethodGet, "/api/v1/admin/users?cursor=bogus", "", ""); rec.Code != 400 {
		t.Fatalf("bad cursor: %d", rec.Code)
	}
}
