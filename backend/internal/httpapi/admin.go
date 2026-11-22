package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
	"github.com/thaihoc1310/tenantry/backend/internal/org"
)

var errNotAdmin = apperr.New(apperr.Forbidden, "requires the platform-admin role")

// requirePlatformAdmin guards every operation under /api/v1/admin/ by path,
// so an admin endpoint added later cannot forget the check
// (IMPLEMENTATION-PLAN §3.5). It runs before validation: someone without
// the role learns nothing about what the endpoints accept. A path that is
// no operation never gets here; its 404 is the same for everyone.
func requirePlatformAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/admin/") && !caller(r.Context()).platformAdmin {
			writeError(w, r, errNotAdmin)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s server) AdminListOrganizations(ctx context.Context, req oapi.AdminListOrganizationsRequestObject) (oapi.AdminListOrganizationsResponseObject, error) {
	var status, cursor string
	var limit int
	if req.Params.Status != nil {
		status = string(*req.Params.Status)
	}
	if req.Params.Cursor != nil {
		cursor = *req.Params.Cursor
	}
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	page, err := s.orgs.AdminList(ctx, status, cursor, limit)
	if err != nil {
		return nil, err
	}
	// One proposal serves every Pending request: it comes from the policy alone.
	var proposal *oapi.Approval
	for _, o := range page.Items {
		if o.Status == org.StatusPending {
			if proposal, err = s.proposal(ctx); err != nil {
				return nil, err
			}
			break
		}
	}
	resp := oapi.AdminOrganizationList{Items: make([]oapi.AdminOrganization, 0, len(page.Items))}
	for _, o := range page.Items {
		item := toAdminOrganization(o)
		if o.Status == org.StatusPending {
			item.Proposal = proposal
		}
		resp.Items = append(resp.Items, item)
	}
	if page.NextCursor != "" {
		resp.NextCursor = &page.NextCursor
	}
	return oapi.AdminListOrganizations200JSONResponse(resp), nil
}

// proposal is the active onboarding policy as an approval form; nil when
// no policy is active. The templates are stored in this shape.
func (s server) proposal(ctx context.Context) (*oapi.Approval, error) {
	p, ok, err := s.orgs.OnboardingPolicy(ctx)
	if err != nil || !ok {
		return nil, err
	}
	var a oapi.Approval
	if err := json.Unmarshal(p.EntitlementTemplate, &a.Entitlement); err != nil {
		return nil, fmt.Errorf("onboarding policy entitlement template: %w", err)
	}
	if err := json.Unmarshal(p.GrantTemplate, &a.ClusterGrant); err != nil {
		return nil, fmt.Errorf("onboarding policy grant template: %w", err)
	}
	a.ClusterGrant.ClusterId = p.DefaultClusterID
	return &a, nil
}

func (s server) ApproveOrganization(ctx context.Context, req oapi.ApproveOrganizationRequestObject) (oapi.ApproveOrganizationResponseObject, error) {
	grant, err := toGrantSpec(req.Body.ClusterGrant)
	if err != nil {
		return nil, err
	}
	a, err := s.orgs.Approve(ctx, userID(ctx), req.OrgId, org.Approval{Entitlement: toEntitlement(req.Body.Entitlement), Grant: grant})
	if err != nil {
		return nil, err
	}
	return oapi.ApproveOrganization200JSONResponse{
		Organization: toAdminOrganization(a.Reviewed),
		Entitlement:  fromEntitlement(a.Entitlement),
		ClusterGrant: toClusterGrant(a.Grant),
	}, nil
}

func (s server) RejectOrganization(ctx context.Context, req oapi.RejectOrganizationRequestObject) (oapi.RejectOrganizationResponseObject, error) {
	o, err := s.orgs.Reject(ctx, userID(ctx), req.OrgId, req.Body.Reason)
	if err != nil {
		return nil, err
	}
	return oapi.RejectOrganization200JSONResponse(toAdminOrganization(o)), nil
}

func toAdminOrganization(o org.Reviewed) oapi.AdminOrganization {
	return oapi.AdminOrganization{
		Id: o.ID, Slug: o.Slug, DisplayName: o.DisplayName, Description: o.Description,
		Status: oapi.OrgStatus(o.Status), ReviewNote: o.ReviewNote, CreatedAt: o.CreatedAt,
		Owner: oapi.UserSummary{Id: o.OwnerUserID, Email: o.OwnerEmail, DisplayName: o.OwnerDisplayName},
	}
}

func (s server) GetPlatformHealth(ctx context.Context, _ oapi.GetPlatformHealthRequestObject) (oapi.GetPlatformHealthResponseObject, error) {
	r, err := s.health.Check(ctx, timeNow())
	if err != nil {
		return nil, err
	}
	out := oapi.PlatformHealth{
		Components: make([]oapi.HealthComponent, len(r.Components)),
		Clusters:   make([]oapi.HealthCluster, len(r.Clusters)),
	}
	for i, c := range r.Components {
		out.Components[i] = oapi.HealthComponent{Name: c.Name, State: oapi.HealthComponentState(c.State), Detail: c.Detail}
	}
	out.Work.OperationsRunning, out.Work.OperationsNeedingAttention = int(r.Work.OperationsRunning), int(r.Work.OperationsAttention)
	out.Work.SyncWaiting, out.Work.RequestsWaiting = int(r.Work.SyncWaiting), int(r.Work.RequestsWaiting)
	for i, c := range r.Clusters {
		hc := oapi.HealthCluster{Id: c.ID, DisplayName: c.DisplayName, Status: oapi.HealthClusterStatus(c.Status),
			LastHealthCheck: c.LastHealthCheck, Pools: make([]oapi.HealthPool, len(c.Pools))}
		for j, p := range c.Pools {
			hc.Pools[j] = oapi.HealthPool{Id: p.ID, Name: p.Name, Allocated: fromCapacity(p.Allocated), Ceiling: fromCapacity(p.Ceiling)}
		}
		out.Clusters[i] = hc
	}
	return oapi.GetPlatformHealth200JSONResponse(out), nil
}
