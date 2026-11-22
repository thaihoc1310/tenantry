package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"strconv"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
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
		w, err := s.toWorkspace(ctx, ws)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, w)
	}
	return oapi.ListWorkspaces200JSONResponse(out), nil
}

// toNew reads a create body: quantities parsed, the requested block too.
func toNew(ctx context.Context, b *oapi.WorkspaceCreate) (workspace.New, error) {
	n := workspace.New{
		Slug: b.Slug, DisplayName: b.DisplayName, ClusterID: b.ClusterId,
		ComputeMode: string(b.Compute.Mode), CorrelationID: requestID(ctx),
	}
	if b.Compute.SharedPoolId != nil {
		n.SharedPoolID = *b.Compute.SharedPoolId
	}
	if b.Compute.DedicatedNodeGrantId != nil {
		n.NodeGrantID = *b.Compute.DedicatedNodeGrantId
	}
	if net := b.Network; net != nil {
		if net.NetworkPoolId != nil {
			n.Network.PoolID = *net.NetworkPoolId
		}
		if net.PrefixLength != nil {
			n.Network.PrefixLength = int32(*net.PrefixLength)
		}
		if net.RequestedCidr != nil {
			p, err := netip.ParsePrefix(*net.RequestedCidr)
			if err != nil || !p.Addr().Is4() {
				return n, apperr.Invalid("network.requestedCidr", "is not an IPv4 CIDR such as 10.240.12.0/24")
			}
			n.Network.RequestedCIDR = &p
		}
	}
	var err error
	if n.Resources, err = toWorkspaceResources(b.Resources); err != nil {
		return n, err
	}
	if b.Namespaces != nil {
		n.Namespaces = *b.Namespaces
	}
	for _, bi := range b.Bindings {
		n.Bindings = append(n.Bindings, workspace.Binding{GroupID: bi.GroupId, Role: workspace.Role(bi.Role)})
	}
	return n, nil
}

