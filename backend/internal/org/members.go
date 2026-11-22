package org

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/audit"
	"github.com/thaihoc1310/tenantry/backend/internal/id"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// Members, leaving, and ownership transfer (PRODUCT-PLAN §9.2, §9.4;
// 03-flows §11; 04-state-machines §8).
//
// Leaving, removing, changing a role and accepting a transfer all lock the
// organization's row first, so they are served one at a time: a recipient
// cannot become the owner while leaving, and an organization never has
// zero or two owners.

// TransferTTL is how long an ownership transfer waits for its recipient.
const TransferTTL = 7 * 24 * time.Hour

const (
	transferProposed  = "Proposed"
	transferAccepted  = "Accepted"
	transferCancelled = "Cancelled"
	transferExpired   = "Expired"
)

var (
	ErrNoSuchMember   = apperr.New(apperr.NotFound, "member not found")
	ErrOwnerRole      = apperr.New(apperr.Forbidden, "the OrgOwner's role changes only by transferring ownership")
	ErrRemoveOwner    = apperr.New(apperr.Forbidden, "the OrgOwner cannot be removed; transfer ownership first")
	ErrLeaveOwner     = apperr.New(apperr.LeaveNotAllowed, "the OrgOwner cannot leave; transfer ownership to an OrgAdmin first")
	ErrNotOwner       = apperr.New(apperr.TransferNotAllowed, "only the OrgOwner can transfer ownership")
	ErrRecipientAdmin = &apperr.Error{
		Code: apperr.RecipientNotOrgAdmin, Message: "the new owner must be an OrgAdmin of this organization",
		Details: map[string]any{"field": "toUserId"},
	}
	ErrTransferInvalid  = apperr.New(apperr.TransferInvalid, "this ownership transfer can no longer be accepted")
	ErrNoTransfer       = apperr.New(apperr.NotFound, "no ownership transfer is waiting")
	ErrNoSuchTransfer   = apperr.New(apperr.NotFound, "ownership transfer not found")
	ErrTransferNotYours = apperr.New(apperr.Forbidden, "only the owner who offered it or its recipient can cancel this transfer")
	ErrTransferDone     = apperr.New(apperr.Conflict, "the ownership transfer was already accepted")
)

// Member is a member of an organization with the groups they are in.
type Member struct {
	UserID      string
	Email       *string
	DisplayName string
	Role        Role
	JoinedAt    time.Time
	GroupIDs    []string
	GroupNames  []string
}

type MemberPage struct {
	Items      []Member
	NextCursor string
}

// Members lists the organization's members for its OrgOwner and OrgAdmins.
func (s *Service) Members(ctx context.Context, userID, idOrSlug, cursor string, pageSize int) (MemberPage, error) {
	if pageSize == 0 {
		pageSize = DefaultPageSize
	}
	if pageSize < 1 || pageSize > MaxPageSize {
		return MemberPage{}, apperr.Invalid("limit", fmt.Sprintf("must be between 1 and %d", MaxPageSize))
	}
	o, err := s.manage(ctx, userID, idOrSlug)
	if err != nil {
		return MemberPage{}, err
	}
	arg := store.ListMembersParams{OrgID: o.ID, PageSize: int32(pageSize + 1)}
	if cursor != "" {
		arg.Cursor = &cursor
	}
	rows, err := s.q.ListMembers(ctx, arg)
	if err != nil {
		return MemberPage{}, err
	}
	p := MemberPage{Items: make([]Member, 0, len(rows))}
	for _, r := range rows {
		p.Items = append(p.Items, Member{
			UserID: r.UserID, Email: r.Email, DisplayName: r.DisplayName, Role: Role(r.Role), JoinedAt: r.CreatedAt,
			GroupIDs: r.GroupIds, GroupNames: r.GroupNames,
		})
	}
	if len(p.Items) > pageSize {
		p.Items = p.Items[:pageSize]
		p.NextCursor = p.Items[pageSize-1].UserID
	}
	return p, nil
}

