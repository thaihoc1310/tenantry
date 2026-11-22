package org

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/audit"
	"github.com/thaihoc1310/tenantry/backend/internal/id"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// Invitations (PRODUCT-PLAN §9.1, 03-flows §3, 04-state-machines §7): an
// OrgOwner or OrgAdmin creates a one-time link and sends it by hand; the
// person accepts it while logged in with the verified email it was sent to.

// InvitationTTL is how long an invitation link works.
const InvitationTTL = 7 * 24 * time.Hour

const (
	invitationPending = "Pending"
	invitationRevoked = "Revoked"
	maxEmail          = 254
)

var (
	ErrInvitationInvalid = apperr.New(apperr.InvitationInvalid,
		"this invitation link no longer works: it expired, was used or was revoked")
	ErrInvitationNotFound = apperr.New(apperr.NotFound, "invitation not found")
	ErrEmailMismatch      = apperr.New(apperr.InvitationEmailMismatch,
		"this invitation is for another email address; log in with the address it was sent to")
	ErrAlreadyMember  = apperr.New(apperr.Conflict, "this person is already a member of the organization")
	ErrInvitationDone = apperr.New(apperr.Conflict, "the invitation was already accepted or has expired")
	ErrMembersFull    = &apperr.Error{
		Code:    apperr.EntitlementExceeded,
		Message: "the organization has as many members as its entitlement allows",
		Details: map[string]any{"limit": "maxMembers"},
	}
)

// InviteRequest is what an OrgOwner or OrgAdmin fills in.
type InviteRequest struct {
	Email    string
	Role     Role
	GroupIDs []string // groups the person joins on accepting; optional
}

// Invitation is an open invitation as its organization's admins see it.
type Invitation struct {
	store.Invitation
	InviterName  string
	InviterEmail *string
}

// Expired tells an open invitation whose link stopped working.
func (i Invitation) Expired(now time.Time) bool { return !now.Before(i.ExpiresAt) }

// CreatedInvitation carries the token, the only time it exists outside the
// person's link: the database keeps its sha256 only.
type CreatedInvitation struct {
	Invitation
	Token string
}

// Invite creates an invitation, replacing an open one for the same email.
// maxMembers is checked under the entitlement's lock; the invitation itself
// takes no place until it is accepted (checked again then).
func (s *Service) Invite(ctx context.Context, userID, idOrSlug string, r InviteRequest) (CreatedInvitation, error) {
	email, err := checkEmail(r.Email)
	if err != nil {
		return CreatedInvitation{}, err
	}
	if r.Role != RoleAdmin && r.Role != RoleMember {
		return CreatedInvitation{}, apperr.Invalid("role", "must be OrgAdmin or OrgMember: ownership is transferred, not given by invitation")
	}
	groups := slices.Clone(r.GroupIDs)
	slices.Sort(groups)
	if groups = slices.Compact(groups); groups == nil {
		groups = []string{}
	}
	o, err := s.manage(ctx, userID, idOrSlug)
	if err != nil {
		return CreatedInvitation{}, err
	}

	var out CreatedInvitation
	err = store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := roomForOneMore(ctx, q, o.ID); err != nil {
			return err
		}
		if _, err := q.MemberByEmail(ctx, store.MemberByEmailParams{OrgID: o.ID, Email: email}); err == nil {
			return ErrAlreadyMember
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if len(groups) > 0 {
			n, err := q.CountLiveGroups(ctx, store.CountLiveGroupsParams{OrgID: o.ID, Ids: groups})
			if err != nil {
				return err
			}
			if int(n) != len(groups) {
				return apperr.Invalid("groupIds", "must be groups of this organization")
			}
		}
		replaced, err := q.RevokePendingInvitationsFor(ctx, store.RevokePendingInvitationsForParams{OrgID: o.ID, Email: email})
		if err != nil {
			return err
		}
		token, hash := newInvitationToken()
		inv, err := q.CreateInvitation(ctx, store.CreateInvitationParams{
			ID: id.New(id.Invitation), OrgID: o.ID, Email: email, TokenHash: hash, Role: string(r.Role),
			GroupIds: groups, InvitedBy: userID, ExpiresAt: time.Now().Add(InvitationTTL),
		})
		if err != nil {
			return err
		}
		inviter, err := q.GetUser(ctx, userID)
		if err != nil {
			return err
		}
		out = CreatedInvitation{Invitation: Invitation{Invitation: inv, InviterName: inviter.DisplayName, InviterEmail: inviter.Email}, Token: token}
		return audit.Record(ctx, q, audit.Event{
			Actor: userID, OrgID: o.ID, Action: audit.InvitationCreate, TargetType: "Invitation", TargetID: inv.ID,
			Detail: map[string]any{"email": email, "role": inv.Role, "groupIds": groups, "replacedOpenInvitation": replaced > 0},
		})
	})
	return out, err
}

