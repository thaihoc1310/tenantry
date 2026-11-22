package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
)

func TestEntitlementAndUsage(t *testing.T) {
	h, db, admin := newReviewAPI(t)
	orgID := pendingRequest(t, db, "zeta")
	if rec := asAdmin(h, admin, http.MethodPost, "/api/v1/admin/organizations/"+orgID+":approve", newKey(), proposalBody); rec.Code != 200 {
		t.Fatalf("approve: %d %s", rec.Code, rec.Body)
	}
	// erin joins as OrgAdmin, bob as OrgMember.
	if _, err := db.Exec(context.Background(),
		`INSERT INTO organization_members (org_id, user_id, role) VALUES ($1, 'usr-erin', 'OrgAdmin'), ($1, 'usr-bob', 'OrgMember')`, orgID); err != nil {
		t.Fatal(err)
	}
	owner := newSession(t, db, "usr-zeta")

	for _, token := range []string{owner, sessionOf("usr-erin")} {
		rec := send(h, request(http.MethodGet, "/api/v1/organizations/zeta/entitlement", token, ""))
		var e oapi.OrganizationEntitlement
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &e) != nil {
			t.Fatalf("entitlement: %d %s", rec.Code, rec.Body)
		}
		if e.Entitlement != (oapi.Entitlement{MaxMembers: 20, MaxGroups: 10, MaxWorkspacesTotal: 8}) || len(e.ClusterGrants) != 1 {
			t.Fatalf("entitlement %+v", e)
		}
		g := e.ClusterGrants[0]
		if !g.IsDefault || g.ClusterId != "cluster-a" || g.Resources.LimitsMemory != "8Gi" ||
			!reflect.DeepEqual(g.SharedPoolIds, []string{"shared-general"}) || !reflect.DeepEqual(g.AllowedStorageClasses, []string{"standard"}) {
			t.Fatalf("grant %+v", g)
		}
	}

	rec := send(h, request(http.MethodGet, "/api/v1/organizations/"+orgID+"/usage", owner, ""))
	var u oapi.OrganizationUsage
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &u) != nil {
		t.Fatalf("usage: %d %s", rec.Code, rec.Body)
	}
	o := u.Organization
	// Counted for real: zeta, erin and bob; the three default groups.
	if o.Members != (oapi.UsageCount{Used: 3, Limit: 20}) || o.Groups != (oapi.UsageCount{Used: 3, Limit: 10}) ||
		o.Workspaces != (oapi.UsageCount{Used: 0, Limit: 8}) {
		t.Fatalf("organization usage %+v", o)
	}
	if len(u.ClusterGrants) != 1 {
		t.Fatalf("grant usage %+v", u.ClusterGrants)
	}
	g := u.ClusterGrants[0]
	if g.ClusterId != "cluster-a" || !g.IsDefault || g.Workspaces != (oapi.AllocatedCount{Allocated: 0, Limit: 2, Remaining: 2}) ||
		g.RequestsCpu != (oapi.AllocatedQuantity{Allocated: "0", Limit: "2", Remaining: "2"}) ||
		g.RequestsStorage != (oapi.AllocatedQuantity{Allocated: "0", Limit: "20Gi", Remaining: "20Gi"}) ||
		g.Pods != (oapi.AllocatedCount{Allocated: 0, Limit: 20, Remaining: 20}) {
		t.Fatalf("grant usage %+v", g)
	}
	// A group waiting for removal no longer counts.
	if _, err := db.Exec(context.Background(), `UPDATE groups SET deleted_at = now() WHERE org_id = $1 AND display_name = 'viewers'`, orgID); err != nil {
		t.Fatal(err)
	}
	rec = send(h, request(http.MethodGet, "/api/v1/organizations/"+orgID+"/usage", owner, ""))
	_ = json.Unmarshal(rec.Body.Bytes(), &u)
	if u.Organization.Groups.Used != 2 {
		t.Fatalf("groups %+v", u.Organization.Groups)
	}
}

// Membership first (404), then role (403), then status (409).
func TestQuotaAccess(t *testing.T) {
	h, db, admin := newReviewAPI(t)
	orgID := pendingRequest(t, db, "zeta")
	if rec := asAdmin(h, admin, http.MethodPost, "/api/v1/admin/organizations/"+orgID+":approve", newKey(), proposalBody); rec.Code != 200 {
		t.Fatalf("approve: %d", rec.Code)
	}
	if _, err := db.Exec(context.Background(), `INSERT INTO organization_members (org_id, user_id, role) VALUES ($1, 'usr-bob', 'OrgMember')`, orgID); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, path, user string
		status           int
		code             oapi.ErrorCode
	}{
		{"not a member", "/api/v1/organizations/zeta", "usr-dave", 404, oapi.NOTFOUND},
		{"OrgMember", "/api/v1/organizations/zeta", "usr-bob", 403, oapi.FORBIDDEN},
		// bob owns the Pending request beta2.
		{"Pending organization", "/api/v1/organizations/org-beta2", "usr-bob", 409, oapi.ORGNOTACTIVE},
		{"Rejected organization", "/api/v1/organizations/org-old", "usr-erin", 409, oapi.ORGNOTACTIVE},
		// Platform Admins see quotas from the admin area (M5), not here.
		{"Platform Admin, not a member", "/api/v1/organizations/zeta", "", 404, oapi.NOTFOUND},
	}
	for _, c := range cases {
		for _, ep := range []string{"/entitlement", "/usage"} {
			token := admin
			if c.user != "" {
				token = sessionOf(c.user)
			}
			rec := send(h, request(http.MethodGet, c.path+ep, token, ""))
			if rec.Code != c.status || errorBody(t, rec).Code != c.code {
				t.Errorf("%s %s: %d %s", c.name, ep, rec.Code, rec.Body)
			}
		}
	}
}