// SetRole makes a member an OrgAdmin or an OrgMember (OrgOwner or OrgAdmin).
// The owner's role changes by transfer only. A recipient of a waiting
// transfer who stops being an OrgAdmin can no longer accept it, so it is cancelled.
func (s *Service) SetRole(ctx context.Context, actorID, idOrSlug, targetID string, role Role) (Member, error) {
	if role != RoleAdmin && role != RoleMember {
		return Member{}, apperr.Invalid("role", "must be OrgAdmin or OrgMember: ownership is transferred")
	}
	o, err := s.manage(ctx, actorID, idOrSlug)
	if err != nil {
		return Member{}, err
	}
	var out Member
	err = store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := q.LockOrganization(ctx, o.ID); err != nil {
			return err
		}
		before, err := memberRole(ctx, q, o.ID, targetID)
		if err != nil {
			return err
		}
		if before == RoleOwner {
			return ErrOwnerRole
		}
		if before != role {
			if err := q.UpdateMemberRole(ctx, store.UpdateMemberRoleParams{OrgID: o.ID, UserID: targetID, Role: string(role)}); err != nil {
				return err
			}
			cancelled := int64(0)
			if role != RoleAdmin {
				if cancelled, err = q.CancelTransfersTo(ctx, store.CancelTransfersToParams{OrgID: o.ID, ToUserID: targetID}); err != nil {
					return err
				}
			}
			if err := audit.Record(ctx, q, audit.Event{
				Actor: actorID, OrgID: o.ID, Action: audit.MemberRoleChange, TargetType: "User", TargetID: targetID,
				Detail: map[string]any{"before": before, "after": role, "cancelledTransfer": cancelled > 0},
			}); err != nil {
				return err
			}
		}
		out, err = member(ctx, q, o.ID, targetID)
		return err
	})
	return out, err
}

// Remove takes someone out of the organization (OrgOwner or OrgAdmin); never the owner.
func (s *Service) Remove(ctx context.Context, actorID, idOrSlug, targetID string) error {
	o, err := s.manage(ctx, actorID, idOrSlug)
	if err != nil {
		return err
	}
	return store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := q.LockOrganization(ctx, o.ID); err != nil {
			return err
		}
		role, err := memberRole(ctx, q, o.ID, targetID)
		if err != nil {
			return err
		}
		if role == RoleOwner {
			return ErrRemoveOwner
		}
		return leave(ctx, q, actorID, o.ID, targetID, audit.MemberRemove)
	})
}

// Leave takes the caller out of the organization, unless they own it.
func (s *Service) Leave(ctx context.Context, userID, idOrSlug string) error {
	o, err := s.Get(ctx, userID, idOrSlug)
	if err != nil {
		return err
	}
	return store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := q.LockOrganization(ctx, o.ID); err != nil {
			return err
		}
		role, err := memberRole(ctx, q, o.ID, userID)
		if errors.Is(err, ErrNoSuchMember) {
			return ErrNotFound // removed meanwhile
		}
		if err != nil {
			return err
		}
		if role == RoleOwner {
			return ErrLeaveOwner
		}
		return leave(ctx, q, userID, o.ID, userID, audit.MemberLeave)
	})
}

// leave ends a membership: the row first, then the group memberships become
// tombstones in a statement of their own (02-data-model §2), and a transfer
// waiting for the person is cancelled. Portal access ends at once; Kubernetes
// access within one token lifetime, after the group sync worker (06 §5).
func leave(ctx context.Context, q *store.Queries, actorID, orgID, userID, action string) error {
	if _, err := q.DeleteMember(ctx, store.DeleteMemberParams{OrgID: orgID, UserID: userID}); err != nil {
		return err
	}
	groups, err := q.TombstoneMemberGroups(ctx, store.TombstoneMemberGroupsParams{OrgID: orgID, UserID: userID})
	if err != nil {
		return err
	}
	cancelled, err := q.CancelTransfersTo(ctx, store.CancelTransfersToParams{OrgID: orgID, ToUserID: userID})
	if err != nil {
		return err
	}
	return audit.Record(ctx, q, audit.Event{
		Actor: actorID, OrgID: orgID, Action: action, TargetType: "User", TargetID: userID,
		Detail: map[string]any{"groupsLeft": groups, "cancelledTransfer": cancelled > 0},
	})
}

