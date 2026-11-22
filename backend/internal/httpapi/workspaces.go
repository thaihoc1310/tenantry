package httpapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
	"github.com/thaihoc1310/tenantry/backend/internal/workspace"
)

func (s server) ListWorkspaces(ctx context.Context, req oapi.ListWorkspacesRequestObject) (oapi.ListWorkspacesResponseObject, error) {
	rows, err := s.workspaces.List(ctx, userID(ctx), req.OrgId)
	if err != nil {
		return nil, err
	}
	out := oapi.WorkspaceList{Items: make([]oapi.Workspace, 0, len(rows))}
	for _, ws := range rows {
		w, err := toWorkspace(ws)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, w)
	}
	return oapi.ListWorkspaces200JSONResponse(out), nil
}

func (s server) CreateWorkspace(ctx context.Context, req oapi.CreateWorkspaceRequestObject) (oapi.CreateWorkspaceResponseObject, error) {
	b := req.Body
	n := workspace.New{
		Slug: b.Slug, DisplayName: b.DisplayName, ClusterID: b.ClusterId,
		ComputeMode: string(b.Compute.Mode), CorrelationID: requestID(ctx),
	}
	if b.Compute.SharedPoolId != nil {
		n.SharedPoolID = *b.Compute.SharedPoolId
	}
	var err error
	if n.Resources, err = toWorkspaceResources(b.Resources); err != nil {
		return nil, err
	}
	if b.Namespaces != nil {
		n.Namespaces = *b.Namespaces
	}
	for _, bi := range b.Bindings {
		n.Bindings = append(n.Bindings, workspace.Binding{GroupID: bi.GroupId, Role: workspace.Role(bi.Role)})
	}
	c, err := s.workspaces.Create(ctx, userID(ctx), req.OrgId, n)
	if err != nil {
		return nil, err
	}
	w, err := toWorkspace(c.Workspace)
	if err != nil {
		return nil, err
	}
	return oapi.CreateWorkspace202JSONResponse{
		Body:    oapi.WorkspaceAccepted{Workspace: w, Operation: toOperation(c.Operation)},
		Headers: oapi.CreateWorkspace202ResponseHeaders{Location: "/api/v1/workspaces/" + c.Workspace.ID},
	}, nil
}

func (s server) GetWorkspace(ctx context.Context, req oapi.GetWorkspaceRequestObject) (oapi.GetWorkspaceResponseObject, error) {
	ws, err := s.workspaces.Get(ctx, userID(ctx), req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	w, err := toWorkspace(ws)
	if err != nil {
		return nil, err
	}
	return oapi.GetWorkspace200JSONResponse(w), nil
}

// toWorkspaceResources parses the quantities; limits left out stay 0, which
// the service reads as "not given".
func toWorkspaceResources(in oapi.WorkspaceResources) (workspace.Resources, error) {
	r := workspace.Resources{Pods: int32(in.Pods), NamespaceQuota: int32(in.NamespaceQuota)}
	var err error
	if r.RequestsCPUMilli, err = parseCPU("resources.requestsCpu", in.RequestsCpu); err != nil {
		return r, err
	}
	if r.RequestsMemoryBytes, err = parseBytes("resources.requestsMemory", in.RequestsMemory); err != nil {
		return r, err
	}
	if r.RequestsStorageBytes, err = parseBytes("resources.requestsStorage", in.RequestsStorage); err != nil {
		return r, err
	}
	if in.LimitsCpu != nil {
		if r.LimitsCPUMilli, err = parseCPU("resources.limitsCpu", *in.LimitsCpu); err != nil {
			return r, err
		}
	}
	if in.LimitsMemory != nil {
		if r.LimitsMemoryBytes, err = parseBytes("resources.limitsMemory", *in.LimitsMemory); err != nil {
			return r, err
		}
	}
	return r, nil
}

func toWorkspace(ws store.Workspace) (oapi.Workspace, error) {
	limitsCPU, limitsMem := formatCPU(ws.LimitsCpuMilli), formatBytes(ws.LimitsMemoryBytes)
	w := oapi.Workspace{
		Id: ws.ID, OrgId: ws.OrgID, ClusterId: ws.ClusterID, Slug: ws.Slug, K8sName: ws.K8sName,
		DisplayName: ws.DisplayName, State: oapi.WorkspaceState(ws.State),
		Resources: oapi.WorkspaceResources{
			RequestsCpu: formatCPU(ws.RequestsCpuMilli), RequestsMemory: formatBytes(ws.RequestsMemoryBytes),
			LimitsCpu: &limitsCPU, LimitsMemory: &limitsMem, Pods: int(ws.Pods),
			RequestsStorage: formatBytes(ws.RequestsStorageBytes), NamespaceQuota: int(ws.NamespaceQuota),
		},
		Compute:   oapi.WorkspaceCompute{Mode: oapi.WorkspaceComputeMode(ws.ComputeMode), SharedPoolId: ws.SharedPoolID},
		CreatedAt: ws.CreatedAt, UpdatedAt: ws.UpdatedAt,
	}
	// The controller's metav1.Conditions, as the worker copied them.
	if err := json.Unmarshal(ws.Conditions, &w.Conditions); err != nil {
		return w, fmt.Errorf("workspace %s conditions: %w", ws.ID, err)
	}
	if w.Conditions == nil {
		w.Conditions = []oapi.WorkspaceCondition{}
	}
	return w, nil
}

func (s server) ListWorkspaceBindings(ctx context.Context, req oapi.ListWorkspaceBindingsRequestObject) (oapi.ListWorkspaceBindingsResponseObject, error) {
	bs, err := s.workspaces.Bindings(ctx, userID(ctx), req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	return oapi.ListWorkspaceBindings200JSONResponse{Items: toBindings(bs)}, nil
}

func (s server) PutWorkspaceBinding(ctx context.Context, req oapi.PutWorkspaceBindingRequestObject) (oapi.PutWorkspaceBindingResponseObject, error) {
	c, err := s.workspaces.PutBinding(ctx, userID(ctx), req.WorkspaceId, req.GroupId, workspace.Role(req.Body.Role), requestID(ctx))
	if err != nil {
		return nil, err
	}
	if c.Operation == nil {
		return oapi.PutWorkspaceBinding200JSONResponse(toBindingChange(c)), nil
	}
	return oapi.PutWorkspaceBinding202JSONResponse(toBindingChange(c)), nil
}

func (s server) DeleteWorkspaceBinding(ctx context.Context, req oapi.DeleteWorkspaceBindingRequestObject) (oapi.DeleteWorkspaceBindingResponseObject, error) {
	c, err := s.workspaces.DeleteBinding(ctx, userID(ctx), req.WorkspaceId, req.GroupId, requestID(ctx))
	if err != nil {
		return nil, err
	}
	if c.Operation == nil {
		return oapi.DeleteWorkspaceBinding204Response{}, nil
	}
	return oapi.DeleteWorkspaceBinding202JSONResponse(toBindingChange(c)), nil
}

func toBindings(bs []workspace.BindingView) []oapi.WorkspaceBinding {
	out := make([]oapi.WorkspaceBinding, len(bs))
	for i, b := range bs {
		out[i] = oapi.WorkspaceBinding{GroupId: b.GroupID, GroupName: b.GroupName, Role: oapi.WorkspaceRole(b.Role)}
	}
	return out
}

func toBindingChange(c workspace.BindingChange) oapi.WorkspaceBindingChange {
	out := oapi.WorkspaceBindingChange{Items: toBindings(c.Bindings)}
	if c.Operation != nil {
		op := toOperation(*c.Operation)
		out.Operation = &op
	}
	return out
}
