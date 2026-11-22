package workspace

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/thaihoc1310/tenantry/backend/internal/audit"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// Cordon freezes a Workspace's Tenant: tenants can change nothing, what runs
// keeps running (PRODUCT-PLAN §17.3). OrgOwner or OrgAdmin, from Ready or
// Degraded. The Workspace keeps its state until the operation succeeds, then
// is Cordoned.
func (s *Service) Cordon(ctx context.Context, userID, wsID, correlationID string) (Changed, error) {
	return s.setCordoned(ctx, userID, wsID, true, correlationID)
}

// Uncordon lets a Cordoned Workspace's tenants change things again.
func (s *Service) Uncordon(ctx context.Context, userID, wsID, correlationID string) (Changed, error) {
	return s.setCordoned(ctx, userID, wsID, false, correlationID)
}

func (s *Service) setCordoned(ctx context.Context, userID, wsID string, cordon bool, correlationID string) (Changed, error) {
	_, a, err := s.access(ctx, userID, wsID)
	if err != nil {
		return Changed{}, err
	}
	if !a.Manages() {
		return Changed{}, ErrForbidden
	}
	var out Changed
	err = store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		ws, err := lockIn(ctx, q, wsID, cordonFrom(cordon)...)
		if err != nil {
			return err
		}
		if ws.Cordoned == cordon {
			out.Workspace = ws
			return nil
		}
		ws, err = q.SetWorkspaceCordoned(ctx, store.SetWorkspaceCordonedParams{ID: ws.ID, Cordoned: cordon})
		if err != nil {
			return err
		}
		typ, action := "workspace.uncordon", audit.WorkspaceUncordon
		if cordon {
			typ, action = "workspace.cordon", audit.WorkspaceCordon
		}
		op, err := newOperation(ctx, q, ws, typ, userID, correlationID)
		if err != nil {
			return err
		}
		if err := enqueueApply(ctx, q, op, ws); err != nil {
			return err
		}
		out = Changed{Workspace: ws, Operation: &op}
		return audit.Record(ctx, q, audit.Event{
			Actor: userID, OrgID: ws.OrgID, Action: action, TargetType: "Workspace", TargetID: ws.ID,
			Detail: map[string]any{"operationId": op.ID},
		})
	})
	return out, err
}

// cordonFrom is the states a Workspace may be cordoned or uncordoned from (04 §1.1).
func cordonFrom(cordon bool) []string {
	if cordon {
		return []string{StateReady, StateDegraded}
	}
	return []string{StateCordoned}
}
