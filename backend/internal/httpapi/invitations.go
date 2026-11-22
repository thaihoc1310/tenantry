package httpapi

import (
	"context"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
	"github.com/thaihoc1310/tenantry/backend/internal/org"
)

func (s server) ListInvitations(ctx context.Context, req oapi.ListInvitationsRequestObject) (oapi.ListInvitationsResponseObject, error) {
	var cursor string
	var limit int
	if req.Params.Cursor != nil {
		cursor = *req.Params.Cursor
	}
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	page, err := s.orgs.Invitations(ctx, userID(ctx), req.OrgId, cursor, limit)
	if err != nil {
		return nil, err
	}
	resp := oapi.InvitationList{Items: make([]oapi.Invitation, 0, len(page.Items))}
	for _, i := range page.Items {
		resp.Items = append(resp.Items, toInvitation(i))
	}
	if page.NextCursor != "" {
		resp.NextCursor = &page.NextCursor
	}
	return oapi.ListInvitations200JSONResponse(resp), nil
}

func (s server) CreateInvitation(ctx context.Context, req oapi.CreateInvitationRequestObject) (oapi.CreateInvitationResponseObject, error) {
	r := org.InviteRequest{Email: req.Body.Email, Role: org.Role(req.Body.Role)}
	if req.Body.GroupIds != nil {
		r.GroupIDs = *req.Body.GroupIds
	}
	created, err := s.orgs.Invite(ctx, userID(ctx), req.OrgId, r)
	if err != nil {
		return nil, err
	}
	inv := toInvitation(created.Invitation)
	// A repeat with the same Idempotency-Key gets the invitation without its
	// link: the token is sent once and kept nowhere (02-data-model §2).
	if err := storeForReplay(ctx, inv); err != nil {
		return nil, err
	}
	link := s.publicURL + "/invitations/" + created.Token
	inv.Link = &link
	return oapi.CreateInvitation201JSONResponse(inv), nil
}

func (s server) RevokeInvitation(ctx context.Context, req oapi.RevokeInvitationRequestObject) (oapi.RevokeInvitationResponseObject, error) {
	if err := s.orgs.Revoke(ctx, userID(ctx), req.Invitation); err != nil {
		return nil, err
	}
	return oapi.RevokeInvitation204Response{}, nil
}

func (s server) PreviewInvitation(ctx context.Context, req oapi.PreviewInvitationRequestObject) (oapi.PreviewInvitationResponseObject, error) {
	p, err := s.orgs.Preview(ctx, caller(ctx).user, req.Invitation)
	if err != nil {
		return nil, err
	}
	groups := p.GroupNames
	if groups == nil {
		groups = []string{}
	}
	return oapi.PreviewInvitation200JSONResponse{
		OrgId: p.OrgID, OrgName: p.OrgName, OrgSlug: p.OrgSlug, Role: oapi.InvitationRole(p.Role),
		Email: p.Email, EmailMatches: p.EmailMatches, InvitedBy: p.InviterName, GroupNames: groups,
		ExpiresAt: p.ExpiresAt, AlreadyMember: p.AlreadyMember,
	}, nil
}

func (s server) AcceptInvitation(ctx context.Context, req oapi.AcceptInvitationRequestObject) (oapi.AcceptInvitationResponseObject, error) {
	o, err := s.orgs.Accept(ctx, caller(ctx).user, req.Token)
	if err != nil {
		return nil, err
	}
	return oapi.AcceptInvitation200JSONResponse(toOrganization(o)), nil
}

func toInvitation(i org.Invitation) oapi.Invitation {
	state := oapi.InvitationStatePending
	if i.Expired(timeNow()) {
		state = oapi.InvitationStateExpired
	}
	groups := i.GroupIds
	if groups == nil {
		groups = []string{}
	}
	return oapi.Invitation{
		Id: i.ID, OrgId: i.OrgID, Email: i.Email, Role: oapi.InvitationRole(i.Role), GroupIds: groups,
		InvitedBy: oapi.UserSummary{Id: i.InvitedBy, Email: i.InviterEmail, DisplayName: i.InviterName},
		State:     state, ExpiresAt: i.ExpiresAt, CreatedAt: i.CreatedAt,
	}
}
