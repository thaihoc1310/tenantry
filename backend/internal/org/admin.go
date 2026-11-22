package org

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/audit"
	"github.com/thaihoc1310/tenantry/backend/internal/id"
	"github.com/thaihoc1310/tenantry/backend/internal/reservation"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// What a Platform Admin does to organizations besides reviewing requests
// (PRODUCT-PLAN §16.5). The caller has checked the platform-admin role.

var ErrOwnerNotFound = apperr.New(apperr.OwnerUserNotFound,
	"nobody with this email has logged in to the portal: ask them to log in once, then try again")

// NewOrganization is an organization an Admin creates for a customer.
type NewOrganization struct {
	Request
	OwnerEmail string
	Approval
}

// CreateActive creates an Active organization with everything an approval
// gives it, in one transaction (architecture/03-flows §2a). Its owner must
// have logged in: an organization always has its one OrgOwner, so there is
// no invitation to become owner.
func (s *Service) CreateActive(ctx context.Context, adminID string, n NewOrganization) (Approved, error) {
	if err := checkSlug(n.Slug); err != nil {
		return Approved{}, err
	}
	name, err := clean("displayName", n.DisplayName, maxDisplayName, true)
	if err != nil {
		return Approved{}, err
	}
	desc, err := clean("description", n.Description, maxDescription, false)
	if err != nil {
		return Approved{}, err
	}
	email, err := checkEmailField("ownerEmail", n.OwnerEmail)
	if err != nil {
		return Approved{}, err
	}
	if err := n.Entitlement.check(); err != nil {
		return Approved{}, err
	}
	var out Approved
	err = store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		owner, err := q.UserByEmail(ctx, email)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrOwnerNotFound
		}
		if err != nil {
			return err
		}
		if owner.Status != "Active" {
			return apperr.Invalid("ownerEmail", "this user is disabled")
		}
		o, err := q.CreateActiveOrganization(ctx, store.CreateActiveOrganizationParams{
			ID: id.New(id.Organization), Slug: n.Slug, DisplayName: name, Description: desc, OwnerUserID: owner.ID,
		})
		if err != nil {
			return err
		}
		if err := q.AddOrganizationMember(ctx, store.AddOrganizationMemberParams{
			OrgID: o.ID, UserID: owner.ID, Role: string(RoleOwner),
		}); err != nil {
			return err
		}
		if out, err = activate(ctx, q, o, n.Approval); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Event{
			Actor: adminID, OrgID: o.ID, Action: audit.OrganizationCreate, TargetType: "Organization", TargetID: o.ID,
			Detail: map[string]any{
				"slug": o.Slug, "displayName": o.DisplayName, "description": o.Description, "status": o.Status,
				"owner": owner.ID, "entitlement": out.Entitlement, "clusterGrant": out.Grant, "byPlatformAdmin": true,
			},
		})
	})
	if c, ok := store.UniqueViolation(err); ok && c == slugIndex {
		return Approved{}, ErrSlugTaken
	}
	return out, err
}

// checkEmailField normalizes one plain email address of a request field.
func checkEmailField(field, v string) (string, error) {
	a, err := mail.ParseAddress(strings.TrimSpace(v))
	if err != nil || a.Name != "" || a.Address != strings.TrimSpace(v) {
		return "", apperr.Invalid(field, "must be one email address, like joe@example.com")
	}
	return strings.ToLower(a.Address), nil
}

// AdminDetail is one organization as a Platform Admin sees it. A request
// under review or rejected has no entitlement, grants or usage yet.
type AdminDetail struct {
	Reviewed
	Usage *Usage
}

