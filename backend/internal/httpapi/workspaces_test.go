package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi"
	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
	"github.com/thaihoc1310/tenantry/backend/internal/workspace"
	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"
)

// workspaceOrg is staffedOrg with its three groups already in Keycloak and
// the nodes of nodesSeed (shared-general: 5 CPU reservable, ×3). It returns
// the ids of the groups admins and developers.
func workspaceOrg(t *testing.T) (h http.Handler, db *pgxpool.Pool, orgID, owner, admins, devs string) {
	t.Helper()
	h, db, orgID, owner = staffedOrg(t)
	if _, err := db.Exec(context.Background(), `UPDATE groups SET sync_status = 'Synced' WHERE org_id = $1`, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(context.Background(), nodesSeed); err != nil {
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
	h, db, orgID, owner, admins, devs := workspaceOrg(t)
	ctx := context.Background()
	// Room for every Workspace below: the grant's limits have their own test.
	if _, err := db.Exec(ctx, `UPDATE organization_cluster_grants SET max_workspaces = 5, requests_cpu_milli = 8000,
		limits_cpu_milli = 8000, requests_memory_bytes = 34359738368, limits_memory_bytes = 34359738368, pods = 100 WHERE org_id = $1`, orgID); err != nil {
		t.Fatal(err)
	}
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

// settle plays the worker: the Workspace's operations succeeded and it is Ready.
func settle(t *testing.T, db *pgxpool.Pool, wsID string) {
	t.Helper()
	for _, q := range []string{
		`UPDATE operations SET state = 'Succeeded', finished_at = now() WHERE resource_id = $1 AND state <> 'Succeeded'`,
		`UPDATE workspaces SET state = 'Ready', observed_generation = desired_generation WHERE id = $1`,
	} {
		if _, err := db.Exec(context.Background(), q, wsID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWorkspaceBindings(t *testing.T) {
	h, db, orgID, owner, admins, devs := workspaceOrg(t)
	viewers := groupID(t, db, orgID, "viewers")
	rec := createWorkspace(h, owner, "zeta", workspaceBody("solar", admins, devs, ""))
	var created oapi.WorkspaceAccepted
	if rec.Code != 202 || json.Unmarshal(rec.Body.Bytes(), &created) != nil {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	wsID := created.Workspace.Id
	bind := func(token, group, role string) *httptest.ResponseRecorder {
		return asAdmin(h, token, http.MethodPut, "/api/v1/workspaces/"+wsID+"/bindings/"+group, "", `{"role":"`+role+`"}`)
	}
	unbind := func(token, group string) *httptest.ResponseRecorder {
		return asAdmin(h, token, http.MethodDelete, "/api/v1/workspaces/"+wsID+"/bindings/"+group, "", "")
	}
	refused := func(name string, rec *httptest.ResponseRecorder, status int, code oapi.ErrorCode) {
		t.Helper()
		if rec.Code != status || errorBody(t, rec).Code != code {
			t.Fatalf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}

	// One operation at a time: not while it is being provisioned.
	refused("while Provisioning", bind(owner, viewers, "Viewer"), 409, oapi.WORKSPACENOTREADY)
	settle(t, db, wsID)

	rec = bind(owner, viewers, "Viewer")
	var change oapi.WorkspaceBindingChange
	if rec.Code != 202 || json.Unmarshal(rec.Body.Bytes(), &change) != nil || len(change.Items) != 3 ||
		change.Operation == nil || change.Operation.Type != "workspace.update" {
		t.Fatalf("bind viewers: %d %s", rec.Code, rec.Body)
	}
	var payload []byte
	var version int64
	if err := db.QueryRow(context.Background(), `SELECT payload, version FROM outbox_events WHERE operation_id = $1`, change.Operation.Id).Scan(&payload, &version); err != nil {
		t.Fatal(err)
	}
	var cr v1.PlatformWorkspace
	if err := json.Unmarshal(payload, &cr); err != nil || version != 2 || cr.Annotations[workspace.AnnotationSpecVersion] != "2" ||
		len(cr.Spec.AccessBindings) != 3 || !slices.Equal(cr.Spec.Namespaces, []string{"frontend"}) {
		t.Fatalf("CR at version %d: %s", version, payload)
	}
	if n := count(t, db, `SELECT count(*) FROM workspaces WHERE id = $1 AND state = 'Updating' AND desired_generation = 2`, wsID); n != 1 {
		t.Fatal("workspace not Updating at generation 2")
	}
	refused("while Updating", bind(owner, viewers, "Developer"), 409, oapi.WORKSPACENOTREADY)
	settle(t, db, wsID)

	// The same role again changes nothing and starts nothing.
	ops := count(t, db, `SELECT count(*) FROM operations WHERE resource_id = $1`, wsID)
	if rec := bind(owner, viewers, "Viewer"); rec.Code != 200 || count(t, db, `SELECT count(*) FROM operations WHERE resource_id = $1`, wsID) != ops {
		t.Fatalf("same role: %d %s", rec.Code, rec.Body)
	}

	// The last TenantOwner stays one.
	refused("demote the last owner", bind(owner, admins, "Developer"), 409, oapi.LASTOWNERBINDING)
	refused("remove the last owner", unbind(owner, admins), 409, oapi.LASTOWNERBINDING)
	// Groups of another organization, or none, are not found; roles need a manager.
	if _, err := db.Exec(context.Background(), `INSERT INTO groups (id, org_id, display_name, machine_name, sync_status)
		VALUES ('grp-acme', 'org-acme', 'x', 'org_acme:group_x', 'Synced')`); err != nil {
		t.Fatal(err)
	}
	refused("group of another organization", bind(owner, "grp-acme", "Viewer"), 404, oapi.NOTFOUND)
	refused("no such group", bind(owner, "grp-nope", "Viewer"), 404, oapi.NOTFOUND)
	refused("OrgMember", bind(sessionOf("usr-bob"), viewers, "Developer"), 403, oapi.FORBIDDEN)
	refused("non-member", bind(sessionOf("usr-dave"), viewers, "Developer"), 404, oapi.NOTFOUND)

	// A group with a role cannot be deleted; without one it can.
	rec = asAdmin(h, owner, http.MethodDelete, "/api/v1/groups/"+viewers, "", "")
	if e := errorBody(t, rec); rec.Code != 409 || e.Code != oapi.CONFLICT || fmt.Sprint((*e.Details)["workspaceIds"]) != "["+wsID+"]" {
		t.Fatalf("delete a bound group: %d %s", rec.Code, rec.Body)
	}

	// bob sees the bindings through developers, then loses the Workspace with them.
	if rec := asAdmin(h, sessionOf("usr-bob"), http.MethodGet, "/api/v1/workspaces/"+wsID+"/bindings", "", ""); rec.Code != 200 {
		t.Fatalf("bob lists bindings: %d", rec.Code)
	}
	if rec := unbind(owner, devs); rec.Code != 202 {
		t.Fatalf("unbind developers: %d %s", rec.Code, rec.Body)
	}
	if n := count(t, db, `SELECT count(*) FROM group_members WHERE group_id = $1 AND user_id = 'usr-bob' AND sync_status = 'Pending'`, devs); n != 1 {
		t.Fatal("bob is not due for a TenantUsers recount")
	}
	settle(t, db, wsID)
	if rec := unbind(owner, devs); rec.Code != 204 {
		t.Fatalf("unbind again: %d %s", rec.Code, rec.Body)
	}
	if rec := asAdmin(h, sessionOf("usr-bob"), http.MethodGet, "/api/v1/workspaces/"+wsID+"/bindings", "", ""); rec.Code != 404 {
		t.Fatalf("bob still sees the Workspace: %d", rec.Code)
	}
	if rec := unbind(owner, viewers); rec.Code != 202 {
		t.Fatalf("unbind viewers: %d %s", rec.Code, rec.Body)
	}
	if rec := asAdmin(h, owner, http.MethodDelete, "/api/v1/groups/"+viewers, "", ""); rec.Code != 204 {
		t.Fatalf("delete an unbound group: %d %s", rec.Code, rec.Body)
	}
	if trail := auditTrail(t, db, "workspace_binding.delete"); len(trail) != 2 {
		t.Fatalf("audit %+v", trail)
	}
}

func TestWorkspaceNamespaces(t *testing.T) {
	h, db, orgID, owner, admins, devs := workspaceOrg(t)
	rec := createWorkspace(h, owner, "zeta", workspaceBody("solar", admins, devs, ""))
	var created oapi.WorkspaceAccepted
	if rec.Code != 202 || json.Unmarshal(rec.Body.Bytes(), &created) != nil {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	wsID := created.Workspace.Id
	path := "/api/v1/workspaces/" + wsID + "/namespaces"
	create := func(token, name string) *httptest.ResponseRecorder {
		return asAdmin(h, token, http.MethodPost, path, newKey(), `{"name":"`+name+`"}`)
	}
	lastCR := func() v1.PlatformWorkspace {
		t.Helper()
		var payload []byte
		if err := db.QueryRow(context.Background(), `SELECT payload FROM outbox_events ORDER BY id DESC LIMIT 1`).Scan(&payload); err != nil {
			t.Fatal(err)
		}
		var cr v1.PlatformWorkspace
		if err := json.Unmarshal(payload, &cr); err != nil {
			t.Fatal(err)
		}
		return cr
	}

	if rec := create(owner, "backend"); rec.Code != 409 || errorBody(t, rec).Code != oapi.WORKSPACENOTREADY {
		t.Fatalf("while Provisioning: %d %s", rec.Code, rec.Body)
	}
	settle(t, db, wsID)
	// bob is a Developer: he sees the Namespaces, he does not change them.
	if rec := asAdmin(h, sessionOf("usr-bob"), http.MethodGet, path, "", ""); rec.Code != 200 {
		t.Fatalf("bob lists: %d", rec.Code)
	}
	if rec := create(sessionOf("usr-bob"), "backend"); rec.Code != 403 {
		t.Fatalf("Developer creates: %d %s", rec.Code, rec.Body)
	}
	// Once his group holds TenantOwner, he does.
	if rec := asAdmin(h, owner, http.MethodPut, "/api/v1/workspaces/"+wsID+"/bindings/"+devs, "", `{"role":"TenantOwner"}`); rec.Code != 202 {
		t.Fatalf("make developers owners: %d %s", rec.Code, rec.Body)
	}
	settle(t, db, wsID)
	rec = create(sessionOf("usr-bob"), "backend")
	var change oapi.WorkspaceNamespaceChange
	if rec.Code != 202 || json.Unmarshal(rec.Body.Bytes(), &change) != nil || change.Namespace.Name != "zeta-solar-backend" ||
		change.Namespace.State != oapi.WorkspaceNamespaceStateDesired || change.Operation.Type != "namespace.create" {
		t.Fatalf("create backend: %d %s", rec.Code, rec.Body)
	}
	if cr := lastCR(); !slices.Equal(cr.Spec.Namespaces, []string{"backend", "frontend"}) || cr.Annotations[workspace.AnnotationSpecVersion] != "3" {
		t.Fatalf("CR namespaces %v at version %s", cr.Spec.Namespaces, cr.Annotations[workspace.AnnotationSpecVersion])
	}
	settle(t, db, wsID)

	// namespaceQuota is 2, and a Namespace made with kubectl counts too.
	if rec := create(owner, "third"); rec.Code != 409 || (*errorBody(t, rec).Details)["limit"] != "namespaceQuota" {
		t.Fatalf("over the quota: %d %s", rec.Code, rec.Body)
	}
	if _, err := db.Exec(context.Background(), `UPDATE workspaces SET namespace_quota = 4 WHERE id = $1`, wsID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(context.Background(), `INSERT INTO workspace_namespaces (id, workspace_id, cluster_id, name, source, state)
		VALUES ('ns-kubectl', $1, 'cluster-a', 'zeta-solar-scratch', 'kubectl', 'Active')`, wsID); err != nil {
		t.Fatal(err)
	}
	if rec := create(owner, "scratch"); rec.Code != 409 || (*errorBody(t, rec).Details)["field"] != "name" {
		t.Fatalf("name taken: %d %s", rec.Code, rec.Body)
	}
	if rec := create(owner, strings.Repeat("n", 42)); rec.Code != 202 {
		t.Fatalf("a 53-character name: %d %s", rec.Code, rec.Body)
	}
	settle(t, db, wsID)
	if rec := create(owner, "fifth"); rec.Code != 409 {
		t.Fatalf("4 of 4 with the kubectl one: %d %s", rec.Code, rec.Body)
	}

	// Deleting the kubectl Namespace puts its full name in namespacesToDelete.
	rec = asAdmin(h, owner, http.MethodDelete, path+"/zeta-solar-scratch", "", "")
	if rec.Code != 202 || json.Unmarshal(rec.Body.Bytes(), &change) != nil || change.Namespace.State != oapi.WorkspaceNamespaceStateDeleting ||
		change.Operation.Type != "namespace.delete" {
		t.Fatalf("delete scratch: %d %s", rec.Code, rec.Body)
	}
	if cr := lastCR(); !slices.Equal(cr.Spec.NamespacesToDelete, []string{"zeta-solar-scratch"}) || slices.Contains(cr.Spec.Namespaces, "scratch") {
		t.Fatalf("CR after delete: %+v", cr.Spec)
	}
	settle(t, db, wsID)
	if rec := asAdmin(h, owner, http.MethodDelete, path+"/zeta-solar-nope", "", ""); rec.Code != 404 {
		t.Fatalf("delete unknown: %d", rec.Code)
	}
	var list oapi.WorkspaceNamespaceList
	rec = asAdmin(h, owner, http.MethodGet, path, "", "")
	if json.Unmarshal(rec.Body.Bytes(), &list) != nil || len(list.Items) != 4 {
		t.Fatalf("list: %s", rec.Body)
	}
	if trail := auditTrail(t, db, "namespace.create"); len(trail) != 2 || trail[0].Org != orgID {
		t.Fatalf("audit %+v", trail)
	}
}

// /usage counts the Workspaces not Deleted and what they hold of their grant;
// the entitlement cannot go below them (PRODUCT-PLAN §5.6).
func TestUsageCountsWorkspaces(t *testing.T) {
	h, db, orgID, owner, admins, devs := workspaceOrg(t)
	for _, slug := range []string{"solar", "wind"} {
		if rec := createWorkspace(h, owner, "zeta", workspaceBody(slug, admins, devs, "")); rec.Code != 202 {
			t.Fatalf("create %s: %d %s", slug, rec.Code, rec.Body)
		}
	}
	if _, err := db.Exec(context.Background(), `UPDATE workspaces SET state = 'Deleted' WHERE slug = 'wind'`); err != nil {
		t.Fatal(err)
	}
	rec := asAdmin(h, owner, http.MethodGet, "/api/v1/organizations/zeta/usage", "", "")
	var u oapi.OrganizationUsage
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &u) != nil || len(u.ClusterGrants) != 1 {
		t.Fatalf("usage: %d %s", rec.Code, rec.Body)
	}
	g := u.ClusterGrants[0]
	if u.Organization.Workspaces.Used != 1 || g.Workspaces.Allocated != 1 || g.Namespaces.Allocated != 2 ||
		g.RequestsCpu.Allocated != "1" || g.RequestsCpu.Remaining != "1" || g.LimitsMemory.Allocated != "4Gi" ||
		g.Pods.Allocated != 10 || g.RequestsStorage.Allocated != "5Gi" {
		t.Fatalf("usage %s", rec.Body)
	}
	admin := newSession(t, db, "usr-alice", "platform-admin")
	rec = asAdmin(h, admin, http.MethodPut, "/api/v1/admin/organizations/"+orgID+"/entitlement", "",
		`{"maxMembers":20,"maxGroups":10,"maxWorkspacesTotal":0}`)
	if e := errorBody(t, rec); rec.Code != 409 || e.Code != oapi.ENTITLEMENTBELOWUSAGE || (*e.Details)["limit"] != "maxWorkspacesTotal" {
		t.Fatalf("entitlement below one Workspace: %d %s", rec.Code, rec.Body)
	}
}

// A create takes its share of the grant under the grant's lock: two creates
// that each fit alone but not together end with exactly one Workspace
// (PRODUCT-PLAN §5.3); the entitlement's maxWorkspacesTotal counts too.
func TestCreateWorkspaceReservesTheGrant(t *testing.T) {
	h, db, orgID, owner, admins, devs := workspaceOrg(t)
	big := `"resources":{"requestsCpu":"1500m","requestsMemory":"1Gi","limitsCpu":"2","limitsMemory":"2Gi","pods":5,"requestsStorage":"1Gi","namespaceQuota":1},"namespaces":[]`
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for _, slug := range []string{"solar", "wind"} {
		wg.Go(func() { codes <- createWorkspace(h, owner, "zeta", workspaceBody(slug, admins, devs, big)).Code })
	}
	wg.Wait()
	close(codes)
	var got []int
	for c := range codes {
		got = append(got, c)
	}
	slices.Sort(got)
	if !slices.Equal(got, []int{202, 409}) || count(t, db, `SELECT count(*) FROM workspaces`) != 1 {
		t.Fatalf("two creates over the grant's CPU: %v", got)
	}
	rec := createWorkspace(h, owner, "zeta", workspaceBody("water", admins, devs, big))
	if e := errorBody(t, rec); rec.Code != 409 || e.Code != oapi.CLUSTERGRANTQUOTAEXCEEDED || (*e.Details)["resource"] != "requestsCpu" ||
		(*e.Details)["limit"] != "2" || (*e.Details)["allocated"] != "1500m" || (*e.Details)["requested"] != "1500m" {
		t.Fatalf("over the grant: %d %s", rec.Code, rec.Body)
	}
	// The grant allows two Workspaces; the entitlement then only one.
	small := `"resources":{"requestsCpu":"100m","requestsMemory":"128Mi","limitsCpu":"100m","limitsMemory":"128Mi","pods":1,"requestsStorage":"1Gi","namespaceQuota":1},"namespaces":[]`
	if rec := createWorkspace(h, owner, "zeta", workspaceBody("water", admins, devs, small)); rec.Code != 202 {
		t.Fatalf("a small second: %d %s", rec.Code, rec.Body)
	}
	if rec := createWorkspace(h, owner, "zeta", workspaceBody("earth", admins, devs, small)); errorBody(t, rec).Code != oapi.CLUSTERGRANTQUOTAEXCEEDED ||
		(*errorBody(t, rec).Details)["resource"] != "workspaces" {
		t.Fatalf("a third on a grant of two: %d %s", rec.Code, rec.Body)
	}
	if _, err := db.Exec(context.Background(), `UPDATE organization_entitlements SET max_workspaces_total = 2 WHERE org_id = $1;`, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(context.Background(), `UPDATE organization_cluster_grants SET max_workspaces = 5 WHERE org_id = $1`, orgID); err != nil {
		t.Fatal(err)
	}
	if rec := createWorkspace(h, owner, "zeta", workspaceBody("earth", admins, devs, small)); errorBody(t, rec).Code != oapi.ENTITLEMENTEXCEEDED {
		t.Fatalf("over maxWorkspacesTotal: %d %s", rec.Code, rec.Body)
	}
}

// An Admin cannot take a grant below what Workspaces hold, nor take away a
// pool they run on, nor delete it under them (PRODUCT-PLAN §5.5).
func TestGrantGuardsWorkspaces(t *testing.T) {
	h, db, orgID, owner, admins, devs := workspaceOrg(t)
	if rec := createWorkspace(h, owner, "zeta", workspaceBody("solar", admins, devs, "")); rec.Code != 202 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	admin := newSession(t, db, "usr-alice", "platform-admin")
	put := func(body string) *httptest.ResponseRecorder {
		return asAdmin(h, admin, http.MethodPut, "/api/v1/admin/organizations/"+orgID+"/cluster-grants/cluster-a", "", body)
	}
	grant := func(cpu, pools string) string {
		return `{"clusterId":"cluster-a","maxWorkspaces":2,"maxNamespacesTotal":6,
"resources":{"requestsCpu":"` + cpu + `","requestsMemory":"4Gi","limitsCpu":"4","limitsMemory":"8Gi","pods":20,"requestsStorage":"20Gi"},
"sharedPoolIds":` + pools + `,"networkPoolIds":["onboarding-pod-network"],"allowedStorageClasses":["standard"]}`
	}
	if rec := put(grant("500m", `["shared-general"]`)); rec.Code != 409 || errorBody(t, rec).Code != oapi.CLUSTERGRANTBELOWALLOCATION ||
		(*errorBody(t, rec).Details)["minimum"] != "1" {
		t.Fatalf("below what solar holds: %d %s", rec.Code, rec.Body)
	}
	if rec := put(grant("2", `[]`)); rec.Code != 409 || (*errorBody(t, rec).Details)["sharedPoolId"] != "shared-general" {
		t.Fatalf("the pool solar runs on: %d %s", rec.Code, rec.Body)
	}
	if rec := put(grant("1", `["shared-general"]`)); rec.Code != 200 {
		t.Fatalf("down to exactly what is held: %d %s", rec.Code, rec.Body)
	}
	rec := asAdmin(h, admin, http.MethodDelete, "/api/v1/admin/organizations/"+orgID+"/cluster-grants/cluster-a", "", "")
	if rec.Code != 409 || (*errorBody(t, rec).Details)["workspaces"] != 1.0 {
		t.Fatalf("delete under a Workspace: %d %s", rec.Code, rec.Body)
	}
}

func TestPatchWorkspace(t *testing.T) {
	h, db, orgID, owner, admins, devs := workspaceOrg(t)
	ctx := context.Background()
	rec := createWorkspace(h, owner, "zeta", workspaceBody("solar", admins, devs, ""))
	var created oapi.WorkspaceAccepted
	if rec.Code != 202 || json.Unmarshal(rec.Body.Bytes(), &created) != nil {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	wsID := created.Workspace.Id
	patch := func(token, body string) *httptest.ResponseRecorder {
		return asAdmin(h, token, http.MethodPatch, "/api/v1/workspaces/"+wsID, "", body)
	}
	resources := func(cpu, nsQuota string) string {
		return `{"resources":{"requestsCpu":"` + cpu + `","requestsMemory":"2Gi","limitsCpu":"4","limitsMemory":"4Gi","pods":10,"requestsStorage":"5Gi","namespaceQuota":` + nsQuota + `}}`
	}
	refused := func(name string, rec *httptest.ResponseRecorder, status int, code oapi.ErrorCode) {
		t.Helper()
		if rec.Code != status || errorBody(t, rec).Code != code {
			t.Fatalf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	refused("while Provisioning", patch(owner, `{"displayName":"Sun"}`), 409, oapi.WORKSPACENOTREADY)
	settle(t, db, wsID)
	refused("OrgMember", patch(sessionOf("usr-bob"), `{"displayName":"Sun"}`), 403, oapi.FORBIDDEN)
	refused("non-member", patch(sessionOf("usr-dave"), `{"displayName":"Sun"}`), 404, oapi.NOTFOUND)

	rec = patch(owner, `{"displayName":"  Sun "}`)
	var change oapi.WorkspaceChange
	if rec.Code != 202 || json.Unmarshal(rec.Body.Bytes(), &change) != nil || change.Workspace.DisplayName != "Sun" ||
		change.Workspace.State != oapi.WorkspaceStateUpdating || change.Operation == nil || change.Operation.Type != "workspace.update" {
		t.Fatalf("rename: %d %s", rec.Code, rec.Body)
	}
	settle(t, db, wsID)
	if rec := patch(owner, `{"displayName":"Sun"}`); rec.Code != 200 || strings.Contains(rec.Body.String(), `"operation"`) {
		t.Fatalf("same name: %d %s", rec.Code, rec.Body)
	}

	// The grant has 2 CPU of requests: 1 is held, 2 asks 1 more and fits; 3 does not.
	if rec := patch(owner, resources("3", "2")); rec.Code != 409 || (*errorBody(t, rec).Details)["resource"] != "requestsCpu" {
		t.Fatalf("over the grant: %d %s", rec.Code, rec.Body)
	}
	if rec := patch(owner, resources("2", "2")); rec.Code != 202 {
		t.Fatalf("grow within the grant: %d %s", rec.Code, rec.Body)
	}
	var payload []byte
	if err := db.QueryRow(ctx, `SELECT payload FROM outbox_events ORDER BY id DESC LIMIT 1`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var cr v1.PlatformWorkspace
	if q := cr.Spec.Resources["requests.cpu"]; json.Unmarshal(payload, &cr) != nil || cr.Spec.DisplayName != "Sun" {
		t.Fatalf("CR %s (%v)", payload, q)
	}
	if q := cr.Spec.Resources["requests.cpu"]; q.String() != "2" {
		t.Fatalf("CR requests.cpu %s", q.String())
	}
	settle(t, db, wsID)

	// Not below what the Pods use, nor below the Namespaces it has.
	if _, err := db.Exec(ctx, `UPDATE workspaces SET usage_snapshot = '{"requests.cpu": "800m", "pods": "4"}' WHERE id = $1`, wsID); err != nil {
		t.Fatal(err)
	}
	rec = patch(owner, resources("500m", "2"))
	if e := errorBody(t, rec); rec.Code != 409 || e.Code != oapi.WORKSPACEQUOTABELOWUSAGE || (*e.Details)["field"] != "resources.requestsCpu" ||
		(*e.Details)["used"] != "800m" {
		t.Fatalf("below usage: %d %s", rec.Code, rec.Body)
	}
	if _, err := db.Exec(ctx, `INSERT INTO workspace_namespaces (id, workspace_id, cluster_id, name, source, state)
		VALUES ('ns-x', $1, 'cluster-a', 'zeta-solar-scratch', 'kubectl', 'Active')`, wsID); err != nil {
		t.Fatal(err)
	}
	if rec := patch(owner, resources("1", "1")); errorBody(t, rec).Code != oapi.WORKSPACEQUOTABELOWUSAGE ||
		(*errorBody(t, rec).Details)["field"] != "resources.namespaceQuota" {
		t.Fatalf("namespaceQuota below 2 Namespaces: %d %s", rec.Code, rec.Body)
	}
	if rec := patch(owner, resources("1", "2")); rec.Code != 202 {
		t.Fatalf("shrink to what is used: %d %s", rec.Code, rec.Body)
	}
	settle(t, db, wsID)

	// A resize waits for the cluster; a rename does not.
	if _, err := db.Exec(ctx, `UPDATE clusters SET status = 'Maintenance' WHERE id = 'cluster-a'`); err != nil {
		t.Fatal(err)
	}
	refused("resize in Maintenance", patch(owner, resources("2", "2")), 409, oapi.CLUSTERNOTREADY)
	if rec := patch(owner, `{"displayName":"Sol"}`); rec.Code != 202 {
		t.Fatalf("rename in Maintenance: %d %s", rec.Code, rec.Body)
	}
	if trail := auditTrail(t, db, "workspace.update"); len(trail) != 4 || trail[0].Org != orgID {
		t.Fatalf("audit %d events", len(trail))
	}
}

func TestCordonWorkspace(t *testing.T) {
	h, db, _, owner, admins, devs := workspaceOrg(t)
	rec := createWorkspace(h, owner, "zeta", workspaceBody("solar", admins, devs, ""))
	var created oapi.WorkspaceAccepted
	if rec.Code != 202 || json.Unmarshal(rec.Body.Bytes(), &created) != nil {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	wsID := created.Workspace.Id
	call := func(token, verb string) *httptest.ResponseRecorder {
		return asAdmin(h, token, http.MethodPost, "/api/v1/workspaces/"+wsID+":"+verb, "", "")
	}
	if rec := call(owner, "cordon"); rec.Code != 409 {
		t.Fatalf("cordon while Provisioning: %d %s", rec.Code, rec.Body)
	}
	settle(t, db, wsID)
	if rec := call(owner, "uncordon"); rec.Code != 409 || errorBody(t, rec).Code != oapi.WORKSPACENOTREADY {
		t.Fatalf("uncordon a Ready Workspace: %d %s", rec.Code, rec.Body)
	}
	if rec := call(sessionOf("usr-bob"), "cordon"); rec.Code != 403 {
		t.Fatalf("OrgMember cordons: %d", rec.Code)
	}
	rec = call(owner, "cordon")
	var change oapi.WorkspaceChange
	if rec.Code != 202 || json.Unmarshal(rec.Body.Bytes(), &change) != nil || change.Operation.Type != "workspace.cordon" ||
		change.Workspace.State != oapi.WorkspaceStateReady {
		t.Fatalf("cordon: %d %s", rec.Code, rec.Body)
	}
	var cordoned bool
	if err := db.QueryRow(context.Background(), `SELECT (payload->'spec'->>'cordoned')::bool FROM outbox_events WHERE operation_id = $1`,
		change.Operation.Id).Scan(&cordoned); err != nil || !cordoned {
		t.Fatalf("CR not cordoned (%v)", err)
	}
	// One operation at a time.
	if rec := asAdmin(h, owner, http.MethodPatch, "/api/v1/workspaces/"+wsID, "", `{"displayName":"x"}`); rec.Code != 409 {
		t.Fatalf("patch during a cordon: %d", rec.Code)
	}
	// Once the worker records it Cordoned, uncordon works; changes wait.
	if _, err := db.Exec(context.Background(), `UPDATE operations SET state = 'Succeeded' WHERE resource_id = $1`, wsID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(context.Background(), `UPDATE workspaces SET state = 'Cordoned' WHERE id = $1`, wsID); err != nil {
		t.Fatal(err)
	}
	if rec := asAdmin(h, owner, http.MethodPatch, "/api/v1/workspaces/"+wsID, "", `{"displayName":"x"}`); rec.Code != 409 {
		t.Fatalf("patch a Cordoned Workspace: %d", rec.Code)
	}
	if rec := call(owner, "uncordon"); rec.Code != 202 || !strings.Contains(rec.Body.String(), `"workspace.uncordon"`) {
		t.Fatalf("uncordon: %d %s", rec.Code, rec.Body)
	}
	if n := count(t, db, `SELECT count(*) FROM audit_events WHERE action IN ('workspace.cordon', 'workspace.uncordon')`); n != 2 {
		t.Fatalf("%d cordon audit events", n)
	}
}

func TestDeleteWorkspace(t *testing.T) {
	h, db, _, owner, admins, devs := workspaceOrg(t)
	rec := createWorkspace(h, owner, "zeta", workspaceBody("solar", admins, devs, ""))
	var created oapi.WorkspaceAccepted
	if rec.Code != 202 || json.Unmarshal(rec.Body.Bytes(), &created) != nil {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	wsID := created.Workspace.Id
	del := func(token, confirm string) *httptest.ResponseRecorder {
		return asAdmin(h, token, http.MethodDelete, "/api/v1/workspaces/"+wsID+"?confirmName="+confirm, "", "")
	}
	if rec := del(owner, "solar"); rec.Code != 409 {
		t.Fatalf("delete while Provisioning: %d %s", rec.Code, rec.Body)
	}
	settle(t, db, wsID)
	if rec := del(owner, "sol"); rec.Code != 400 || (*errorBody(t, rec).Details)["field"] != "confirmName" {
		t.Fatalf("wrong confirmation: %d %s", rec.Code, rec.Body)
	}
	if rec := del(sessionOf("usr-bob"), "solar"); rec.Code != 403 {
		t.Fatalf("OrgMember deletes: %d", rec.Code)
	}
	rec = del(owner, "solar")
	var change oapi.WorkspaceChange
	if rec.Code != 202 || json.Unmarshal(rec.Body.Bytes(), &change) != nil || change.Workspace.State != oapi.WorkspaceStateDeleting ||
		change.Operation.Type != "workspace.delete" {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	if n := count(t, db, `SELECT count(*) FROM outbox_events WHERE operation_id = $1 AND type = 'workspace.delete' AND payload->>'name' = 'zeta-solar'`,
		change.Operation.Id); n != 1 {
		t.Fatal("no delete event")
	}
	// Still held until the cluster is clean; nothing else meanwhile.
	if rec := asAdmin(h, owner, http.MethodGet, "/api/v1/organizations/zeta/usage", "", ""); !strings.Contains(rec.Body.String(), `"workspaces":{"allocated":1`) {
		t.Fatalf("usage while Deleting: %s", rec.Body)
	}
	if rec := del(owner, "solar"); rec.Code != 409 {
		t.Fatalf("delete twice: %d", rec.Code)
	}

	// Blocked: OrgOwner retries; only a Platform Admin forces.
	if _, err := db.Exec(context.Background(), `UPDATE operations SET state = 'Blocked', error_code = 'BLOCKED_BY_FINALIZER', error_message = 'pvc data held' WHERE id = $1`,
		change.Operation.Id); err != nil {
		t.Fatal(err)
	}
	force := func(token string) *httptest.ResponseRecorder {
		return asAdmin(h, token, http.MethodPost, "/api/v1/operations/"+change.Operation.Id+":force", "", "")
	}
	if rec := force(owner); rec.Code != 403 {
		t.Fatalf("OrgOwner forces: %d", rec.Code)
	}
	if rec := asAdmin(h, owner, http.MethodPost, "/api/v1/operations/"+change.Operation.Id+":retry", "", ""); rec.Code != 200 ||
		!strings.Contains(rec.Body.String(), `"state":"Running"`) {
		t.Fatalf("retry a Blocked delete: %d %s", rec.Code, rec.Body)
	}
	if _, err := db.Exec(context.Background(), `UPDATE operations SET state = 'Blocked' WHERE id = $1`, change.Operation.Id); err != nil {
		t.Fatal(err)
	}
	admin := newSession(t, db, "usr-alice", "platform-admin") // not a member of zeta
	if rec := force(admin); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"state":"Running"`) {
		t.Fatalf("force: %d %s", rec.Code, rec.Body)
	}
	if n := count(t, db, `SELECT count(*) FROM outbox_events WHERE operation_id = $1 AND type = 'workspace.force'`, change.Operation.Id); n != 1 {
		t.Fatal("no force event")
	}
	if trail := auditTrail(t, db, "operation.force"); len(trail) != 1 || trail[0].Actor != "usr-alice" {
		t.Fatalf("force audit %+v", trail)
	}
	if rec := force(admin); rec.Code != 409 {
		t.Fatalf("force a Running delete: %d", rec.Code)
	}
}

// The OrgOwner deletes an organization once nothing is left in it; after,
// it is 404 everywhere and its groups leave Keycloak (04-state-machines §11).
func TestDeleteOrganization(t *testing.T) {
	h, db, orgID, owner, admins, devs := workspaceOrg(t)
	ctx := context.Background()
	rec := createWorkspace(h, owner, "zeta", workspaceBody("solar", admins, devs, ""))
	var created oapi.WorkspaceAccepted
	if rec.Code != 202 || json.Unmarshal(rec.Body.Bytes(), &created) != nil {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	del := func(token string) *httptest.ResponseRecorder {
		return asAdmin(h, token, http.MethodDelete, "/api/v1/organizations/zeta", "", "")
	}
	if rec := del(sessionOf("usr-erin")); rec.Code != 403 {
		t.Fatalf("OrgAdmin deletes: %d", rec.Code)
	}
	if rec := del(sessionOf("usr-dave")); rec.Code != 404 {
		t.Fatalf("non-member deletes: %d", rec.Code)
	}
	if rec := del(owner); rec.Code != 409 || errorBody(t, rec).Code != oapi.ORGANIZATIONNOTEMPTY || (*errorBody(t, rec).Details)["workspaces"] != 1.0 {
		t.Fatalf("with a Workspace: %d %s", rec.Code, rec.Body)
	}
	if _, err := db.Exec(ctx, `DELETE FROM workspaces WHERE id = $1`, created.Workspace.Id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO invitations (id, org_id, email, role, token_hash, invited_by, expires_at)
		VALUES ('inv-x', $1, 'x@example.com', 'OrgMember', decode(repeat('ab', 32), 'hex'), 'usr-zeta', now() + interval '1 day')`, orgID); err != nil {
		t.Fatal(err)
	}
	if rec := del(owner); rec.Code != 204 {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	for q, want := range map[string]int{
		`SELECT count(*) FROM organizations WHERE id = $1 AND status = 'Deleted'`:                                                 1,
		`SELECT count(*) FROM organization_cluster_grants WHERE org_id = $1`:                                                      0,
		`SELECT count(*) FROM invitations WHERE org_id = $1 AND state = 'Pending'`:                                                0,
		`SELECT count(*) FROM groups WHERE org_id = $1 AND deleted_at IS NULL`:                                                    0,
		`SELECT count(*) FROM group_members gm JOIN groups g ON g.id = gm.group_id WHERE g.org_id = $1 AND gm.deleted_at IS NULL`: 0,
	} {
		if n := count(t, db, q, orgID); n != want {
			t.Errorf("%s: %d, want %d", q, n, want)
		}
	}
	if rec := asAdmin(h, owner, http.MethodGet, "/api/v1/organizations/zeta", "", ""); rec.Code != 404 {
		t.Fatalf("a Deleted organization: %d", rec.Code)
	}
	if trail := auditTrail(t, db, "organization.delete"); len(trail) != 1 {
		t.Fatalf("audit %+v", trail)
	}
}

// Changing a grant's storage classes renders every Workspace of it again:
// an idle one gets a workspace.update; a busy one the new CR under its
// operation under way (PRODUCT-PLAN §5.5).
func TestStorageClassesRenderEveryWorkspace(t *testing.T) {
	h, db, orgID, owner, admins, devs := workspaceOrg(t)
	ids := map[string]string{}
	for _, slug := range []string{"solar", "wind"} {
		rec := createWorkspace(h, owner, "zeta", workspaceBody(slug, admins, devs, `"resources":{"requestsCpu":"500m","requestsMemory":"1Gi","limitsCpu":"1","limitsMemory":"2Gi","pods":5,"requestsStorage":"1Gi","namespaceQuota":1},"namespaces":[]`))
		var c oapi.WorkspaceAccepted
		if rec.Code != 202 || json.Unmarshal(rec.Body.Bytes(), &c) != nil {
			t.Fatalf("create %s: %d %s", slug, rec.Code, rec.Body)
		}
		ids[slug] = c.Workspace.Id
	}
	settle(t, db, ids["solar"]) // wind is still Provisioning
	admin := newSession(t, db, "usr-alice", "platform-admin")
	put := func(classes string) int {
		return asAdmin(h, admin, http.MethodPut, "/api/v1/admin/organizations/"+orgID+"/cluster-grants/cluster-a", "", `{"clusterId":"cluster-a","maxWorkspaces":2,"maxNamespacesTotal":6,
"resources":{"requestsCpu":"2","requestsMemory":"4Gi","limitsCpu":"4","limitsMemory":"8Gi","pods":20,"requestsStorage":"20Gi"},
"sharedPoolIds":["shared-general"],"networkPoolIds":["onboarding-pod-network"],"allowedStorageClasses":`+classes+`}`).Code
	}
	if put(`["standard"]`) != 200 || count(t, db, `SELECT count(*) FROM outbox_events`) != 2 {
		t.Fatal("an unchanged grant rendered something")
	}
	if code := put(`["standard","fast"]`); code != 200 {
		t.Fatalf("change classes: %d", code)
	}
	classes := func(wsID string) string {
		var c string
		if err := db.QueryRow(context.Background(), `SELECT payload->'spec'->>'storageClasses' FROM outbox_events e JOIN operations o ON o.id = e.operation_id
			WHERE o.resource_id = $1 ORDER BY e.id DESC LIMIT 1`, wsID).Scan(&c); err != nil {
			t.Fatal(err)
		}
		return c
	}
	if c := classes(ids["solar"]); c != `["standard", "fast"]` {
		t.Fatalf("solar classes %s", c)
	}
	if c := classes(ids["wind"]); c != `["standard", "fast"]` {
		t.Fatalf("wind classes %s", c)
	}
	if n := count(t, db, `SELECT count(*) FROM operations WHERE resource_id = $1 AND type = 'workspace.update'`, ids["solar"]); n != 1 {
		t.Fatal("solar has no workspace.update")
	}
	if n := count(t, db, `SELECT count(*) FROM operations WHERE resource_id = $1`, ids["wind"]); n != 1 {
		t.Fatal("busy wind got a second operation")
	}
	if n := count(t, db, `SELECT count(*) FROM workspaces WHERE id = $1 AND desired_generation = 2 AND state = 'Provisioning'`, ids["wind"]); n != 1 {
		t.Fatal("wind not at generation 2, still Provisioning")
	}
}

// Usage, the operation list and the event stream of a Workspace's operation.
func TestWorkspaceUsageOperationsEvents(t *testing.T) {
	defer httpapi.SetEventPoll(20 * time.Millisecond)()
	h, db, _, owner, admins, devs := workspaceOrg(t)
	ctx := context.Background()
	rec := createWorkspace(h, owner, "zeta", workspaceBody("solar", admins, devs, ""))
	var created oapi.WorkspaceAccepted
	if rec.Code != 202 || json.Unmarshal(rec.Body.Bytes(), &created) != nil {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	wsID, opID := created.Workspace.Id, created.Operation.Id
	if _, err := db.Exec(ctx, `UPDATE workspaces SET usage_snapshot = '{"requests.cpu": "800m", "pods": "4"}' WHERE id = $1`, wsID); err != nil {
		t.Fatal(err)
	}
	rec = asAdmin(h, sessionOf("usr-bob"), http.MethodGet, "/api/v1/workspaces/"+wsID+"/usage", "", "")
	var u oapi.WorkspaceUsage
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &u) != nil || len(u.Items) != 6 ||
		u.Items[0] != (oapi.QuotaUse{Resource: oapi.RequestsCpu, Used: "800m", Limit: "1"}) || u.Items[1].Used != "0" || len(u.Namespaces) != 1 {
		t.Fatalf("usage: %d %s", rec.Code, rec.Body)
	}
	rec = asAdmin(h, owner, http.MethodGet, "/api/v1/workspaces/"+wsID+"/operations", "", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), opID) {
		t.Fatalf("operations: %d %s", rec.Code, rec.Body)
	}
	if rec := asAdmin(h, sessionOf("usr-dave"), http.MethodGet, "/api/v1/workspaces/"+wsID+"/operations", "", ""); rec.Code != 404 {
		t.Fatalf("non-member lists operations: %d", rec.Code)
	}

	// The stream sends the operation, then each change, and ends when it is done.
	go func() {
		time.Sleep(100 * time.Millisecond)
		_, _ = db.Exec(ctx, `UPDATE operations SET state = 'Running', updated_at = now() WHERE id = $1`, opID)
		time.Sleep(100 * time.Millisecond)
		_, _ = db.Exec(ctx, `UPDATE operations SET state = 'Succeeded', updated_at = now() WHERE id = $1`, opID)
	}()
	rec = asAdmin(h, owner, http.MethodGet, "/api/v1/operations/"+opID+"/events", "", "")
	body := rec.Body.String()
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "text/event-stream" || strings.Count(body, "event: operation\n") != 3 ||
		strings.Index(body, `"state":"Pending"`) > strings.Index(body, `"state":"Running"`) ||
		strings.Index(body, `"state":"Running"`) > strings.Index(body, `"state":"Succeeded"`) {
		t.Fatalf("events: %d %q", rec.Code, body)
	}
	if rec := asAdmin(h, sessionOf("usr-dave"), http.MethodGet, "/api/v1/operations/"+opID+"/events", "", ""); rec.Code != 404 {
		t.Fatalf("non-member follows: %d", rec.Code)
	}
}

// A shared pool is shared between organizations: a create past its ceiling
// (ratio × reservable) is refused, and an Admin cannot take nodes away from
// under what its Workspaces hold (PRODUCT-PLAN §6.2).
func TestSharedPoolCeiling(t *testing.T) {
	h, db, orgID, owner, admins, devs := workspaceOrg(t)
	ctx := context.Background()
	// Room in the grant, but a pool of ratio 1 with one node: 2.5 CPU.
	for _, q := range []string{
		`UPDATE organization_cluster_grants SET requests_cpu_milli = 10000, limits_cpu_milli = 10000 WHERE org_id = '` + orgID + `'`,
		`UPDATE shared_compute_pools SET allocation_overcommit_ratio = 1 WHERE id = 'shared-general'`,
		`UPDATE cluster_nodes SET shared_pool_id = NULL WHERE node_name = 'wk3'`,
	} {
		if _, err := db.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	body := func(slug, cpu string) string {
		return workspaceBody(slug, admins, devs, `"resources":{"requestsCpu":"`+cpu+`","requestsMemory":"1Gi","limitsCpu":"`+cpu+`","limitsMemory":"1Gi","pods":5,"requestsStorage":"1Gi","namespaceQuota":1},"namespaces":[]`)
	}
	if rec := createWorkspace(h, owner, "zeta", body("solar", "2")); rec.Code != 202 {
		t.Fatalf("2 of 2.5: %d %s", rec.Code, rec.Body)
	}
	rec := createWorkspace(h, owner, "zeta", body("wind", "1"))
	if e := errorBody(t, rec); rec.Code != 409 || e.Code != oapi.COMPUTEPOOLCAPACITYEXCEEDED || (*e.Details)["resource"] != "requestsCpu" ||
		(*e.Details)["limit"] != "2500m" || (*e.Details)["allocated"] != "2" {
		t.Fatalf("past the ceiling: %d %s", rec.Code, rec.Body)
	}
	// The pool's own numbers show what is held.
	admin := newSession(t, db, "usr-alice", "platform-admin")
	rec = asAdmin(h, admin, http.MethodGet, "/api/v1/admin/clusters/cluster-a/shared-pools", "", "")
	if !strings.Contains(rec.Body.String(), `"allocated":{"cpu":"2","memory":"1Gi","pods":5}`) {
		t.Fatalf("pool allocation: %s", rec.Body)
	}
	// Taking wk2 out would leave 0 under 2 CPU held.
	rec = asAdmin(h, admin, http.MethodPut, "/api/v1/admin/clusters/cluster-a/shared-pools/shared-general", "",
		`{"name":"Shared general","allocationOvercommitRatio":1,"systemReserve":`+reserve+`,"nodes":[]}`)
	if rec.Code != 409 || errorBody(t, rec).Code != oapi.COMPUTEPOOLCAPACITYEXCEEDED {
		t.Fatalf("empty a pool in use: %d %s", rec.Code, rec.Body)
	}
}
