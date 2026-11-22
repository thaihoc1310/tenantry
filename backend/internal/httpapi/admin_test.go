package httpapi_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi"
	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
	"github.com/thaihoc1310/tenantry/backend/internal/id"
)

// Clusters and pools for approvals: cluster-a is Ready, cluster-b is in
// maintenance. acme-net belongs to acme; the other pools are shared.
const reviewSeed = `
INSERT INTO clusters (id, display_name, status, proxy_endpoint, console_url, apps_domain) VALUES
  ('cluster-a', 'Cluster A', 'Ready', 'https://kube-a.test', 'https://console-a.test', 'apps.test'),
  ('cluster-b', 'Cluster B', 'Maintenance', 'https://kube-b.test', 'https://console-b.test', 'apps-b.test');
INSERT INTO shared_compute_pools (id, cluster_id, name, allocation_overcommit_ratio) VALUES
  ('shared-general', 'cluster-a', 'Shared general', 3.0), ('shared-b', 'cluster-b', 'Shared B', 2.0);
INSERT INTO network_pools (id, cluster_id, org_id, cidrs, default_prefix_length, allowed_prefix_lengths) VALUES
  ('onboarding-pod-network', 'cluster-a', NULL, '{10.240.0.0/16}', 24, '{24}'),
  ('acme-net', 'cluster-a', 'org-acme', '{10.241.0.0/16}', 24, '{20,24}'),
  ('net-b', 'cluster-b', NULL, '{10.250.0.0/16}', 24, '{24}');
INSERT INTO onboarding_policies (id, default_cluster_id, entitlement_template, grant_template, active) VALUES
  ('default', 'cluster-a', '{"maxMembers":20,"maxGroups":10,"maxWorkspacesTotal":8}',
   '{"maxWorkspaces":2,"maxNamespacesTotal":6,"resources":{"requestsCpu":"2","requestsMemory":"4Gi","limitsCpu":"4","limitsMemory":"8Gi","pods":20,"requestsStorage":"20Gi"},"sharedPoolIds":["shared-general"],"networkPoolIds":["onboarding-pod-network"],"allowedStorageClasses":["standard"]}',
   true);
`

// proposalBody is what the seed's policy proposes, as an approval body.
const proposalBody = `{"entitlement":{"maxMembers":20,"maxGroups":10,"maxWorkspacesTotal":8},
"clusterGrant":{"clusterId":"cluster-a","maxWorkspaces":2,"maxNamespacesTotal":6,
"resources":{"requestsCpu":"2","requestsMemory":"4Gi","limitsCpu":"4","limitsMemory":"8Gi","pods":20,"requestsStorage":"20Gi"},
"sharedPoolIds":["shared-general"],"networkPoolIds":["onboarding-pod-network"],"allowedStorageClasses":["standard"]}}`

func newReviewAPI(t *testing.T) (http.Handler, *pgxpool.Pool, string) {
	t.Helper()
	h, db := newAPI(t)
	if _, err := db.Exec(context.Background(), reviewSeed); err != nil {
		t.Fatal(err)
	}
	return h, db, newSession(t, db, "usr-dave", "platform-admin")
}

// asAdmin sends a portal request with session token; key "" sends none.
func asAdmin(h http.Handler, token, method, path, key, body string) *httptest.ResponseRecorder {
	req := request(method, path, token, body)
	if key != "" {
		req.Header.Set(httpapi.IdempotencyHeader, key)
	}
	return send(h, req)
}

