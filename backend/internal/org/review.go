package org

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/audit"
	"github.com/thaihoc1310/tenantry/backend/internal/id"
	"github.com/thaihoc1310/tenantry/backend/internal/reservation"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// A Platform Admin's review of organization requests (PRODUCT-PLAN §4.2,
// architecture/03-flows.md §1b). The caller has checked the platform-admin
// role; these functions only check the request.

const (
	StatusPending = "Pending"
	StatusActive  = "Active"
	maxReason     = 500
)

// DefaultGroups are created when an organization becomes Active; the owner
// joins the first (PRODUCT-PLAN §4.2).
var DefaultGroups = []string{"admins", "developers", "viewers"}

var (
	ErrNoSuchOrg  = apperr.New(apperr.NotFound, "organization not found")
	ErrNotPending = apperr.New(apperr.Conflict, "the organization request was already reviewed")
)

// Entitlement holds the limits that do not depend on a cluster (PRODUCT-PLAN §5.1).
type Entitlement struct {
	MaxMembers         int32 `json:"maxMembers"`
	MaxGroups          int32 `json:"maxGroups"` // the default groups count
	MaxWorkspacesTotal int32 `json:"maxWorkspacesTotal"`
}

func (e Entitlement) check() error {
	switch {
	case e.MaxMembers < 1:
		return apperr.Invalid("maxMembers", "must be at least 1")
	case int(e.MaxGroups) < len(DefaultGroups):
		return apperr.Invalid("maxGroups", fmt.Sprintf("must be at least %d, the default groups", len(DefaultGroups)))
	case e.MaxWorkspacesTotal < 0:
		return apperr.Invalid("maxWorkspacesTotal", "must not be negative")
	}
	return nil
}

// Reviewed is an organization as a Platform Admin sees it.
type Reviewed struct {
	store.Organization
	OwnerEmail       *string
	OwnerDisplayName string
}

// ReviewPage is one page of AdminList. NextCursor is empty on the last page.
type ReviewPage struct {
	Items      []Reviewed
	NextCursor string
}

// AdminList returns every organization, oldest first; status "" means any.
func (s *Service) AdminList(ctx context.Context, status, cursor string, pageSize int) (ReviewPage, error) {
	if pageSize == 0 {
		pageSize = DefaultPageSize
	}
	if pageSize < 1 || pageSize > MaxPageSize {
		return ReviewPage{}, apperr.Invalid("limit", fmt.Sprintf("must be between 1 and %d", MaxPageSize))
	}
	arg := store.ListOrganizationsForAdminParams{PageSize: int32(pageSize + 1)}
	if status != "" {
		arg.Status = &status
	}
	if cursor != "" {
		arg.Cursor = &cursor
	}
	rows, err := s.q.ListOrganizationsForAdmin(ctx, arg)
	if err != nil {
		return ReviewPage{}, err
	}
	p := ReviewPage{Items: make([]Reviewed, 0, len(rows))}
	for _, r := range rows {
		p.Items = append(p.Items, Reviewed{Organization: r.Organization, OwnerEmail: r.OwnerEmail, OwnerDisplayName: r.OwnerDisplayName})
	}
	if len(p.Items) > pageSize {
		p.Items = p.Items[:pageSize]
		p.NextCursor = p.Items[pageSize-1].ID
	}
	return p, nil
}

// OnboardingPolicy is what an approval proposes; ok is false without an active policy.
func (s *Service) OnboardingPolicy(ctx context.Context) (p reservation.Policy, ok bool, err error) {
	return reservation.ActivePolicy(ctx, s.q)
}

// Approval is what a Platform Admin grants with an approval: usually the
// onboarding policy's proposal, possibly edited.
type Approval struct {
	Entitlement Entitlement
	Grant       reservation.GrantSpec
}

type Approved struct {
	Reviewed
	Entitlement Entitlement
	Grant       reservation.Grant
}

