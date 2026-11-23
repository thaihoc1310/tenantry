package org

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/audit"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

var ErrOwnerOnly = apperr.New(apperr.Forbidden, "only the OrgOwner can delete the organization")

// Delete deletes an Active organization (OrgOwner; 04-state-machines §11).
// Not while it has a Workspace row (a Deleted one waiting for its CIDR's
// quarantine included) or a dedicated node: ORGANIZATION_NOT_EMPTY. One
// transaction, under the organization's and then the entitlement's lock, so
// it queues with every create that counts against the entitlement: the
// status becomes Deleted, its grants go, its open invitations and transfers
// end, and its groups and memberships become tombstones the group sync
// worker removes from Keycloak. A Deleted organization is 404 everywhere.
func (s *Service) Delete(ctx context.Context, userID, idOrSlug string) error {
	o, err := s.Get(ctx, userID, idOrSlug)
	if err != nil {
		return err
	}
	if o.Role != RoleOwner {
		return ErrOwnerOnly
	}
	if o.Status != StatusActive {
		return ErrNotActive
	}
	return s.deleteActive(ctx, userID, o.ID, false)
}

// AdminDelete is Delete by a Platform Admin (PRODUCT-PLAN §16.5), for an
// organization whose owner cannot do it (gone, or a test fixture): the same
// checks and effects, the admin as the actor of the audit event. Deleting a
// Deleted organization again changes nothing; a request (Pending or
// Rejected) is not deleted this way (reject it).
func (s *Service) AdminDelete(ctx context.Context, adminID, orgID string) error {
	return s.deleteActive(ctx, adminID, orgID, true)
}

// deleteActive deletes an Active organization in one transaction, under
// the organization's and then the entitlement's lock. A Deleted one is
// done already when idempotent (the admin's call), not Active otherwise.
func (s *Service) deleteActive(ctx context.Context, actorID, orgID string, idempotent bool) error {
	return store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		cur, err := q.LockOrganization(ctx, orgID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSuchOrg
		}
		if err != nil {
			return err
		}
		if cur.Status == StatusDeleted && idempotent {
			return nil
		}
		if cur.Status != StatusActive {
			return ErrNotActive
		}
		o := cur
		if _, err := q.LockEntitlement(ctx, o.ID); err != nil {
			return err
		}
		left, err := q.OrganizationLeftovers(ctx, o.ID)
		if err != nil {
			return err
		}
		if left.Workspaces > 0 || left.QuarantinedWorkspaces > 0 || left.DedicatedNodes > 0 {
			var until *time.Time
			if left.QuarantinedWorkspaces > 0 {
				if until, err = q.LastQuarantineEnd(ctx, o.ID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
					return err
				}
			}
			return notEmpty(left, until)
		}
		if err := q.MarkOrganizationDeleted(ctx, o.ID); err != nil {
			return err
		}
		grants, err := q.DeleteOrganizationGrants(ctx, o.ID)
		if err != nil {
			return err
		}
		invitations, err := q.RevokeOrganizationInvitations(ctx, o.ID)
		if err != nil {
			return err
		}
		transfers, err := q.CancelProposedTransfer(ctx, o.ID)
		if err != nil {
			return err
		}
		groups, err := q.TombstoneOrganizationGroups(ctx, o.ID)
		if err != nil {
			return err
		}
		if _, err := q.TombstoneOrganizationGroupMembers(ctx, o.ID); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Event{
			Actor: actorID, OrgID: o.ID, Action: audit.OrganizationDelete, TargetType: "Organization", TargetID: o.ID,
			Detail: map[string]any{
				"slug": o.Slug, "grants": grants, "invitationsRevoked": invitations, "transfersCancelled": transfers, "groups": groups,
			},
		})
	})
}

// notEmpty says what keeps the organization: live Workspaces and dedicated
// nodes the person (or an Admin) must remove, and Deleted Workspaces whose
// address block is still in quarantine, which only time removes.
func notEmpty(left store.OrganizationLeftoversRow, until *time.Time) error {
	var todo []string
	if left.Workspaces > 0 {
		todo = append(todo, fmt.Sprintf("delete its %d Workspace(s)", left.Workspaces))
	}
	if left.DedicatedNodes > 0 {
		todo = append(todo, fmt.Sprintf("ask a Platform Admin to take back its %d dedicated node(s)", left.DedicatedNodes))
	}
	details := map[string]any{
		"workspaces": left.Workspaces, "quarantinedWorkspaces": left.QuarantinedWorkspaces, "dedicatedNodes": left.DedicatedNodes,
	}
	if left.QuarantinedWorkspaces > 0 {
		wait := fmt.Sprintf("wait for the quarantine of %d deleted Workspace(s)", left.QuarantinedWorkspaces)
		if until != nil {
			at := until.UTC().Format(time.RFC3339)
			wait = fmt.Sprintf("wait until %s, when the quarantine of %d deleted Workspace(s) ends", at, left.QuarantinedWorkspaces)
			details["deletableAfter"] = at
		}
		todo = append(todo, wait)
	}
	return &apperr.Error{
		Code:    apperr.OrganizationNotEmpty,
		Message: "the organization is not empty yet: " + strings.Join(todo, "; "),
		Details: details,
	}
}
