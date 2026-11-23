package httpapi

import (
	"context"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
	"github.com/thaihoc1310/tenantry/backend/internal/org"
	"github.com/thaihoc1310/tenantry/backend/internal/reservation"
	"github.com/thaihoc1310/tenantry/backend/internal/user"
)

// Organizations and users as a Platform Admin manages them (PRODUCT-PLAN §16.5).

func (s server) CreateAdminOrganization(ctx context.Context, req oapi.CreateAdminOrganizationRequestObject) (oapi.CreateAdminOrganizationResponseObject, error) {
	grant, err := toGrantSpec(req.Body.ClusterGrant)
	if err != nil {
		return nil, err
	}
	b := req.Body
	n := org.NewOrganization{
		Request:    org.Request{Slug: b.Slug, DisplayName: b.DisplayName},
		OwnerEmail: b.OwnerEmail,
		Approval:   org.Approval{Entitlement: toEntitlement(b.Entitlement), Grant: grant},
	}
	if b.Description != nil {
		n.Description = *b.Description
	}
	a, err := s.orgs.CreateActive(ctx, userID(ctx), n)
	if err != nil {
		return nil, err
	}
	return oapi.CreateAdminOrganization201JSONResponse{
		Organization: toAdminOrganization(a.Reviewed),
		Entitlement:  fromEntitlement(a.Entitlement),
		ClusterGrant: toClusterGrant(a.Grant),
	}, nil
}

func (s server) GetAdminOrganization(ctx context.Context, req oapi.GetAdminOrganizationRequestObject) (oapi.GetAdminOrganizationResponseObject, error) {
	d, err := s.orgs.AdminDetail(ctx, req.OrgId)
	if err != nil {
		return nil, err
	}
	resp := oapi.AdminOrganizationDetail{Organization: toAdminOrganization(d.Reviewed), ClusterGrants: []oapi.ClusterGrant{}}
	if d.Usage != nil {
		e := fromEntitlement(d.Usage.Entitlement)
		u := toUsage(*d.Usage)
		resp.Entitlement, resp.Usage = &e, &u
		resp.ClusterGrants = grantsOf(d.Usage.Grants)
		nodes := []oapi.DedicatedNode{}
		for _, g := range d.Usage.Grants {
			held, err := s.clusters.DedicatedNodes(ctx, d.ID, g.ClusterID)
			if err != nil {
				return nil, err
			}
			for _, n := range held {
				nodes = append(nodes, toDedicatedNode(n))
			}
		}
		resp.DedicatedNodes = &nodes
	}
	return oapi.GetAdminOrganization200JSONResponse(resp), nil
}

func (s server) DeleteAdminOrganization(ctx context.Context, req oapi.DeleteAdminOrganizationRequestObject) (oapi.DeleteAdminOrganizationResponseObject, error) {
	if err := s.orgs.AdminDelete(ctx, userID(ctx), req.OrgId); err != nil {
		return nil, err
	}
	return oapi.DeleteAdminOrganization204Response{}, nil
}

func grantsOf(gs []reservation.Grant) []oapi.ClusterGrant {
	out := make([]oapi.ClusterGrant, 0, len(gs))
	for _, g := range gs {
		out = append(out, toClusterGrant(g))
	}
	return out
}

func (s server) PutAdminEntitlement(ctx context.Context, req oapi.PutAdminEntitlementRequestObject) (oapi.PutAdminEntitlementResponseObject, error) {
	e, err := s.orgs.SetEntitlement(ctx, userID(ctx), req.OrgId, toEntitlement(*req.Body))
	if err != nil {
		return nil, err
	}
	return oapi.PutAdminEntitlement200JSONResponse(fromEntitlement(e)), nil
}

func (s server) ListAdminUsers(ctx context.Context, req oapi.ListAdminUsersRequestObject) (oapi.ListAdminUsersResponseObject, error) {
	var q, cursor string
	var limit int
	if req.Params.Q != nil {
		q = *req.Params.Q
	}
	if req.Params.Cursor != nil {
		cursor = *req.Params.Cursor
	}
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	p, err := s.users.List(ctx, q, cursor, limit)
	if err != nil {
		return nil, err
	}
	resp := oapi.AdminUserList{Items: make([]oapi.AdminUser, 0, len(p.Items))}
	for _, u := range p.Items {
		resp.Items = append(resp.Items, toAdminUser(u))
	}
	if p.NextCursor != "" {
		resp.NextCursor = &p.NextCursor
	}
	return oapi.ListAdminUsers200JSONResponse(resp), nil
}

func (s server) DisableUser(ctx context.Context, req oapi.DisableUserRequestObject) (oapi.DisableUserResponseObject, error) {
	u, err := s.users.Disable(ctx, userID(ctx), req.UserId)
	if err != nil {
		return nil, err
	}
	return oapi.DisableUser200JSONResponse(toAdminUser(u)), nil
}

func (s server) EnableUser(ctx context.Context, req oapi.EnableUserRequestObject) (oapi.EnableUserResponseObject, error) {
	u, err := s.users.Enable(ctx, userID(ctx), req.UserId)
	if err != nil {
		return nil, err
	}
	return oapi.EnableUser200JSONResponse(toAdminUser(u)), nil
}

func toAdminUser(u user.User) oapi.AdminUser {
	return oapi.AdminUser{
		Id: u.ID, Email: u.Email, DisplayName: u.DisplayName, Status: oapi.AdminUserStatus(u.Status),
		SyncStatus: oapi.SyncStatus(u.SyncStatus), CreatedAt: u.CreatedAt,
	}
}
