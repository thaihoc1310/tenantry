package org

import (
	"context"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/reservation"
)

// What an organization was granted and what it uses (PRODUCT-PLAN §5.6).
// Only its OrgOwner and OrgAdmins may read them (§9.2).

var ErrNotActive = apperr.New(apperr.OrgNotActive, "the organization is not approved yet")

// Quota is an organization's entitlement and its cluster grants.
type Quota struct {
	Org         Org
	Entitlement Entitlement
	Grants      []reservation.Grant
}

// Usage is what an organization uses, next to its limits.
type Usage struct {
	Quota
	Members     int32
	Groups      int32
	Workspaces  int32 // ponytail: 0 until the workspaces table exists (M6)
	Allocations []reservation.Allocation
}

// manage resolves idOrSlug for an OrgOwner or OrgAdmin of an Active
// organization, checking in this order: membership (404), role (403),
// Active (409 ORG_NOT_ACTIVE) (PRODUCT-PLAN §9.2).
func (s *Service) manage(ctx context.Context, userID, idOrSlug string) (Org, error) {
	o, err := s.Get(ctx, userID, idOrSlug)
	if err != nil {
		return Org{}, err
	}
	if o.Role != RoleOwner && o.Role != RoleAdmin {
		return Org{}, ErrForbidden
	}
	if o.Status != StatusActive {
		return Org{}, ErrNotActive
	}
	return o, nil
}

// Quota is read by the OrgOwner and OrgAdmins of an Active organization.
func (s *Service) Quota(ctx context.Context, userID, idOrSlug string) (Quota, error) {
	o, err := s.manage(ctx, userID, idOrSlug)
	if err != nil {
		return Quota{}, err
	}
	e, err := s.q.GetEntitlement(ctx, o.ID)
	if err != nil {
		return Quota{}, err // an Active organization always has one (Approve)
	}
	grants, err := reservation.Grants(ctx, s.q, o.ID)
	if err != nil {
		return Quota{}, err
	}
	return Quota{
		Org:         o,
		Entitlement: Entitlement{MaxMembers: e.MaxMembers, MaxGroups: e.MaxGroups, MaxWorkspacesTotal: e.MaxWorkspacesTotal},
		Grants:      grants,
	}, nil
}

func (s *Service) Usage(ctx context.Context, userID, idOrSlug string) (Usage, error) {
	q, err := s.Quota(ctx, userID, idOrSlug)
	if err != nil {
		return Usage{}, err
	}
	c, err := s.q.CountOrganizationUsage(ctx, q.Org.ID)
	if err != nil {
		return Usage{}, err
	}
	return Usage{Quota: q, Members: c.Members, Groups: c.Groups, Allocations: reservation.Allocations(q.Grants)}, nil
}