func (s *Service) AdminDetail(ctx context.Context, orgID string) (AdminDetail, error) {
	r, err := s.q.GetOrganizationForAdmin(ctx, orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminDetail{}, ErrNoSuchOrg
	}
	if err != nil {
		return AdminDetail{}, err
	}
	d := AdminDetail{Reviewed: Reviewed{Organization: r.Organization, OwnerEmail: r.OwnerEmail, OwnerDisplayName: r.OwnerDisplayName}}
	if r.Organization.Status != StatusActive {
		return d, nil
	}
	u, err := s.usageOf(ctx, s.q, Org{Organization: r.Organization})
	if err != nil {
		return AdminDetail{}, err
	}
	d.Usage = &u
	return d, nil
}

// usageOf reads an Active organization's entitlement, grants and what it uses.
func (s *Service) usageOf(ctx context.Context, q *store.Queries, o Org) (Usage, error) {
	e, err := q.GetEntitlement(ctx, o.ID)
	if err != nil {
		return Usage{}, err // an Active organization always has one
	}
	grants, err := reservation.Grants(ctx, q, o.ID)
	if err != nil {
		return Usage{}, err
	}
	c, err := q.CountOrganizationUsage(ctx, o.ID)
	if err != nil {
		return Usage{}, err
	}
	allocs, err := reservation.Allocations(ctx, q, grants)
	if err != nil {
		return Usage{}, err
	}
	return Usage{
		Quota: Quota{
			Org:         o,
			Entitlement: Entitlement{MaxMembers: e.MaxMembers, MaxGroups: e.MaxGroups, MaxWorkspacesTotal: e.MaxWorkspacesTotal},
			Grants:      grants,
		},
		Members: c.Members, Groups: c.Groups, Workspaces: c.Workspaces, Allocations: allocs,
	}, nil
}

// SetEntitlement changes an Active organization's entitlement. Under the
// entitlement's lock, which every use of a limit takes too, no limit may go
// below what is used and maxGroups stays at 3 or more (02-data-model §3).
func (s *Service) SetEntitlement(ctx context.Context, adminID, orgID string, e Entitlement) (Entitlement, error) {
	err := store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		o, err := q.LockOrganization(ctx, orgID)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && o.Status == "Deleted") {
			return ErrNoSuchOrg
		}
		if err != nil {
			return err
		}
		if o.Status != StatusActive {
			return ErrNotActive
		}
		before, err := q.LockEntitlement(ctx, o.ID)
		if err != nil {
			return err
		}
		members, err := q.CountMembers(ctx, o.ID)
		if err != nil {
			return err
		}
		groups, err := q.CountLiveGroupsOf(ctx, o.ID)
		if err != nil {
			return err
		}
		workspaces, err := q.CountWorkspaces(ctx, o.ID)
		if err != nil {
			return err
		}
		for _, c := range []struct {
			limit           string
			used, requested int32
		}{
			{"maxMembers", members, e.MaxMembers},
			{"maxGroups", max(groups, int32(len(DefaultGroups))), e.MaxGroups},
			{"maxWorkspacesTotal", workspaces, e.MaxWorkspacesTotal},
		} {
			if c.requested < c.used {
				return &apperr.Error{
					Code:    apperr.EntitlementBelowUsage,
					Message: fmt.Sprintf("%s cannot go below %d, what the organization uses (at least %d for groups: the default ones)", c.limit, c.used, len(DefaultGroups)),
					Details: map[string]any{"limit": c.limit, "used": c.used, "requested": c.requested},
				}
			}
		}
		if err := q.UpdateEntitlement(ctx, store.UpdateEntitlementParams{
			OrgID: o.ID, MaxMembers: e.MaxMembers, MaxGroups: e.MaxGroups, MaxWorkspacesTotal: e.MaxWorkspacesTotal,
		}); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Event{
			Actor: adminID, OrgID: o.ID, Action: audit.EntitlementUpdate, TargetType: "Organization", TargetID: o.ID,
			Detail: map[string]any{
				"before": Entitlement{MaxMembers: before.MaxMembers, MaxGroups: before.MaxGroups, MaxWorkspacesTotal: before.MaxWorkspacesTotal},
				"after":  e,
			},
		})
	})
	return e, err
}
