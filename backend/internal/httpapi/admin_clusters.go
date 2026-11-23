package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
	"github.com/thaihoc1310/tenantry/backend/internal/org"
	"github.com/thaihoc1310/tenantry/backend/internal/reservation"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// The cluster registry, the onboarding policy, cluster grants and operations
// as a Platform Admin manages them (PRODUCT-PLAN §16.5).

func (s server) ListAdminClusters(ctx context.Context, _ oapi.ListAdminClustersRequestObject) (oapi.ListAdminClustersResponseObject, error) {
	cs, err := s.clusters.Clusters(ctx)
	if err != nil {
		return nil, err
	}
	resp := oapi.AdminClusterList{Items: make([]oapi.AdminCluster, 0, len(cs))}
	for _, c := range cs {
		resp.Items = append(resp.Items, toAdminCluster(c))
	}
	return oapi.ListAdminClusters200JSONResponse(resp), nil
}

func (s server) RegisterCluster(ctx context.Context, req oapi.RegisterClusterRequestObject) (oapi.RegisterClusterResponseObject, error) {
	b := req.Body
	cidrs, err := parseCIDRs("reservedCidrs", b.ReservedCidrs)
	if err != nil {
		return nil, err
	}
	c, err := s.clusters.Register(ctx, userID(ctx), reservation.Cluster{
		ID: b.Id, DisplayName: b.DisplayName, ProxyEndpoint: b.ProxyEndpoint, ConsoleURL: b.ConsoleUrl,
		AppsDomain: b.AppsDomain, ReservedCIDRs: cidrs,
	})
	if err != nil {
		return nil, err
	}
	return oapi.RegisterCluster201JSONResponse(toAdminCluster(c)), nil
}

func (s server) DeleteCluster(ctx context.Context, req oapi.DeleteClusterRequestObject) (oapi.DeleteClusterResponseObject, error) {
	if err := s.clusters.DeleteCluster(ctx, userID(ctx), req.ClusterId); err != nil {
		return nil, err
	}
	return oapi.DeleteCluster204Response{}, nil
}

func (s server) PatchCluster(ctx context.Context, req oapi.PatchClusterRequestObject) (oapi.PatchClusterResponseObject, error) {
	b := req.Body
	p := reservation.ClusterPatch{DisplayName: b.DisplayName, ProxyEndpoint: b.ProxyEndpoint, ConsoleURL: b.ConsoleUrl, AppsDomain: b.AppsDomain}
	if b.Status != nil {
		st := string(*b.Status)
		p.Status = &st
	}
	if b.ReservedCidrs != nil {
		cidrs, err := parseCIDRs("reservedCidrs", b.ReservedCidrs)
		if err != nil {
			return nil, err
		}
		p.ReservedCIDRs = &cidrs
	}
	c, err := s.clusters.UpdateCluster(ctx, userID(ctx), req.ClusterId, p)
	if err != nil {
		return nil, err
	}
	return oapi.PatchCluster200JSONResponse(toAdminCluster(c)), nil
}

// parseCIDRs reads a list of CIDRs of a request field.
func parseCIDRs(field string, in *[]string) ([]netip.Prefix, error) {
	if in == nil {
		return []netip.Prefix{}, nil
	}
	out := make([]netip.Prefix, 0, len(*in))
	for _, v := range *in {
		p, err := netip.ParsePrefix(v)
		if err != nil {
			return nil, apperr.Invalid(field, fmt.Sprintf("%q is not a CIDR", v))
		}
		out = append(out, p)
	}
	return out, nil
}

func toAdminCluster(c store.Cluster) oapi.AdminCluster {
	cidrs := make([]string, len(c.ReservedCidrs))
	for i, p := range c.ReservedCidrs {
		cidrs[i] = p.String()
	}
	return oapi.AdminCluster{
		Id: c.ID, DisplayName: c.DisplayName, Status: oapi.AdminClusterStatus(c.Status), ProxyEndpoint: c.ProxyEndpoint,
		ConsoleUrl: c.ConsoleUrl, AppsDomain: c.AppsDomain, ReservedCidrs: cidrs, LastHealthCheck: c.LastHealthCheck, CreatedAt: c.CreatedAt,
	}
}

func (s server) GetOnboardingPolicy(ctx context.Context, _ oapi.GetOnboardingPolicyRequestObject) (oapi.GetOnboardingPolicyResponseObject, error) {
	p, err := s.proposal(ctx)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, org.ErrNoPolicy
	}
	return oapi.GetOnboardingPolicy200JSONResponse(*p), nil
}

