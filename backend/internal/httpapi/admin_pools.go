package httpapi

import (
	"context"
	"math"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
	"github.com/thaihoc1310/tenantry/backend/internal/reservation"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// Shared pools, network pools, nodes and dedicated nodes as a Platform Admin
// manages them (PRODUCT-PLAN §6.2–6.3, §7.1).

func toCapacity(field string, in oapi.NodeCapacity) (reservation.Capacity, error) {
	cpu, err := parseCPU(field+".cpu", in.Cpu)
	if err != nil {
		return reservation.Capacity{}, err
	}
	mem, err := parseBytes(field+".memory", in.Memory)
	if err != nil {
		return reservation.Capacity{}, err
	}
	return reservation.Capacity{CPUMilli: cpu, MemoryBytes: mem, Pods: int64(in.Pods)}, nil
}

func fromCapacity(c reservation.Capacity) oapi.NodeCapacity {
	return oapi.NodeCapacity{Cpu: formatCPU(c.CPUMilli), Memory: formatBytes(c.MemoryBytes), Pods: int(c.Pods)}
}

func (s server) ListClusterNodes(ctx context.Context, req oapi.ListClusterNodesRequestObject) (oapi.ListClusterNodesResponseObject, error) {
	nodes, err := s.clusters.Nodes(ctx, string(req.ClusterId))
	if err != nil {
		return nil, err
	}
	resp := oapi.ClusterNodeList{Items: make([]oapi.ClusterNode, 0, len(nodes))}
	for _, n := range nodes {
		item := oapi.ClusterNode{
			Name: n.NodeName, Kind: oapi.ClusterNodeKind(n.Kind), Ready: n.Ready, Schedulable: n.Schedulable,
			Allocatable:  fromCapacity(reservation.Capacity{CPUMilli: n.AllocatableCpuMilli, MemoryBytes: n.AllocatableMemoryBytes, Pods: int64(n.AllocatablePods)}),
			SharedPoolId: n.SharedPoolID, ObservedAt: n.ObservedAt,
		}
		if n.DedicatedOrgID != nil {
			item.DedicatedTo = &oapi.OrganizationRef{Id: *n.DedicatedOrgID, Slug: deref(n.DedicatedOrgSlug), DisplayName: deref(n.DedicatedOrgName)}
		}
		resp.Items = append(resp.Items, item)
	}
	return oapi.ListClusterNodes200JSONResponse(resp), nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func toSharedPool(in oapi.SharedPoolInput) (reservation.SharedPool, error) {
	reserve, err := toCapacity("systemReserve", in.SystemReserve)
	if err != nil {
		return reservation.SharedPool{}, err
	}
	return reservation.SharedPool{
		Name: in.Name, RatioHundredths: int64(math.Round(float64(in.AllocationOvercommitRatio) * 100)),
		SystemReserve: reserve, Nodes: in.Nodes,
	}, nil
}

func fromSharedPool(v reservation.PoolView) oapi.SharedPool {
	reserve := reservation.Capacity{CPUMilli: v.SystemReserveCpuMilli, MemoryBytes: v.SystemReserveMemoryBytes, Pods: int64(v.SystemReservePods)}
	p := oapi.SharedPool{
		Id: v.ID, ClusterId: v.ClusterID, Name: v.Name, AllocationOvercommitRatio: float32(v.RatioHundredths) / 100,
		SystemReserve: fromCapacity(reserve), Nodes: v.Nodes,
	}
	p.Capacity.Reservable, p.Capacity.Ceiling, p.Capacity.Allocated = fromCapacity(v.Reservable), fromCapacity(v.Ceiling), fromCapacity(v.Allocated)
	if v.Operation != nil {
		op := toOperation(*v.Operation)
		p.Operation = &op
	}
	return p
}

func (s server) ListSharedPools(ctx context.Context, req oapi.ListSharedPoolsRequestObject) (oapi.ListSharedPoolsResponseObject, error) {
	pools, err := s.clusters.SharedPools(ctx, string(req.ClusterId))
	if err != nil {
		return nil, err
	}
	resp := oapi.SharedPoolList{Items: make([]oapi.SharedPool, 0, len(pools))}
	for _, p := range pools {
		resp.Items = append(resp.Items, fromSharedPool(p))
	}
	return oapi.ListSharedPools200JSONResponse(resp), nil
}

func (s server) CreateSharedPool(ctx context.Context, req oapi.CreateSharedPoolRequestObject) (oapi.CreateSharedPoolResponseObject, error) {
	b := req.Body
	p, err := toSharedPool(oapi.SharedPoolInput{Name: b.Name, AllocationOvercommitRatio: b.AllocationOvercommitRatio, SystemReserve: b.SystemReserve, Nodes: b.Nodes})
	if err != nil {
		return nil, err
	}
	p.ID = string(b.Id)
	v, err := s.clusters.CreateSharedPool(ctx, userID(ctx), requestID(ctx), string(req.ClusterId), p)
	if err != nil {
		return nil, err
	}
	return oapi.CreateSharedPool201JSONResponse(fromSharedPool(v)), nil
}

func (s server) PutSharedPool(ctx context.Context, req oapi.PutSharedPoolRequestObject) (oapi.PutSharedPoolResponseObject, error) {
	p, err := toSharedPool(*req.Body)
	if err != nil {
		return nil, err
	}
	v, err := s.clusters.UpdateSharedPool(ctx, userID(ctx), requestID(ctx), string(req.ClusterId), string(req.PoolId), p)
	if err != nil {
		return nil, err
	}
	return oapi.PutSharedPool200JSONResponse(fromSharedPool(v)), nil
}

func toNetworkPool(p store.NetworkPool) oapi.NetworkPool {
	cidrs := make([]string, len(p.Cidrs))
	for i, c := range p.Cidrs {
		cidrs[i] = c.String()
	}
	allowed := make([]int, len(p.AllowedPrefixLengths))
	for i, l := range p.AllowedPrefixLengths {
		allowed[i] = int(l)
	}
	return oapi.NetworkPool{Id: p.ID, ClusterId: p.ClusterID, OrganizationId: p.OrgID, Cidrs: cidrs,
		DefaultPrefixLength: int(p.DefaultPrefixLength), AllowedPrefixLengths: allowed}
}

func int32s(v []int) []int32 {
	out := make([]int32, len(v))
	for i, x := range v {
		out[i] = int32(x)
	}
	return out
}

func (s server) ListNetworkPools(ctx context.Context, req oapi.ListNetworkPoolsRequestObject) (oapi.ListNetworkPoolsResponseObject, error) {
	pools, err := s.clusters.NetworkPools(ctx, string(req.ClusterId))
	if err != nil {
		return nil, err
	}
	resp := oapi.NetworkPoolList{Items: make([]oapi.NetworkPool, 0, len(pools))}
	for _, p := range pools {
		resp.Items = append(resp.Items, toNetworkPool(p))
	}
	return oapi.ListNetworkPools200JSONResponse(resp), nil
}

func (s server) CreateNetworkPool(ctx context.Context, req oapi.CreateNetworkPoolRequestObject) (oapi.CreateNetworkPoolResponseObject, error) {
	b := req.Body
	cidrs, err := parseCIDRs("cidrs", &b.Cidrs)
	if err != nil {
		return nil, err
	}
	p := reservation.NetworkPool{ID: string(b.Id), CIDRs: cidrs, DefaultPrefix: int32(b.DefaultPrefixLength), AllowedPrefixs: int32s(b.AllowedPrefixLengths)}
	if b.OrganizationId != nil {
		p.OrgID = *b.OrganizationId
	}
	out, err := s.clusters.CreateNetworkPool(ctx, userID(ctx), string(req.ClusterId), p)
	if err != nil {
		return nil, err
	}
	return oapi.CreateNetworkPool201JSONResponse(toNetworkPool(out)), nil
}

func (s server) PatchNetworkPool(ctx context.Context, req oapi.PatchNetworkPoolRequestObject) (oapi.PatchNetworkPoolResponseObject, error) {
	b := req.Body
	var p reservation.NetworkPoolPatch
	if b.Cidrs != nil {
		cidrs, err := parseCIDRs("cidrs", b.Cidrs)
		if err != nil {
			return nil, err
		}
		p.CIDRs = &cidrs
	}
	if b.DefaultPrefixLength != nil {
		d := int32(*b.DefaultPrefixLength)
		p.DefaultPrefix = &d
	}
	if b.AllowedPrefixLengths != nil {
		a := int32s(*b.AllowedPrefixLengths)
		p.AllowedPrefixs = &a
	}
	out, err := s.clusters.UpdateNetworkPool(ctx, userID(ctx), string(req.ClusterId), string(req.PoolId), p)
	if err != nil {
		return nil, err
	}
	return oapi.PatchNetworkPool200JSONResponse(toNetworkPool(out)), nil
}

func toDedicatedNode(d reservation.DedicatedNode) oapi.DedicatedNode {
	return oapi.DedicatedNode{
		Id: d.ID, ClusterId: d.ClusterID, Name: d.NodeName,
		Allocatable: fromCapacity(d.Allocatable), SystemReserve: fromCapacity(d.SystemReserve),
		Reservable: fromCapacity(d.Reservable), Reserved: fromCapacity(d.Reserved), Remaining: fromCapacity(d.Remaining),
		NodeState: oapi.DedicatedNodeNodeState(d.NodeState), CapacityState: oapi.DedicatedNodeCapacityState(d.CapacityState),
	}
}

func (s server) PutDedicatedNodes(ctx context.Context, req oapi.PutDedicatedNodesRequestObject) (oapi.PutDedicatedNodesResponseObject, error) {
	nodes := make([]reservation.NodeRequest, 0, len(req.Body.Nodes))
	for _, n := range req.Body.Nodes {
		r, err := toCapacity("nodes.systemReserve", n.SystemReserve)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, reservation.NodeRequest{Name: n.Name, SystemReserve: r})
	}
	out, ops, err := s.clusters.SetDedicatedNodes(ctx, userID(ctx), requestID(ctx), req.OrgId, string(req.ClusterId), nodes)
	if err != nil {
		return nil, err
	}
	resp := oapi.DedicatedNodeList{Items: make([]oapi.DedicatedNode, 0, len(out))}
	for _, d := range out {
		resp.Items = append(resp.Items, toDedicatedNode(d))
	}
	if len(ops) > 0 {
		list := make([]oapi.Operation, len(ops))
		for i, op := range ops {
			list[i] = toOperation(op)
		}
		resp.Operations = &list
	}
	return oapi.PutDedicatedNodes200JSONResponse(resp), nil
}
