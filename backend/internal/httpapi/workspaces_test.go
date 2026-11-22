package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
	"github.com/thaihoc1310/tenantry/backend/internal/workspace"
	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
)

// workspaceOrg is staffedOrg with its three groups already in Keycloak. It
// returns the ids of the groups admins and developers.
func workspaceOrg(t *testing.T) (h http.Handler, db *pgxpool.Pool, orgID, owner, admins, devs string) {
	t.Helper()
	h, db, orgID, owner = staffedOrg(t)
	if _, err := db.Exec(context.Background(), `UPDATE groups SET sync_status = 'Synced' WHERE org_id = $1`, orgID); err != nil {
		t.Fatal(err)
	}
	return h, db, orgID, owner, groupID(t, db, orgID, "admins"), groupID(t, db, orgID, "developers")
}

// workspaceBody is a valid Shared create body; extra replaces fields by
// name (a JSON object's later key wins).
func workspaceBody(slug, owners, devs, extra string) string {
	b := fmt.Sprintf(`{"slug":%q,"displayName":"Solar","clusterId":"cluster-a",
"compute":{"mode":"Shared","sharedPoolId":"shared-general"},
"resources":{"requestsCpu":"1","requestsMemory":"2Gi","limitsCpu":"2","limitsMemory":"4Gi","pods":10,"requestsStorage":"5Gi","namespaceQuota":2},
"namespaces":["frontend"],
"bindings":[{"groupId":%q,"role":"TenantOwner"},{"groupId":%q,"role":"Developer"}]`, slug, owners, devs)
	if extra != "" {
		b += "," + extra
	}
	return b + "}"
}

func createWorkspace(h http.Handler, token, org, body string) *httptest.ResponseRecorder {
	return asAdmin(h, token, http.MethodPost, "/api/v1/organizations/"+org+"/workspaces", newKey(), body)
}

func TestCreateWorkspace(t *testing.T) {
	h, db, orgID, owner, admins, devs := workspaceOrg(t)
	key := newKey()
	body := workspaceBody("solar", admins, devs, "")
	rec := asAdmin(h, owner, http.MethodPost, "/api/v1/organizations/zeta/workspaces", key, body)
	var got oapi.WorkspaceAccepted
	if rec.Code != 202 || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	ws, op := got.Workspace, got.Operation
	if rec.Header().Get("Location") != "/api/v1/workspaces/"+ws.Id || ws.K8sName != "zeta-solar" || ws.OrgId != orgID ||
		ws.State != oapi.WorkspaceStateProvisioning || *ws.Resources.LimitsCpu != "2" || ws.Resources.RequestsMemory != "2Gi" ||
		*ws.Compute.SharedPoolId != "shared-general" || len(ws.Conditions) != 0 {
		t.Fatalf("workspace %+v", ws)
	}
	if op.Type != "workspace.create" || op.State != oapi.OperationStatePending || op.ResourceId != ws.Id || *op.OrgId != orgID {
		t.Fatalf("operation %+v", op)
	}

	// The outbox holds the whole CR at version 1.
	var payload []byte
	var version int64
	if err := db.QueryRow(context.Background(), `SELECT payload, version FROM outbox_events WHERE operation_id = $1 AND type = 'workspace.apply'
		AND ordering_key = $2 AND target_cluster_id = 'cluster-a'`, op.Id, "Workspace/"+ws.Id).Scan(&payload, &version); err != nil {
		t.Fatal(err)
	}
	var cr v1.PlatformWorkspace
	if err := json.Unmarshal(payload, &cr); err != nil {
		t.Fatal(err)
	}
	machine := func(gid string) string {
		var m string
		if err := db.QueryRow(context.Background(), `SELECT machine_name FROM groups WHERE id = $1`, gid).Scan(&m); err != nil {
			t.Fatal(err)
		}
		return workspace.GroupSubjectPrefix + m
	}
	cpu, mem := cr.Spec.Resources["limits.cpu"], cr.Spec.Resources["requests.memory"]
	if version != 1 || cr.Kind != "PlatformWorkspace" || cr.APIVersion != "tenantry.io/v1alpha1" || cr.Name != "zeta-solar" ||
		cr.Annotations[workspace.AnnotationSpecVersion] != "1" || cr.Labels[v1.LabelWorkspaceID] != ws.Id ||
		cr.Spec.WorkspaceID != ws.Id || cr.Spec.OrganizationID != orgID || cr.Spec.ClusterID != "cluster-a" ||
		cr.Spec.NamespaceQuota != 2 || !slices.Equal(cr.Spec.Namespaces, []string{"frontend"}) ||
		cpu.String() != "2" || mem.String() != "2Gi" ||
		cr.Spec.Compute != (v1.Compute{Mode: v1.ComputeShared, SharedPoolID: "shared-general"}) ||
		!slices.Equal(cr.Spec.StorageClasses, []string{"standard"}) || len(cr.Spec.AccessBindings) != 2 {
		t.Fatalf("CR %s", payload)
	}
	for _, b := range cr.Spec.AccessBindings {
		if (b.Role == v1.RoleTenantOwner && b.GroupSubject != machine(admins)) || (b.Role == v1.RoleDeveloper && b.GroupSubject != machine(devs)) {
			t.Fatalf("binding %+v", b)
		}
	}

	// Rows: the Namespace waits for the cluster; the bound groups' members
	// are due for a TenantUsers recount; the audit trail has it.
	if n := count(t, db, `SELECT count(*) FROM workspace_namespaces WHERE workspace_id = $1 AND name = 'zeta-solar-frontend'
		AND source = 'portal' AND state = 'Desired'`, ws.Id); n != 1 {
		t.Fatal("namespace row missing")
	}
	if n := count(t, db, `SELECT count(*) FROM group_members WHERE group_id = ANY($1) AND sync_status = 'Pending'`, []string{admins, devs}); n != 2 {
		t.Fatalf("%d bound members due for sync, want zeta and bob", n)
	}
	if trail := auditTrail(t, db, "workspace.create"); len(trail) != 1 || trail[0].TargetID != ws.Id {
		t.Fatalf("audit %+v", trail)
	}

	// A retry with the same key gets the same answer and creates nothing.
	again := asAdmin(h, owner, http.MethodPost, "/api/v1/organizations/zeta/workspaces", key, body)
	if again.Code != 202 || again.Body.String() != rec.Body.String() || count(t, db, `SELECT count(*) FROM workspaces`) != 1 {
		t.Fatalf("replay: %d %s", again.Code, again.Body)
	}
}

