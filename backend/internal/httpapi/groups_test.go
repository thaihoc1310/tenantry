package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
	"github.com/thaihoc1310/tenantry/backend/internal/id"
)

func listGroups(t *testing.T, h http.Handler, token string) map[string]oapi.Group {
	t.Helper()
	rec := send(h, request(http.MethodGet, "/api/v1/organizations/zeta/groups", token, ""))
	var l oapi.GroupList
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &l) != nil {
		t.Fatalf("list groups: %d %s", rec.Code, rec.Body)
	}
	out := map[string]oapi.Group{}
	for _, g := range l.Items {
		out[g.Name] = g
	}
	return out
}

func createGroup(h http.Handler, token, name string) (int, oapi.Group) {
	rec := send(h, request(http.MethodPost, "/api/v1/organizations/zeta/groups", token, `{"name":"`+name+`"}`))
	var g oapi.Group
	_ = json.Unmarshal(rec.Body.Bytes(), &g)
	return rec.Code, g
}

func TestGroups(t *testing.T) {
	h, db, orgID, owner := staffedOrg(t)
	groups := listGroups(t, h, owner)
	if len(groups) != 3 || len(groups["admins"].Members) != 1 || groups["admins"].Members[0].UserId != "usr-zeta" ||
		groups["admins"].SyncStatus != oapi.SyncStatusPending {
		t.Fatalf("default groups %+v", groups)
	}

	code, g := createGroup(h, sessionOf("usr-erin"), "  backend ")
	if code != 201 || g.Name != "backend" || g.SyncStatus != oapi.SyncStatusPending || len(g.Members) != 0 ||
		!strings.HasPrefix(g.MachineName, "org_"+id.ULID(orgID)+":group_") {
		t.Fatalf("create: %d %+v", code, g)
	}
	rec := send(h, request(http.MethodPost, "/api/v1/organizations/zeta/groups", owner, `{"name":"backend"}`))
	if e := errorBody(t, rec); rec.Code != 409 || e.Code != oapi.CONFLICT || (*e.Details)["field"] != "name" {
		t.Fatalf("same name: %d %s", rec.Code, rec.Body)
	}

	// Renaming changes the display name only.
	rec = send(h, request(http.MethodPatch, "/api/v1/groups/"+g.Id, owner, `{"name":"api"}`))
	var renamed oapi.Group
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &renamed) != nil || renamed.Name != "api" || renamed.MachineName != g.MachineName {
		t.Fatalf("rename: %d %s", rec.Code, rec.Body)
	}
	if rec := send(h, request(http.MethodPatch, "/api/v1/groups/"+g.Id, owner, `{"name":"admins"}`)); rec.Code != 409 {
		t.Fatalf("rename onto another group's name: %d", rec.Code)
	}

	// Deleting leaves a tombstone for the sync worker and frees the name.
	if rec := send(h, request(http.MethodDelete, "/api/v1/groups/"+g.Id, owner, "")); rec.Code != 204 {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	if _, ok := listGroups(t, h, owner)["api"]; ok {
		t.Fatal("deleted group still listed")
	}
	if n := count(t, db, `SELECT count(*) FROM groups WHERE id = $1 AND deleted_at IS NOT NULL AND sync_status = 'Pending'`, g.Id); n != 1 {
		t.Fatal("deleted group is not a tombstone waiting for sync")
	}
	if rec := send(h, request(http.MethodPatch, "/api/v1/groups/"+g.Id, owner, `{"name":"x"}`)); rec.Code != 404 {
		t.Fatalf("rename a deleted group: %d", rec.Code)
	}
	if code, _ := createGroup(h, owner, "api"); code != 201 {
		t.Fatalf("name of a deleted group not reusable: %d", code)
	}
	for _, action := range []string{"group.create", "group.update", "group.delete"} {
		if n := count(t, db, `SELECT count(*) FROM audit_events WHERE action = $1`, action); n < 1 {
			t.Errorf("%s not audited", action)
		}
	}
}

// maxGroups counts the default groups and not tombstones.
func TestGroupsRespectMaxGroups(t *testing.T) {
	h, db, orgID, owner := staffedOrg(t)
	if _, err := db.Exec(context.Background(), `UPDATE organization_entitlements SET max_groups = 4 WHERE org_id = $1`, orgID); err != nil {
		t.Fatal(err)
	}
	code, g := createGroup(h, owner, "backend")
	if code != 201 {
		t.Fatalf("fourth group: %d", code)
	}
	rec := send(h, request(http.MethodPost, "/api/v1/organizations/zeta/groups", owner, `{"name":"frontend"}`))
	if e := errorBody(t, rec); rec.Code != 409 || e.Code != oapi.ENTITLEMENTEXCEEDED || (*e.Details)["limit"] != "maxGroups" {
		t.Fatalf("fifth group: %d %s", rec.Code, rec.Body)
	}
	send(h, request(http.MethodDelete, "/api/v1/groups/"+g.Id, owner, ""))
	if code, _ := createGroup(h, owner, "frontend"); code != 201 {
		t.Fatalf("a tombstone still counts: %d", code)
	}
}

