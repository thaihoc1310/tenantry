package httpapi

import (
	"context"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
	"github.com/thaihoc1310/tenantry/backend/internal/reservation"
)

func (s server) GetEntitlement(ctx context.Context, req oapi.GetEntitlementRequestObject) (oapi.GetEntitlementResponseObject, error) {
	q, err := s.orgs.Quota(ctx, userID(ctx), req.OrgId)
	if err != nil {
		return nil, err
	}
	resp := oapi.OrganizationEntitlement{Entitlement: fromEntitlement(q.Entitlement), ClusterGrants: make([]oapi.ClusterGrant, 0, len(q.Grants))}
	for _, g := range q.Grants {
		resp.ClusterGrants = append(resp.ClusterGrants, toClusterGrant(g))
	}
	return oapi.GetEntitlement200JSONResponse(resp), nil
}

func (s server) GetUsage(ctx context.Context, req oapi.GetUsageRequestObject) (oapi.GetUsageResponseObject, error) {
	u, err := s.orgs.Usage(ctx, userID(ctx), req.OrgId)
	if err != nil {
		return nil, err
	}
	e := u.Entitlement
	var resp oapi.OrganizationUsage
	resp.Organization.Members = oapi.UsageCount{Used: int(u.Members), Limit: int(e.MaxMembers)}
	resp.Organization.Groups = oapi.UsageCount{Used: int(u.Groups), Limit: int(e.MaxGroups)}
	resp.Organization.Workspaces = oapi.UsageCount{Used: int(u.Workspaces), Limit: int(e.MaxWorkspacesTotal)}
	resp.ClusterGrants = make([]oapi.GrantUsage, 0, len(u.Grants))
	for i, g := range u.Grants {
		resp.ClusterGrants = append(resp.ClusterGrants, grantUsage(g, u.Allocations[i]))
	}
	return oapi.GetUsage200JSONResponse(resp), nil
}

func grantUsage(g reservation.Grant, a reservation.Allocation) oapi.GrantUsage {
	count := func(used, limit int32) oapi.AllocatedCount {
		return oapi.AllocatedCount{Allocated: int(used), Limit: int(limit), Remaining: int(max(limit-used, 0))}
	}
	amount := func(used, limit int64, format func(int64) oapi.Quantity) oapi.AllocatedQuantity {
		return oapi.AllocatedQuantity{Allocated: format(used), Limit: format(limit), Remaining: format(max(limit-used, 0))}
	}
	r, ar := g.Resources, a.Resources
	return oapi.GrantUsage{
		GrantId: g.ID, ClusterId: g.ClusterID, IsDefault: g.IsDefault,
		Workspaces:      count(a.Workspaces, g.MaxWorkspaces),
		Namespaces:      count(a.Namespaces, g.MaxNamespacesTotal),
		RequestsCpu:     amount(ar.RequestsCPUMilli, r.RequestsCPUMilli, formatCPU),
		LimitsCpu:       amount(ar.LimitsCPUMilli, r.LimitsCPUMilli, formatCPU),
		RequestsMemory:  amount(ar.RequestsMemoryBytes, r.RequestsMemoryBytes, formatBytes),
		LimitsMemory:    amount(ar.LimitsMemoryBytes, r.LimitsMemoryBytes, formatBytes),
		Pods:            count(ar.Pods, r.Pods),
		RequestsStorage: amount(ar.RequestsStorageBytes, r.RequestsStorageBytes, formatBytes),
	}
}