func (s server) CreateWorkspace(ctx context.Context, req oapi.CreateWorkspaceRequestObject) (oapi.CreateWorkspaceResponseObject, error) {
	n, err := toNew(ctx, req.Body)
	if err != nil {
		return nil, err
	}
	c, err := s.workspaces.Create(ctx, userID(ctx), req.OrgId, n)
	if err != nil {
		return nil, err
	}
	w, err := s.toWorkspace(ctx, c.Workspace)
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
	w, err := s.toWorkspace(ctx, ws)
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

// toWorkspace is a Workspace as the API shows it, with its node grant when Dedicated.
func (s server) toWorkspace(ctx context.Context, ws store.Workspace) (oapi.Workspace, error) {
	w, err := toWorkspaceRow(ws)
	if err != nil {
		return w, err
	}
	g, err := s.workspaces.NodeGrantOf(ctx, ws)
	if err != nil {
		return w, err
	}
	if g != "" {
		w.Compute.DedicatedNodeGrantId = &g
	}
	a, err := s.workspaces.AllocationOf(ctx, ws)
	if a != nil {
		w.Network = &oapi.WorkspaceNetwork{NetworkPoolId: a.PoolID, Cidr: a.Cidr.String(), State: oapi.WorkspaceNetworkState(a.State)}
	}
	return w, err
}

func toWorkspaceRow(ws store.Workspace) (oapi.Workspace, error) {
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

func (s server) ListWorkspaceNamespaces(ctx context.Context, req oapi.ListWorkspaceNamespacesRequestObject) (oapi.ListWorkspaceNamespacesResponseObject, error) {
	rows, err := s.workspaces.Namespaces(ctx, userID(ctx), req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	out := oapi.WorkspaceNamespaceList{Items: make([]oapi.WorkspaceNamespace, 0, len(rows))}
	for _, n := range rows {
		v, err := toNamespace(n)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, v)
	}
	return oapi.ListWorkspaceNamespaces200JSONResponse(out), nil
}

func (s server) CreateWorkspaceNamespace(ctx context.Context, req oapi.CreateWorkspaceNamespaceRequestObject) (oapi.CreateWorkspaceNamespaceResponseObject, error) {
	c, err := s.workspaces.CreateNamespace(ctx, userID(ctx), req.WorkspaceId, req.Body.Name, requestID(ctx))
	if err != nil {
		return nil, err
	}
	out, err := toNamespaceChange(c)
	return oapi.CreateWorkspaceNamespace202JSONResponse(out), err
}

func (s server) DeleteWorkspaceNamespace(ctx context.Context, req oapi.DeleteWorkspaceNamespaceRequestObject) (oapi.DeleteWorkspaceNamespaceResponseObject, error) {
	c, err := s.workspaces.DeleteNamespace(ctx, userID(ctx), req.WorkspaceId, req.Name, requestID(ctx))
	if err != nil {
		return nil, err
	}
	out, err := toNamespaceChange(c)
	return oapi.DeleteWorkspaceNamespace202JSONResponse(out), err
}

func toNamespace(n store.WorkspaceNamespace) (oapi.WorkspaceNamespace, error) {
	v := oapi.WorkspaceNamespace{
		Name: n.Name, Source: oapi.WorkspaceNamespaceSource(n.Source), State: oapi.WorkspaceNamespaceState(n.State),
		Usage: map[string]string{}, CreatedAt: n.CreatedAt,
	}
	if err := json.Unmarshal(n.UsageSnapshot, &v.Usage); err != nil {
		return v, fmt.Errorf("namespace %s usage: %w", n.Name, err)
	}
	return v, nil
}

func toNamespaceChange(c workspace.NamespaceChange) (oapi.WorkspaceNamespaceChange, error) {
	n, err := toNamespace(c.Namespace)
	return oapi.WorkspaceNamespaceChange{Namespace: n, Operation: toOperation(c.Operation)}, err
}

func (s server) PatchWorkspace(ctx context.Context, req oapi.PatchWorkspaceRequestObject) (oapi.PatchWorkspaceResponseObject, error) {
	p := workspace.Patch{DisplayName: req.Body.DisplayName, CorrelationID: requestID(ctx)}
	if req.Body.Resources != nil {
		r, err := toWorkspaceResources(*req.Body.Resources)
		if err != nil {
			return nil, err
		}
		p.Resources = &r
	}
	c, err := s.workspaces.Update(ctx, userID(ctx), req.WorkspaceId, p)
	if err != nil {
		return nil, err
	}
	out, err := s.toChange(ctx, c)
	if err != nil || c.Operation == nil {
		return oapi.PatchWorkspace200JSONResponse(out), err
	}
	return oapi.PatchWorkspace202JSONResponse(out), nil
}

func (s server) CordonWorkspace(ctx context.Context, req oapi.CordonWorkspaceRequestObject) (oapi.CordonWorkspaceResponseObject, error) {
	c, err := s.workspaces.Cordon(ctx, userID(ctx), req.WorkspaceId, requestID(ctx))
	if err != nil {
		return nil, err
	}
	out, err := s.toChange(ctx, c)
	if err != nil || c.Operation == nil {
		return oapi.CordonWorkspace200JSONResponse(out), err
	}
	return oapi.CordonWorkspace202JSONResponse(out), nil
}

func (s server) UncordonWorkspace(ctx context.Context, req oapi.UncordonWorkspaceRequestObject) (oapi.UncordonWorkspaceResponseObject, error) {
	c, err := s.workspaces.Uncordon(ctx, userID(ctx), req.WorkspaceId, requestID(ctx))
	if err != nil {
		return nil, err
	}
	out, err := s.toChange(ctx, c)
	if err != nil || c.Operation == nil {
		return oapi.UncordonWorkspace200JSONResponse(out), err
	}
	return oapi.UncordonWorkspace202JSONResponse(out), nil
}

func (s server) toChange(ctx context.Context, c workspace.Changed) (oapi.WorkspaceChange, error) {
	w, err := s.toWorkspace(ctx, c.Workspace)
	out := oapi.WorkspaceChange{Workspace: w}
	if c.Operation != nil {
		op := toOperation(*c.Operation)
		out.Operation = &op
	}
	return out, err
}

func (s server) DeleteWorkspace(ctx context.Context, req oapi.DeleteWorkspaceRequestObject) (oapi.DeleteWorkspaceResponseObject, error) {
	c, err := s.workspaces.Delete(ctx, userID(ctx), req.WorkspaceId, req.Params.ConfirmName, requestID(ctx))
	if err != nil {
		return nil, err
	}
	out, err := s.toChange(ctx, c)
	return oapi.DeleteWorkspace202JSONResponse(out), err
}

func (s server) ForceOperation(ctx context.Context, req oapi.ForceOperationRequestObject) (oapi.ForceOperationResponseObject, error) {
	c := caller(ctx)
	op, err := s.workspaces.Force(ctx, c.user.ID, c.platformAdmin, req.OperationId, requestID(ctx))
	if err != nil {
		return nil, err
	}
	return oapi.ForceOperation200JSONResponse(toOperation(op)), nil
}

func (s server) GetWorkspaceUsage(ctx context.Context, req oapi.GetWorkspaceUsageRequestObject) (oapi.GetWorkspaceUsageResponseObject, error) {
	u, err := s.workspaces.Usage(ctx, userID(ctx), req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	ws := u.Workspace
	used := map[string]string{}
	if err := json.Unmarshal(ws.UsageSnapshot, &used); err != nil {
		return nil, fmt.Errorf("workspace %s usage: %w", ws.ID, err)
	}
	out := oapi.WorkspaceUsage{Namespaces: make([]oapi.WorkspaceNamespace, 0, len(u.Namespaces))}
	for _, q := range []struct {
		resource oapi.QuotaUseResource
		key      string
		limit    oapi.Quantity
	}{
		{oapi.RequestsCpu, "requests.cpu", formatCPU(ws.RequestsCpuMilli)},
		{oapi.RequestsMemory, "requests.memory", formatBytes(ws.RequestsMemoryBytes)},
		{oapi.LimitsCpu, "limits.cpu", formatCPU(ws.LimitsCpuMilli)},
		{oapi.LimitsMemory, "limits.memory", formatBytes(ws.LimitsMemoryBytes)},
		{oapi.Pods, "pods", oapi.Quantity(fmt.Sprint(ws.Pods))},
		{oapi.RequestsStorage, "requests.storage", formatBytes(ws.RequestsStorageBytes)},
	} {
		v := used[q.key]
		if v == "" {
			v = "0"
		}
		out.Items = append(out.Items, oapi.QuotaUse{Resource: q.resource, Used: v, Limit: string(q.limit)})
	}
	for _, n := range u.Namespaces {
		v, err := toNamespace(n)
		if err != nil {
			return nil, err
		}
		out.Namespaces = append(out.Namespaces, v)
	}
	return oapi.GetWorkspaceUsage200JSONResponse(out), nil
}

func (s server) ListWorkspaceOperations(ctx context.Context, req oapi.ListWorkspaceOperationsRequestObject) (oapi.ListWorkspaceOperationsResponseObject, error) {
	var cursor string
	var limit int
	if req.Params.Cursor != nil {
		cursor = *req.Params.Cursor
	}
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	p, err := s.workspaces.Operations(ctx, userID(ctx), req.WorkspaceId, cursor, limit)
	if err != nil {
		return nil, err
	}
	out := oapi.OperationList{Items: make([]oapi.Operation, 0, len(p.Items))}
	for _, op := range p.Items {
		out.Items = append(out.Items, toOperation(op))
	}
	if p.NextCursor != "" {
		out.NextCursor = &p.NextCursor
	}
	return oapi.ListWorkspaceOperations200JSONResponse(out), nil
}

func (s server) GetNetworkPoolAvailability(ctx context.Context, req oapi.GetNetworkPoolAvailabilityRequestObject) (oapi.GetNetworkPoolAvailabilityResponseObject, error) {
	a, err := s.orgs.NetworkPoolAvailability(ctx, userID(ctx), req.OrgId, string(req.PoolId))
	if err != nil {
		return nil, err
	}
	out := oapi.NetworkPoolAvailability{Id: a.Pool.ID, DefaultPrefixLength: int(a.Pool.DefaultPrefixLength),
		Cidrs: make([]string, len(a.Pool.Cidrs)), Blocks: make([]oapi.NetworkBlock, len(a.Blocks))}
	for i, c := range a.Pool.Cidrs {
		out.Cidrs[i] = c.String()
	}
	for _, l := range a.Pool.AllowedPrefixLengths {
		out.Free = append(out.Free, oapi.FreeBlocks{PrefixLength: int(l), Blocks: a.Free[l]})
	}
	for i, b := range a.Blocks {
		out.Blocks[i] = oapi.NetworkBlock{Workspace: b.K8sName, Cidr: b.Cidr.String(), State: oapi.NetworkBlockState(b.State)}
	}
	return oapi.GetNetworkPoolAvailability200JSONResponse(out), nil
}

func (s server) ValidateWorkspace(ctx context.Context, req oapi.ValidateWorkspaceRequestObject) (oapi.ValidateWorkspaceResponseObject, error) {
	n, err := toNew(ctx, req.Body)
	if err != nil {
		return nil, err
	}
	c, err := s.workspaces.Validate(ctx, userID(ctx), req.OrgId, n)
	if err != nil {
		return nil, err
	}
	out := oapi.WorkspaceValidation{Valid: true}
	out.Network.NetworkPoolId, out.Network.Cidr = c.NetworkPoolID, c.CIDR.String()
	return oapi.ValidateWorkspace200JSONResponse(out), nil
}

func (s server) ListAuditEvents(ctx context.Context, req oapi.ListAuditEventsRequestObject) (oapi.ListAuditEventsResponseObject, error) {
	var cursor int64
	var limit int
	if req.Params.Cursor != nil {
		c, err := strconv.ParseInt(*req.Params.Cursor, 10, 64)
		if err != nil || c < 1 {
			return nil, apperr.Invalid("cursor", "is not a cursor this API gave")
		}
		cursor = c
	}
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	p, err := s.orgs.Audit(ctx, userID(ctx), req.OrgId, cursor, limit)
	if err != nil {
		return nil, err
	}
	out := oapi.AuditEventList{Items: make([]oapi.AuditEvent, 0, len(p.Items))}
	for _, e := range p.Items {
		ev := oapi.AuditEvent{Id: strconv.FormatInt(e.ID, 10), At: e.At, Action: e.Action, TargetType: e.TargetType,
			TargetId: e.TargetID, Result: e.Result, Detail: map[string]any{}}
		if err := json.Unmarshal(e.Detail, &ev.Detail); err != nil {
			return nil, fmt.Errorf("audit event %d detail: %w", e.ID, err)
		}
		if e.ActorUserID != nil {
			ev.Actor = &oapi.UserSummary{Id: *e.ActorUserID, Email: e.ActorEmail}
			if e.ActorName != nil {
				ev.Actor.DisplayName = *e.ActorName
			}
		}
		out.Items = append(out.Items, ev)
	}
	if p.NextCursor > 0 {
		c := strconv.FormatInt(p.NextCursor, 10)
		out.NextCursor = &c
	}
	return oapi.ListAuditEvents200JSONResponse(out), nil
}
