package provisioning

import (
	"context"
	"slices"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"

	"github.com/thaihoc1310/tenantry/backend/internal/k8s"
	"github.com/thaihoc1310/tenantry/backend/internal/reservation"
)

const nodesSeed = `
	INSERT INTO cluster_nodes (cluster_id, node_name, kind, ready, schedulable, allocatable_cpu_milli, allocatable_memory_bytes, allocatable_pods, shared_pool_id) VALUES
		('cluster-a', 'wk1', 'Worker', true, true, 4000, 8589934592, 110, 'shared-general'),
		('cluster-a', 'wk2', 'Worker', true, true, 4000, 8589934592, 110, 'shared-general'),
		('cluster-a', 'wk3', 'Worker', true, true, 3000, 4294967296, 110, NULL),
		('cluster-a', 'wk5', 'Worker', true, true, 3000, 4294967296, 110, NULL);
	INSERT INTO dedicated_node_grants (id, org_id, cluster_id, node_name, allocatable_cpu_milli, allocatable_memory_bytes, allocatable_pods,
		system_reserve_cpu_milli, system_reserve_memory_bytes, system_reserve_pods)
		VALUES ('nodegrant-1', 'org-01', 'cluster-a', 'wk3', 3000, 4294967296, 110, 500, 1073741824, 10),
		       ('nodegrant-old', 'org-01', 'cluster-a', 'wk5', 0, 0, 0, 0, 0, 0);
	UPDATE dedicated_node_grants SET revoked_at = now() WHERE id = 'nodegrant-old';`

// What M5 wrote to the database reaches the cluster when the worker starts:
// a pool CR listing its nodes, a node grant CR per live dedicated node; CRs
// of this cluster the database no longer has go, others stay.
func TestRebuildFromTheDatabase(t *testing.T) {
	e := newEnv(t)
	if _, err := e.db.Exec(context.Background(), nodesSeed); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for name, labels := range map[string]map[string]string{
		"gone-pool":   {v1.LabelClusterID: "cluster-a"},
		"optest-pool": nil, // someone else's
		"other-pool":  {v1.LabelClusterID: "cluster-b"},
	} {
		u, _ := k8s.ToUnstructured(&v1.PlatformSharedPool{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}})
		if err := e.cluster.Apply(ctx, k8s.SharedPools, u); err != nil {
			t.Fatal(err)
		}
	}
	u, _ := k8s.ToUnstructured(&v1.PlatformNodeGrant{ObjectMeta: metav1.ObjectMeta{Name: "nodegrant-old", Labels: map[string]string{v1.LabelClusterID: "cluster-a"}}})
	if err := e.cluster.Apply(ctx, k8s.NodeGrants, u); err != nil {
		t.Fatal(err)
	}

	if err := e.syncer.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	pools, _ := e.cluster.SharedPools()
	got := map[string][]string{}
	for _, p := range pools {
		got[p.Name] = p.Spec.Nodes
	}
	if len(got) != 3 || !slices.Equal(got["shared-general"], []string{"wk1", "wk2"}) {
		t.Fatalf("pools after rebuild: %v", got)
	}
	if _, ok := got["gone-pool"]; ok {
		t.Fatal("a pool the database no longer has is still on the cluster")
	}
	if _, ok := got["optest-pool"]; !ok {
		t.Fatal("deleted a pool CR the worker does not own")
	}
	grants, _ := e.cluster.NodeGrants()
	if len(grants) != 1 || grants[0].Name != "nodegrant-1" || grants[0].Spec != (v1.PlatformNodeGrantSpec{NodeName: "wk3", OrganizationID: "org-01"}) ||
		grants[0].Labels[v1.LabelClusterID] != "cluster-a" {
		t.Fatalf("node grants after rebuild: %+v", grants)
	}
	// Twice is the same.
	applies := e.cluster.applies
	if err := e.syncer.Rebuild(ctx); err != nil || e.cluster.get(k8s.SharedPools, "shared-general").GetGeneration() != 1 || e.cluster.applies != applies+2 {
		t.Fatalf("second rebuild: %v", err)
	}
}