// InvitationPage is one page of open invitations, newest first.
type InvitationPage struct {
	Items      []Invitation
	NextCursor string
}

func (s *Service) Invitations(ctx context.Context, userID, idOrSlug, cursor string, pageSize int) (InvitationPage, error) {
	if pageSize == 0 {
		pageSize = DefaultPageSize
	}
	if pageSize < 1 || pageSize > MaxPageSize {
		return InvitationPage{}, apperr.Invalid("limit", fmt.Sprintf("must be between 1 and %d", MaxPageSize))
	}
	o, err := s.manage(ctx, userID, idOrSlug)
	if err != nil {
		return InvitationPage{}, err
	}
	arg := store.ListInvitationsParams{OrgID: o.ID, PageSize: int32(pageSize + 1)}
	if cursor != "" {
		arg.Cursor = &cursor
	}
	rows, err := s.q.ListInvitations(ctx, arg)
	if err != nil {
		return InvitationPage{}, err
	}
	p := InvitationPage{Items: make([]Invitation, 0, len(rows))}
	for _, r := range rows {
		p.Items = append(p.Items, Invitation{Invitation: r.Invitation, InviterName: r.InviterName, InviterEmail: r.InviterEmail})
	}
	if len(p.Items) > pageSize {
		p.Items = p.Items[:pageSize]
		p.NextCursor = p.Items[pageSize-1].ID
	}
	return p, nil
}

// Revoke ends an open invitation (OrgOwner or OrgAdmin of its organization).
// Revoking a revoked one is a no-op; an accepted or expired one is a conflict.
func (s *Service) Revoke(ctx context.Context, userID, invitationID string) error {
	inv, err := s.q.GetInvitation(ctx, invitationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvitationNotFound
	}
	if err != nil {
		return err
	}
	role, err := s.q.GetMembership(ctx, store.GetMembershipParams{OrgID: inv.OrgID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvitationNotFound // non-members cannot tell it exists
	}
	if err != nil {
		return err
	}
	if Role(role) != RoleOwner && Role(role) != RoleAdmin {
		return ErrForbidden
	}
	return store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		revoked, err := q.RevokeInvitation(ctx, inv.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			cur, err := q.GetInvitation(ctx, inv.ID)
			if err != nil {
				return err
			}
			if cur.State == invitationRevoked {
				return nil
			}
			return ErrInvitationDone
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Event{
			Actor: userID, OrgID: revoked.OrgID, Action: audit.InvitationRevoke, TargetType: "Invitation", TargetID: revoked.ID,
			Detail: map[string]any{"email": revoked.Email},
		})
	})
}

// InvitationPreview is what the invited person sees before accepting.
type InvitationPreview struct {
	OrgID, OrgName, OrgSlug string
	Role                    Role
	// Email is the invited address, partly hidden unless it is the caller's own.
	Email         string
	EmailMatches  bool
	InviterName   string
	GroupNames    []string
	ExpiresAt     time.Time
	AlreadyMember bool
}

// Preview shows an invitation to a logged-in person without accepting it.
// The token is the capability: no membership is needed.
func (s *Service) Preview(ctx context.Context, user store.User, token string) (InvitationPreview, error) {
	row, err := s.openInvitation(ctx, token)
	if err != nil {
		return InvitationPreview{}, err
	}
	inv := row.Invitation
	matches := sameEmail(user.Email, inv.Email)
	shown := inv.Email
	if !matches {
		shown = maskEmail(inv.Email)
	}
	_, err = s.q.GetMembership(ctx, store.GetMembershipParams{OrgID: inv.OrgID, UserID: user.ID})
	member := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return InvitationPreview{}, err
	}
	return InvitationPreview{
		OrgID: inv.OrgID, OrgName: row.OrgName, OrgSlug: row.OrgSlug, Role: Role(inv.Role),
		Email: shown, EmailMatches: matches, InviterName: row.InviterName, GroupNames: row.GroupNames,
		ExpiresAt: inv.ExpiresAt, AlreadyMember: member,
	}, nil
}

// openInvitation finds a usable invitation by its token.
func (s *Service) openInvitation(ctx context.Context, token string) (store.PreviewInvitationRow, error) {
	hash, ok := invitationHash(token)
	if !ok {
		return store.PreviewInvitationRow{}, ErrInvitationInvalid
	}
	row, err := s.q.PreviewInvitation(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, ErrInvitationInvalid
	}
	if err != nil {
		return row, err
	}
	if row.Invitation.State != invitationPending || !time.Now().Before(row.Invitation.ExpiresAt) || row.OrgStatus != StatusActive {
		return row, ErrInvitationInvalid
	}
	return row, nil
}

