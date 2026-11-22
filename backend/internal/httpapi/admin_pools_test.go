package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
)

// The nodes of cluster-a, like the lab: a control plane, a system node, two
// workers in shared-general (3 CPU / 11.5Gi / 110 pods each) and a worker
// that is not Ready.
const nodesSeed = `
INSERT INTO cluster_nodes (cluster_id, node_name, kind, ready, schedulable, allocatable_cpu_milli, allocatable_memory_bytes, allocatable_pods, shared_pool_id) VALUES
  ('cluster-a', 'cp1', 'ControlPlane', true, false, 4000, 8589934592, 110, NULL),
  ('cluster-a', 'wk1', 'System', true, true, 4000, 8589934592, 110, NULL),
  ('cluster-a', 'wk2', 'Worker', true, true, 3000, 12348030976, 110, 'shared-general'),
  ('cluster-a', 'wk3', 'Worker', true, true, 3000, 12348030976, 110, 'shared-general'),
  ('cluster-a', 'wk4', 'Worker', false, true, 3000, 12348030976, 110, NULL);
UPDATE shared_compute_pools SET system_reserve_cpu_milli = 500, system_reserve_memory_bytes = 1610612736, system_reserve_pods = 10
WHERE id = 'shared-general';`

const reserve = `{"cpu":"500m","memory":"1536Mi","pods":10}`

func TestSharedPools(t *testing.T) {
	h, db, admin := newReviewAPI(t)
	if _, err := db.Exec(context.Background(), nodesSeed); err != nil {
		t.Fatal(err)
	}
	pools := func() map[string]oapi.SharedPool {
		t.Helper()
		rec := asAdmin(h, admin, http.MethodGet, "/api/v1/admin/clusters/cluster-a/shared-pools", "", "")
		var l oapi.SharedPoolList
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &l) != nil {
			t.Fatalf("list: %d %s", rec.Code, rec.Body)
		}
		out := map[string]oapi.SharedPool{}
		for _, p := range l.Items {
			out[p.Id] = p
		}
		return out
	}
	sg := pools()["shared-general"]
	// Two nodes of 3 CPU / 11.5Gi less 0.5 CPU / 1.5Gi each: 5 CPU / 20Gi reservable, ×3.0.
	if strings.Join(sg.Nodes, ",") != "wk2,wk3" || sg.Capacity.Reservable.Cpu != "5" || sg.Capacity.Reservable.Memory != "20Gi" ||
		sg.Capacity.Ceiling.Cpu != "15" || sg.Capacity.Reservable.Pods != 200 || sg.AllocationOvercommitRatio != 3 {
		t.Fatalf("shared-general %+v", sg)
	}

	create := func(body string) (int, []byte) {
		rec := asAdmin(h, admin, http.MethodPost, "/api/v1/admin/clusters/cluster-a/shared-pools", newKey(), body)
		return rec.Code, rec.Body.Bytes()
	}
	pool := func(id, nodes string) string {
		return `{"id":"` + id + `","name":"GPU","allocationOvercommitRatio":2.55,"systemReserve":` + reserve + `,"nodes":` + nodes + `}`
	}
	for name, c := range map[string]struct {
		body   string
		status int
	}{
		"the system pool's id":        {pool("system", "[]"), 400},
		"an id taken across clusters": {pool("shared-b", "[]"), 409},
		"a node of another pool":      {pool("gpu", `["wk2"]`), 400},
		"the control plane":           {pool("gpu", `["cp1"]`), 400},
		"the system node":             {pool("gpu", `["wk1"]`), 400},
		"a node not Ready":            {pool("gpu", `["wk4"]`), 400},
		"an unknown node":             {pool("gpu", `["wk9"]`), 400},
		"a ratio below 1":             {strings.Replace(pool("gpu", "[]"), "2.55", "0.5", 1), 400},
	} {
		if code, body := create(c.body); code != c.status {
			t.Errorf("%s: %d %s", name, code, body)
		}
	}
	code, body := create(pool("gpu", "[]"))
	var gpu oapi.SharedPool
	if code != 201 || json.Unmarshal(body, &gpu) != nil || gpu.AllocationOvercommitRatio != 2.55 || len(gpu.Nodes) != 0 {
		t.Fatalf("create: %d %s", code, body)
	}

	// wk3 leaves shared-general (its capacity with it), then joins gpu.
	put := func(id, body string) (int, []byte) {
		rec := asAdmin(h, admin, http.MethodPut, "/api/v1/admin/clusters/cluster-a/shared-pools/"+id, "", body)
		return rec.Code, rec.Body.Bytes()
	}
	sgBody := func(nodes string) string {
		return `{"name":"Shared general","allocationOvercommitRatio":3,"systemReserve":` + reserve + `,"nodes":` + nodes + `}`
	}
	if code, body := put("shared-general", sgBody(`["wk2"]`)); code != 200 || !strings.Contains(string(body), `"reservable":{"cpu":"2500m"`) {
		t.Fatalf("take wk3 out: %d %s", code, body)
	}
	if code, body := put("gpu", strings.Replace(pool("gpu", `["wk3"]`), `"id":"gpu",`, "", 1)); code != 200 {
		t.Fatalf("wk3 into gpu: %d %s", code, body)
	}
	if p := pools(); strings.Join(p["gpu"].Nodes, ",") != "wk3" || strings.Join(p["shared-general"].Nodes, ",") != "wk2" {
		t.Fatalf("after moving wk3 %+v", p)
	}
	var detail string
	if err := db.QueryRow(context.Background(), `SELECT detail::text FROM audit_events WHERE action = 'shared_pool.update' AND target_id = 'shared-general'`).Scan(&detail); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail, `"nodes": ["wk2", "wk3"]`) || !strings.Contains(detail, `"nodes": ["wk2"]`) {
		t.Fatalf("audit %s", detail)
	}
	if code, _ := put("shared-b", sgBody("[]")); code != 404 {
		t.Fatalf("pool of another cluster: %d", code)
	}
}

