package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
)

const clusterC = `{"id":"cluster-c","displayName":"Cluster C","proxyEndpoint":"https://kube-c.test",
"consoleUrl":"https://console-c.test","appsDomain":"apps-c.test","reservedCidrs":["192.168.0.0/16","10.96.0.0/12"]}`

func TestClusterRegistry(t *testing.T) {
	h, db, admin := newReviewAPI(t)
	rec := asAdmin(h, admin, http.MethodPost, "/api/v1/admin/clusters", newKey(), clusterC)
	var c oapi.AdminCluster
	if rec.Code != 201 || json.Unmarshal(rec.Body.Bytes(), &c) != nil || c.Status != "Registered" || len(c.ReservedCidrs) != 2 {
		t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
	if rec := asAdmin(h, admin, http.MethodPost, "/api/v1/admin/clusters", newKey(), clusterC); rec.Code != 409 {
		t.Fatalf("same id: %d", rec.Code)
	}
	for name, body := range map[string]string{
		"http endpoint":      strings.Replace(clusterC, "https://kube-c.test", "http://kube-c.test", 1),
		"apps domain":        strings.Replace(strings.Replace(clusterC, `"cluster-c"`, `"cluster-d"`, 1), "apps-c.test", "apps_c", 1),
		"overlapping own":    strings.Replace(strings.Replace(clusterC, `"cluster-c"`, `"cluster-d"`, 1), "10.96.0.0/12", "192.168.1.0/24", 1),
		"not a CIDR":         strings.Replace(strings.Replace(clusterC, `"cluster-c"`, `"cluster-d"`, 1), "10.96.0.0/12", "10.96.0.0", 1),
		"id not a DNS label": strings.Replace(clusterC, `"cluster-c"`, `"Cluster_D"`, 1),
		"unknown field":      strings.Replace(clusterC, `"id"`, `"zone":"x","id"`, 1),
	} {
		if rec := asAdmin(h, admin, http.MethodPost, "/api/v1/admin/clusters", newKey(), body); rec.Code != 400 {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}

	patch := func(id, body string) (int, oapi.ErrorResponse) {
		rec := asAdmin(h, admin, http.MethodPatch, "/api/v1/admin/clusters/"+id, "", body)
		if rec.Code == 200 {
			return 200, oapi.ErrorResponse{}
		}
		return rec.Code, errorBody(t, rec)
	}
	// A Registered cluster takes no grant yet.
	org := pendingRequest(t, db, "gamma")
	if rec := asAdmin(h, admin, http.MethodPost, "/api/v1/admin/organizations/"+org+":approve", newKey(),
		strings.Replace(proposalBody, `"clusterId":"cluster-a"`, `"clusterId":"cluster-c"`, 1)); rec.Code != 400 && rec.Code != 409 {
		t.Fatalf("approve on a Registered cluster: %d %s", rec.Code, rec.Body)
	}
	if code, _ := patch("cluster-c", `{"status":"Ready","displayName":"Cluster C (Hanoi)"}`); code != 200 {
		t.Fatal("Registered → Ready refused")
	}
	if code, e := patch("cluster-c", `{"status":"Registered"}`); code != 409 || (*e.Details)["field"] != "status" {
		t.Fatalf("back to Registered: %d %+v", code, e)
	}
	for _, st := range []string{"Maintenance", "Unavailable", "Ready"} {
		if code, _ := patch("cluster-c", `{"status":"`+st+`"}`); code != 200 {
			t.Fatalf("to %s refused", st)
		}
	}
	// Reserved ranges may not cover a network pool of the cluster.
	if code, e := patch("cluster-a", `{"reservedCidrs":["10.240.0.0/16"]}`); code != 409 || e.Code != oapi.CIDROVERLAP {
		t.Fatalf("reserved over a pool: %d %+v", code, e)
	}
	if code, _ := patch("cluster-nope", `{"displayName":"x"}`); code != 404 {
		t.Fatal("unknown cluster")
	}
	rec = asAdmin(h, admin, http.MethodGet, "/api/v1/admin/clusters", "", "")
	var l oapi.AdminClusterList
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &l) != nil || len(l.Items) != 3 || l.Items[2].DisplayName != "Cluster C (Hanoi)" {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	if n := count(t, db, `SELECT count(*) FROM audit_events WHERE action IN ('cluster.register', 'cluster.update')`); n != 5 {
		t.Fatalf("%d cluster audit events, want 5 (refusals leave none)", n)
	}
}

func TestOnboardingPolicy(t *testing.T) {
	h, db, admin := newReviewAPI(t)
	rec := asAdmin(h, admin, http.MethodGet, "/api/v1/admin/onboarding-policy", "", "")
	var p oapi.Approval
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &p) != nil || p.ClusterGrant.ClusterId != "cluster-a" || p.Entitlement.MaxGroups != 10 {
		t.Fatalf("get: %d %s", rec.Code, rec.Body)
	}
	changed := strings.Replace(strings.Replace(proposalBody, `"maxWorkspaces":2`, `"maxWorkspaces":3`, 1), `"requestsMemory":"4Gi"`, `"requestsMemory":"4096Mi"`, 1)
	if rec := asAdmin(h, admin, http.MethodPut, "/api/v1/admin/onboarding-policy", "", changed); rec.Code != 200 {
		t.Fatalf("put: %d %s", rec.Code, rec.Body)
	}
	// What approvals now propose, normalized.
	rec = asAdmin(h, admin, http.MethodGet, "/api/v1/admin/onboarding-policy", "", "")
	p = oapi.Approval{}
	if json.Unmarshal(rec.Body.Bytes(), &p) != nil || p.ClusterGrant.MaxWorkspaces != 3 || p.ClusterGrant.Resources.RequestsMemory != "4Gi" {
		t.Fatalf("after put %s", rec.Body)
	}
	pendingRequest(t, db, "delta")
	if rec := asAdmin(h, admin, http.MethodGet, "/api/v1/admin/organizations?status=Pending", "", ""); !strings.Contains(rec.Body.String(), `"maxWorkspaces":3`) {
		t.Fatalf("the review queue does not propose the new policy: %s", rec.Body)
	}
	for name, body := range map[string]string{
		"an organization's own network pool": strings.Replace(proposalBody, `"onboarding-pod-network"`, `"acme-net"`, 1),
		"fewer than 3 groups":                strings.Replace(proposalBody, `"maxGroups":10`, `"maxGroups":2`, 1),
		"pool of another cluster":            strings.Replace(proposalBody, `"shared-general"`, `"shared-b"`, 1),
	} {
		if rec := asAdmin(h, admin, http.MethodPut, "/api/v1/admin/onboarding-policy", "", body); rec.Code != 400 {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	if n := count(t, db, `SELECT count(*) FROM audit_events WHERE action = 'onboarding_policy.update'`); n != 1 {
		t.Fatalf("%d policy audit events", n)
	}
	if _, err := db.Exec(context.Background(), `UPDATE onboarding_policies SET active = false`); err != nil {
		t.Fatal(err)
	}
	if rec := asAdmin(h, admin, http.MethodGet, "/api/v1/admin/onboarding-policy", "", ""); rec.Code != 404 {
		t.Fatalf("no policy: %d", rec.Code)
	}
	// Putting one makes it active again.
	if rec := asAdmin(h, admin, http.MethodPut, "/api/v1/admin/onboarding-policy", "", proposalBody); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	if n := count(t, db, `SELECT count(*) FROM onboarding_policies WHERE active`); n != 1 {
		t.Fatalf("%d active policies", n)
	}
}

// grantBody is the proposal's grant on a cluster with some network pool.
func grantBody(cluster, netPool string, maxWorkspaces string) string {
	b := strings.SplitN(proposalBody, `"clusterGrant":`, 2)[1]
	b = strings.TrimSuffix(strings.TrimSpace(b), "}")
	b = strings.Replace(b, `"clusterId":"cluster-a"`, `"clusterId":"`+cluster+`"`, 1)
	b = strings.Replace(b, `"onboarding-pod-network"`, `"`+netPool+`"`, 1)
	b = strings.Replace(b, `"maxWorkspaces":2`, `"maxWorkspaces":`+maxWorkspaces, 1)
	if cluster != "cluster-a" {
		b = strings.Replace(b, `["shared-general"]`, `[]`, 1)
	}
	return b
}

func TestClusterGrants(t *testing.T) {
	h, db, orgID, _ := activeOrg(t)
	admin := newSession(t, db, "usr-dave", "platform-admin")
	put := func(cluster, body string) (int, []byte) {
		rec := asAdmin(h, admin, http.MethodPut, "/api/v1/admin/organizations/"+orgID+"/cluster-grants/"+cluster, "", body)
		return rec.Code, rec.Body.Bytes()
	}

	code, body := put("cluster-a", grantBody("cluster-a", "onboarding-pod-network", "5"))
	var g oapi.ClusterGrant
	if code != 200 || json.Unmarshal(body, &g) != nil || g.MaxWorkspaces != 5 || !g.IsDefault {
		t.Fatalf("change: %d %s", code, body)
	}
	var detail string
	if err := db.QueryRow(context.Background(), `SELECT detail::text FROM audit_events WHERE action = 'cluster_grant.update'`).Scan(&detail); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail, `"before": {"id"`) || !strings.Contains(detail, `"maxWorkspaces": 2`) || !strings.Contains(detail, `"maxWorkspaces": 5`) {
		t.Fatalf("audit before/after %s", detail)
	}

	refusals := []struct {
		name, cluster, body string
		status              int
	}{
		{"no network pool", "cluster-a", strings.Replace(grantBody("cluster-a", "x", "2"), `["x"]`, `[]`, 1), 400},
		{"pool of another cluster", "cluster-a", grantBody("cluster-a", "net-b", "2"), 400},
		{"another organization's pool", "cluster-a", grantBody("cluster-a", "acme-net", "2"), 400},
		{"cluster of the body not the path's", "cluster-a", grantBody("cluster-b", "net-b", "2"), 400},
		{"new grant on a cluster in maintenance", "cluster-b", grantBody("cluster-b", "net-b", "2"), 409},
	}
	for _, c := range refusals {
		if code, body := put(c.cluster, c.body); code != c.status {
			t.Errorf("%s: %d %s", c.name, code, body)
		}
	}

	// A second cluster: a new grant, not the default.
	if rec := asAdmin(h, admin, http.MethodPost, "/api/v1/admin/clusters", newKey(), clusterC); rec.Code != 201 {
		t.Fatal(rec.Code)
	}
	asAdmin(h, admin, http.MethodPatch, "/api/v1/admin/clusters/cluster-c", "", `{"status":"Ready"}`)
	if _, err := db.Exec(context.Background(), `INSERT INTO network_pools (id, cluster_id, cidrs, default_prefix_length, allowed_prefix_lengths)
		VALUES ('net-c', 'cluster-c', '{10.251.0.0/16}', 24, '{24}')`); err != nil {
		t.Fatal(err)
	}
	code, body = put("cluster-c", grantBody("cluster-c", "net-c", "1"))
	g = oapi.ClusterGrant{}
	if code != 201 || json.Unmarshal(body, &g) != nil || g.IsDefault {
		t.Fatalf("new grant: %d %s", code, body)
	}

	// Every grant, for the Admin's list.
	rec := asAdmin(h, admin, http.MethodGet, "/api/v1/admin/cluster-grants", "", "")
	var l oapi.AdminClusterGrantList
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &l) != nil || len(l.Items) != 2 || l.Items[0].Organization.Slug != "zeta" ||
		l.Items[0].Usage.Workspaces.Limit != int(l.Items[0].ClusterGrant.MaxWorkspaces) {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	rec = asAdmin(h, admin, http.MethodGet, "/api/v1/admin/cluster-grants?clusterId=cluster-c", "", "")
	l = oapi.AdminClusterGrantList{}
	if json.Unmarshal(rec.Body.Bytes(), &l) != nil || len(l.Items) != 1 || l.Items[0].ClusterGrant.ClusterId != "cluster-c" {
		t.Fatalf("filtered: %s", rec.Body)
	}

	// Not while the organization holds a dedicated node there.
	if _, err := db.Exec(context.Background(), `INSERT INTO dedicated_node_grants (id, org_id, cluster_id, node_name)
		VALUES ('dng-1', $1, 'cluster-a', 'wk9')`, orgID); err != nil {
		t.Fatal(err)
	}
	rec = asAdmin(h, admin, http.MethodDelete, "/api/v1/admin/organizations/"+orgID+"/cluster-grants/cluster-a", "", "")
	if e := errorBody(t, rec); rec.Code != 409 || (*e.Details)["dedicatedNodes"] != float64(1) {
		t.Fatalf("delete with a dedicated node: %d %s", rec.Code, rec.Body)
	}
	if _, err := db.Exec(context.Background(), `UPDATE dedicated_node_grants SET revoked_at = now()`); err != nil {
		t.Fatal(err)
	}
	// Deleting the default grant makes the one left the default.
	if rec := asAdmin(h, admin, http.MethodDelete, "/api/v1/admin/organizations/"+orgID+"/cluster-grants/cluster-a", "", ""); rec.Code != 204 {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	if n := count(t, db, `SELECT count(*) FROM organization_cluster_grants WHERE org_id = $1 AND cluster_id = 'cluster-c' AND is_default`, orgID); n != 1 {
		t.Fatal("the grant left is not the default")
	}
	if rec := asAdmin(h, admin, http.MethodDelete, "/api/v1/admin/organizations/"+orgID+"/cluster-grants/cluster-a", "", ""); rec.Code != 404 {
		t.Fatalf("delete again: %d", rec.Code)
	}
	pending := pendingRequest(t, db, "pending")
	if rec := asAdmin(h, admin, http.MethodPut, "/api/v1/admin/organizations/"+pending+"/cluster-grants/cluster-a", "", grantBody("cluster-a", "onboarding-pod-network", "2")); rec.Code != 409 {
		t.Fatalf("grant to a pending request: %d", rec.Code)
	}
}

func TestAdminOperations(t *testing.T) {
	h, db, orgID, _ := activeOrg(t)
	admin := newSession(t, db, "usr-dave", "platform-admin")
	if _, err := db.Exec(context.Background(), `INSERT INTO operations (id, org_id, resource_type, resource_id, type, correlation_id, state) VALUES
		('op-1', $1, 'Workspace', 'ws-1', 'workspace.create', 'r1', 'Succeeded'),
		('op-2', $1, 'Workspace', 'ws-2', 'workspace.create', 'r2', 'Failed'),
		('op-3', $1, 'Workspace', 'ws-3', 'workspace.create', 'r3', 'Pending')`, orgID); err != nil {
		t.Fatal(err)
	}
	list := func(q string) oapi.OperationList {
		rec := asAdmin(h, admin, http.MethodGet, "/api/v1/admin/operations"+q, "", "")
		var l oapi.OperationList
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &l) != nil {
			t.Fatalf("%s: %d %s", q, rec.Code, rec.Body)
		}
		return l
	}
	if l := list(""); len(l.Items) != 3 || l.Items[0].Id != "op-3" {
		t.Fatalf("all %+v", l.Items)
	}
	if l := list("?state=Failed"); len(l.Items) != 1 || l.Items[0].Id != "op-2" {
		t.Fatalf("failed %+v", l.Items)
	}
	first := list("?limit=2")
	if len(first.Items) != 2 || first.NextCursor == nil {
		t.Fatalf("page 1 %+v", first)
	}
	if rest := list("?limit=2&cursor=" + *first.NextCursor); len(rest.Items) != 1 || rest.Items[0].Id != "op-1" || rest.NextCursor != nil {
		t.Fatalf("page 2 %+v", rest)
	}

	// Needs attention: a Failed or Blocked operation still the last word on
	// its target. op-2 is; op-4 was retried (op-5); op-6's Workspace is
	// Deleted; op-7 is Blocked.
	var grant string
	if err := db.QueryRow(context.Background(), `SELECT id FROM organization_cluster_grants WHERE org_id = $1 LIMIT 1`, orgID).Scan(&grant); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(context.Background(), `
		INSERT INTO workspaces (id, org_id, cluster_id, cluster_grant_id, slug, k8s_name, display_name, state,
			requests_cpu_milli, requests_memory_bytes, limits_cpu_milli, limits_memory_bytes, pods, requests_storage_bytes,
			namespace_quota, compute_mode, shared_pool_id)
		SELECT 'ws-6', $1, cluster_id, id, 'gone', 'x-gone', 'Gone', 'Deleted', 0, 0, 0, 0, 0, 0, 1, 'Shared', 'shared-general'
		FROM organization_cluster_grants WHERE id = $2`, orgID, grant); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(context.Background(), `
		INSERT INTO operations (id, org_id, resource_type, resource_id, type, correlation_id, state) VALUES
		('op-4', $1, 'Workspace', 'ws-4', 'workspace.update', 'r4', 'Failed'),
		('op-5', $1, 'Workspace', 'ws-4', 'workspace.update', 'r5', 'Succeeded'),
		('op-6', $1, 'Workspace', 'ws-6', 'workspace.delete', 'r6', 'Failed'),
		('op-7', $1, 'Workspace', 'ws-7', 'workspace.delete', 'r7', 'Blocked')`, orgID); err != nil {
		t.Fatal(err)
	}
	ids := func(l oapi.OperationList) []string {
		out := []string{}
		for _, op := range l.Items {
			out = append(out, op.Id)
		}
		return out
	}
	if got := ids(list("?attention=true")); strings.Join(got, ",") != "op-7,op-2" {
		t.Fatalf("needs attention: %v", got)
	}
	if got := ids(list("?state=Failed")); strings.Join(got, ",") != "op-6,op-4,op-2" {
		t.Fatalf("all Failed, superseded included: %v", got)
	}
	var health oapi.PlatformHealth
	if rec := asAdmin(h, admin, http.MethodGet, "/api/v1/admin/health", "", ""); rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &health) != nil ||
		health.Work.OperationsNeedingAttention != 2 {
		t.Fatalf("health counts the same: %d %s", rec.Code, rec.Body)
	}
}

// The health page warns about a cluster never checked and a stale backlog,
// and reports the work in hand.
func TestPlatformHealth(t *testing.T) {
	h, db, admin := newReviewAPI(t)
	get := func() oapi.PlatformHealth {
		t.Helper()
		rec := asAdmin(h, admin, http.MethodGet, "/api/v1/admin/health", "", "")
		var out oapi.PlatformHealth
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
			t.Fatalf("health: %d %s", rec.Code, rec.Body)
		}
		return out
	}
	state := func(r oapi.PlatformHealth) map[string]string {
		out := map[string]string{}
		for _, c := range r.Components {
			out[c.Name] = string(c.State)
		}
		return out
	}
	r := get()
	if s := state(r); s["Cluster A"] != "warn" || s["Cluster B"] != "warn" || s["Provisioning worker"] != "ok" || s["Keycloak"] != "ok" {
		t.Fatalf("never checked clusters should warn: %+v", r.Components)
	}
	if r.Work.RequestsWaiting != 1 || len(r.Clusters) != 2 || len(r.Clusters[0].Pools) != 1 || r.Clusters[0].Pools[0].Id != "shared-general" {
		t.Fatalf("report %+v", r)
	}
	for _, q := range []string{
		`UPDATE clusters SET last_health_check = now() - interval '10 seconds' WHERE id = 'cluster-a'`,
		`INSERT INTO operations (id, org_id, resource_type, resource_id, type, state) VALUES ('op-1', 'org-acme', 'Workspace', 'ws-1', 'workspace.create', 'Failed')`,
		`INSERT INTO outbox_events (operation_id, ordering_key, target_cluster_id, type, payload, version, created_at)
			VALUES ('op-1', 'Workspace/ws-1', 'cluster-a', 'workspace.apply', '{}', 1, now() - interval '5 minutes')`,
		`INSERT INTO groups (id, org_id, display_name, machine_name, sync_status, last_error) VALUES ('grp-x', 'org-acme', 'x', 'org_acme:group_x', 'Error', 'keycloak token: 401')`,
	} {
		if _, err := db.Exec(context.Background(), q); err != nil {
			t.Fatal(err)
		}
	}
	r = get()
	if s := state(r); s["Cluster A"] != "ok" || s["Cluster B"] != "warn" || s["Provisioning worker"] != "warn" ||
		s["Keycloak"] != "warn" || s["Group sync worker"] != "ok" {
		t.Fatalf("after a check, a stale event and a sync error: %+v", r.Components)
	}
	if r.Work.OperationsNeedingAttention != 1 || r.Work.SyncWaiting != 1 {
		t.Fatalf("work %+v", r.Work)
	}
	if rec := asAdmin(h, sessionOf("usr-bob"), http.MethodGet, "/api/v1/admin/health", "", ""); rec.Code != 403 {
		t.Fatalf("ordinary user: %d", rec.Code)
	}
}
