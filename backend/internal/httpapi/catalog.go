package httpapi

import (
	"context"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
	"github.com/thaihoc1310/tenantry/backend/internal/org"
)

// The catalog an organization chooses from (PRODUCT-PLAN §16.3).

func toOrgCluster(c org.CatalogCluster) oapi.OrgCluster {
	return oapi.OrgCluster{
		Id: c.Cluster.ID, DisplayName: c.Cluster.DisplayName, Status: oapi.OrgClusterStatus(c.Cluster.Status),
		ProxyEndpoint: c.Cluster.ProxyEndpoint, ConsoleUrl: c.Cluster.ConsoleUrl, AppsDomain: c.Cluster.AppsDomain,
		GrantId: c.Grant.ID, IsDefault: c.Grant.IsDefault, SharedPools: len(c.Grant.SharedPoolIDs), DedicatedNodes: c.DedicatedNodes,
	}
}

func (s server) ListOrgClusters(ctx context.Context, req oapi.ListOrgClustersRequestObject) (oapi.ListOrgClustersResponseObject, error) {
	cs, err := s.orgs.Clusters(ctx, userID(ctx), req.OrgId)
	if err != nil {
		return nil, err
	}
	resp := oapi.OrgClusterList{Items: make([]oapi.OrgCluster, 0, len(cs))}
	for _, c := range cs {
		resp.Items = append(resp.Items, toOrgCluster(c))
	}
	return oapi.ListOrgClusters200JSONResponse(resp), nil
}

func (s server) GetOrgCluster(ctx context.Context, req oapi.GetOrgClusterRequestObject) (oapi.GetOrgClusterResponseObject, error) {
	c, err := s.orgs.Cluster(ctx, userID(ctx), req.OrgId, string(req.ClusterId))
	if err != nil {
		return nil, err
	}
	return oapi.GetOrgCluster200JSONResponse(toOrgCluster(c)), nil
}

func (s server) GetComputeOptions(ctx context.Context, req oapi.GetComputeOptionsRequestObject) (oapi.GetComputeOptionsResponseObject, error) {
	opts, err := s.orgs.ComputeOptions(ctx, userID(ctx), req.OrgId, string(req.ClusterId))
	if err != nil {
		return nil, err
	}
	var resp oapi.ComputeOptions
	for _, o := range opts {
		item := struct {
			Available bool                         `json:"available"`
			Mode      oapi.ComputeOptionsItemsMode `json:"mode"`
			Reason    *string                      `json:"reason,omitempty"`
		}{Available: o.Available, Mode: oapi.ComputeOptionsItemsMode(o.Mode)}
		if o.Reason != "" {
			r := o.Reason
			item.Reason = &r
		}
		resp.Items = append(resp.Items, item)
	}
	return oapi.GetComputeOptions200JSONResponse(resp), nil
}

func (s server) ListOrgSharedPools(ctx context.Context, req oapi.ListOrgSharedPoolsRequestObject) (oapi.ListOrgSharedPoolsResponseObject, error) {
	pools, err := s.orgs.CatalogSharedPools(ctx, userID(ctx), req.OrgId, string(req.ClusterId))
	if err != nil {
		return nil, err
	}
	resp := oapi.OrgSharedPoolList{Items: make([]oapi.OrgSharedPool, 0, len(pools))}
	for _, p := range pools {
		item := oapi.OrgSharedPool{Id: p.ID, Name: p.Name, NodeCount: len(p.Nodes)}
		item.Capacity.Ceiling, item.Capacity.Allocated = fromCapacity(p.Ceiling), fromCapacity(p.Allocated)
		resp.Items = append(resp.Items, item)
	}
	return oapi.ListOrgSharedPools200JSONResponse(resp), nil
}

func (s server) ListOrgDedicatedNodes(ctx context.Context, req oapi.ListOrgDedicatedNodesRequestObject) (oapi.ListOrgDedicatedNodesResponseObject, error) {
	nodes, err := s.orgs.CatalogDedicatedNodes(ctx, userID(ctx), req.OrgId, string(req.ClusterId))
	if err != nil {
		return nil, err
	}
	resp := oapi.OrgDedicatedNodeList{Items: make([]oapi.OrgDedicatedNode, 0, len(nodes))}
	for _, d := range nodes {
		resp.Items = append(resp.Items, oapi.OrgDedicatedNode{
			Id: d.ID, Name: d.NodeName,
			NodeState: oapi.OrgDedicatedNodeNodeState(d.NodeState), CapacityState: oapi.OrgDedicatedNodeCapacityState(d.CapacityState),
			Reservable: fromCapacity(d.Reservable), Reserved: fromCapacity(d.Reserved), Remaining: fromCapacity(d.Remaining),
			Workspaces: d.Workspaces,
		})
	}
	return oapi.ListOrgDedicatedNodes200JSONResponse(resp), nil
}

func (s server) ListOrgNetworkPools(ctx context.Context, req oapi.ListOrgNetworkPoolsRequestObject) (oapi.ListOrgNetworkPoolsResponseObject, error) {
	pools, err := s.orgs.CatalogNetworkPools(ctx, userID(ctx), req.OrgId, string(req.ClusterId))
	if err != nil {
		return nil, err
	}
	resp := oapi.OrgNetworkPoolList{Items: make([]oapi.OrgNetworkPool, 0, len(pools))}
	for _, p := range pools {
		n := toNetworkPool(p)
		resp.Items = append(resp.Items, oapi.OrgNetworkPool{Id: n.Id, Cidrs: n.Cidrs, DefaultPrefixLength: n.DefaultPrefixLength,
			AllowedPrefixLengths: n.AllowedPrefixLengths, Shared: p.OrgID == nil})
	}
	return oapi.ListOrgNetworkPools200JSONResponse(resp), nil
}
