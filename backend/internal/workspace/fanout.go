package workspace

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// RenderGrant writes again the CR of every Workspace of a grant whose
// storage classes changed: they are rendered into each Tenant
// (PRODUCT-PLAN §5.5). It runs in the grant change's transaction, which
// holds the grant's lock; each Workspace is locked after it (02 §4).
//
// A Ready or Degraded Workspace gets a workspace.update operation of its
// own. One with an operation under way gets the new CR under that
// operation, which then ends with it; a Cordoned one gets a
// workspace.update that leaves it Cordoned. A Failed one waits for its
// retry, which renders the CR from the database again; Deleting and
// Deleted ones are left alone.
func RenderGrant(ctx context.Context, q *store.Queries, grantID, actor, correlationID string) error {
	ids, err := q.WorkspacesOfGrant(ctx, grantID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		ws, err := q.LockWorkspace(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		switch ws.State {
		case StateFailed, StateDeleting, StateDeleted:
			continue
		}
		active, err := q.ActiveWorkspaceOperation(ctx, ws.ID)
		switch {
		case err == nil:
			if ws, err = q.BumpWorkspaceVersion(ctx, ws.ID); err != nil {
				return err
			}
			op, err := q.GetOperation(ctx, active.ID)
			if err != nil {
				return err
			}
			if err := enqueueApply(ctx, q, op, ws); err != nil {
				return err
			}
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		case ws.State == StateReady || ws.State == StateDegraded:
			if _, err := beginUpdate(ctx, q, ws, "workspace.update", actor, correlationID); err != nil {
				return err
			}
		default: // Cordoned
			if ws, err = q.BumpWorkspaceVersion(ctx, ws.ID); err != nil {
				return err
			}
			op, err := newOperation(ctx, q, ws, "workspace.update", actor, correlationID)
			if err != nil {
				return err
			}
			if err := enqueueApply(ctx, q, op, ws); err != nil {
				return err
			}
		}
	}
	return nil
}