func newKey() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// pendingRequest files a Pending request for a new user and returns its id.
func pendingRequest(t *testing.T, db *pgxpool.Pool, slug string) string {
	t.Helper()
	ctx := context.Background()
	user, orgID := "usr-"+slug, id.New(id.Organization)
	for _, s := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users (id, issuer, subject, email, display_name) VALUES ($1, 'iss', $1, $1 || '@example.com', 'Requester ' || $1)`, []any{user}},
		{`INSERT INTO organizations (id, slug, display_name, owner_user_id, status) VALUES ($1, $2, $2, $3, 'Pending')`, []any{orgID, slug, user}},
		{`INSERT INTO organization_members (org_id, user_id, role) VALUES ($1, $2, 'OrgOwner')`, []any{orgID, user}},
	} {
		if _, err := db.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatal(err)
		}
	}
	return orgID
}

func TestAdminNeedsTheRole(t *testing.T) {
	h, db, _ := newReviewAPI(t)
	plain := newSession(t, db, "usr-erin")
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/admin/organizations", ""},
		// Refused before validation: a malformed body still gets 403, not 400.
		{http.MethodPost, "/api/v1/admin/organizations/org-beta2:approve", `{"nonsense":1}`},
		{http.MethodPost, "/api/v1/admin/organizations/org-beta2:reject", `{"reason":"x"}`},
	} {
		rec := asAdmin(h, plain, c.method, c.path, newKey(), c.body)
		if rec.Code != 403 || errorBody(t, rec).Code != oapi.FORBIDDEN {
			t.Errorf("%s %s: %d %s", c.method, c.path, rec.Code, rec.Body)
		}
	}
	if rec := send(h, bareRequest(http.MethodGet, "/api/v1/admin/organizations", "", "")); rec.Code != 401 {
		t.Errorf("anonymous: %d", rec.Code)
	}
	if n := count(t, db, `SELECT count(*) FROM organizations WHERE status <> 'Pending' AND id = 'org-beta2'`); n != 0 {
		t.Fatal("a non-admin reviewed a request")
	}
}

func TestAdminListOrganizations(t *testing.T) {
	h, db, admin := newReviewAPI(t)
	first, second := pendingRequest(t, db, "zeta"), pendingRequest(t, db, "eta")
	list := func(query string) oapi.AdminOrganizationList {
		t.Helper()
		rec := asAdmin(h, admin, http.MethodGet, "/api/v1/admin/organizations"+query, "", "")
		var l oapi.AdminOrganizationList
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &l) != nil {
			t.Fatalf("%s: %d %s", query, rec.Code, rec.Body)
		}
		return l
	}

	// Arrival order is id order (ULIDs); the seed's hand-written ids sort last.
	queue := list("?status=Pending")
	if len(queue.Items) != 3 || queue.Items[0].Id != first || queue.Items[1].Id != second || queue.Items[2].Id != "org-beta2" {
		t.Fatalf("queue %+v", queue.Items)
	}
	zeta := queue.Items[0]
	if zeta.Owner.Id != "usr-zeta" || zeta.Owner.DisplayName != "Requester usr-zeta" || zeta.Owner.Email == nil {
		t.Fatalf("owner %+v", zeta.Owner)
	}
	var want oapi.Approval
	if err := json.Unmarshal([]byte(proposalBody), &want); err != nil {
		t.Fatal(err)
	}
	if zeta.Proposal == nil || !reflect.DeepEqual(*zeta.Proposal, want) {
		t.Fatalf("proposal %+v, want the policy's %+v", zeta.Proposal, want)
	}

	// Everything, oldest first, a page at a time; only Pending items carry a proposal.
	var ids []string
	cursor := ""
	for {
		page := list("?limit=2" + cursor)
		for _, o := range page.Items {
			ids = append(ids, o.Id+":"+string(o.Status))
			if (o.Proposal != nil) != (o.Status == oapi.OrgStatusPending) {
				t.Errorf("%s (%s) proposal %v", o.Id, o.Status, o.Proposal)
			}
		}
		if page.NextCursor == nil {
			break
		}
		cursor = "&cursor=" + *page.NextCursor
	}
	if len(ids) != 7 || !strings.Contains(strings.Join(ids, ","), "org-gone:Deleted") {
		t.Fatalf("all organizations: %v", ids)
	}

	// Without an active policy the Admin fills the form in.
	if _, err := db.Exec(context.Background(), `UPDATE onboarding_policies SET active = false`); err != nil {
		t.Fatal(err)
	}
	if q := list("?status=Pending"); q.Items[0].Proposal != nil {
		t.Fatal("proposal without a policy")
	}
}

var groupULID = regexp.MustCompile(`^[0-9a-z]{26}$`)

func TestApproveOrganization(t *testing.T) {
	h, db, admin := newReviewAPI(t)
	orgID := pendingRequest(t, db, "zeta")
	key := newKey()
	// Edited before approving: memory written another way, a second network pool.
	body := strings.Replace(proposalBody, `"requestsMemory":"4Gi"`, `"requestsMemory":"4096Mi"`, 1)

	rec := asAdmin(h, admin, http.MethodPost, "/api/v1/admin/organizations/"+orgID+":approve", key, body)
	if rec.Code != 200 {
		t.Fatalf("approve: %d %s", rec.Code, rec.Body)
	}
	var got oapi.ApprovedOrganization
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	g := got.ClusterGrant
	if got.Organization.Status != oapi.OrgStatusActive || got.Organization.Owner.Id != "usr-zeta" ||
		got.Entitlement != (oapi.Entitlement{MaxMembers: 20, MaxGroups: 10, MaxWorkspacesTotal: 8}) ||
		!strings.HasPrefix(g.Id, "grant-") || !g.IsDefault || g.ClusterId != "cluster-a" || g.MaxWorkspaces != 2 ||
		g.Resources != (oapi.GrantResources{RequestsCpu: "2", RequestsMemory: "4Gi", LimitsCpu: "4", LimitsMemory: "8Gi", Pods: 20, RequestsStorage: "20Gi"}) ||
		!reflect.DeepEqual(g.SharedPoolIds, []string{"shared-general"}) || !reflect.DeepEqual(g.NetworkPoolIds, []string{"onboarding-pod-network"}) {
		t.Fatalf("approved %+v", got)
	}

	// Stored as one consistent whole.
	var cpu, mem int64
	var pools, nets int
	if err := db.QueryRow(context.Background(), `
SELECT requests_cpu_milli, requests_memory_bytes,
  (SELECT count(*) FROM cluster_grant_shared_pools WHERE grant_id = g.id),
  (SELECT count(*) FROM cluster_grant_network_pools WHERE grant_id = g.id)
FROM organization_cluster_grants g WHERE org_id = $1 AND is_default`, orgID).Scan(&cpu, &mem, &pools, &nets); err != nil {
		t.Fatal(err)
	}
	if cpu != 2000 || mem != 4<<30 || pools != 1 || nets != 1 {
		t.Fatalf("grant row: cpu %d mem %d pools %d nets %d", cpu, mem, pools, nets)
	}
	if n := count(t, db, `SELECT count(*) FROM organization_entitlements WHERE org_id = $1 AND max_members = 20`, orgID); n != 1 {
		t.Fatal("no entitlement")
	}
	rows, err := db.Query(context.Background(),
		`SELECT display_name, machine_name, sync_status FROM groups WHERE org_id = $1 ORDER BY display_name`, orgID)
	if err != nil {
		t.Fatal(err)
	}
	var groups []string
	for rows.Next() {
		var name, machine, status string
		if err := rows.Scan(&name, &machine, &status); err != nil {
			t.Fatal(err)
		}
		// org_<ulid>:group_<ulid>, from the organization's and the group's ids.
		g, ok := strings.CutPrefix(machine, "org_"+id.ULID(orgID)+":group_")
		if !ok || !groupULID.MatchString(g) || status != "Pending" {
			t.Errorf("group %s: machine name %q, status %s", name, machine, status)
		}
		groups = append(groups, name)
	}
	rows.Close()
	if strings.Join(groups, ",") != "admins,developers,viewers" {
		t.Fatalf("groups %v", groups)
	}
	if n := count(t, db, `
SELECT count(*) FROM group_members m JOIN groups g ON g.id = m.group_id
WHERE g.org_id = $1 AND g.display_name = 'admins' AND m.user_id = 'usr-zeta' AND m.sync_status = 'Pending'`, orgID); n != 1 {
		t.Fatal("the owner is not in admins")
	}
	trail := auditTrail(t, db, "organization.approve")
	if len(trail) != 1 || trail[0].Actor != "usr-dave" || trail[0].Org != orgID || trail[0].Detail["clusterGrant"] == nil {
		t.Fatalf("audited as %+v", trail)
	}

	// A retry of the same approval gets the same answer; a second approval is refused.
	again := asAdmin(h, admin, http.MethodPost, "/api/v1/admin/organizations/"+orgID+":approve", key, body)
	if again.Code != 200 || again.Body.String() != rec.Body.String() || again.Header().Get(httpapi.ReplayedHeader) != "true" {
		t.Fatalf("retry: %d %s", again.Code, again.Body)
	}
	if rec := asAdmin(h, admin, http.MethodPost, "/api/v1/admin/organizations/"+orgID+":approve", newKey(), body); rec.Code != 409 ||
		errorBody(t, rec).Code != oapi.CONFLICT {
		t.Fatalf("second approval: %d %s", rec.Code, rec.Body)
	}
	// The owner now sees an Active organization.
	owner := newSession(t, db, "usr-zeta")
	if rec := send(h, request(http.MethodGet, "/api/v1/organizations/zeta", owner, "")); !strings.Contains(rec.Body.String(), `"status":"Active"`) {
		t.Fatalf("owner sees %s", rec.Body)
	}
}

// A refused approval leaves the request Pending and writes nothing.
func TestApproveRefused(t *testing.T) {
	h, db, admin := newReviewAPI(t)
	orgID := pendingRequest(t, db, "zeta")
	edit := func(from, to string) string {
		if !strings.Contains(proposalBody, from) {
			t.Fatalf("proposal has no %s", from)
		}
		return strings.Replace(proposalBody, from, to, 1)
	}
	cases := []struct {
		name, org, key, body string
		status               int
		code                 oapi.ErrorCode
		field                string
	}{
		{"no idempotency key", orgID, "-", proposalBody, 400, oapi.VALIDATIONFAILED, ""},
		{"unknown cluster", orgID, "", edit(`"clusterId":"cluster-a"`, `"clusterId":"cluster-x"`), 400, oapi.VALIDATIONFAILED, "clusterId"},
		{"cluster in maintenance", orgID, "", edit(`"clusterId":"cluster-a"`, `"clusterId":"cluster-b"`), 409, oapi.CLUSTERNOTREADY, ""},
		{"shared pool of another cluster", orgID, "", edit(`["shared-general"]`, `["shared-b"]`), 400, oapi.VALIDATIONFAILED, "sharedPoolIds"},
		{"unknown shared pool", orgID, "", edit(`["shared-general"]`, `["shared-x"]`), 400, oapi.VALIDATIONFAILED, "sharedPoolIds"},
		{"another organization's network pool", orgID, "", edit(`["onboarding-pod-network"]`, `["acme-net"]`), 400, oapi.VALIDATIONFAILED, "networkPoolIds"},
		{"network pool of another cluster", orgID, "", edit(`["onboarding-pod-network"]`, `["onboarding-pod-network","net-b"]`), 400, oapi.VALIDATIONFAILED, "networkPoolIds"},
		{"no network pool", orgID, "", edit(`["onboarding-pod-network"]`, `[]`), 400, oapi.VALIDATIONFAILED, ""},
		{"limits below requests", orgID, "", edit(`"limitsCpu":"4"`, `"limitsCpu":"1500m"`), 400, oapi.VALIDATIONFAILED, "limitsCpu"},
		{"not a quantity", orgID, "", edit(`"requestsCpu":"2"`, `"requestsCpu":"lots"`), 400, oapi.VALIDATIONFAILED, "requestsCpu"},
		{"negative quantity", orgID, "", edit(`"requestsStorage":"20Gi"`, `"requestsStorage":"-1Gi"`), 400, oapi.VALIDATIONFAILED, "requestsStorage"},
		{"absurd quantity", orgID, "", edit(`"limitsMemory":"8Gi"`, `"limitsMemory":"9Ei"`), 400, oapi.VALIDATIONFAILED, "limitsMemory"},
		{"fewer groups than the defaults", orgID, "", edit(`"maxGroups":10`, `"maxGroups":2`), 400, oapi.VALIDATIONFAILED, ""},
		{"bad StorageClass name", orgID, "", edit(`["standard"]`, `["Not_A_Name"]`), 400, oapi.VALIDATIONFAILED, "allowedStorageClasses"},
		{"same pool twice", orgID, "", edit(`["shared-general"]`, `["shared-general","shared-general"]`), 400, oapi.VALIDATIONFAILED, ""},
		{"unknown field", orgID, "", edit(`"maxWorkspaces":2`, `"maxWorkspaces":2,"computeModes":["Shared"]`), 400, oapi.VALIDATIONFAILED, ""},
		{"already reviewed", "org-acme", "", proposalBody, 409, oapi.CONFLICT, ""},
		{"no such organization", "org-nope", "", proposalBody, 404, oapi.NOTFOUND, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			key := c.key
			switch key {
			case "":
				key = newKey()
			case "-":
				key = ""
			}
			rec := asAdmin(h, admin, http.MethodPost, "/api/v1/admin/organizations/"+c.org+":approve", key, c.body)
			if rec.Code != c.status {
				t.Fatalf("status %d, want %d: %s", rec.Code, c.status, rec.Body)
			}
			e := errorBody(t, rec)
			if e.Code != c.code {
				t.Fatalf("code %s, want %s: %s", e.Code, c.code, e.Message)
			}
			if c.field != "" && (e.Details == nil || (*e.Details)["field"] != c.field) {
				t.Fatalf("details %v, want field %s", e.Details, c.field)
			}
		})
	}
	if n := count(t, db, `SELECT count(*) FROM organizations WHERE id = $1 AND status = 'Pending'`, orgID); n != 1 {
		t.Fatal("a refused approval changed the request")
	}
	for _, table := range []string{"organization_cluster_grants", "organization_entitlements", "groups", "group_members", "cluster_grant_network_pools"} {
		if n := count(t, db, `SELECT count(*) FROM `+table); n != 0 {
			t.Errorf("%d rows in %s after refused approvals", n, table)
		}
	}
}

// Two Admins approving the same request at once: one wins, the other is told
// it was already reviewed, and the organization gets one grant.
func TestApproveConcurrently(t *testing.T) {
	h, db, admin := newReviewAPI(t)
	other := newSession(t, db, "usr-erin", "platform-admin")
	orgID := pendingRequest(t, db, "zeta")
	codes := make([]int, 2)
	var wg sync.WaitGroup
	for i, token := range []string{admin, other} {
		wg.Go(func() {
			codes[i] = asAdmin(h, token, http.MethodPost, "/api/v1/admin/organizations/"+orgID+":approve", newKey(), proposalBody).Code
		})
	}
	wg.Wait()
	if codes[0]+codes[1] != 200+409 {
		t.Fatalf("statuses %v, want one 200 and one 409", codes)
	}
	if n := count(t, db, `SELECT count(*) FROM organization_cluster_grants WHERE org_id = $1`, orgID); n != 1 {
		t.Fatalf("%d grants", n)
	}
}

func TestRejectOrganization(t *testing.T) {
	h, db, admin := newReviewAPI(t)
	const path = "/api/v1/admin/organizations/org-beta2:reject"
	if rec := asAdmin(h, admin, http.MethodPost, path, "", `{"reason":"   "}`); rec.Code != 400 {
		t.Fatalf("blank reason: %d %s", rec.Code, rec.Body)
	}
	rec := asAdmin(h, admin, http.MethodPost, path, "", `{"reason":"  Please use your company email. "}`)
	var o oapi.AdminOrganization
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &o) != nil ||
		o.Status != oapi.OrgStatusRejected || o.ReviewNote != "Please use your company email." || o.Owner.Id != "usr-bob" {
		t.Fatalf("reject: %d %s", rec.Code, rec.Body)
	}
	if rec := asAdmin(h, admin, http.MethodPost, path, "", `{"reason":"again"}`); rec.Code != 409 {
		t.Fatalf("second review: %d", rec.Code)
	}
	trail := auditTrail(t, db, "organization.reject")
	if len(trail) != 1 || trail[0].Detail["reason"] != "Please use your company email." {
		t.Fatalf("audited as %+v", trail)
	}
	// The requester sees why; the slug is free and they may ask again.
	if rec := call(h, http.MethodGet, "/api/v1/organizations/org-beta2", "usr-bob"); !strings.Contains(rec.Body.String(), "company email") {
		t.Fatalf("requester sees %s", rec.Body)
	}
	if rec := callBody(h, http.MethodPost, "/api/v1/organizations", "usr-dave", `{"slug":"beta","displayName":"Beta"}`); rec.Code != 201 {
		t.Fatalf("slug not released: %d %s", rec.Code, rec.Body)
	}
	if rec := callBody(h, http.MethodPost, "/api/v1/organizations", "usr-bob", `{"slug":"bob","displayName":"Bob"}`); rec.Code != 201 {
		t.Fatalf("requester cannot ask again: %d %s", rec.Code, rec.Body)
	}
}
