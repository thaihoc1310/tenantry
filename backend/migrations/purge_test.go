package migrations_test

import (
	"context"
	"os"
	"regexp"
	"testing"

	"github.com/thaihoc1310/tenantry/backend/internal/testdb"
)

// The hard purge of test fixtures (deploy/demo/purge.sql) on a migrated
// database: e2e*/optest* organizations and accounts go with everything
// theirs; a real organization and its people stay, so do a test
// organization the cluster may still hold and a test account a kept
// organization refers to.
func TestPurgeTestData(t *testing.T) {
	script, err := os.ReadFile("../../deploy/demo/purge.sql")
	if err != nil {
		t.Fatal(err)
	}
	db := testdb.Open(t)
	ctx := context.Background()
	if _, err := provider(t, db).Up(ctx); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `
INSERT INTO users (id, issuer, subject, email) VALUES
  ('usr-admin', 'iss', 'admin', 'admin@tenantry.site'), ('usr-alice', 'iss', 'alice', 'alice@tenantry.site'),
  ('usr-e1', 'iss', 'e1', 'e2eaaa@tenantry.site'), ('usr-e2', 'iss', 'e2', 'e2ebbb@tenantry.site'),
  ('usr-o1', 'iss', 'o1', 'optest-1@tenantry.site'), ('usr-e3', 'iss', 'e3', 'e2eccc@tenantry.site');
INSERT INTO clusters (id, display_name, status, proxy_endpoint, console_url, apps_domain) VALUES
  ('cluster-a', 'Cluster A', 'Ready', 'https://kube-a.test', 'https://console-a.test', 'apps.test'),
  ('e2ereg1', 'E2E', 'Registered', 'https://kube-e2ereg1.invalid', 'https://console-e2ereg1.invalid', 'e2ereg1.invalid'),
  ('e2ereg2', 'E2E ready', 'Ready', 'https://kube-e2ereg2.invalid', 'https://console-e2ereg2.invalid', 'e2ereg2.invalid');
INSERT INTO shared_compute_pools (id, cluster_id, name, allocation_overcommit_ratio) VALUES ('shared-general', 'cluster-a', 'General', 3);
INSERT INTO network_pools (id, cluster_id, cidrs, default_prefix_length, allowed_prefix_lengths)
  VALUES ('pods', 'cluster-a', '{10.240.0.0/18}', 24, '{24}');
INSERT INTO organizations (id, slug, display_name, owner_user_id, status) VALUES
  ('org-acme', 'acme', 'Acme', 'usr-alice', 'Active'),
  ('org-e1', 'e2eone', 'E2E one', 'usr-e1', 'Active'),
  ('org-e2', 'e2ereject', 'E2E reject', 'usr-e2', 'Rejected'),
  ('org-o1', 'optestx', 'Optest', 'usr-o1', 'Deleted'),
  ('org-live', 'e2elive', 'E2E live', 'usr-e1', 'Active');
INSERT INTO organization_members (org_id, user_id, role) VALUES
  ('org-acme', 'usr-alice', 'OrgOwner'), ('org-acme', 'usr-e3', 'OrgMember'),
  ('org-e1', 'usr-e1', 'OrgOwner'), ('org-e1', 'usr-e2', 'OrgMember'), ('org-live', 'usr-e1', 'OrgOwner');
INSERT INTO organization_entitlements (org_id, max_members, max_groups, max_workspaces_total) VALUES
  ('org-acme', 20, 10, 8), ('org-e1', 20, 10, 8), ('org-live', 20, 10, 8);
INSERT INTO organization_cluster_grants (id, org_id, cluster_id, is_default, max_workspaces, max_namespaces_total,
  requests_cpu_milli, requests_memory_bytes, limits_cpu_milli, limits_memory_bytes, pods, requests_storage_bytes) VALUES
  ('grant-acme', 'org-acme', 'cluster-a', true, 5, 10, 4000, 8589934592, 8000, 17179869184, 50, 21474836480),
  ('grant-e1', 'org-e1', 'cluster-a', true, 5, 10, 4000, 8589934592, 8000, 17179869184, 50, 21474836480),
  ('grant-live', 'org-live', 'cluster-a', true, 5, 10, 4000, 8589934592, 8000, 17179869184, 50, 21474836480);
INSERT INTO cluster_grant_shared_pools (grant_id, pool_id) VALUES ('grant-acme', 'shared-general'), ('grant-e1', 'shared-general');
INSERT INTO groups (id, org_id, display_name, machine_name) VALUES
  ('grp-acme', 'org-acme', 'admins', 'org_acme:group_1'), ('grp-e1', 'org-e1', 'admins', 'org_e1:group_1');
INSERT INTO group_members (group_id, user_id) VALUES ('grp-acme', 'usr-alice'), ('grp-e1', 'usr-e1'), ('grp-e1', 'usr-e2');
INSERT INTO workspaces (id, org_id, cluster_id, cluster_grant_id, slug, k8s_name, display_name, state,
  requests_cpu_milli, requests_memory_bytes, limits_cpu_milli, limits_memory_bytes, pods, requests_storage_bytes,
  namespace_quota, compute_mode, shared_pool_id) VALUES
  ('ws-e1', 'org-e1', 'cluster-a', 'grant-e1', 'solar', 'e2eone-solar', 'Solar', 'Deleted', 0, 0, 0, 0, 0, 0, 1, 'Shared', 'shared-general'),
  ('ws-live', 'org-live', 'cluster-a', 'grant-live', 'solar', 'e2elive-solar', 'Solar', 'Ready', 0, 0, 0, 0, 0, 0, 1, 'Shared', 'shared-general'),
  ('ws-acme', 'org-acme', 'cluster-a', 'grant-acme', 'solar', 'acme-solar', 'Solar', 'Ready', 0, 0, 0, 0, 0, 0, 1, 'Shared', 'shared-general');
INSERT INTO network_allocations (id, workspace_id, cluster_id, pool_id, cidr, state) VALUES
  ('na-e1', 'ws-e1', 'cluster-a', 'pods', '10.240.1.0/24', 'Released'),
  ('na-acme', 'ws-acme', 'cluster-a', 'pods', '10.240.2.0/24', 'Allocated');
INSERT INTO operations (id, org_id, resource_type, resource_id, type, created_by) VALUES
  ('op-e1', 'org-e1', 'Workspace', 'ws-e1', 'workspace.delete', 'usr-e1'),
  ('op-acme', 'org-acme', 'Workspace', 'ws-acme', 'workspace.create', 'usr-alice');
INSERT INTO outbox_events (operation_id, ordering_key, target_cluster_id, type, payload, version)
  VALUES ('op-e1', 'ws-e1', 'cluster-a', 'workspace.delete', '{}', 1), ('op-acme', 'ws-acme', 'cluster-a', 'workspace.apply', '{}', 1);
INSERT INTO sessions (id_hash, user_id, refresh_token_enc, access_expires_at, expires_at) VALUES
  (decode(repeat('01', 32), 'hex'), 'usr-e1', 'x', now(), now() + interval '1 hour'),
  (decode(repeat('02', 32), 'hex'), 'usr-alice', 'x', now(), now() + interval '1 hour');
INSERT INTO idempotency_keys (user_id, key, method, path, request_hash) VALUES
  ('usr-e1', 'k1', 'POST', '/x', decode(repeat('ab', 32), 'hex'));
INSERT INTO audit_events (actor_user_id, org_id, action, target_type, target_id, result) VALUES
  ('usr-admin', 'org-e1', 'organization.approve', 'Organization', 'org-e1', 'success'),
  ('usr-admin', NULL, 'user.disable', 'User', 'usr-e2', 'success'),
  ('usr-alice', 'org-acme', 'workspace.create', 'Workspace', 'ws-acme', 'success');`)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, string(script)); err != nil {
		_ = tx.Rollback()
		t.Fatalf("purge: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	for q, want := range map[string]int{
		// Real data stays whole.
		`SELECT count(*) FROM users WHERE id IN ('usr-admin', 'usr-alice')`:  2,
		`SELECT count(*) FROM organizations WHERE id = 'org-acme'`:           1,
		`SELECT count(*) FROM workspaces WHERE id = 'ws-acme'`:               1,
		`SELECT count(*) FROM operations WHERE id = 'op-acme'`:               1,
		`SELECT count(*) FROM outbox_events WHERE operation_id = 'op-acme'`:  1,
		`SELECT count(*) FROM sessions WHERE user_id = 'usr-alice'`:          1,
		`SELECT count(*) FROM audit_events WHERE org_id = 'org-acme'`:        1,
		`SELECT count(*) FROM shared_compute_pools`:                          1,
		`SELECT count(*) FROM network_pools`:                                 1,
		`SELECT count(*) FROM clusters WHERE id IN ('cluster-a', 'e2ereg2')`: 2,
		`SELECT count(*) FROM clusters WHERE id = 'e2ereg1'`:                 0,
		// A test organization the cluster may still hold stays, with its owner.
		`SELECT count(*) FROM organizations WHERE id = 'org-live'`: 1,
		`SELECT count(*) FROM users WHERE id = 'usr-e1'`:           1,
		// A test account a real organization refers to stays.
		`SELECT count(*) FROM users WHERE id = 'usr-e3'`: 1,
		// The rest of the test data is gone.
		`SELECT count(*) FROM organizations WHERE id IN ('org-e1', 'org-e2', 'org-o1')`:     0,
		`SELECT count(*) FROM users WHERE id IN ('usr-e2', 'usr-o1')`:                       0,
		`SELECT count(*) FROM workspaces WHERE id = 'ws-e1'`:                                0,
		`SELECT count(*) FROM network_allocations WHERE id = 'na-e1'`:                       0,
		`SELECT count(*) FROM operations WHERE id = 'op-e1'`:                                0,
		`SELECT count(*) FROM outbox_events WHERE operation_id = 'op-e1'`:                   0,
		`SELECT count(*) FROM groups WHERE id = 'grp-e1'`:                                   0,
		`SELECT count(*) FROM organization_cluster_grants WHERE id = 'grant-e1'`:            0,
		`SELECT count(*) FROM audit_events WHERE org_id = 'org-e1' OR target_id = 'usr-e2'`: 0,
	} {
		var n int
		if err := db.QueryRowContext(ctx, q).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		if n != want {
			t.Errorf("%s = %d, want %d", q, n, want)
		}
	}
}

// Every table that can hold a test organization's or account's rows is in
// the purge, so a new table cannot be left behind; the others are the
// cluster registry and platform settings, which it never touches.
func TestPurgeCoversEveryTable(t *testing.T) {
	script, err := os.ReadFile("../../deploy/demo/purge.sql")
	if err != nil {
		t.Fatal(err)
	}
	db := testdb.Open(t)
	ctx := context.Background()
	if _, err := provider(t, db).Up(ctx); err != nil {
		t.Fatal(err)
	}
	platform := map[string]bool{"clusters": true, "cluster_nodes": true, "shared_compute_pools": true, "onboarding_policies": true, "goose_db_version": true}
	rows, err := db.QueryContext(ctx, `SELECT tablename FROM pg_tables WHERE schemaname = 'public'`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatal(err)
		}
		if platform[table] {
			continue
		}
		if !regexp.MustCompile(`DELETE FROM ` + table + ` `).Match(script) {
			t.Errorf("purge.sql does not delete from %s", table)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