func memberRole(ctx context.Context, q *store.Queries, orgID, userID string) (Role, error) {
	r, err := q.GetMembership(ctx, store.GetMembershipParams{OrgID: orgID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoSuchMember
	}
	return Role(r), err
}

func member(ctx context.Context, q *store.Queries, orgID, userID string) (Member, error) {
	r, err := q.GetMember(ctx, store.GetMemberParams{OrgID: orgID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrNoSuchMember
	}
	if err != nil {
		return Member{}, err
	}
	return Member{
		UserID: r.UserID, Email: r.Email, DisplayName: r.DisplayName, Role: Role(r.Role), JoinedAt: r.CreatedAt,
		GroupIDs: r.GroupIds, GroupNames: r.GroupNames,
	}, nil
}

// Transfer is an ownership transfer with the two people it is between.
type Transfer struct {
	store.OwnershipTransfer
	FromEmail *string
	FromName  string
	ToEmail   *string
	ToName    string
}

// ProposeTransfer offers the organization to one of its OrgAdmins (OrgOwner
// only). A new offer cancels the one waiting; the recipient accepts in the portal.
func (s *Service) ProposeTransfer(ctx context.Context, actorID, idOrSlug, toUserID string) (Transfer, error) {
	o, err := s.Get(ctx, actorID, idOrSlug)
	if err != nil {
		return Transfer{}, err
	}
	if o.Role != RoleOwner {
		return Transfer{}, ErrNotOwner
	}
	if o.Status != StatusActive {
		return Transfer{}, ErrNotActive
	}
	var out Transfer
	err = store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := q.LockOrganization(ctx, o.ID); err != nil {
			return err
		}
		if r, err := memberRole(ctx, q, o.ID, actorID); err != nil || r != RoleOwner {
			return ErrNotOwner // transferred or removed meanwhile
		}
		if r, err := memberRole(ctx, q, o.ID, toUserID); errors.Is(err, ErrNoSuchMember) || (err == nil && r != RoleAdmin) {
			return ErrRecipientAdmin
		} else if err != nil {
			return err
		}
		replaced, err := q.CancelProposedTransfer(ctx, o.ID)
		if err != nil {
			return err
		}
		t, err := q.CreateTransfer(ctx, store.CreateTransferParams{
			ID: id.New(id.Transfer), OrgID: o.ID, FromUserID: actorID, ToUserID: toUserID, ExpiresAt: time.Now().Add(TransferTTL),
		})
		if err != nil {
			return err
		}
		if out, err = proposedTransfer(ctx, q, o.ID); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Event{
			Actor: actorID, OrgID: o.ID, Action: audit.TransferPropose, TargetType: "OwnershipTransfer", TargetID: t.ID,
			Detail: map[string]any{"to": toUserID, "replacedWaitingTransfer": replaced > 0},
		})
	})
	return out, err
}

// PendingTransfer is the transfer waiting in the organization, shown to its
// owner and to its recipient only; anyone else is told there is none.
func (s *Service) PendingTransfer(ctx context.Context, userID, idOrSlug string) (Transfer, error) {
	o, err := s.Get(ctx, userID, idOrSlug)
	if err != nil {
		return Transfer{}, err
	}
	t, err := proposedTransfer(ctx, s.q, o.ID)
	if err != nil {
		return Transfer{}, err
	}
	if !time.Now().Before(t.ExpiresAt) || (userID != t.FromUserID && userID != t.ToUserID) {
		return Transfer{}, ErrNoTransfer
	}
	return t, nil
}

func proposedTransfer(ctx context.Context, q *store.Queries, orgID string) (Transfer, error) {
	r, err := q.GetProposedTransfer(ctx, orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Transfer{}, ErrNoTransfer
	}
	if err != nil {
		return Transfer{}, err
	}
	return Transfer{OwnershipTransfer: r.OwnershipTransfer, FromEmail: r.FromEmail, FromName: r.FromName, ToEmail: r.ToEmail, ToName: r.ToName}, nil
}

// transferFor finds a transfer for someone who belongs to its organization;
// to anyone else it does not exist.
func (s *Service) transferFor(ctx context.Context, userID, transferID string) (store.OwnershipTransfer, error) {
	t, err := s.q.GetTransfer(ctx, transferID)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, ErrNoSuchTransfer
	}
	if err != nil {
		return t, err
	}
	if _, err := memberRole(ctx, s.q, t.OrgID, userID); errors.Is(err, ErrNoSuchMember) {
		return t, ErrNoSuchTransfer
	} else if err != nil {
		return t, err
	}
	return t, nil
}