// Approve makes a Pending organization Active, in one transaction with
// everything an Active organization has: its entitlement, its default
// cluster grant, the default groups with the owner in admins, and the audit
// event (03-flows §1b). The groups start Pending for the group sync worker.
func (s *Service) Approve(ctx context.Context, adminID, orgID string, a Approval) (Approved, error) {
	if err := a.Entitlement.check(); err != nil {
		return Approved{}, err
	}
	var out Approved
	err := store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		o, err := lockPending(ctx, q, orgID)
		if err != nil {
			return err
		}
		grant, err := reservation.CreateGrant(ctx, q, o.ID, true, a.Grant)
		if err != nil {
			return err
		}
		if o, err = q.ApproveOrganization(ctx, o.ID); err != nil {
			return err
		}
		e := a.Entitlement
		if err := q.CreateEntitlement(ctx, store.CreateEntitlementParams{
			OrgID: o.ID, MaxMembers: e.MaxMembers, MaxGroups: e.MaxGroups, MaxWorkspacesTotal: e.MaxWorkspacesTotal,
		}); err != nil {
			return err
		}
		groups := map[string]string{}
		for _, name := range DefaultGroups {
			gid := id.New(id.Group)
			groups[name] = gid
			if err := q.CreateGroup(ctx, store.CreateGroupParams{
				ID: gid, OrgID: o.ID, DisplayName: name, MachineName: MachineName(o.ID, gid),
			}); err != nil {
				return err
			}
		}
		if err := q.AddGroupMember(ctx, store.AddGroupMemberParams{GroupID: groups[DefaultGroups[0]], UserID: o.OwnerUserID}); err != nil {
			return err
		}
		owner, err := q.GetUser(ctx, o.OwnerUserID)
		if err != nil {
			return err
		}
		out = Approved{
			Reviewed:    Reviewed{Organization: o, OwnerEmail: owner.Email, OwnerDisplayName: owner.DisplayName},
			Entitlement: e, Grant: grant,
		}
		return audit.Record(ctx, q, audit.Event{
			Actor: adminID, OrgID: o.ID, Action: audit.OrganizationApprove, TargetType: "Organization", TargetID: o.ID,
			Detail: map[string]any{"entitlement": e, "clusterGrant": grant},
		})
	})
	return out, err
}

// Reject closes a Pending request with a reason the requester sees. Its
// slug is free again and the requester may file a new request.
func (s *Service) Reject(ctx context.Context, adminID, orgID, reason string) (Reviewed, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return Reviewed{}, apperr.Invalid("reason", "must not be blank: the requester is shown it")
	}
	if utf8.RuneCountInString(reason) > maxReason {
		return Reviewed{}, apperr.Invalid("reason", fmt.Sprintf("must be at most %d characters", maxReason))
	}
	var out Reviewed
	err := store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		o, err := lockPending(ctx, q, orgID)
		if err != nil {
			return err
		}
		if o, err = q.RejectOrganization(ctx, store.RejectOrganizationParams{ID: o.ID, ReviewNote: reason}); err != nil {
			return err
		}
		owner, err := q.GetUser(ctx, o.OwnerUserID)
		if err != nil {
			return err
		}
		out = Reviewed{Organization: o, OwnerEmail: owner.Email, OwnerDisplayName: owner.DisplayName}
		return audit.Record(ctx, q, audit.Event{
			Actor: adminID, OrgID: o.ID, Action: audit.OrganizationReject, TargetType: "Organization", TargetID: o.ID,
			Detail: map[string]any{"reason": reason},
		})
	})
	return out, err
}

// lockPending locks the organization for a review: two Admins reviewing the
// same request at once are served one after the other, and the second
// finds it reviewed.
func lockPending(ctx context.Context, q *store.Queries, orgID string) (store.Organization, error) {
	o, err := q.LockOrganization(ctx, orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, ErrNoSuchOrg
	}
	if err != nil {
		return o, err
	}
	if o.Status != StatusPending {
		return o, ErrNotPending
	}
	return o, nil
}

// MachineName is the group's name in Keycloak and in Kubernetes RBAC:
// globally unique and never renamed, unlike the display name.
func MachineName(orgID, groupID string) string {
	return "org_" + id.ULID(orgID) + ":group_" + id.ULID(groupID)
}