func TestDedicatedNodes(t *testing.T) {
	h, db, orgID, _ := activeOrg(t)
	if _, err := db.Exec(context.Background(), nodesSeed); err != nil {
		t.Fatal(err)
	}
	admin := newSession(t, db, "usr-dave", "platform-admin")
	put := func(org, cluster, body string) (int, []byte) {
		rec := asAdmin(h, admin, http.MethodPut, "/api/v1/admin/organizations/"+org+"/cluster-grants/"+cluster+"/dedicated-nodes", "", body)
		return rec.Code, rec.Body.Bytes()
	}
	node := func(name string) string { return `{"nodes":[{"name":"` + name + `","systemReserve":` + reserve + `}]}` }

	// wk3 is in shared-general: out of the pool first (M5: the database only).
	if code, body := put(orgID, "cluster-a", node("wk3")); code != 400 || !strings.Contains(string(body), "take it out of the pool") {
		t.Fatalf("a pool's node: %d %s", code, body)
	}
	pool := func(nodes string) int {
		return asAdmin(h, admin, http.MethodPut, "/api/v1/admin/clusters/cluster-a/shared-pools/shared-general", "",
			`{"name":"Shared general","allocationOvercommitRatio":3,"systemReserve":`+reserve+`,"nodes":`+nodes+`}`).Code
	}
	if pool(`["wk2"]`) != 200 {
		t.Fatal("take wk3 out of the pool")
	}
	code, body := put(orgID, "cluster-a", node("wk3"))
	var l oapi.DedicatedNodeList
	if code != 200 || json.Unmarshal(body, &l) != nil || len(l.Items) != 1 {
		t.Fatalf("grant wk3: %d %s", code, body)
	}
	// 3 CPU / 11.5Gi less 0.5 CPU / 1.5Gi: 2.5 CPU / 10Gi reservable (the M8 demo node).
	d := l.Items[0]
	if d.Name != "wk3" || d.NodeState != "NotReady" || d.CapacityState != "Available" || d.Allocatable.Cpu != "3" ||
		d.Reservable.Cpu != "2500m" || d.Reservable.Memory != "10Gi" || d.Remaining.Cpu != "2500m" {
		t.Fatalf("wk3 %+v", d)
	}
	rec := asAdmin(h, admin, http.MethodGet, "/api/v1/admin/clusters/cluster-a/nodes", "", "")
	var nodes oapi.ClusterNodeList
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &nodes) != nil || len(nodes.Items) != 5 {
		t.Fatalf("nodes: %d %s", rec.Code, rec.Body)
	}
	for _, n := range nodes.Items {
		if dedicated := n.DedicatedTo != nil; dedicated != (n.Name == "wk3") || (dedicated && (n.DedicatedTo.Id != orgID || n.DedicatedTo.Slug != "zeta")) {
			t.Fatalf("node %s dedicated to %+v", n.Name, n.DedicatedTo)
		}
	}
	rec = asAdmin(h, admin, http.MethodGet, "/api/v1/admin/organizations/"+orgID, "", "")
	if !strings.Contains(rec.Body.String(), `"dedicatedNodes":[{`) {
		t.Fatalf("detail without its dedicated nodes: %s", rec.Body)
	}
	// A dedicated node joins no pool; a pool's node is dedicated to nobody.
	if pool(`["wk2","wk3"]`) != 400 {
		t.Fatal("a dedicated node joined a pool")
	}
	if code, _ := put(orgID, "cluster-a", node("wk2")); code != 400 {
		t.Fatal("a pool's node was dedicated")
	}
	if code, _ := put(orgID, "cluster-a", node("cp1")); code != 400 {
		t.Fatal("the control plane was dedicated")
	}
	// Changing the reserve of a node kept.
	code, body = put(orgID, "cluster-a", `{"nodes":[{"name":"wk3","systemReserve":{"cpu":"1","memory":"2Gi","pods":10}}]}`)
	if code != 200 || !strings.Contains(string(body), `"reservable":{"cpu":"2","memory":"9728Mi"`) {
		t.Fatalf("change the reserve: %d %s", code, body)
	}
	if code, _ := put(orgID, "cluster-b", node("wk3")); code != 404 {
		t.Fatalf("no grant on cluster-b: %d", code)
	}

	// Revoke, and wk3 goes back to the pool: the lab's database as before.
	if code, body := put(orgID, "cluster-a", `{"nodes":[]}`); code != 200 || !strings.Contains(string(body), `"items":[]`) {
		t.Fatalf("revoke: %d %s", code, body)
	}
	if n := count(t, db, `SELECT count(*) FROM dedicated_node_grants WHERE node_name = 'wk3' AND revoked_at IS NOT NULL`); n != 1 {
		t.Fatal("not revoked")
	}
	if pool(`["wk2","wk3"]`) != 200 {
		t.Fatal("wk3 back into the pool")
	}
	if n := count(t, db, `SELECT count(*) FROM audit_events WHERE action = 'dedicated_nodes.update'`); n != 3 {
		t.Fatalf("%d dedicated node audit events, want 3", n)
	}
}

