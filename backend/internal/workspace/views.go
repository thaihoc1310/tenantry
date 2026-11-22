package workspace

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// Usage is what a Workspace's Pods use of its quota, as the controller last
// reported it, and per Namespace (IMPLEMENTATION-PLAN M7 step 9).
type Usage struct {
	Workspace  store.Workspace
	Namespaces []store.WorkspaceNamespace
}

// Usage is read by anyone who sees the Workspace.
func (s *Service) Usage(ctx context.Context, userID, wsID string) (Usage, error) {
	ws, _, err := s.access(ctx, userID, wsID)
	if err != nil {
		return Usage{}, err
	}
	nss, err := s.q.ListWorkspaceNamespaces(ctx, ws.ID)
	return Usage{Workspace: ws, Namespaces: nss}, err
}

// OperationPage is one page of a Workspace's operations, newest first.
type OperationPage struct {
	Items      []store.Operation
	NextCursor string
}

// Operations lists a Workspace's operations for anyone who sees it.
func (s *Service) Operations(ctx context.Context, userID, wsID, cursor string, pageSize int) (OperationPage, error) {
	if pageSize == 0 {
		pageSize = 20
	}
	if pageSize < 1 || pageSize > 100 {
		return OperationPage{}, apperr.Invalid("limit", "must be between 1 and 100")
	}
	if _, _, err := s.access(ctx, userID, wsID); err != nil {
		return OperationPage{}, err
	}
	arg := store.ListWorkspaceOperationsParams{WorkspaceID: wsID, PageSize: int32(pageSize + 1)}
	if cursor != "" {
		arg.Cursor = &cursor
	}
	rows, err := s.q.ListWorkspaceOperations(ctx, arg)
	if err != nil {
		return OperationPage{}, err
	}
	p := OperationPage{Items: rows}
	if len(rows) > pageSize {
		p.Items = rows[:pageSize]
		p.NextCursor = p.Items[pageSize-1].ID
	}
	return p, nil
}

// NodeGrantOf is the node grant a Dedicated Workspace reserves on; "" for a
// Shared one.
func (s *Service) NodeGrantOf(ctx context.Context, ws store.Workspace) (string, error) {
	if ws.ComputeMode != ComputeDedicated {
		return "", nil
	}
	// In the request's transaction: a create answers before it commits.
	var grant string
	err := store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		r, err := s.q.WithTx(tx).GetNodeReservation(ctx, ws.ID)
		grant = r.NodeGrantID
		return err
	})
	return grant, err
}
