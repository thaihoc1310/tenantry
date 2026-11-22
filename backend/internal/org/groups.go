package org

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/audit"
	"github.com/thaihoc1310/tenantry/backend/internal/id"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// Organization groups (PRODUCT-PLAN §9.3, §9.4; 04-state-machines §9.1).
// PostgreSQL is the source of truth; the group sync worker mirrors every
// change to Keycloak. Removals are tombstones: a row leaves the database only
// after Keycloak has caught up, so no change is lost while Keycloak is down.
// A tombstone no longer counts anywhere in the portal.
//
// Deleting a group still bound to a Workspace is refused from M6, with the
// table that holds the bindings (workspace_group_bindings); before M6 a group
// cannot have bindings.

// groupNameIndex keeps live group names unique in an organization.
const groupNameIndex = "groups_display_name_uq"

const maxGroupName = 63

var (
	ErrNoSuchGroup   = apperr.New(apperr.NotFound, "group not found")
	ErrGroupNameUsed = &apperr.Error{Code: apperr.Conflict, Message: "the organization already has a group with this name", Details: map[string]any{"field": "name"}}
	ErrGroupsFull    = &apperr.Error{
		Code:    apperr.EntitlementExceeded,
		Message: "the organization has as many groups as its entitlement allows (the three default groups count)",
		Details: map[string]any{"limit": "maxGroups"},
	}
	ErrNotOrgMember = &apperr.Error{Code: apperr.Validation, Message: "userId: must be a member of the organization", Details: map[string]any{"field": "userId"}}
)

// Group is a live group with its live members.
type Group struct {
	store.Group
	Members []GroupMember
}

type GroupMember struct {
	UserID      string
	DisplayName string
	Email       *string
	SyncStatus  string // Pending until the worker has added them in Keycloak
}

// Groups lists the organization's groups for its OrgOwner and OrgAdmins.
func (s *Service) Groups(ctx context.Context, userID, idOrSlug string) ([]Group, error) {
	o, err := s.manage(ctx, userID, idOrSlug)
	if err != nil {
		return nil, err
	}
	groups, err := s.q.ListGroups(ctx, o.ID)
	if err != nil {
		return nil, err
	}
	members, err := s.q.ListGroupMembers(ctx, o.ID)
	if err != nil {
		return nil, err
	}
	byGroup := map[string][]GroupMember{}
	for _, m := range members {
		byGroup[m.GroupID] = append(byGroup[m.GroupID], GroupMember{UserID: m.UserID, DisplayName: m.DisplayName, Email: m.Email, SyncStatus: m.SyncStatus})
	}
	out := make([]Group, len(groups))
	for i, g := range groups {
		out[i] = Group{Group: g, Members: byGroup[g.ID]}
	}
	return out, nil
}

// CreateGroup adds a group (OrgOwner or OrgAdmin), within maxGroups checked
// under the entitlement's lock.
func (s *Service) CreateGroup(ctx context.Context, userID, idOrSlug, name string) (Group, error) {
	name, err := clean("name", name, maxGroupName, true)
	if err != nil {
		return Group{}, err
	}
	o, err := s.manage(ctx, userID, idOrSlug)
	if err != nil {
		return Group{}, err
	}
	var out Group
	err = store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		e, err := q.LockEntitlement(ctx, o.ID)
		if err != nil {
			return err
		}
		n, err := q.CountGroups(ctx, o.ID)
		if err != nil {
			return err
		}
		if n+1 > e.MaxGroups {
			return ErrGroupsFull
		}
		gid := id.New(id.Group)
		g, err := q.CreateGroup(ctx, store.CreateGroupParams{ID: gid, OrgID: o.ID, DisplayName: name, MachineName: MachineName(o.ID, gid)})
		if err != nil {
			return err
		}
		out = Group{Group: g, Members: []GroupMember{}}
		return audit.Record(ctx, q, audit.Event{
			Actor: userID, OrgID: o.ID, Action: audit.GroupCreate, TargetType: "Group", TargetID: g.ID,
			Detail: map[string]any{"name": name, "machineName": g.MachineName},
		})
	})
	if c, ok := store.UniqueViolation(err); ok && c == groupNameIndex {
		return Group{}, ErrGroupNameUsed
	}
	return out, err
}

// groupFor resolves a live group for an OrgOwner or OrgAdmin of its
// organization: a non-member cannot tell it exists (404), a member without
// the role gets 403.
func (s *Service) groupFor(ctx context.Context, userID, groupID string) (store.Group, error) {
	g, err := s.q.GetGroup(ctx, groupID)
	if errors.Is(err, pgx.ErrNoRows) {
		return g, ErrNoSuchGroup
	}
	if err != nil {
		return g, err
	}
	if _, err := s.manage(ctx, userID, g.OrgID); errors.Is(err, ErrNotFound) {
		return g, ErrNoSuchGroup
	} else if err != nil {
		return g, err
	}
	return g, nil
}

