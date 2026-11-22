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
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
	"github.com/thaihoc1310/tenantry/backend/internal/testdb"
	"github.com/thaihoc1310/tenantry/backend/migrations"
)

var lab = options{
	clusterID: "cluster-a", clusterName: "Cluster A",
	proxyEndpoint: "https://kube-a.test", consoleURL: "https://console-a.test", appsDomain: "apps.test",
	networkCIDR: "10.240.0.0/18", storageClass: "standard",
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
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
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
	if _, err := pool.Exec(ctx, `UPDATE shared_compute_pools SET allocation_overcommit_ratio = 2.5`); err != nil {
		t.Fatal(err)
	}
	run()

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
