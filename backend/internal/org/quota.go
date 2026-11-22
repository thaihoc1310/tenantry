package org

import (
	"context"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/reservation"
)

// What an organization was granted and what it uses (PRODUCT-PLAN §5.6).
// Only its OrgOwner and OrgAdmins may read them (§9.2).

var (
	ErrNotQuotaReader = apperr.New(apperr.Forbidden, "requires OrgOwner or OrgAdmin")
	ErrNotActive      = apperr.New(apperr.OrgNotActive, "the organization is not approved yet, so it has no entitlement")
)

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
	Workspaces  int32 // ponytail: 0 until Workspaces exist (M7)
	Allocations []reservation.Allocation
}

// Quota checks, in this order: membership (404), role (403), Active (409).
func (s *Service) Quota(ctx context.Context, userID, idOrSlug string) (Quota, error) {
	o, err := s.Get(ctx, userID, idOrSlug)
	if err != nil {
		return Quota{}, err
	}
	if o.Role != RoleOwner && o.Role != RoleAdmin {
		return Quota{}, ErrNotQuotaReader
	}
	if o.Status != StatusActive {
		return Quota{}, ErrNotActive
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
