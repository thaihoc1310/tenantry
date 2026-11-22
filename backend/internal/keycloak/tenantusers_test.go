package keycloak

import (
	"context"
	"testing"

	"github.com/thaihoc1310/tenantry/backend/internal/org"
	"github.com/thaihoc1310/tenantry/backend/internal/workspace"
)

// A grant on a Ready cluster for the organization of setup, Ann its OrgOwner.
const workspaceSeed = `
	INSERT INTO organization_members (org_id, user_id, role) VALUES ('org-01', 'usr-ann', 'OrgOwner');
	INSERT INTO organization_entitlements (org_id, max_members, max_groups, max_workspaces_total) VALUES ('org-01', 10, 10, 5);
	INSERT INTO clusters (id, display_name, status, proxy_endpoint, console_url, apps_domain)
		VALUES ('cluster-a', 'A', 'Ready', 'https://proxy', 'https://console', 'apps.example');
	INSERT INTO shared_compute_pools (id, cluster_id, name, allocation_overcommit_ratio) VALUES ('shared-general', 'cluster-a', 'General', 2);
	INSERT INTO organization_cluster_grants (id, org_id, cluster_id, is_default, max_workspaces, max_namespaces_total,
		requests_cpu_milli, requests_memory_bytes, limits_cpu_milli, limits_memory_bytes, pods, requests_storage_bytes)
		VALUES ('grant-1', 'org-01', 'cluster-a', true, 5, 10, 4000, 8589934592, 8000, 17179869184, 50, 21474836480);
	INSERT INTO cluster_grant_shared_pools (grant_id, pool_id) VALUES ('grant-1', 'shared-general');
	INSERT INTO cluster_nodes (cluster_id, node_name, kind, ready, schedulable, allocatable_cpu_milli, allocatable_memory_bytes, allocatable_pods, shared_pool_id)
		VALUES ('cluster-a', 'wk1', 'Worker', true, true, 4000, 8589934592, 110, 'shared-general');`

// Members of a group bound to a Workspace join TenantUsers once the
// Workspace is created; members of other groups do not (PRODUCT-PLAN §9.3).
func TestTenantUsersFollowBindings(t *testing.T) {
	db, kc, w := setup(t)
	exec(t, db, workspaceSeed+`INSERT INTO group_members (group_id, user_id) VALUES ('grp-b', 'usr-bob');`)
	runOnce(t, w) // groups and memberships reach Keycloak
	if kc.isMember("kc-ann", TenantUsers) || kc.isMember("kc-bob", TenantUsers) {
		t.Fatal("in TenantUsers without a binding")
	}

	ws := workspace.NewService(db, org.NewService(db))
	created, err := ws.Create(context.Background(), "usr-ann", "acme", workspace.New{
		Slug: "solar", DisplayName: "Solar", ClusterID: "cluster-a", ComputeMode: workspace.ComputeShared,
		SharedPoolID: "shared-general",
		Resources: workspace.Resources{RequestsCPUMilli: 1000, RequestsMemoryBytes: 1 << 30, LimitsCPUMilli: 1000,
			LimitsMemoryBytes: 1 << 30, Pods: 10, RequestsStorageBytes: 1 << 30, NamespaceQuota: 2},
		Bindings: []workspace.Binding{{GroupID: "grp-a", Role: workspace.RoleTenantOwner}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rowState(t, db, "grp-a", "usr-ann") != "live:Pending" || rowState(t, db, "grp-b", "usr-bob") != "live:Synced" {
		t.Fatal("the bound group's members are not due for a recount, or another group's are")
	}
	runOnce(t, w)
	if !kc.isMember("kc-ann", TenantUsers) || kc.isMember("kc-bob", TenantUsers) {
		t.Fatalf("TenantUsers: ann %v (want true), bob %v (want false)", kc.isMember("kc-ann", TenantUsers), kc.isMember("kc-bob", TenantUsers))
	}

	// A role for group B brings Bob in; taking it away takes him out.
	ready := func() {
		exec(t, db, `UPDATE operations SET state = 'Succeeded' WHERE resource_id = $1`, created.Workspace.ID)
		exec(t, db, `UPDATE workspaces SET state = 'Ready' WHERE id = $1`, created.Workspace.ID)
	}
	ready()
	if _, err := ws.PutBinding(context.Background(), "usr-ann", created.Workspace.ID, "grp-b", workspace.RoleViewer, ""); err != nil {
		t.Fatal(err)
	}
	runOnce(t, w)
	if !kc.isMember("kc-bob", TenantUsers) {
		t.Fatal("Bob not in TenantUsers with a Viewer binding")
	}
	ready()
	if _, err := ws.DeleteBinding(context.Background(), "usr-ann", created.Workspace.ID, "grp-b", ""); err != nil {
		t.Fatal(err)
	}
	runOnce(t, w)
	if kc.isMember("kc-bob", TenantUsers) || !kc.isMember("kc-ann", TenantUsers) {
		t.Fatal("Bob still in TenantUsers without a binding, or Ann taken out")
	}
}
