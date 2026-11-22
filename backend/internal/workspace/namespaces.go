package workspace

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/audit"
	"github.com/thaihoc1310/tenantry/backend/internal/id"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// Namespaces of a Workspace are declared through its CR (PRODUCT-PLAN §4.5):
// creating one adds its suffix to spec.namespaces, deleting one moves its
// full name to spec.namespacesToDelete; the controller does the rest and the
// worker records what it observed (04-state-machines §9.2).

var (
	ErrNoNamespace     = apperr.New(apperr.NotFound, "namespace not found")
	ErrNotTenantOwner  = apperr.New(apperr.Forbidden, "requires OrgOwner, OrgAdmin or a group holding TenantOwner in the Workspace")
	errNamespaceExists = &apperr.Error{Code: apperr.Conflict, Message: "a Namespace with this name exists", Details: map[string]any{"field": "name"}}
)

// NamespaceChange is a Namespace row and the operation carrying it to the cluster.
type NamespaceChange struct {
	Namespace store.WorkspaceNamespace
	Operation store.Operation
}

// Namespaces lists a Workspace's Namespaces, however created, for anyone who sees it.
func (s *Service) Namespaces(ctx context.Context, userID, wsID string) ([]store.WorkspaceNamespace, error) {
	if _, _, err := s.access(ctx, userID, wsID); err != nil {
		return nil, err
	}
	return s.q.ListWorkspaceNamespaces(ctx, wsID)
}

// CreateNamespace asks for Namespace <k8s_name>-<suffix>, within the
// Workspace's namespaceQuota counting every Namespace it has.
func (s *Service) CreateNamespace(ctx context.Context, userID, wsID, suffix, correlationID string) (NamespaceChange, error) {
	ws, err := s.tenantOwner(ctx, userID, wsID)
	if err != nil {
		return NamespaceChange{}, err
	}
	name := ws.K8sName + "-" + suffix
	if !suffixPattern.MatchString(suffix) || len(name) > maxNamespace {
		return NamespaceChange{}, apperr.Invalid("name", fmt.Sprintf("lowercase letters, digits and hyphens, at most %d characters here", maxNamespace-len(ws.K8sName)-1))
	}
	var out NamespaceChange
	err = store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		ws, err := lockForChange(ctx, q, wsID)
		if err != nil {
			return err
		}
		n, err := q.CountWorkspaceNamespaces(ctx, ws.ID)
		if err != nil {
			return err
		}
		if n >= ws.NamespaceQuota {
			return &apperr.Error{
				Code:    apperr.Conflict,
				Message: fmt.Sprintf("the Workspace has %d Namespaces, as many as its namespaceQuota allows", n),
				Details: map[string]any{"limit": "namespaceQuota", "allocated": n},
			}
		}
		ns, err := q.CreateWorkspaceNamespace(ctx, store.CreateWorkspaceNamespaceParams{
			ID: id.New(id.Namespace), WorkspaceID: ws.ID, ClusterID: ws.ClusterID, Name: name, Source: SourcePortal, State: nsDesired,
		})
		if err != nil {
			return err
		}
		op, err := beginUpdate(ctx, q, ws, "namespace.create", userID, correlationID)
		if err != nil {
			return err
		}
		out = NamespaceChange{Namespace: ns, Operation: op}
		return audit.Record(ctx, q, audit.Event{
			Actor: userID, OrgID: ws.OrgID, Action: audit.NamespaceCreate, TargetType: "Workspace", TargetID: ws.ID,
			Detail: map[string]any{"name": name, "operationId": op.ID},
		})
	})
	if c, ok := store.UniqueViolation(err); ok && c == "workspace_namespaces_name_uq" {
		return NamespaceChange{}, errNamespaceExists
	}
	return out, err
}

// DeleteNamespace deletes a Namespace of the Workspace by its full name,
// whoever created it: the controller deletes it with everything inside.
func (s *Service) DeleteNamespace(ctx context.Context, userID, wsID, name, correlationID string) (NamespaceChange, error) {
	if _, err := s.tenantOwner(ctx, userID, wsID); err != nil {
		return NamespaceChange{}, err
	}
	var out NamespaceChange
	err := store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		ws, err := lockForChange(ctx, q, wsID)
		if err != nil {
			return err
		}
		ns, err := q.GetWorkspaceNamespace(ctx, store.GetWorkspaceNamespaceParams{WorkspaceID: ws.ID, Name: name})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoNamespace
		}
		if err != nil {
			return err
		}
		if ns, err = q.SetNamespaceState(ctx, store.SetNamespaceStateParams{ID: ns.ID, State: nsDeleting}); err != nil {
			return err
		}
		op, err := beginUpdate(ctx, q, ws, "namespace.delete", userID, correlationID)
		if err != nil {
			return err
		}
		out = NamespaceChange{Namespace: ns, Operation: op}
		return audit.Record(ctx, q, audit.Event{
			Actor: userID, OrgID: ws.OrgID, Action: audit.NamespaceDelete, TargetType: "Workspace", TargetID: ws.ID,
			Detail: map[string]any{"name": name, "source": ns.Source, "operationId": op.ID},
		})
	})
	return out, err
}

// tenantOwner resolves a Workspace for someone who may create and delete
// its Namespaces; Developers and Viewers see them only (403).
func (s *Service) tenantOwner(ctx context.Context, userID, wsID string) (store.Workspace, error) {
	ws, a, err := s.access(ctx, userID, wsID)
	if err != nil {
		return ws, err
	}
	if !a.OwnsTenant() {
		return ws, ErrNotTenantOwner
	}
	return ws, nil
}