func (s server) PutOnboardingPolicy(ctx context.Context, req oapi.PutOnboardingPolicyRequestObject) (oapi.PutOnboardingPolicyResponseObject, error) {
	grant, err := toGrantSpec(req.Body.ClusterGrant)
	if err != nil {
		return nil, err
	}
	// Stored in the API's shape, normalized, without the cluster: the
	// policy's default cluster fills it in (as the seed writes it).
	tmpl := toGrantInput(grant)
	b, err := json.Marshal(tmpl)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	delete(doc, "clusterId")
	if b, err = json.Marshal(doc); err != nil {
		return nil, err
	}
	if err := s.orgs.SetPolicy(ctx, userID(ctx), toEntitlement(req.Body.Entitlement), grant, b); err != nil {
		return nil, err
	}
	return oapi.PutOnboardingPolicy200JSONResponse{Entitlement: req.Body.Entitlement, ClusterGrant: tmpl}, nil
}

// toGrantInput is a grant spec in the API's input shape, quantities normalized.
func toGrantInput(g reservation.GrantSpec) oapi.ClusterGrantInput {
	c := toClusterGrant(reservation.Grant{GrantSpec: g})
	return oapi.ClusterGrantInput{
		ClusterId: c.ClusterId, MaxWorkspaces: c.MaxWorkspaces, MaxNamespacesTotal: c.MaxNamespacesTotal,
		Resources: c.Resources, SharedPoolIds: c.SharedPoolIds, NetworkPoolIds: c.NetworkPoolIds,
		AllowedStorageClasses: c.AllowedStorageClasses,
	}
}

func (s server) ListAdminClusterGrants(ctx context.Context, req oapi.ListAdminClusterGrantsRequestObject) (oapi.ListAdminClusterGrantsResponseObject, error) {
	var cluster, cursor string
	var limit int
	if req.Params.ClusterId != nil {
		cluster = string(*req.Params.ClusterId)
	}
	if req.Params.Cursor != nil {
		cursor = *req.Params.Cursor
	}
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	p, err := s.orgs.AllGrants(ctx, cluster, cursor, limit)
	if err != nil {
		return nil, err
	}
	resp := oapi.AdminClusterGrantList{Items: make([]oapi.AdminClusterGrant, 0, len(p.Items))}
	for _, g := range p.Items {
		resp.Items = append(resp.Items, oapi.AdminClusterGrant{
			Organization: oapi.OrganizationRef{Id: g.OrgID, Slug: g.OrgSlug, DisplayName: g.OrgDisplayName},
			ClusterGrant: toClusterGrant(g.Grant),
			Usage:        grantUsage(g.Grant, g.Allocation),
		})
	}
	if p.NextCursor != "" {
		resp.NextCursor = &p.NextCursor
	}
	return oapi.ListAdminClusterGrants200JSONResponse(resp), nil
}

func (s server) PutClusterGrant(ctx context.Context, req oapi.PutClusterGrantRequestObject) (oapi.PutClusterGrantResponseObject, error) {
	if req.Body.ClusterId != req.ClusterId {
		return nil, apperr.Invalid("clusterId", "must be the cluster of the path")
	}
	spec, err := toGrantSpec(*req.Body)
	if err != nil {
		return nil, err
	}
	g, created, err := s.orgs.PutGrant(ctx, userID(ctx), req.OrgId, spec)
	if err != nil {
		return nil, err
	}
	if created {
		return oapi.PutClusterGrant201JSONResponse(toClusterGrant(g)), nil
	}
	return oapi.PutClusterGrant200JSONResponse(toClusterGrant(g)), nil
}

func (s server) DeleteClusterGrant(ctx context.Context, req oapi.DeleteClusterGrantRequestObject) (oapi.DeleteClusterGrantResponseObject, error) {
	if err := s.orgs.DeleteGrant(ctx, userID(ctx), req.OrgId, req.ClusterId); err != nil {
		return nil, err
	}
	return oapi.DeleteClusterGrant204Response{}, nil
}

func (s server) ListAdminOperations(ctx context.Context, req oapi.ListAdminOperationsRequestObject) (oapi.ListAdminOperationsResponseObject, error) {
	var state, cursor string
	var limit int
	if req.Params.State != nil {
		state = string(*req.Params.State)
	}
	if req.Params.Cursor != nil {
		cursor = *req.Params.Cursor
	}
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	attention := req.Params.Attention != nil && *req.Params.Attention
	p, err := s.ops.AdminList(ctx, state, attention, cursor, limit)
	if err != nil {
		return nil, err
	}
	resp := oapi.OperationList{Items: make([]oapi.Operation, 0, len(p.Items))}
	for _, op := range p.Items {
		resp.Items = append(resp.Items, toOperation(op))
	}
	if p.NextCursor != "" {
		resp.NextCursor = &p.NextCursor
	}
	return oapi.ListAdminOperations200JSONResponse(resp), nil
}