// The inventory replaces what cluster_nodes knew, keeping each node's pool;
// dedicated nodes get their state and allocatable; a pool's report lands in
// its snapshot.
func TestNodeInventory(t *testing.T) {
	e := newEnv(t)
	if _, err := e.db.Exec(context.Background(), nodesSeed); err != nil {
		t.Fatal(err)
	}
	q := func(n string) resource.Quantity { return resource.MustParse(n) }
	alloc := func(cpu, mem string) v1.ResourceList {
		return v1.ResourceList{"cpu": q(cpu), "memory": q(mem), "pods": q("110")}
	}

	// Nothing reported yet: nothing forgotten.
	e.cluster.inventory = &v1.PlatformNodeInventory{}
	e.sync()
	if n := countRows(t, e, `SELECT count(*) FROM cluster_nodes`); n != 4 {
		t.Fatalf("%d nodes after an empty inventory, want 4", n)
	}

	e.cluster.inventory = &v1.PlatformNodeInventory{Status: v1.PlatformNodeInventoryStatus{Nodes: []v1.InventoryNode{
		{Name: "cp1", Kind: v1.NodeControlPlane, Ready: true, Schedulable: true, Allocatable: alloc("2", "4Gi")},
		{Name: "wk1", Kind: v1.NodeWorker, Ready: true, Schedulable: true, Allocatable: alloc("3800m", "7Gi")},
		{Name: "wk2", Kind: v1.NodeWorker, Ready: true, Schedulable: false, Allocatable: alloc("4", "8Gi")},
		{Name: "wk3", Kind: v1.NodeWorker, Ready: false, Schedulable: true, Allocatable: alloc("2500m", "3Gi")},
	}}}
	u, _ := k8s.ToUnstructured(&v1.PlatformSharedPool{ObjectMeta: metav1.ObjectMeta{Name: "shared-general", Labels: map[string]string{v1.LabelClusterID: "cluster-a"}}})
	if err := e.cluster.Apply(context.Background(), k8s.SharedPools, u); err != nil {
		t.Fatal(err)
	}
	e.cluster.setStatus(k8s.SharedPools, "shared-general", &v1.PlatformSharedPoolStatus{
		ObservedGeneration: 1, ReadyNodes: 2, Allocatable: alloc("7800m", "15Gi"),
		Conditions: []metav1.Condition{cond(v1.ConditionReady, true, "Labelled", "")},
	})
	e.sync()

	type node struct {
		pool         *string
		ready, sched bool
		cpu          int64
		kind         string
	}
	nodes := map[string]node{}
	rows, err := e.db.Query(context.Background(), `SELECT node_name, shared_pool_id, ready, schedulable, allocatable_cpu_milli, kind FROM cluster_nodes`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name string
		var n node
		if err := rows.Scan(&name, &n.pool, &n.ready, &n.sched, &n.cpu, &n.kind); err != nil {
			t.Fatal(err)
		}
		nodes[name] = n
	}
	if len(nodes) != 4 || nodes["cp1"].kind != "ControlPlane" || nodes["wk1"].cpu != 3800 || nodes["wk1"].pool == nil ||
		*nodes["wk1"].pool != "shared-general" || nodes["wk2"].sched || nodes["wk3"].ready {
		t.Fatalf("cluster_nodes %+v", nodes)
	}
	if _, ok := nodes["wk5"]; ok {
		t.Fatal("a node the cluster no longer has is still known")
	}
	var state, capacity string
	var cpu int64
	if err := e.db.QueryRow(context.Background(), `SELECT node_state, capacity_state, allocatable_cpu_milli FROM dedicated_node_grants WHERE id = 'nodegrant-1'`).
		Scan(&state, &capacity, &cpu); err != nil || state != "NotReady" || capacity != "Available" || cpu != 2500 {
		t.Fatalf("dedicated node: %s %s %d (%v)", state, capacity, cpu, err)
	}
	var snapshot map[string]any
	if err := e.db.QueryRow(context.Background(), `SELECT capacity_snapshot FROM shared_compute_pools WHERE id = 'shared-general'`).Scan(&snapshot); err != nil ||
		snapshot["cpuMilli"] != 7800.0 || snapshot["memoryBytes"] != 16106127360.0 || snapshot["readyNodes"] != 2.0 || snapshot["ready"] != true {
		t.Fatalf("pool snapshot %v (%v)", snapshot, err)
	}
}

