package httpapi

import (
	"context"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
	"github.com/thaihoc1310/tenantry/backend/internal/workspace"
)

func (s server) ListWorkspaceRoutes(ctx context.Context, req oapi.ListWorkspaceRoutesRequestObject) (oapi.ListWorkspaceRoutesResponseObject, error) {
	rows, err := s.workspaces.Routes(ctx, userID(ctx), req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	out := oapi.WorkspaceRouteList{Items: make([]oapi.WorkspaceRoute, 0, len(rows))}
	for _, r := range rows {
		out.Items = append(out.Items, toRoute(r))
	}
	return oapi.ListWorkspaceRoutes200JSONResponse(out), nil
}

func (s server) CreateWorkspaceRoute(ctx context.Context, req oapi.CreateWorkspaceRouteRequestObject) (oapi.CreateWorkspaceRouteResponseObject, error) {
	b := req.Body
	c, err := s.workspaces.CreateRoute(ctx, userID(ctx), req.WorkspaceId, workspace.NewRoute{
		Namespace: b.Namespace, Name: b.Name, Service: b.Service, Port: int32(b.Port),
	}, requestID(ctx))
	if err != nil {
		return nil, err
	}
	return oapi.CreateWorkspaceRoute202JSONResponse(toRouteChange(c)), nil
}

func (s server) DeleteWorkspaceRoute(ctx context.Context, req oapi.DeleteWorkspaceRouteRequestObject) (oapi.DeleteWorkspaceRouteResponseObject, error) {
	c, err := s.workspaces.DeleteRoute(ctx, userID(ctx), req.WorkspaceId, req.RouteId, requestID(ctx))
	if err != nil {
		return nil, err
	}
	return oapi.DeleteWorkspaceRoute202JSONResponse(toRouteChange(c)), nil
}

func toRoute(r store.WorkspaceRoute) oapi.WorkspaceRoute {
	return oapi.WorkspaceRoute{
		Id: r.ID, Namespace: r.Namespace, Name: r.Name, Hostname: r.Hostname, Url: "https://" + r.Hostname,
		Service: r.ServiceName, Port: int(r.ServicePort), State: oapi.WorkspaceRouteState(r.State),
		Message: r.Message, CreatedAt: r.CreatedAt,
	}
}

func toRouteChange(c workspace.RouteChange) oapi.WorkspaceRouteChange {
	return oapi.WorkspaceRouteChange{Route: toRoute(c.Route), Operation: toOperation(c.Operation)}
}
