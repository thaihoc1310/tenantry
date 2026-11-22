package httpapi

import (
	"context"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
	"github.com/thaihoc1310/tenantry/backend/internal/org"
)

func (s server) ListMembers(ctx context.Context, req oapi.ListMembersRequestObject) (oapi.ListMembersResponseObject, error) {
	var cursor string
	var limit int
	if req.Params.Cursor != nil {
		cursor = *req.Params.Cursor
	}
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	page, err := s.orgs.Members(ctx, userID(ctx), req.OrgId, cursor, limit)
	if err != nil {
		return nil, err
	}
	resp := oapi.MemberList{Items: make([]oapi.Member, 0, len(page.Items))}
	for _, m := range page.Items {
		resp.Items = append(resp.Items, toMember(m))
	}
	if page.NextCursor != "" {
		resp.NextCursor = &page.NextCursor
	}
	return oapi.ListMembers200JSONResponse(resp), nil
}

func (s server) PatchMember(ctx context.Context, req oapi.PatchMemberRequestObject) (oapi.PatchMemberResponseObject, error) {
	m, err := s.orgs.SetRole(ctx, userID(ctx), req.OrgId, req.UserId, org.Role(req.Body.Role))
	if err != nil {
		return nil, err
	}
	return oapi.PatchMember200JSONResponse(toMember(m)), nil
}

func (s server) RemoveMember(ctx context.Context, req oapi.RemoveMemberRequestObject) (oapi.RemoveMemberResponseObject, error) {
	if err := s.orgs.Remove(ctx, userID(ctx), req.OrgId, req.UserId); err != nil {
		return nil, err
	}
	return oapi.RemoveMember204Response{}, nil
}

func (s server) LeaveOrganization(ctx context.Context, req oapi.LeaveOrganizationRequestObject) (oapi.LeaveOrganizationResponseObject, error) {
	if err := s.orgs.Leave(ctx, userID(ctx), req.OrgId); err != nil {
		return nil, err
	}
	return oapi.LeaveOrganization204Response{}, nil
}

func (s server) TransferOwnership(ctx context.Context, req oapi.TransferOwnershipRequestObject) (oapi.TransferOwnershipResponseObject, error) {
	t, err := s.orgs.ProposeTransfer(ctx, userID(ctx), req.OrgId, req.Body.ToUserId)
	if err != nil {
		return nil, err
	}
	return oapi.TransferOwnership201JSONResponse(toTransfer(t)), nil
}

func (s server) GetOwnershipTransfer(ctx context.Context, req oapi.GetOwnershipTransferRequestObject) (oapi.GetOwnershipTransferResponseObject, error) {
	t, err := s.orgs.PendingTransfer(ctx, userID(ctx), req.OrgId)
	if err != nil {
		return nil, err
	}
	return oapi.GetOwnershipTransfer200JSONResponse(toTransfer(t)), nil
}

func (s server) AcceptOwnershipTransfer(ctx context.Context, req oapi.AcceptOwnershipTransferRequestObject) (oapi.AcceptOwnershipTransferResponseObject, error) {
	o, err := s.orgs.AcceptTransfer(ctx, userID(ctx), req.TransferId)
	if err != nil {
		return nil, err
	}
	return oapi.AcceptOwnershipTransfer200JSONResponse(toOrganization(o)), nil
}

func (s server) CancelOwnershipTransfer(ctx context.Context, req oapi.CancelOwnershipTransferRequestObject) (oapi.CancelOwnershipTransferResponseObject, error) {
	if err := s.orgs.CancelTransfer(ctx, userID(ctx), req.TransferId); err != nil {
		return nil, err
	}
	return oapi.CancelOwnershipTransfer204Response{}, nil
}

func toMember(m org.Member) oapi.Member {
	groups := make([]oapi.GroupRef, len(m.GroupIDs))
	for i := range m.GroupIDs {
		groups[i] = oapi.GroupRef{Id: m.GroupIDs[i], Name: m.GroupNames[i]}
	}
	return oapi.Member{
		UserId: m.UserID, Email: m.Email, DisplayName: m.DisplayName, Role: oapi.OrgRole(m.Role),
		JoinedAt: m.JoinedAt, Groups: groups,
	}
}

func toTransfer(t org.Transfer) oapi.OwnershipTransfer {
	return oapi.OwnershipTransfer{
		Id: t.ID, OrgId: t.OrgID,
		From:  oapi.UserSummary{Id: t.FromUserID, Email: t.FromEmail, DisplayName: t.FromName},
		To:    oapi.UserSummary{Id: t.ToUserID, Email: t.ToEmail, DisplayName: t.ToName},
		State: oapi.Proposed, ExpiresAt: t.ExpiresAt, CreatedAt: t.CreatedAt,
	}
}
