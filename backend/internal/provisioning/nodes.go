package provisioning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"

	"github.com/thaihoc1310/tenantry/backend/internal/k8s"
	"github.com/thaihoc1310/tenantry/backend/internal/reservation"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// Shared pools and dedicated nodes reach the cluster through two small CRs
// (IMPLEMENTATION-PLAN §4.5). The database is their source: Rebuild applies
// them from it when the worker starts and at every resync, so a change
// written while no worker ran (M5 wrote the database only) still lands.

// Rebuild applies a PlatformSharedPool per shared pool and a
// PlatformNodeGrant per live dedicated node of the cluster, and deletes the
// ones of this cluster the database no longer has. The operator labels the
// nodes of each pool and takes the label off every other node, so the
// database must list every node of a pool.
func (s *Syncer) Rebuild(ctx context.Context) error {
	pools, err := reservation.SharedPoolCRs(ctx, s.q, s.clusterID)
	if err != nil {
		return err
	}
	grants, err := reservation.NodeGrantCRs(ctx, s.q, s.clusterID)
	if err != nil {
		return err
	}
	var errs []error
	keepPools, keepGrants := map[string]bool{}, map[string]bool{}
	for _, p := range pools {
		keepPools[p.Name] = true
		errs = append(errs, s.apply(ctx, k8s.SharedPools, p))
	}
	for _, g := range grants {
		keepGrants[g.Name] = true
		errs = append(errs, s.apply(ctx, k8s.NodeGrants, g))
	}

	existingPools, err := s.cluster.SharedPools()
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, p := range existingPools {
		if p.Labels[v1.LabelClusterID] == s.clusterID && !keepPools[p.Name] {
			errs = append(errs, s.cluster.Delete(ctx, k8s.SharedPools, p.Name))
		}
	}
	existingGrants, err := s.cluster.NodeGrants()
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, g := range existingGrants {
		if g.Labels[v1.LabelClusterID] == s.clusterID && !keepGrants[g.Name] {
			errs = append(errs, s.cluster.Delete(ctx, k8s.NodeGrants, g.Name))
		}
	}
	return errors.Join(errs...)
}

func (s *Syncer) apply(ctx context.Context, gvr schema.GroupVersionResource, obj any) error {
	u, err := k8s.ToUnstructured(obj)
	if err != nil {
		return err
	}
	if err := s.cluster.Apply(ctx, gvr, u); err != nil {
		return fmt.Errorf("apply %s %s: %w", gvr.Resource, u.GetName(), err)
	}
	return nil
}

// syncNodes copies the node inventory into cluster_nodes, and the state of
// each dedicated node into its grant; then each pool's report into its
// capacity snapshot.
func (s *Syncer) syncNodes(ctx context.Context) error {
	inv, found, err := s.cluster.Inventory()
	if err != nil {
		return err
	}
	// An empty inventory is an operator that has not reported yet, not a
	// cluster without nodes: forget nothing on its word.
	if found && len(inv.Status.Nodes) > 0 {
		if err := s.recordInventory(ctx, inv.Status.Nodes); err != nil {
			return err
		}
	}
	pools, err := s.cluster.SharedPools()
	if err != nil {
		return err
	}
	for _, p := range pools {
		if p.Labels[v1.LabelClusterID] != s.clusterID || p.Status.ObservedGeneration != p.Generation {
			continue
		}
		snapshot, err := poolSnapshot(p)
		if err != nil {
			return err
		}
		if err := s.q.RecordPoolSnapshot(ctx, store.RecordPoolSnapshotParams{ID: p.Name, ClusterID: s.clusterID, CapacitySnapshot: snapshot}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Syncer) recordInventory(ctx context.Context, nodes []v1.InventoryNode) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		byName := map[string]v1.InventoryNode{}
		names := make([]string, len(nodes))
		for i, n := range nodes {
			names[i] = n.Name
			byName[n.Name] = n
			c := capacityOf(n.Allocatable)
			if err := q.RecordObservedNode(ctx, store.RecordObservedNodeParams{
				ClusterID: s.clusterID, NodeName: n.Name, Kind: string(n.Kind), Ready: n.Ready, Schedulable: n.Schedulable,
				AllocatableCpuMilli: c.CPUMilli, AllocatableMemoryBytes: c.MemoryBytes, AllocatablePods: int32(c.Pods),
			}); err != nil {
				return err
			}
		}
		if _, err := q.ForgetNodesNotIn(ctx, store.ForgetNodesNotInParams{ClusterID: s.clusterID, Names: names}); err != nil {
			return err
		}
		grants, err := q.ListLiveNodeGrantsOfCluster(ctx, s.clusterID)
		if err != nil {
			return err
		}
		for _, g := range grants {
			n, ok := byName[g.NodeName]
			state := nodeState(n, ok)
			alloc := reservation.Capacity{CPUMilli: g.AllocatableCpuMilli, MemoryBytes: g.AllocatableMemoryBytes, Pods: int64(g.AllocatablePods)}
			if ok {
				alloc = capacityOf(n.Allocatable)
			}
			reserve := reservation.Capacity{CPUMilli: g.SystemReserveCpuMilli, MemoryBytes: g.SystemReserveMemoryBytes, Pods: int64(g.SystemReservePods)}
			// ponytail: nothing is reserved on a dedicated node before node reservations (M8).
			capacity := reservation.CapacityState(reservation.Reservable(alloc, reserve), reservation.Capacity{})
			if state == g.NodeState && capacity == g.CapacityState && alloc.CPUMilli == g.AllocatableCpuMilli &&
				alloc.MemoryBytes == g.AllocatableMemoryBytes && alloc.Pods == int64(g.AllocatablePods) {
				continue
			}
			if err := q.RecordNodeGrantNode(ctx, store.RecordNodeGrantNodeParams{
				ID: g.ID, AllocatableCpuMilli: alloc.CPUMilli, AllocatableMemoryBytes: alloc.MemoryBytes,
				AllocatablePods: int32(alloc.Pods), NodeState: state, CapacityState: capacity,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

// nodeState is a dedicated node's health (04-state-machines §5.2); a node
// the cluster no longer reports is NotReady.
func nodeState(n v1.InventoryNode, found bool) string {
	switch {
	case !found || !n.Ready:
		return "NotReady"
	case !n.Schedulable:
		return "Cordoned"
	}
	return "Ready"
}

func capacityOf(r v1.ResourceList) reservation.Capacity {
	cpu, mem, pods := r["cpu"], r["memory"], r["pods"]
	return reservation.Capacity{CPUMilli: cpu.MilliValue(), MemoryBytes: mem.Value(), Pods: pods.Value()}
}

// poolSnapshot is what the operator reports of a pool, in stored units.
func poolSnapshot(p v1.PlatformSharedPool) ([]byte, error) {
	snap := struct {
		reservation.Capacity
		ReadyNodes int32  `json:"readyNodes"`
		Ready      bool   `json:"ready"`
		Message    string `json:"message,omitempty"`
	}{Capacity: capacityOf(p.Status.Allocatable), ReadyNodes: p.Status.ReadyNodes}
	if c := meta.FindStatusCondition(p.Status.Conditions, v1.ConditionReady); c != nil {
		snap.Ready, snap.Message = c.Status == "True", c.Message
	}
	return json.Marshal(snap)
}
