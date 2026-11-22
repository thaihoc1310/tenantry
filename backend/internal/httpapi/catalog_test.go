package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
)

func TestCatalog(t *testing.T) {
	h, db, orgID, owner := activeOrg(t)
	if _, err := db.Exec(context.Background(), nodesSeed+`
		UPDATE clusters SET reserved_cidrs = '{192.168.0.0/16}' WHERE id = 'cluster-a';`); err != nil {
		t.Fatal(err)
	}
	get := func(token, path string, out any) int {
		t.Helper()
		rec := asAdmin(h, token, http.MethodGet, "/api/v1/organizations/zeta"+path, "", "")
		if rec.Code == 200 && out != nil {
			if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(rec.Body.String(), "192.168") {
				t.Fatalf("%s shows the cluster's reserved ranges: %s", path, rec.Body)
			}
		}
		return rec.Code
	}

	var clusters oapi.OrgClusterList
	if code := get(owner, "/clusters", &clusters); code != 200 || len(clusters.Items) != 1 {
		t.Fatalf("clusters: %d %+v", code, clusters)
	}
	c := clusters.Items[0]
	if c.Id != "cluster-a" || !c.IsDefault || c.SharedPools != 1 || c.DedicatedNodes != 0 || c.Status != "Ready" {
		t.Fatalf("cluster-a %+v", c)
	}
	options := func() map[oapi.ComputeOptionsItemsMode]string {
		t.Helper()
		var o oapi.ComputeOptions
		if code := get(owner, "/clusters/cluster-a/compute-options", &o); code != 200 {
			t.Fatalf("options: %d", code)
		}
		out := map[oapi.ComputeOptionsItemsMode]string{}
		for _, i := range o.Items {
			out[i.Mode] = "available"
			if !i.Available {
				out[i.Mode] = *i.Reason
			}
		}
		return out
	}
	if o := options(); o["Shared"] != "available" || !strings.Contains(o["Dedicated"], "No node") {
		t.Fatalf("options %+v", o)
	}

	var pools oapi.OrgSharedPoolList
	if code := get(owner, "/clusters/cluster-a/shared-pools", &pools); code != 200 || len(pools.Items) != 1 ||
		pools.Items[0].Id != "shared-general" || pools.Items[0].NodeCount != 2 || pools.Items[0].Capacity.Ceiling.Cpu != "15" {
		t.Fatalf("shared pools: %d %+v", code, pools)
	}
	var nets oapi.OrgNetworkPoolList
	if code := get(owner, "/clusters/cluster-a/network-pools", &nets); code != 200 || len(nets.Items) != 1 ||
		nets.Items[0].Id != "onboarding-pod-network" || !nets.Items[0].Shared {
		t.Fatalf("network pools (acme-net is not in the grant): %d %+v", code, nets)
	}

	// The Admin dedicates wk3: the organization sees it and its remaining capacity.
	admin := newSession(t, db, "usr-dave", "platform-admin")
	asAdmin(h, admin, http.MethodPut, "/api/v1/admin/clusters/cluster-a/shared-pools/shared-general", "",
		`{"name":"Shared general","allocationOvercommitRatio":3,"systemReserve":`+reserve+`,"nodes":["wk2"]}`)
	if rec := asAdmin(h, admin, http.MethodPut, "/api/v1/admin/organizations/"+orgID+"/cluster-grants/cluster-a/dedicated-nodes", "",
		`{"nodes":[{"name":"wk3","systemReserve":`+reserve+`}]}`); rec.Code != 200 {
		t.Fatalf("dedicate wk3: %d %s", rec.Code, rec.Body)
	}
	var nodes oapi.OrgDedicatedNodeList
	if code := get(owner, "/clusters/cluster-a/dedicated-nodes", &nodes); code != 200 || len(nodes.Items) != 1 {
		t.Fatalf("dedicated nodes: %d %+v", code, nodes)
	}
	if n := nodes.Items[0]; n.Name != "wk3" || n.Reserved.Cpu != "0" || n.Remaining.Cpu != "2500m" || n.Remaining.Memory != "10Gi" || len(n.Workspaces) != 0 {
		t.Fatalf("wk3 %+v", n)
	}
	if o := options(); o["Dedicated"] != "available" {
		t.Fatalf("options with a dedicated node %+v", o)
	}
	// A cluster in maintenance offers no mode.
	if _, err := db.Exec(context.Background(), `UPDATE clusters SET status = 'Maintenance' WHERE id = 'cluster-a'`); err != nil {
		t.Fatal(err)
	}
	if o := options(); !strings.Contains(o["Shared"], "Maintenance") || !strings.Contains(o["Dedicated"], "Maintenance") {
		t.Fatalf("options in maintenance %+v", o)
	}

	for _, c := range []struct {
		name, token, path string
		status            int
	}{
		{"a cluster not granted", owner, "/clusters/cluster-b", 404},
		{"its pools", owner, "/clusters/cluster-b/shared-pools", 404},
		{"an OrgMember", sessionOf("usr-bob"), "/clusters", 403},
		{"an outsider", sessionOf("usr-dave"), "/clusters", 404},
	} {
		if code := get(c.token, c.path, nil); code != c.status {
			t.Errorf("%s: %d, want %d", c.name, code, c.status)
		}
	}
}