func groupMember(t *testing.T, db *pgxpool.Pool, groupID, user string) string {
	t.Helper()
	var state string
	if err := db.QueryRow(context.Background(), `
SELECT coalesce((SELECT CASE WHEN deleted_at IS NULL THEN 'member' ELSE 'tombstone' END || ':' || sync_status
FROM group_members WHERE group_id = $1 AND user_id = $2), 'absent')`, groupID, user).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestGroupMembers(t *testing.T) {
	h, db, orgID, owner := staffedOrg(t)
	viewers := groupID(t, db, orgID, "viewers")
	put := func(token, user string) int {
		return send(h, request(http.MethodPut, "/api/v1/groups/"+viewers+"/members/"+user, token, "")).Code
	}
	del := func(token, user string) int {
		return send(h, request(http.MethodDelete, "/api/v1/groups/"+viewers+"/members/"+user, token, "")).Code
	}
	if code := put(owner, "usr-bob"); code != 204 || groupMember(t, db, viewers, "usr-bob") != "member:Pending" {
		t.Fatalf("add: %d %s", code, groupMember(t, db, viewers, "usr-bob"))
	}
	if code := put(sessionOf("usr-erin"), "usr-bob"); code != 204 {
		t.Fatalf("add again: %d", code)
	}
	if n := count(t, db, `SELECT count(*) FROM audit_events WHERE action = 'group_member.add'`); n != 1 {
		t.Fatalf("%d additions audited, want 1 (adding again changes nothing)", n)
	}
	if g := listGroups(t, h, owner)["viewers"]; len(g.Members) != 1 || g.Members[0].SyncStatus != oapi.SyncStatusPending {
		t.Fatalf("viewers %+v", g)
	}
	rec := send(h, request(http.MethodPut, "/api/v1/groups/"+viewers+"/members/usr-dave", owner, ""))
	if e := errorBody(t, rec); rec.Code != 400 || (*e.Details)["field"] != "userId" {
		t.Fatalf("add a non-member: %d %s", rec.Code, rec.Body)
	}

	// Removal is a tombstone; adding back before the worker ran revives it.
	if code := del(owner, "usr-bob"); code != 204 || groupMember(t, db, viewers, "usr-bob") != "tombstone:Pending" {
		t.Fatalf("remove: %d %s", code, groupMember(t, db, viewers, "usr-bob"))
	}
	if g := listGroups(t, h, owner)["viewers"]; len(g.Members) != 0 {
		t.Fatal("removed member still listed")
	}
	if code := del(owner, "usr-bob"); code != 204 {
		t.Fatalf("remove again: %d", code)
	}
	if code := put(owner, "usr-bob"); code != 204 || groupMember(t, db, viewers, "usr-bob") != "member:Pending" {
		t.Fatalf("add back: %d %s", code, groupMember(t, db, viewers, "usr-bob"))
	}
}

func TestGroupAccess(t *testing.T) {
	h, db, orgID, owner := staffedOrg(t)
	dev := groupID(t, db, orgID, "developers")
	pending := pendingRequest(t, db, "other")
	other := newSession(t, db, "usr-other")
	cases := []struct {
		name, token, method, path, body string
		status                          int
	}{
		{"OrgMember lists", sessionOf("usr-bob"), http.MethodGet, "/api/v1/organizations/zeta/groups", "", 403},
		{"OrgMember creates", sessionOf("usr-bob"), http.MethodPost, "/api/v1/organizations/zeta/groups", `{"name":"x"}`, 403},
		{"OrgMember renames", sessionOf("usr-bob"), http.MethodPatch, "/api/v1/groups/" + dev, `{"name":"x"}`, 403},
		{"OrgMember adds", sessionOf("usr-bob"), http.MethodPut, "/api/v1/groups/" + dev + "/members/usr-erin", "", 403},
		{"outsider lists", sessionOf("usr-dave"), http.MethodGet, "/api/v1/organizations/zeta/groups", "", 404},
		{"outsider renames", sessionOf("usr-dave"), http.MethodPatch, "/api/v1/groups/" + dev, `{"name":"x"}`, 404},
		{"outsider deletes", sessionOf("usr-dave"), http.MethodDelete, "/api/v1/groups/" + dev, "", 404},
		{"organization not approved", other, http.MethodPost, "/api/v1/organizations/" + pending + "/groups", `{"name":"x"}`, 409},
		{"blank name", owner, http.MethodPost, "/api/v1/organizations/zeta/groups", `{"name":"   "}`, 400},
		{"unknown group", owner, http.MethodDelete, "/api/v1/groups/grp-nope", "", 404},
	}
	for _, c := range cases {
		if rec := send(h, request(c.method, c.path, c.token, c.body)); rec.Code != c.status {
			t.Errorf("%s: %d, want %d: %s", c.name, rec.Code, c.status, rec.Body)
		}
	}
}

// Joining a group while leaving the organization at the same moment never
// ends with a live group member who is not an organization member.
func TestJoinGroupWhileLeaving(t *testing.T) {
	for range 5 {
		h, db, orgID, owner := staffedOrg(t)
		viewers := groupID(t, db, orgID, "viewers")
		var wg sync.WaitGroup
		wg.Go(func() { send(h, request(http.MethodPut, "/api/v1/groups/"+viewers+"/members/usr-bob", owner, "")) })
		wg.Go(func() { call(h, http.MethodPost, "/api/v1/organizations/zeta/members/me:leave", "usr-bob") })
		wg.Wait()
		if n := count(t, db, `SELECT count(*) FROM group_members gm JOIN groups g ON g.id = gm.group_id
			WHERE g.org_id = $1 AND gm.deleted_at IS NULL
			  AND NOT EXISTS (SELECT 1 FROM organization_members m WHERE m.org_id = g.org_id AND m.user_id = gm.user_id)`, orgID); n != 0 {
			t.Fatalf("%d live group members outside the organization", n)
		}
	}
}