// RenameGroup changes the display name; the machine name stays.
func (s *Service) RenameGroup(ctx context.Context, userID, groupID, name string) (Group, error) {
	name, err := clean("name", name, maxGroupName, true)
	if err != nil {
		return Group{}, err
	}
	g, err := s.groupFor(ctx, userID, groupID)
	if err != nil {
		return Group{}, err
	}
	var out store.Group
	err = store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		cur, err := q.LockGroup(ctx, g.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSuchGroup // deleted meanwhile
		}
		if err != nil {
			return err
		}
		if cur.DisplayName == name {
			out = cur
			return nil
		}
		if out, err = q.RenameGroup(ctx, store.RenameGroupParams{ID: g.ID, DisplayName: name}); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Event{
			Actor: userID, OrgID: g.OrgID, Action: audit.GroupUpdate, TargetType: "Group", TargetID: g.ID,
			Detail: map[string]any{"before": map[string]any{"name": cur.DisplayName}, "after": map[string]any{"name": name}},
		})
	})
	if c, ok := store.UniqueViolation(err); ok && c == groupNameIndex {
		return Group{}, ErrGroupNameUsed
	}
	if err != nil {
		return Group{}, err
	}
	return s.groupWithMembers(ctx, out)
}

// DeleteGroup turns the group into a tombstone; the worker removes it from
// Keycloak, which takes its memberships with it, then deletes the rows.
func (s *Service) DeleteGroup(ctx context.Context, userID, groupID string) error {
	g, err := s.groupFor(ctx, userID, groupID)
	if err != nil {
		return err
	}
	return store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := q.LockGroup(ctx, g.ID); errors.Is(err, pgx.ErrNoRows) {
			return nil // deleted meanwhile
		} else if err != nil {
			return err
		}
		if err := q.TombstoneGroup(ctx, g.ID); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Event{
			Actor: userID, OrgID: g.OrgID, Action: audit.GroupDelete, TargetType: "Group", TargetID: g.ID,
			Detail: map[string]any{"name": g.DisplayName, "machineName": g.MachineName},
		})
	})
}

// AddToGroup puts a member of the organization in the group. Their
// membership row is held FOR SHARE meanwhile: leaving deletes it first, so
// leaving and joining a group at the same moment cannot end with a group
// member who is not an organization member.
func (s *Service) AddToGroup(ctx context.Context, actorID, groupID, userID string) error {
	g, err := s.groupFor(ctx, actorID, groupID)
	if err != nil {
		return err
	}
	return store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := q.ShareLockMembership(ctx, store.ShareLockMembershipParams{OrgID: g.OrgID, UserID: userID}); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotOrgMember
		} else if err != nil {
			return err
		}
		if _, err := q.LockGroup(ctx, g.ID); errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSuchGroup // deleted meanwhile
		} else if err != nil {
			return err
		}
		changed, err := q.JoinGroup(ctx, store.JoinGroupParams{GroupID: g.ID, UserID: userID})
		if err != nil || changed == 0 {
			return err // already a member: nothing to do
		}
		return audit.Record(ctx, q, audit.Event{
			Actor: actorID, OrgID: g.OrgID, Action: audit.GroupMemberAdd, TargetType: "Group", TargetID: g.ID,
			Detail: map[string]any{"userId": userID},
		})
	})
}

// RemoveFromGroup takes someone out of the group: portal-side at once,
// Kubernetes-side within one token lifetime once Keycloak is synced (06 §5).
// Removing someone who is not in the group changes nothing.
func (s *Service) RemoveFromGroup(ctx context.Context, actorID, groupID, userID string) error {
	g, err := s.groupFor(ctx, actorID, groupID)
	if err != nil {
		return err
	}
	return store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		changed, err := q.LeaveGroup(ctx, store.LeaveGroupParams{GroupID: g.ID, UserID: userID})
		if err != nil || changed == 0 {
			return err
		}
		return audit.Record(ctx, q, audit.Event{
			Actor: actorID, OrgID: g.OrgID, Action: audit.GroupMemberRemove, TargetType: "Group", TargetID: g.ID,
			Detail: map[string]any{"userId": userID},
		})
	})
}

func (s *Service) groupWithMembers(ctx context.Context, g store.Group) (Group, error) {
	members, err := s.q.ListGroupMembers(ctx, g.OrgID)
	if err != nil {
		return Group{}, err
	}
	out := Group{Group: g, Members: []GroupMember{}}
	for _, m := range members {
		if m.GroupID == g.ID {
			out.Members = append(out.Members, GroupMember{UserID: m.UserID, DisplayName: m.DisplayName, Email: m.Email, SyncStatus: m.SyncStatus})
		}
	}
	return out, nil
}