// Accept makes the logged-in person a member with the invitation's role and
// groups. Locks: the entitlement, then the invitation, so two accepts of one
// link are served one after the other and the second finds it used; maxMembers
// is checked again because members may have joined since the invitation.
//
// A wrong email does not use up the link (the right person may still open
// it); the refusal is audited in a transaction of its own, after the
// refused one rolled back.
func (s *Service) Accept(ctx context.Context, user store.User, token string) (Org, error) {
	row, err := s.openInvitation(ctx, token)
	if err != nil {
		return Org{}, err
	}
	expired := false
	var joined Org
	err = store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := q.LockEntitlement(ctx, row.Invitation.OrgID); err != nil {
			return err
		}
		inv, err := q.LockInvitation(ctx, row.Invitation.ID)
		if err != nil {
			return err
		}
		if inv.State != invitationPending {
			return ErrInvitationInvalid // the other tab, or a revoke, got here first
		}
		if !time.Now().Before(inv.ExpiresAt) {
			expired = true
			return q.ExpireInvitation(ctx, inv.ID) // recorded, then refused below
		}
		if !sameEmail(user.Email, inv.Email) {
			return ErrEmailMismatch
		}
		if _, err := q.GetMembership(ctx, store.GetMembershipParams{OrgID: inv.OrgID, UserID: user.ID}); err == nil {
			return ErrAlreadyMember
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err := roomForOneMore(ctx, q, inv.OrgID); err != nil {
			return err
		}
		if err := q.AddOrganizationMember(ctx, store.AddOrganizationMemberParams{OrgID: inv.OrgID, UserID: user.ID, Role: inv.Role}); err != nil {
			return err
		}
		if len(inv.GroupIds) > 0 {
			if err := q.JoinGroups(ctx, store.JoinGroupsParams{UserID: user.ID, OrgID: inv.OrgID, Ids: inv.GroupIds}); err != nil {
				return err
			}
		}
		if err := q.AcceptInvitation(ctx, store.AcceptInvitationParams{ID: inv.ID, AcceptedBy: &user.ID}); err != nil {
			return err
		}
		// Read back inside the transaction: with an Idempotency-Key it is the
		// request's, and the pool would not see the membership yet.
		m, err := q.GetOrganizationForMember(ctx, store.GetOrganizationForMemberParams{UserID: user.ID, IDOrSlug: inv.OrgID})
		if err != nil {
			return err
		}
		joined = Org{Organization: m.Organization, Role: Role(m.Role)}
		return audit.Record(ctx, q, audit.Event{
			Actor: user.ID, OrgID: inv.OrgID, Action: audit.InvitationAccept, TargetType: "Invitation", TargetID: inv.ID,
			Detail: map[string]any{"role": inv.Role, "groupIds": inv.GroupIds},
		})
	})
	switch {
	case expired:
		return Org{}, ErrInvitationInvalid
	case errors.Is(err, ErrEmailMismatch):
		if aerr := audit.Record(ctx, s.q, audit.Event{
			Actor: user.ID, OrgID: row.Invitation.OrgID, Action: audit.InvitationAccept, TargetType: "Invitation",
			TargetID: row.Invitation.ID, Result: audit.Refused, Detail: map[string]any{"reason": "email does not match"},
		}); aerr != nil {
			return Org{}, aerr
		}
		return Org{}, err
	case err != nil:
		return Org{}, err
	}
	return joined, nil
}

// roomForOneMore locks the organization's entitlement and checks that one
// more member fits.
func roomForOneMore(ctx context.Context, q *store.Queries, orgID string) error {
	e, err := q.LockEntitlement(ctx, orgID)
	if err != nil {
		return err
	}
	n, err := q.CountMembers(ctx, orgID)
	if err != nil {
		return err
	}
	if n+1 > e.MaxMembers {
		return ErrMembersFull
	}
	return nil
}

// checkEmail normalizes an invited address: trimmed, lowercase, one plain address.
func checkEmail(v string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(v))
	a, err := mail.ParseAddress(e)
	if err != nil || a.Address != e || a.Name != "" || len(e) > maxEmail {
		return "", apperr.Invalid("email", "must be one email address, like joe@example.com")
	}
	return e, nil
}

func sameEmail(verified *string, invited string) bool {
	return verified != nil && strings.ToLower(*verified) == invited
}

// maskEmail shows enough of an address to recognise it, not to learn it:
// joe@example.com becomes j•••@example.com.
func maskEmail(e string) string {
	local, domain, ok := strings.Cut(e, "@")
	if !ok || local == "" {
		return "•••"
	}
	return local[:1] + "•••@" + domain
}

// newInvitationToken returns 32 random bytes for the link and their sha256 for the table.
func newInvitationToken() (token string, hash []byte) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	h := sha256.Sum256(b)
	return base64.RawURLEncoding.EncodeToString(b), h[:]
}

func invitationHash(token string) ([]byte, bool) {
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(b) != 32 {
		return nil, false
	}
	h := sha256.Sum256(b)
	return h[:], true
}
