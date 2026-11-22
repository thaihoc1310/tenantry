package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/netip"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
	"github.com/thaihoc1310/tenantry/backend/internal/testdb"
	"github.com/thaihoc1310/tenantry/backend/migrations"
)

var lab = options{
	clusterID: "cluster-a", clusterName: "Cluster A",
	proxyEndpoint: "https://kube-a.test", consoleURL: "https://console-a.test", appsDomain: "apps.test",
	networkCIDR: "10.240.0.0/18", storageClass: "standard",
	reservedCIDRs: []netip.Prefix{netip.MustParsePrefix("192.168.0.0/16"), netip.MustParsePrefix("10.96.0.0/12")},
	nodes: []node{
		{name: "cp1", kind: "ControlPlane", ready: true, cpuMilli: 4000, memory: 8 << 30, pods: 110},
		{name: "wk2", kind: "Worker", ready: true, schedule: true, cpuMilli: 3000, memory: 11776 << 20, pods: 110, pool: "shared-general"},
	},
}

func TestParseNode(t *testing.T) {
	n, err := parseNode("name=wk2,kind=Worker,cpu=3,memory=11776Mi,pods=110,ready=true,schedulable=true,pool=shared-general")
	if err != nil || n != lab.nodes[1] {
		t.Fatalf("got %+v, %v", n, err)
	}
	for _, bad := range []string{"", "kind=Worker", "name=wk2,cpu=lots", "name=wk2,size=3", "name=wk2,ready"} {
		if _, err := parseNode(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// The templates are exactly what the approval form takes, minus the cluster.
func TestPolicyTemplatesFitTheAPI(t *testing.T) {
	entitlement, grant, err := policyTemplates("standard")
	if err != nil {
		t.Fatal(err)
	}
	strict := func(doc []byte, v any) {
		t.Helper()
		d := json.NewDecoder(bytes.NewReader(doc))
		d.DisallowUnknownFields()
		if err := d.Decode(v); err != nil {
			t.Fatalf("%s: %v", doc, err)
		}
	}
	var e oapi.Entitlement
	strict(entitlement, &e)
	var g oapi.ClusterGrantInput
	strict(grant, &g)
	if e.MaxGroups != 10 || g.Resources.RequestsMemory != "4Gi" || g.AllowedStorageClasses[0] != "standard" ||
		strings.Contains(string(grant), "clusterId") {
		t.Fatalf("templates %s %s", entitlement, grant)
	}
}

func TestOptionsCheck(t *testing.T) {
	if _, err := lab.check(); err != nil {
		t.Fatal(err)
	}
	bad := lab
	bad.proxyEndpoint, bad.consoleURL, bad.appsDomain, bad.networkCIDR = "http://kube-a.test", "", "", "10.240.1.0/16"
	_, err := bad.check()
	for _, want := range []string{"--proxy-endpoint", "--console-url", "--apps-domain", "--network-cidr"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s: %v", want, err)
		}
	}
}

// A second run changes nothing, and never undoes an Admin's change.
func TestSeedTwice(t *testing.T) {
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
	pool := testdb.Pool(t, url)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	run := func() {
		t.Helper()
		if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return seed(ctx, store.New(tx), lab, netip.MustParsePrefix(lab.networkCIDR), quiet)
		}); err != nil {
			t.Fatal(err)
		}
	}

	run()
	// An Admin's changes since: none is undone by the next run.
	if _, err := pool.Exec(ctx, `UPDATE shared_compute_pools SET allocation_overcommit_ratio = 2.5, system_reserve_cpu_milli = 1000;
		UPDATE clusters SET reserved_cidrs = '{10.0.0.0/8}';
		UPDATE cluster_nodes SET shared_pool_id = NULL WHERE node_name = 'wk2'`); err != nil {
		t.Fatal(err)
	}
	run()
	var nodes string
	if err := pool.QueryRow(ctx, `SELECT (SELECT array_to_string(reserved_cidrs, ',') FROM clusters) || ' ' ||
		(SELECT system_reserve_cpu_milli::text FROM shared_compute_pools) || ' ' ||
		(SELECT string_agg(node_name || ':' || kind || ':' || coalesce(shared_pool_id, '-'), ',' ORDER BY node_name) FROM cluster_nodes)`).Scan(&nodes); err != nil {
		t.Fatal(err)
	}
	if want := "10.0.0.0/8 1000 cp1:ControlPlane:-,wk2:Worker:-"; nodes != want {
		t.Fatalf("nodes after two runs: %s, want %s", nodes, want)
	}

	var state string
	if err := pool.QueryRow(ctx, `
SELECT (SELECT string_agg(id || ':' || status, ',') FROM clusters) || ' ' ||
       (SELECT string_agg(id || ':' || allocation_overcommit_ratio, ',') FROM shared_compute_pools) || ' ' ||
       (SELECT string_agg(id || ':' || array_to_string(cidrs, ','), ',') FROM network_pools WHERE org_id IS NULL) || ' ' ||
       (SELECT string_agg(id || ':' || default_cluster_id, ',') FROM onboarding_policies WHERE active)`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if want := "cluster-a:Ready shared-general:2.50 onboarding-pod-network:10.240.0.0/18 default:cluster-a"; state != want {
		t.Fatalf("after two runs: %s, want %s", state, want)
	}
}
