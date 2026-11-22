package httpapi

import (
	"context"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
	"github.com/thaihoc1310/tenantry/backend/internal/org"
)

func (s server) ListGroups(ctx context.Context, req oapi.ListGroupsRequestObject) (oapi.ListGroupsResponseObject, error) {
	groups, err := s.orgs.Groups(ctx, userID(ctx), req.OrgId)
	if err != nil {
		return nil, err
	}
	resp := oapi.GroupList{Items: make([]oapi.Group, 0, len(groups))}
	for _, g := range groups {
		resp.Items = append(resp.Items, toGroup(g))
	}
	return oapi.ListGroups200JSONResponse(resp), nil
}

func (s server) CreateGroup(ctx context.Context, req oapi.CreateGroupRequestObject) (oapi.CreateGroupResponseObject, error) {
	g, err := s.orgs.CreateGroup(ctx, userID(ctx), req.OrgId, req.Body.Name)
	if err != nil {
		return nil, err
	}
	return oapi.CreateGroup201JSONResponse(toGroup(g)), nil
}

func (s server) PatchGroup(ctx context.Context, req oapi.PatchGroupRequestObject) (oapi.PatchGroupResponseObject, error) {
	g, err := s.orgs.RenameGroup(ctx, userID(ctx), req.GroupId, req.Body.Name)
	if err != nil {
		return nil, err
	}
	return oapi.PatchGroup200JSONResponse(toGroup(g)), nil
}

func (s server) DeleteGroup(ctx context.Context, req oapi.DeleteGroupRequestObject) (oapi.DeleteGroupResponseObject, error) {
	if err := s.orgs.DeleteGroup(ctx, userID(ctx), req.GroupId); err != nil {
		return nil, err
	}
	return oapi.DeleteGroup204Response{}, nil
}

func (s server) AddGroupMember(ctx context.Context, req oapi.AddGroupMemberRequestObject) (oapi.AddGroupMemberResponseObject, error) {
	if err := s.orgs.AddToGroup(ctx, userID(ctx), req.GroupId, req.UserId); err != nil {
		return nil, err
	}
	return oapi.AddGroupMember204Response{}, nil
}

func (s server) RemoveGroupMember(ctx context.Context, req oapi.RemoveGroupMemberRequestObject) (oapi.RemoveGroupMemberResponseObject, error) {
	if err := s.orgs.RemoveFromGroup(ctx, userID(ctx), req.GroupId, req.UserId); err != nil {
		return nil, err
	}
	return oapi.RemoveGroupMember204Response{}, nil
}

func toGroup(g org.Group) oapi.Group {
	members := make([]oapi.GroupMember, len(g.Members))
	for i, m := range g.Members {
		members[i] = oapi.GroupMember{UserId: m.UserID, DisplayName: m.DisplayName, Email: m.Email, SyncStatus: oapi.SyncStatus(m.SyncStatus)}
	}
	return oapi.Group{
		Id: g.ID, OrgId: g.OrgID, Name: g.DisplayName, MachineName: g.MachineName,
		SyncStatus: oapi.SyncStatus(g.SyncStatus), Members: members, CreatedAt: g.CreatedAt,
	}
}
