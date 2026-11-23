package gauges_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/thaihoc1310/tenantry/backend/internal/metrics"
	"github.com/thaihoc1310/tenantry/backend/internal/metrics/gauges"
	"github.com/thaihoc1310/tenantry/backend/internal/testdb"
	"github.com/thaihoc1310/tenantry/backend/migrations"
)

// The gauges read the database at each scrape: the outbox backlog of the
// cluster, the free blocks of its pools, the ceiling and allocation of its
// shared pools, and what Keycloak has not caught up with.
func TestGaugesReadTheDatabase(t *testing.T) {
	url := testdb.New(t)
	ctx := context.Background()
	sqlDB, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sqlDB.Close() }()
	p, err := migrations.NewProvider(sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`
		INSERT INTO users (id, issuer, subject) VALUES ('usr-a', 'iss', 'a');
		INSERT INTO organizations (id, slug, display_name, owner_user_id, status) VALUES ('org-a', 'acme', 'ACME', 'usr-a', 'Active');
		INSERT INTO clusters (id, display_name, status, proxy_endpoint, console_url, apps_domain, reserved_cidrs)
		VALUES ('cluster-a', 'Cluster A', 'Ready', 'https://kube-a.test', 'https://console-a.test', 'apps.test', '{10.240.0.0/23}'),
		       ('cluster-b', 'Cluster B', 'Ready', 'https://kube-b.test', 'https://console-b.test', 'apps-b.test', '{}');
		INSERT INTO network_pools (id, cluster_id, cidrs, default_prefix_length, allowed_prefix_lengths)
		VALUES ('pods', 'cluster-a', '{10.240.0.0/22}', 24, '{24,23}');
		INSERT INTO operations (id, org_id, resource_type, resource_id, type) VALUES ('op-1', 'org-a', 'Workspace', 'ws-1', 'workspace.create');
		INSERT INTO outbox_events (operation_id, ordering_key, target_cluster_id, type, payload, version, created_at)
		VALUES ('op-1', 'ws-1', 'cluster-a', 'workspace.apply', '{}', 1, now() - interval '30 seconds'),
		       ('op-1', 'ws-1', 'cluster-b', 'workspace.apply', '{}', 1, now());
		INSERT INTO groups (id, org_id, display_name, machine_name) VALUES ('grp-1', 'org-a', 'admins', 'org_a:group_1');
		-- shared-general: two workers of 3 CPU / 12 GiB / 110 Pods, 500m / 2 GiB / 10 Pods kept per node, ratio 1.5.
		INSERT INTO shared_compute_pools (id, cluster_id, name, allocation_overcommit_ratio,
			system_reserve_cpu_milli, system_reserve_memory_bytes, system_reserve_pods)
		VALUES ('shared-general', 'cluster-a', 'General', 1.5, 500, 2147483648, 10),
		       ('pool-b', 'cluster-b', 'Other cluster', 2, 0, 0, 0);
		INSERT INTO cluster_nodes (cluster_id, node_name, kind, ready, schedulable, allocatable_cpu_milli, allocatable_memory_bytes, allocatable_pods, shared_pool_id)
		VALUES ('cluster-a', 'wk2', 'Worker', true, true, 3000, 12884901888, 110, 'shared-general'),
		       ('cluster-a', 'wk3', 'Worker', true, true, 3000, 12884901888, 110, 'shared-general');
		INSERT INTO organization_cluster_grants (id, org_id, cluster_id, is_default, max_workspaces, max_namespaces_total,
			requests_cpu_milli, requests_memory_bytes, limits_cpu_milli, limits_memory_bytes, pods, requests_storage_bytes)
			VALUES ('grant-1', 'org-a', 'cluster-a', true, 5, 10, 8000, 17179869184, 8000, 17179869184, 50, 21474836480);
		-- Two Shared Workspaces count; a Deleted one does not.
		INSERT INTO workspaces (id, org_id, cluster_id, cluster_grant_id, slug, k8s_name, display_name, state,
			requests_cpu_milli, requests_memory_bytes, limits_cpu_milli, limits_memory_bytes, pods, requests_storage_bytes,
			namespace_quota, compute_mode, shared_pool_id)
		VALUES ('ws-1', 'org-a', 'cluster-a', 'grant-1', 'solar', 'acme-solar', 'Solar', 'Ready', 2000, 4294967296, 4000, 8589934592, 20, 0, 3, 'Shared', 'shared-general'),
		       ('ws-2', 'org-a', 'cluster-a', 'grant-1', 'wind', 'acme-wind', 'Wind', 'Provisioning', 500, 1073741824, 500, 1073741824, 5, 0, 1, 'Shared', 'shared-general'),
		       ('ws-3', 'org-a', 'cluster-a', 'grant-1', 'old', 'acme-old', 'Old', 'Deleted', 1000, 1073741824, 1000, 1073741824, 5, 0, 1, 'Shared', 'shared-general');`); err != nil {
		t.Fatal(err)
	}
	pool := testdb.Pool(t, url)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	gauges.Provisioning(pool, "cluster-a", log)
	gauges.GroupSync(pool, log)
	metrics.RegisterPool(pool)

	families, err := metrics.Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]float64{}
	for _, f := range families {
		for _, m := range f.GetMetric() {
			key := f.GetName()
			for _, l := range m.GetLabel() {
				key += "," + l.GetName() + "=" + l.GetValue()
			}
			got[key] = m.GetGauge().GetValue()
		}
	}
	for key, want := range map[string]float64{
		"tenantry_outbox_pending_events,cluster=cluster-a": 1,
		// The pool's /22 less the cluster's reserved /23: two /24s, one /23.
		"tenantry_network_pool_free_blocks,pool=pods,prefix_length=24": 2,
		"tenantry_network_pool_free_blocks,pool=pods,prefix_length=23": 1,
		"tenantry_groupsync_pending,kind=group":                        1,
		"tenantry_groupsync_pending,kind=member":                       0,
		// Reservable: 2 × (3 − 0.5) CPU, 2 × (12 − 2) GiB, 2 × (110 − 10) Pods; × 1.5.
		"tenantry_shared_pool_allocation_ceiling,pool=shared-general,resource=cpu":    7.5,
		"tenantry_shared_pool_allocation_ceiling,pool=shared-general,resource=memory": 30 * (1 << 30),
		"tenantry_shared_pool_allocation_ceiling,pool=shared-general,resource=pods":   300,
		"tenantry_shared_pool_allocated,pool=shared-general,resource=cpu":             2.5,
		"tenantry_shared_pool_allocated,pool=shared-general,resource=memory":          5 * (1 << 30),
		"tenantry_shared_pool_allocated,pool=shared-general,resource=pods":            25,
	} {
		if got[key] != want {
			t.Errorf("%s = %v, want %v", key, got[key], want)
		}
	}
	// Only the worker's own cluster.
	if _, ok := got["tenantry_shared_pool_allocated,pool=pool-b,resource=cpu"]; ok {
		t.Error("a pool of another cluster is reported")
	}
	if lag := got["tenantry_outbox_oldest_pending_seconds,cluster=cluster-a"]; lag < 29 || lag > 120 {
		t.Errorf("outbox lag %v", lag)
	}
}