func countRows(t *testing.T, e *env, query string) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(context.Background(), query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Operations on pools and dedicated nodes end when the operator has done
// what the database says: a pool reconciled with its nodes, a node grant
// Ready (a refusal fails it, a node not Ready yet is waited for), a revoked
// grant's CR gone.
func TestNodeOperations(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.db.Exec(ctx, nodesSeed); err != nil {
		t.Fatal(err)
	}
	res := reservation.NewService(e.db)
	pool, err := res.UpdateSharedPool(ctx, "usr-ann", "req-1", "cluster-a", "shared-general", reservation.SharedPool{
		Name: "General", RatioHundredths: 200, Nodes: []string{"wk1"},
	})
	if err != nil || pool.Operation == nil {
		t.Fatalf("pool change: %v %+v", err, pool.Operation)
	}
	e.deliver()
	if got := e.cluster.get(k8s.SharedPools, "shared-general"); got == nil {
		t.Fatal("pool CR not applied")
	}
	e.sync()
	if o := e.op(pool.Operation.ID); o.State != "Running" {
		t.Fatalf("succeeded before the operator reported: %s", o.State)
	}
	e.cluster.setStatus(k8s.SharedPools, "shared-general", &v1.PlatformSharedPoolStatus{ObservedGeneration: 1,
		Conditions: []metav1.Condition{cond(v1.ConditionReady, true, "Labelled", "")}})
	e.sync()
	if o := e.op(pool.Operation.ID); o.State != "Succeeded" {
		t.Fatalf("pool operation %+v", o)
	}

	_, ops, err := res.SetDedicatedNodes(ctx, "usr-ann", "req-2", "org-01", "cluster-a", []reservation.NodeRequest{{Name: "wk3"}, {Name: "wk2"}})
	if err != nil || len(ops) != 1 {
		t.Fatalf("give wk2: %v %d operations", err, len(ops))
	}
	grant := ops[0].ResourceID
	e.deliver()
	report := func(ok bool, reason string) {
		e.cluster.setStatus(k8s.NodeGrants, grant, &v1.PlatformNodeGrantStatus{ObservedGeneration: 1,
			Conditions: []metav1.Condition{cond(v1.ConditionReady, ok, reason, "the node has tenant Pods")}})
		e.sync()
	}
	report(false, v1.ReasonNodeNotReady)
	if o := e.op(ops[0].ID); o.State != "Running" {
		t.Fatalf("a node not Ready yet failed the operation: %s", o.State)
	}
	report(false, v1.ReasonNodeNotClean)
	if o := e.op(ops[0].ID); o.State != "Failed" || o.ErrorCode != "RECONCILE_FAILED" || o.ErrorMessage != "NodeNotClean: the node has tenant Pods" {
		t.Fatalf("refused grant: %+v", o)
	}

	_, ops, err = res.SetDedicatedNodes(ctx, "usr-ann", "req-3", "org-01", "cluster-a", nil)
	if err != nil || len(ops) != 2 || ops[0].Type != "node.revoke" {
		t.Fatalf("revoke both: %v %+v", err, ops)
	}
	// nodegrant-1 (wk3, from the seed) was never applied: its delete finds nothing.
	e.deliver()
	e.deliver()
	e.sync()
	for _, op := range ops {
		if o := e.op(op.ID); o.State != "Succeeded" {
			t.Fatalf("revoke %s: %+v", op.ResourceID, o)
		}
	}
	if e.cluster.get(k8s.NodeGrants, grant) != nil {
		t.Fatal("revoked grant's CR still there")
	}

	// A pool change the operator never reports on times out.
	pool, err = res.UpdateSharedPool(ctx, "usr-ann", "req-4", "cluster-a", "shared-general", reservation.SharedPool{
		Name: "General", RatioHundredths: 200, Nodes: []string{"wk1", "wk2"},
	})
	if err != nil || pool.Operation == nil {
		t.Fatalf("wk2 back into the pool: %v", err)
	}
	e.deliver() // the operator has not reconciled this generation
	if _, err := e.db.Exec(ctx, `UPDATE operations SET updated_at = now() - interval '11 minutes' WHERE id = $1`, pool.Operation.ID); err != nil {
		t.Fatal(err)
	}
	e.sync()
	if o := e.op(pool.Operation.ID); o.State != "Failed" || o.ErrorCode != ReconcileTimeout {
		t.Fatalf("pool operation past the timeout: %+v", o)
	}
}
