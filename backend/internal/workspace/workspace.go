// Package workspace holds the Workspace rules (PRODUCT-PLAN §4.4, §17): who
// may see or change a Workspace and what a valid change is. Every change is
// written with an operation and an outbox event carrying the whole
// PlatformWorkspace the cluster should hold (cr.go), in one transaction.
//
// M6 checks the grant, the cluster and the bindings; the reservation sums
// (entitlement, grant, pool, node) come in M7 and M8.
package workspace

import (
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/org"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// States (architecture/04-state-machines §1).
const (
	StateProvisioning = "Provisioning"
	StateReady        = "Ready"
	StateUpdating     = "Updating"
	StateCordoned     = "Cordoned"
	StateDegraded     = "Degraded"
	StateFailed       = "Failed"
	StateDeleting     = "Deleting"
	StateDeleted      = "Deleted"
)

// Roles a group holds in a Workspace (PRODUCT-PLAN §10.3).
type Role string

const (
	RoleTenantOwner Role = "TenantOwner"
	RoleDeveloper   Role = "Developer"
	RoleViewer      Role = "Viewer"
)

const (
	ComputeShared    = "Shared"
	ComputeDedicated = "Dedicated"
)

var (
	// ErrNotFound covers "does not exist" and "you may not see it" alike.
	ErrNotFound  = apperr.New(apperr.NotFound, "workspace not found")
	ErrForbidden = apperr.New(apperr.Forbidden, "requires OrgOwner or OrgAdmin")
)

type Service struct {
	db   *pgxpool.Pool
	q    *store.Queries
	orgs *org.Service
}

// NewService also has orgs render a grant's Workspaces again when its
// storage classes change (RenderGrant).
func NewService(db *pgxpool.Pool, orgs *org.Service) *Service {
	orgs.StorageClassesChanged = func(ctx context.Context, q *store.Queries, grantID, actor string) error {
		return RenderGrant(ctx, q, grantID, actor, "")
	}
	return &Service{db: db, q: store.New(db), orgs: orgs}
}

// Access is what the caller may do with one Workspace.
type Access struct {
	Org   org.Org // the organization, with the caller's role in it
	Roles []Role  // roles the caller holds in the Workspace through groups
}

// Manages: the OrgOwner and OrgAdmins manage every Workspace (architecture/06 §8).
func (a Access) Manages() bool { return a.Org.Role == org.RoleOwner || a.Org.Role == org.RoleAdmin }

// OwnsTenant: managers, and members of a group holding TenantOwner, create
// and delete Namespaces and Routes (PRODUCT-PLAN §4.5).
func (a Access) OwnsTenant() bool { return a.Manages() || slices.Contains(a.Roles, RoleTenantOwner) }

// access resolves a Workspace for userID from the row, never from the client
// (IMPLEMENTATION-PLAN §3.1). Someone who may not see it gets ErrNotFound:
// non-members, and members without a binding to it.
func (s *Service) access(ctx context.Context, userID, wsID string) (store.Workspace, Access, error) {
	ws, err := s.q.GetWorkspace(ctx, wsID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ws, Access{}, ErrNotFound
	}
	if err != nil {
		return ws, Access{}, err
	}
	o, err := s.orgs.Get(ctx, userID, ws.OrgID)
	if errors.Is(err, org.ErrNotFound) {
		return ws, Access{}, ErrNotFound
	}
	if err != nil {
		return ws, Access{}, err
	}
	roles, err := s.q.WorkspaceRolesOfUser(ctx, store.WorkspaceRolesOfUserParams{WorkspaceID: ws.ID, UserID: userID})
	if err != nil {
		return ws, Access{}, err
	}
	a := Access{Org: o}
	for _, r := range roles {
		a.Roles = append(a.Roles, Role(r))
	}
	if !a.Manages() && len(a.Roles) == 0 {
		return ws, Access{}, ErrNotFound
	}
	return ws, a, nil
}

// Get returns a Workspace to someone who may see it, with what they may do.
func (s *Service) Get(ctx context.Context, userID, wsID string) (store.Workspace, Access, error) {
	return s.access(ctx, userID, wsID)
}

// List returns the organization's Workspaces the caller may see: all of them
// for the OrgOwner and OrgAdmins, those their groups are bound to otherwise.
func (s *Service) List(ctx context.Context, userID, orgIDOrSlug string) ([]store.Workspace, error) {
	o, err := s.orgs.Get(ctx, userID, orgIDOrSlug)
	if err != nil {
		return nil, err
	}
	if (Access{Org: o}).Manages() {
		return s.q.ListWorkspaces(ctx, o.ID)
	}
	return s.q.ListWorkspacesOfMember(ctx, store.ListWorkspacesOfMemberParams{OrgID: o.ID, UserID: userID})
}

// manage resolves an organization for a change only its OrgOwner and
// OrgAdmins make, in the order 404 → 403 → 409 (PRODUCT-PLAN §9.2).
func (s *Service) manage(ctx context.Context, userID, orgIDOrSlug string) (org.Org, error) {
	o, err := s.orgs.Get(ctx, userID, orgIDOrSlug)
	if err != nil {
		return o, err
	}
	if !(Access{Org: o}).Manages() {
		return o, ErrForbidden
	}
	if o.Status != org.StatusActive {
		return o, org.ErrNotActive
	}
	return o, nil
}
