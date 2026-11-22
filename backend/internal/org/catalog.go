package org

import (
	"context"
	"slices"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/reservation"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// The catalog an organization chooses from when it creates a Workspace
// (PRODUCT-PLAN §16.3): only what its cluster grants give it, for its
// OrgOwner and OrgAdmins (an OrgMember sees no clusters, §9.2), and
// sanitized: no ranges the cluster reserves, no other organization's nodes.

var ErrClusterNotGranted = apperr.New(apperr.NotFound, "this organization has no grant on that cluster")

// CatalogCluster is a cluster an organization has a grant on.
type CatalogCluster struct {
	Cluster        store.Cluster
	Grant          reservation.Grant
	DedicatedNodes int
}

// Clusters lists the clusters the organization is granted, the default first.
func (s *Service) Clusters(ctx context.Context, userID, idOrSlug string) ([]CatalogCluster, error) {
	o, err := s.manage(ctx, userID, idOrSlug)
	if err != nil {
		return nil, err
	}
	return s.catalog(ctx, o.ID, "")
}

// Cluster is one cluster the organization is granted.
func (s *Service) Cluster(ctx context.Context, userID, idOrSlug, clusterID string) (CatalogCluster, error) {
	o, err := s.manage(ctx, userID, idOrSlug)
	if err != nil {
		return CatalogCluster{}, err
	}
	cs, err := s.catalog(ctx, o.ID, clusterID)
	if err != nil {
		return CatalogCluster{}, err
	}
	if len(cs) == 0 {
		return CatalogCluster{}, ErrClusterNotGranted
	}
	return cs[0], nil
}

// catalog reads the organization's granted clusters; only clusterID's when it is set.
func (s *Service) catalog(ctx context.Context, orgID, clusterID string) ([]CatalogCluster, error) {
	grants, err := reservation.Grants(ctx, s.q, orgID)
	if err != nil {
		return nil, err
	}
	clusters, err := s.q.ListClusters(ctx)
	if err != nil {
		return nil, err
	}
	out := []CatalogCluster{}
	for _, g := range grants {
		if clusterID != "" && g.ClusterID != clusterID {
			continue
		}
		i := slices.IndexFunc(clusters, func(c store.Cluster) bool { return c.ID == g.ClusterID })
		if i < 0 {
			continue
		}
		nodes, err := s.q.ListLiveDedicatedNodes(ctx, store.ListLiveDedicatedNodesParams{OrgID: orgID, ClusterID: g.ClusterID})
		if err != nil {
			return nil, err
		}
		out = append(out, CatalogCluster{Cluster: clusters[i], Grant: g, DedicatedNodes: len(nodes)})
	}
	return out, nil
}

// ComputeOption says whether a compute mode can be chosen on a cluster, and why not.
type ComputeOption struct {
	Mode      string // Shared or Dedicated
	Available bool
	Reason    string
}

// ComputeOptions are the compute modes the organization can use on a
// cluster (PRODUCT-PLAN §6.1): Shared when its grant lists a shared pool,
// Dedicated when it holds nodes there; neither while the cluster is not Ready.
func (s *Service) ComputeOptions(ctx context.Context, userID, idOrSlug, clusterID string) ([]ComputeOption, error) {
	c, err := s.Cluster(ctx, userID, idOrSlug, clusterID)
	if err != nil {
		return nil, err
	}
	shared := ComputeOption{Mode: "Shared", Available: len(c.Grant.SharedPoolIDs) > 0}
	if !shared.Available {
		shared.Reason = "Your grant on this cluster lists no shared pool."
	}
	dedicated := ComputeOption{Mode: "Dedicated", Available: c.DedicatedNodes > 0}
	if !dedicated.Available {
		dedicated.Reason = "No node of this cluster is dedicated to your organization."
	}
	if c.Cluster.Status != reservation.ClusterReady {
		reason := "The cluster is " + c.Cluster.Status + ": creating Workspaces there is paused."
		shared.Available, shared.Reason = false, reason
		dedicated.Available, dedicated.Reason = false, reason
	}
	return []ComputeOption{shared, dedicated}, nil
}

// CatalogSharedPools are the shared pools of the organization's grant on a cluster.
func (s *Service) CatalogSharedPools(ctx context.Context, userID, idOrSlug, clusterID string) ([]reservation.PoolView, error) {
	c, err := s.Cluster(ctx, userID, idOrSlug, clusterID)
	if err != nil {
		return nil, err
	}
	pools, err := reservation.PoolViews(ctx, s.q, clusterID)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(pools, func(p reservation.PoolView) bool { return !slices.Contains(c.Grant.SharedPoolIDs, p.ID) }), nil
}

// CatalogDedicatedNodes are the nodes the organization holds on a cluster.
//
// ponytail: reserved is 0 and no Workspace holds a node before M8.
func (s *Service) CatalogDedicatedNodes(ctx context.Context, userID, idOrSlug, clusterID string) ([]reservation.DedicatedNode, error) {
	c, err := s.Cluster(ctx, userID, idOrSlug, clusterID)
	if err != nil {
		return nil, err
	}
	return reservation.DedicatedNodesOf(ctx, s.q, c.Grant.OrgID, clusterID)
}

// CatalogNetworkPools are the network pools of the organization's grant on a cluster.
func (s *Service) CatalogNetworkPools(ctx context.Context, userID, idOrSlug, clusterID string) ([]store.NetworkPool, error) {
	c, err := s.Cluster(ctx, userID, idOrSlug, clusterID)
	if err != nil {
		return nil, err
	}
	pools, err := s.q.ListNetworkPools(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(pools, func(p store.NetworkPool) bool { return !slices.Contains(c.Grant.NetworkPoolIDs, p.ID) }), nil
}