func TestNetworkPools(t *testing.T) {
	h, db, admin := newReviewAPI(t)
	if rec := asAdmin(h, admin, http.MethodPatch, "/api/v1/admin/clusters/cluster-a", "", `{"reservedCidrs":["192.168.0.0/16","10.96.0.0/12"]}`); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	create := func(body string) (int, oapi.ErrorResponse) {
		rec := asAdmin(h, admin, http.MethodPost, "/api/v1/admin/clusters/cluster-a/network-pools", newKey(), body)
		if rec.Code == 201 {
			return 201, oapi.ErrorResponse{}
		}
		return rec.Code, errorBody(t, rec)
	}
	pool := func(id, cidr string, prefix int) string {
		return `{"id":"` + id + `","cidrs":["` + cidr + `"],"defaultPrefixLength":` + itoa(prefix) + `,"allowedPrefixLengths":[` + itoa(prefix) + `]}`
	}
	if code, e := create(pool("more", "10.240.32.0/19", 24)); code != 409 || e.Code != oapi.CIDROVERLAP || (*e.Details)["conflictsWith"] != "10.240.0.0/18" {
		t.Fatalf("over onboarding-pod-network: %d %+v", code, e)
	}
	if code, e := create(pool("more", "10.100.0.0/16", 24)); code != 409 || (*e.Details)["in"] != "the cluster's own ranges" {
		t.Fatalf("over the Service CIDR: %d %+v", code, e)
	}
	if code, _ := create(pool("more", "10.240.64.0/18", 16)); code != 400 {
		t.Fatalf("a prefix shorter than the range: %d", code)
	}
	if code, _ := create(pool("more", "10.240.64.1/18", 24)); code != 400 {
		t.Fatalf("not a network address: %d", code)
	}
	if code, _ := create(pool("onboarding-pod-network", "10.240.64.0/18", 24)); code != 409 {
		t.Fatalf("an id taken: %d", code)
	}
	// acme-net (10.241.0.0/16) belongs to cluster-a too: a range of another
	// cluster's pool (net-b, 10.250.0.0/16) is fine.
	if code, e := create(pool("more", "10.240.64.0/18", 24)); code != 201 {
		t.Fatalf("create: %d %+v", code, e)
	}
	if code, _ := create(pool("again", "10.250.0.0/24", 24)); code != 201 {
		t.Fatalf("a range only another cluster uses: %d", code)
	}

	patch := func(id, body string) (int, []byte) {
		rec := asAdmin(h, admin, http.MethodPatch, "/api/v1/admin/clusters/cluster-a/network-pools/"+id, "", body)
		return rec.Code, rec.Body.Bytes()
	}
	if code, body := patch("more", `{"cidrs":["10.240.64.0/18","10.240.128.0/18"],"allowedPrefixLengths":[22,24],"defaultPrefixLength":22}`); code != 200 ||
		!strings.Contains(string(body), `"cidrs":["10.240.64.0/18","10.240.128.0/18"]`) {
		t.Fatalf("add a range: %d %s", code, body)
	}
	if code, _ := patch("more", `{"cidrs":["10.240.64.0/18","192.168.5.0/24"]}`); code != 409 {
		t.Fatalf("add the Pod CIDR: %d", code)
	}
	if code, _ := patch("net-b", `{"defaultPrefixLength":24}`); code != 404 {
		t.Fatalf("a pool of another cluster: %d", code)
	}
	rec := asAdmin(h, admin, http.MethodGet, "/api/v1/admin/clusters/cluster-a/network-pools", "", "")
	var l oapi.NetworkPoolList
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &l) != nil || len(l.Items) != 4 {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	if n := count(t, db, `SELECT count(*) FROM audit_events WHERE action IN ('network_pool.create', 'network_pool.update')`); n != 3 {
		t.Fatalf("%d network pool audit events, want 3", n)
	}
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }
