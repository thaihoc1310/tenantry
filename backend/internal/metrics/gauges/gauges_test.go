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
// cluster, the free blocks of its pools, and what Keycloak has not caught up with.
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
		INSERT INTO groups (id, org_id, display_name, machine_name) VALUES ('grp-1', 'org-a', 'admins', 'org_a:group_1');`); err != nil {
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
	} {
		if got[key] != want {
			t.Errorf("%s = %v, want %v", key, got[key], want)
		}
	}
	if lag := got["tenantry_outbox_oldest_pending_seconds,cluster=cluster-a"]; lag < 29 || lag > 120 {
		t.Errorf("outbox lag %v", lag)
	}
}
