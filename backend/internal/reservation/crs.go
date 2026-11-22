package reservation

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"

	"github.com/thaihoc1310/tenantry/backend/internal/operation"
	"github.com/thaihoc1310/tenantry/backend/internal/outbox"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// The CRs a cluster gets for its shared pools and dedicated nodes
// (IMPLEMENTATION-PLAN §4.5), built from the database alone. They carry the
// cluster's id as a label: the provisioning worker deletes the ones of its
// cluster the database no longer has, and never touches others.

// SharedPoolCRs is a PlatformSharedPool per shared pool of the cluster,
// listing the nodes the database puts in it.
func SharedPoolCRs(ctx context.Context, q *store.Queries, clusterID string) ([]*v1.PlatformSharedPool, error) {
	rows, err := q.ListSharedPools(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	out := make([]*v1.PlatformSharedPool, len(rows))
	for i, r := range rows {
		out[i] = &v1.PlatformSharedPool{
			TypeMeta:   metav1.TypeMeta{APIVersion: v1.GroupVersion.String(), Kind: "PlatformSharedPool"},
			ObjectMeta: crMeta(r.SharedComputePool.ID, clusterID),
			Spec:       v1.PlatformSharedPoolSpec{Nodes: r.Nodes},
		}
	}
	return out, nil
}

// NodeGrantCRs is a PlatformNodeGrant per live dedicated node of the cluster.
func NodeGrantCRs(ctx context.Context, q *store.Queries, clusterID string) ([]*v1.PlatformNodeGrant, error) {
	rows, err := q.ListLiveNodeGrantsOfCluster(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	out := make([]*v1.PlatformNodeGrant, len(rows))
	for i, g := range rows {
		out[i] = NodeGrantCR(g)
	}
	return out, nil
}

func NodeGrantCR(g store.DedicatedNodeGrant) *v1.PlatformNodeGrant {
	return &v1.PlatformNodeGrant{
		TypeMeta:   metav1.TypeMeta{APIVersion: v1.GroupVersion.String(), Kind: "PlatformNodeGrant"},
		ObjectMeta: crMeta(g.ID, g.ClusterID),
		Spec:       v1.PlatformNodeGrantSpec{NodeName: g.NodeName, OrganizationID: g.OrgID},
	}
}

func crMeta(name, clusterID string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Labels: map[string]string{v1.LabelClusterID: clusterID}}
}

// Event types and operation resources of the pool and node CRs.
const (
	EventSharedPoolApply = "sharedpool.apply"
	EventNodeGrantApply  = "nodegrant.apply"
	EventNodeGrantDelete = "nodegrant.delete"

	ResourceSharedPool = "SharedPool"
	ResourceNodeGrant  = "NodeGrant"
)

// enqueuePool starts a sharedpool.update operation, which belongs to no
// organization, carrying the pool's CR as the database now has it.
func enqueuePool(ctx context.Context, q *store.Queries, clusterID, poolID, adminID, correlationID string) (store.Operation, error) {
	op, err := operation.Create(ctx, q, operation.New{
		ResourceType: ResourceSharedPool, ResourceID: poolID, Type: "sharedpool.update",
		CorrelationID: correlationID, CreatedBy: adminID,
	})
	if err != nil {
		return op, err
	}
	crs, err := SharedPoolCRs(ctx, q, clusterID)
	if err != nil {
		return op, err
	}
	for _, cr := range crs {
		if cr.Name == poolID {
			_, err = outbox.Enqueue(ctx, q, op, outbox.Event{TargetClusterID: clusterID, Type: EventSharedPoolApply, Payload: cr, Version: 1})
			return op, err
		}
	}
	return op, fmt.Errorf("shared pool %s vanished", poolID)
}

// enqueueNodeGrant starts node.grant (apply) or node.revoke (delete) for g.
func enqueueNodeGrant(ctx context.Context, q *store.Queries, g store.DedicatedNodeGrant, revoke bool, adminID, correlationID string) (store.Operation, error) {
	typ, event, payload := "node.grant", EventNodeGrantApply, any(NodeGrantCR(g))
	if revoke {
		typ, event, payload = "node.revoke", EventNodeGrantDelete, map[string]string{"name": g.ID}
	}
	op, err := operation.Create(ctx, q, operation.New{
		OrgID: g.OrgID, ResourceType: ResourceNodeGrant, ResourceID: g.ID, Type: typ,
		CorrelationID: correlationID, CreatedBy: adminID,
	})
	if err != nil {
		return op, err
	}
	_, err = outbox.Enqueue(ctx, q, op, outbox.Event{TargetClusterID: g.ClusterID, Type: event, Payload: payload, Version: 1})
	return op, err
}