// AcceptTransfer makes the recipient the owner. Under the organization's
// lock, everything is checked again (still waiting, not expired, the caller
// is the recipient and still an OrgAdmin, the proposer still the owner);
// then the old owner becomes an OrgAdmin first and the recipient the owner
// second, because the one-owner index cannot be deferred to the commit.
func (s *Service) AcceptTransfer(ctx context.Context, userID, transferID string) (Org, error) {
	t, err := s.transferFor(ctx, userID, transferID)
	if err != nil {
		return Org{}, err
	}
	expired := false
	var out Org
	err = store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		o, err := q.LockOrganization(ctx, t.OrgID)
		if err != nil {
			return err
		}
		cur, err := q.LockTransfer(ctx, t.ID)
		if err != nil {
			return err
		}
		if cur.State != transferProposed || userID != cur.ToUserID {
			return ErrTransferInvalid
		}
		if !time.Now().Before(cur.ExpiresAt) {
			expired = true
			return q.SetTransferState(ctx, store.SetTransferStateParams{ID: cur.ID, State: transferExpired})
		}
		to, err := memberRole(ctx, q, o.ID, cur.ToUserID)
		if err != nil || to != RoleAdmin {
			return ErrTransferInvalid
		}
		from, err := memberRole(ctx, q, o.ID, cur.FromUserID)
		if err != nil || from != RoleOwner || o.OwnerUserID != cur.FromUserID {
			return ErrTransferInvalid
		}
		// Demote first: the unique "one OrgOwner per organization" index is checked per statement.
		if err := q.UpdateMemberRole(ctx, store.UpdateMemberRoleParams{OrgID: o.ID, UserID: cur.FromUserID, Role: string(RoleAdmin)}); err != nil {
			return err
		}
		if err := q.UpdateMemberRole(ctx, store.UpdateMemberRoleParams{OrgID: o.ID, UserID: cur.ToUserID, Role: string(RoleOwner)}); err != nil {
			return err
		}
		if err := q.SetOrganizationOwner(ctx, store.SetOrganizationOwnerParams{ID: o.ID, OwnerUserID: cur.ToUserID}); err != nil {
			return err
		}
		if err := q.SetTransferState(ctx, store.SetTransferStateParams{ID: cur.ID, State: transferAccepted}); err != nil {
			return err
		}
		m, err := q.GetOrganizationForMember(ctx, store.GetOrganizationForMemberParams{UserID: userID, IDOrSlug: o.ID})
		if err != nil {
			return err
		}
		out = Org{Organization: m.Organization, Role: Role(m.Role)}
		return audit.Record(ctx, q, audit.Event{
			Actor: userID, OrgID: o.ID, Action: audit.TransferAccept, TargetType: "OwnershipTransfer", TargetID: cur.ID,
			Detail: map[string]any{"from": cur.FromUserID, "to": cur.ToUserID},
		})
	})
	if expired {
		return Org{}, ErrTransferInvalid
	}
	return out, err
}

// CancelTransfer withdraws (by the owner who offered it) or declines (by its
// recipient) a waiting transfer. Cancelling one that ended changes nothing,
// unless it was accepted.
func (s *Service) CancelTransfer(ctx context.Context, userID, transferID string) error {
	t, err := s.transferFor(ctx, userID, transferID)
	if err != nil {
		return err
	}
	if userID != t.FromUserID && userID != t.ToUserID {
		return ErrTransferNotYours
	}
	return store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := q.LockOrganization(ctx, t.OrgID); err != nil {
			return err
		}
		cur, err := q.LockTransfer(ctx, t.ID)
		if err != nil {
			return err
		}
		switch {
		case cur.State == transferAccepted:
			return ErrTransferDone
		case cur.State != transferProposed:
			return nil // cancelled or expired already
		case !time.Now().Before(cur.ExpiresAt):
			return q.SetTransferState(ctx, store.SetTransferStateParams{ID: cur.ID, State: transferExpired})
		}
		if err := q.SetTransferState(ctx, store.SetTransferStateParams{ID: cur.ID, State: transferCancelled}); err != nil {
			return err
		}
		reason := "withdrawn by the owner"
		if userID == cur.ToUserID {
			reason = "declined by the recipient"
		}
		return audit.Record(ctx, q, audit.Event{
			Actor: userID, OrgID: cur.OrgID, Action: audit.TransferCancel, TargetType: "OwnershipTransfer", TargetID: cur.ID,
			Detail: map[string]any{"reason": reason},
		})
	})
}
