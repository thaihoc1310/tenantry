package reservation

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/thaihoc1310/tenantry/operator/api/v1alpha1"

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
