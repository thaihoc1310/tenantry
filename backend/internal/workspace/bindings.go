package workspace

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/audit"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

var (
	ErrNotReady  = apperr.New(apperr.WorkspaceNotReady, "the Workspace is busy with another change or not Ready; try again once it is")
	ErrLastOwner = &apperr.Error{
		Code: apperr.LastOwnerBinding, Message: "this group is the Workspace's last TenantOwner; give another group the role first",
	}
	ErrNotAGroup = apperr.New(apperr.NotFound, "group not found")
)

// BindingView is a binding with its group's name.
type BindingView struct {
	GroupID, GroupName string
	Role               Role
}

// BindingChange is the bindings after a change and the operation carrying
// it to the cluster; Operation is nil when nothing changed.
type BindingChange struct {
	Bindings  []BindingView
	Operation *store.Operation
}

// Bindings lists the groups bound to a Workspace, for anyone who sees it.
func (s *Service) Bindings(ctx context.Context, userID, wsID string) ([]BindingView, error) {
	if _, _, err := s.access(ctx, userID, wsID); err != nil {
		return nil, err
	}
	return bindingViews(ctx, s.q, wsID)
}

// PutBinding gives a group of the organization a role in the Workspace
// (OrgOwner or OrgAdmin, PRODUCT-PLAN §17.2).
func (s *Service) PutBinding(ctx context.Context, userID, wsID, groupID string, role Role, correlationID string) (BindingChange, error) {
	if role != RoleTenantOwner && role != RoleDeveloper && role != RoleViewer {
		return BindingChange{}, apperr.Invalid("role", "must be TenantOwner, Developer or Viewer")
	}
	return s.changeBinding(ctx, userID, wsID, groupID, &role, correlationID)
}

// DeleteBinding takes a group's role in the Workspace away (OrgOwner or OrgAdmin).
func (s *Service) DeleteBinding(ctx context.Context, userID, wsID, groupID, correlationID string) (BindingChange, error) {
	return s.changeBinding(ctx, userID, wsID, groupID, nil, correlationID)
}

// changeBinding sets the group's role (nil: none) under the Workspace's lock,
// then the group's (FOR SHARE: a group being bound is not deleted half-way).
func (s *Service) changeBinding(ctx context.Context, userID, wsID, groupID string, role *Role, correlationID string) (BindingChange, error) {
	_, a, err := s.access(ctx, userID, wsID)
	if err != nil {
		return BindingChange{}, err
	}
	if !a.Manages() {
		return BindingChange{}, ErrForbidden
	}

	var out BindingChange
	err = store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		ws, err := lockForChange(ctx, q, wsID)
		if err != nil {
			return err
		}
		current, err := bindingViews(ctx, q, ws.ID)
		if err != nil {
			return err
		}
		var before *Role
		owners := 0
		for _, b := range current {
			if b.GroupID == groupID {
				before = &b.Role
			}
			if b.Role == RoleTenantOwner {
				owners++
			}
		}
		if equalRole(before, role) {
			out.Bindings = current
			return nil
		}
		// The last TenantOwner can neither leave nor change role (04 §1.1).
		if before != nil && *before == RoleTenantOwner && owners == 1 {
			return ErrLastOwner
		}

		if role != nil {
			if _, err := s.lockBoundGroups(ctx, q, ws.OrgID, []Binding{{GroupID: groupID, Role: *role}}); err != nil {
				var e *apperr.Error
				if errors.As(err, &e) && e.Code == apperr.Validation {
					return ErrNotAGroup // the path names it: not a group of this organization
				}
				return err
			}
			if err := q.UpsertWorkspaceBinding(ctx, store.UpsertWorkspaceBindingParams{
				WorkspaceID: ws.ID, GroupID: groupID, OrgID: ws.OrgID, Role: string(*role),
			}); err != nil {
				return err
			}
		} else if err := q.DeleteWorkspaceBinding(ctx, store.DeleteWorkspaceBindingParams{WorkspaceID: ws.ID, GroupID: groupID}); err != nil {
			return err
		}
		if _, err := q.ResyncGroupMembers(ctx, []string{groupID}); err != nil {
			return err
		}
		op, err := beginUpdate(ctx, q, ws, userID, correlationID)
		if err != nil {
			return err
		}
		if out.Bindings, err = bindingViews(ctx, q, ws.ID); err != nil {
			return err
		}
		out.Operation = &op
		action, detail := audit.WorkspaceBindingUpdate, map[string]any{"groupId": groupID, "before": roleOrNil(before), "operationId": op.ID}
		if role == nil {
			action = audit.WorkspaceBindingDelete
		} else {
			detail["after"] = string(*role)
		}
		return audit.Record(ctx, q, audit.Event{
			Actor: userID, OrgID: ws.OrgID, Action: action, TargetType: "Workspace", TargetID: ws.ID, Detail: detail,
		})
	})
	return out, err
}

// lockForChange locks a Workspace that may take a change that needs a new
// CR: Ready or Degraded, with no operation under way (04 §1.1).
func lockForChange(ctx context.Context, q *store.Queries, wsID string) (store.Workspace, error) {
	ws, err := q.LockWorkspace(ctx, wsID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ws, ErrNotFound
	}
	if err != nil {
		return ws, err
	}
	if ws.State != StateReady && ws.State != StateDegraded {
		return ws, ErrNotReady
	}
	if _, err := q.ActiveWorkspaceOperation(ctx, ws.ID); err == nil {
		return ws, ErrNotReady
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return ws, err
	}
	return ws, nil
}

// beginUpdate moves a locked Workspace to its next spec version, in
// Updating, and enqueues the new CR under a workspace.update operation.
func beginUpdate(ctx context.Context, q *store.Queries, ws store.Workspace, actor, correlationID string) (store.Operation, error) {
	ws, err := q.BeginWorkspaceUpdate(ctx, ws.ID)
	if err != nil {
		return store.Operation{}, err
	}
	op, err := newOperation(ctx, q, ws, "workspace.update", actor, correlationID)
	if err != nil {
		return op, err
	}
	return op, enqueueApply(ctx, q, op, ws)
}

func bindingViews(ctx context.Context, q *store.Queries, wsID string) ([]BindingView, error) {
	rows, err := q.ListWorkspaceBindings(ctx, wsID)
	if err != nil {
		return nil, err
	}
	out := make([]BindingView, len(rows))
	for i, r := range rows {
		out[i] = BindingView{GroupID: r.GroupID, GroupName: r.DisplayName, Role: Role(r.Role)}
	}
	return out, nil
}

func equalRole(a, b *Role) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

func roleOrNil(r *Role) any {
	if r == nil {
		return nil
	}
	return string(*r)
}
