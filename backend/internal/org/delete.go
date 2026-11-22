package org

import (
	"context"
	"fmt"

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
	return store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		cur, err := q.LockOrganization(ctx, o.ID)
		if err != nil {
			return err
		}
		if cur.Status != StatusActive {
			return ErrNotActive
		}
		if _, err := q.LockEntitlement(ctx, o.ID); err != nil {
			return err
		}
		left, err := q.OrganizationLeftovers(ctx, o.ID)
		if err != nil {
			return err
		}
		if left.Workspaces > 0 || left.DedicatedNodes > 0 {
			return &apperr.Error{
				Code: apperr.OrganizationNotEmpty,
				Message: fmt.Sprintf("the organization still has %d Workspace(s) and %d dedicated node(s): delete the Workspaces, and ask a Platform Admin to take the nodes back",
					left.Workspaces, left.DedicatedNodes),
				Details: map[string]any{"workspaces": left.Workspaces, "dedicatedNodes": left.DedicatedNodes},
			}
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
			Actor: userID, OrgID: o.ID, Action: audit.OrganizationDelete, TargetType: "Organization", TargetID: o.ID,
			Detail: map[string]any{
				"slug": o.Slug, "grants": grants, "invitationsRevoked": invitations, "transfersCancelled": transfers, "groups": groups,
			},
		})
	})
}