func TestCreateWorkspaceRefusals(t *testing.T) {
	h, db, _, owner, admins, devs := workspaceOrg(t)
	ctx := context.Background()
	if rec := createWorkspace(h, owner, "zeta", workspaceBody("solar", admins, devs, "")); rec.Code != 202 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	if _, err := db.Exec(ctx, `INSERT INTO groups (id, org_id, display_name, machine_name, sync_status)
		VALUES ('grp-acme', 'org-acme', 'x', 'org_acme:group_x', 'Synced')`); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, token, org, body string
		status                 int
		code                   oapi.ErrorCode
	}{
		{"nests under solar", owner, "zeta", workspaceBody("solar-prod", admins, devs, ""), 409, oapi.SLUGTAKEN},
		{"same slug", owner, "zeta", workspaceBody("solar", admins, devs, ""), 409, oapi.SLUGTAKEN},
		{"no TenantOwner", owner, "zeta", workspaceBody("wind", devs, admins, `"bindings":[{"groupId":"`+devs+`","role":"Developer"}]`), 409, oapi.LASTOWNERBINDING},
		{"group of another organization", owner, "zeta", workspaceBody("wind", "grp-acme", devs, ""), 400, oapi.VALIDATIONFAILED},
		{"group twice", owner, "zeta", workspaceBody("wind", admins, admins, ""), 400, oapi.VALIDATIONFAILED},
		{"cluster not granted", owner, "zeta", workspaceBody("wind", admins, devs, `"clusterId":"cluster-b"`), 409, oapi.CLUSTERNOTGRANTED},
		{"unknown cluster", owner, "zeta", workspaceBody("wind", admins, devs, `"clusterId":"nowhere"`), 409, oapi.CLUSTERNOTGRANTED},
		{"pool outside the grant", owner, "zeta", workspaceBody("wind", admins, devs, `"compute":{"mode":"Shared","sharedPoolId":"shared-b"}`), 400, oapi.VALIDATIONFAILED},
		{"Dedicated before M8", owner, "zeta", workspaceBody("wind", admins, devs, `"compute":{"mode":"Dedicated","dedicatedNodeGrantId":"nodegrant-x"}`), 400, oapi.VALIDATIONFAILED},
		{"limits under requests", owner, "zeta", workspaceBody("wind", admins, devs,
			`"resources":{"requestsCpu":"2","requestsMemory":"2Gi","limitsCpu":"1","limitsMemory":"4Gi","pods":10,"requestsStorage":"5Gi","namespaceQuota":2}`), 400, oapi.VALIDATIONFAILED},
		{"more namespaces than the quota", owner, "zeta", workspaceBody("wind", admins, devs, `"namespaces":["a","b","c"]`), 400, oapi.VALIDATIONFAILED},
		{"namespace name over 63 characters", owner, "zeta", workspaceBody(strings.Repeat("w", 20), admins, devs, `"namespaces":["`+strings.Repeat("a", 42)+`"]`), 400, oapi.VALIDATIONFAILED},
		{"OrgMember", sessionOf("usr-bob"), "zeta", workspaceBody("wind", admins, devs, ""), 403, oapi.FORBIDDEN},
		{"non-member", sessionOf("usr-dave"), "zeta", workspaceBody("wind", admins, devs, ""), 404, oapi.NOTFOUND},
		{"organization not approved", sessionOf("usr-bob"), "org-beta2", workspaceBody("wind", admins, devs, ""), 409, oapi.ORGNOTACTIVE},
	}
	for _, c := range cases {
		rec := createWorkspace(h, c.token, c.org, c.body)
		if rec.Code != c.status || errorBody(t, rec).Code != c.code {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body)
		}
	}
	// "sol" does not nest with "solar": nesting is at a hyphen.
	if rec := createWorkspace(h, owner, "zeta", workspaceBody("sol", admins, devs, `"namespaces":[]`)); rec.Code != 202 {
		t.Fatalf("sol: %d %s", rec.Code, rec.Body)
	}

	// A group not yet in Keycloak, and a cluster that left Ready.
	if _, err := db.Exec(ctx, `UPDATE groups SET sync_status = 'Pending' WHERE id = $1`, devs); err != nil {
		t.Fatal(err)
	}
	rec := createWorkspace(h, owner, "zeta", workspaceBody("wind", admins, devs, ""))
	if e := errorBody(t, rec); rec.Code != 409 || e.Code != oapi.GROUPSYNCPENDING || (*e.Details)["groupId"] != devs {
		t.Fatalf("pending group: %d %s", rec.Code, rec.Body)
	}
	if _, err := db.Exec(ctx, `UPDATE groups SET sync_status = 'Synced' WHERE id = $1;`, devs); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE clusters SET status = 'Maintenance' WHERE id = 'cluster-a'`); err != nil {
		t.Fatal(err)
	}
	if rec := createWorkspace(h, owner, "zeta", workspaceBody("wind", admins, devs, "")); errorBody(t, rec).Code != oapi.CLUSTERNOTREADY {
		t.Fatalf("cluster in Maintenance: %d %s", rec.Code, rec.Body)
	}
	if n := count(t, db, `SELECT count(*) FROM workspaces`); n != 2 {
		t.Fatalf("%d workspaces, want solar and sol", n)
	}
}

// The OrgOwner and OrgAdmins see every Workspace; an OrgMember only those a
// group of theirs is bound to; anyone else none (architecture/06 §8).
func TestWorkspaceVisibility(t *testing.T) {
	h, _, _, owner, admins, devs := workspaceOrg(t)
	ids := map[string]string{}
	for slug, body := range map[string]string{
		"solar": workspaceBody("solar", admins, devs, ""),
		"wind":  workspaceBody("wind", admins, devs, `"bindings":[{"groupId":"`+admins+`","role":"TenantOwner"}]`),
	} {
		rec := createWorkspace(h, owner, "zeta", body)
		var got oapi.WorkspaceAccepted
		if rec.Code != 202 || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
			t.Fatalf("create %s: %d %s", slug, rec.Code, rec.Body)
		}
		ids[slug] = got.Workspace.Id
	}
	list := func(token string) (int, []string) {
		rec := asAdmin(h, token, http.MethodGet, "/api/v1/organizations/zeta/workspaces", "", "")
		var l oapi.WorkspaceList
		_ = json.Unmarshal(rec.Body.Bytes(), &l)
		var slugs []string
		for _, w := range l.Items {
			slugs = append(slugs, w.Slug)
		}
		slices.Sort(slugs)
		return rec.Code, slugs
	}
	for _, c := range []struct {
		token string
		code  int
		slugs []string
	}{
		{owner, 200, []string{"solar", "wind"}},
		{sessionOf("usr-erin"), 200, []string{"solar", "wind"}}, // OrgAdmin, in no group
		{sessionOf("usr-bob"), 200, []string{"solar"}},          // developers: bound to solar only
		{sessionOf("usr-dave"), 404, nil},
	} {
		if code, slugs := list(c.token); code != c.code || !slices.Equal(slugs, c.slugs) {
			t.Errorf("list: %d %v, want %d %v", code, slugs, c.code, c.slugs)
		}
	}
	get := func(token, slug string) int {
		return asAdmin(h, token, http.MethodGet, "/api/v1/workspaces/"+ids[slug], "", "").Code
	}
	if get(sessionOf("usr-bob"), "solar") != 200 || get(sessionOf("usr-bob"), "wind") != 404 ||
		get(sessionOf("usr-erin"), "wind") != 200 || get(sessionOf("usr-dave"), "solar") != 404 ||
		asAdmin(h, owner, http.MethodGet, "/api/v1/workspaces/ws-nope", "", "").Code != 404 {
		t.Fatal("detail visibility")
	}
}
