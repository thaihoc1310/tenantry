package workspace

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/audit"
	"github.com/thaihoc1310/tenantry/backend/internal/id"
	"github.com/thaihoc1310/tenantry/backend/internal/reservation"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// Namespace sources and states (04-state-machines §9.2).
const (
	SourcePortal  = "portal"
	SourceKubectl = "kubectl"

	nsDesired  = "Desired"
	nsCreating = "Creating"
	nsActive   = "Active"
	nsDeleting = "Deleting"
	nsDeleted  = "Deleted"
)

const (
	maxDisplayName = 80
	maxNamespace   = 63 // a DNS label
)

var (
	slugPattern   = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	suffixPattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

	ErrClusterNotGranted = apperr.New(apperr.ClusterNotGranted, "the organization has no grant on this cluster")
	ErrNoOwnerBinding    = &apperr.Error{
		Code: apperr.LastOwnerBinding, Message: "a group must hold the TenantOwner role", Details: map[string]any{"field": "bindings"},
	}
)

// Resources is a Workspace's quota in stored units (PRODUCT-PLAN §5.4).
type Resources struct {
	RequestsCPUMilli     int64
	RequestsMemoryBytes  int64
	LimitsCPUMilli       int64 // 0: not given
	LimitsMemoryBytes    int64 // 0: not given
	Pods                 int32
	RequestsStorageBytes int64
	NamespaceQuota       int32
}

type Binding struct {
	GroupID string
	Role    Role
}

// New is a create request (PRODUCT-PLAN §16.4).
type New struct {
	Slug          string
	DisplayName   string
	ClusterID     string
	ComputeMode   string
	SharedPoolID  string
	Resources     Resources
	Namespaces    []string // suffixes
	Bindings      []Binding
	CorrelationID string // request id
}

// Created is a new Workspace and the operation provisioning it.
type Created struct {
	Workspace store.Workspace
	Operation store.Operation
}

// Create writes a Workspace in Provisioning with its bindings and
// Namespaces, a workspace.create operation and the CR to apply, in one
// transaction holding the locks of 02-data-model §4 in their order
// (PRODUCT-PLAN §17.1). OrgOwner or OrgAdmin of an Active organization.
func (s *Service) Create(ctx context.Context, userID, orgIDOrSlug string, n New) (Created, error) {
	o, err := s.manage(ctx, userID, orgIDOrSlug)
	if err != nil {
		return Created{}, err
	}
	if err := n.check(o.Slug); err != nil {
		return Created{}, err
	}

	var out Created
	err = store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		// The entitlement's lock queues every create of the organization, so
		// the count and the nesting check below cannot race.
		e, err := q.LockEntitlement(ctx, o.ID)
		if err != nil {
			return err
		}
		if n, err := q.CountWorkspaces(ctx, o.ID); err != nil {
			return err
		} else if n+1 > e.MaxWorkspacesTotal {
			return &apperr.Error{
				Code:    apperr.EntitlementExceeded,
				Message: fmt.Sprintf("the organization has %d Workspaces, as many as its entitlement allows", n),
				Details: map[string]any{"limit": "maxWorkspacesTotal"},
			}
		}
		if other, err := q.NestedWorkspaceSlug(ctx, store.NestedWorkspaceSlugParams{OrgID: o.ID, Slug: n.Slug}); err == nil {
			return &apperr.Error{
				Code:    apperr.SlugTaken,
				Message: fmt.Sprintf("the slug nests with Workspace %q of the organization", other.Slug),
				Details: map[string]any{"field": "slug", "workspaceId": other.ID},
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		cluster, err := q.ShareLockCluster(ctx, n.ClusterID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrClusterNotGranted // an unknown cluster is not granted either
		}
		if err != nil {
			return err
		}
		grant, err := q.LockClusterGrant(ctx, store.LockClusterGrantParams{OrgID: o.ID, ClusterID: n.ClusterID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrClusterNotGranted
		}
		if err != nil {
			return err
		}
		if cluster.Status != reservation.ClusterReady {
			return apperr.New(apperr.ClusterNotReady, "the cluster is not Ready")
		}
		// What the Workspace takes of the grant, under the grant's lock (§5.3).
		holds, err := reservation.Holds(ctx, q, grant.ID)
		if err != nil {
			return err
		}
		share := reservation.Share(n.ComputeMode == ComputeShared, n.Resources.NamespaceQuota, n.Resources.grant())
		if err := reservation.GrantFits(reservation.FromRow(grant, nil, nil).GrantSpec, holds, share); err != nil {
			return err
		}
		if ok, err := q.GrantHasSharedPool(ctx, store.GrantHasSharedPoolParams{GrantID: grant.ID, PoolID: n.SharedPoolID}); err != nil {
			return err
		} else if !ok {
			return apperr.Invalid("compute.sharedPoolId", "is not a shared pool of the organization's grant on this cluster")
		}

		groupIDs, err := s.lockBoundGroups(ctx, q, o.ID, n.Bindings)
		if err != nil {
			return err
		}

		pool := n.SharedPoolID
		ws, err := q.CreateWorkspace(ctx, store.CreateWorkspaceParams{
			ID: id.New(id.Workspace), OrgID: o.ID, ClusterID: n.ClusterID, ClusterGrantID: grant.ID,
			Slug: n.Slug, K8sName: o.Slug + "-" + n.Slug, DisplayName: n.DisplayName,
			RequestsCpuMilli: n.Resources.RequestsCPUMilli, RequestsMemoryBytes: n.Resources.RequestsMemoryBytes,
			LimitsCpuMilli: n.Resources.LimitsCPUMilli, LimitsMemoryBytes: n.Resources.LimitsMemoryBytes,
			Pods: n.Resources.Pods, RequestsStorageBytes: n.Resources.RequestsStorageBytes,
			NamespaceQuota: n.Resources.NamespaceQuota, ComputeMode: n.ComputeMode, SharedPoolID: &pool,
		})
		if err != nil {
			return err
		}
		for _, b := range n.Bindings {
			if err := q.CreateWorkspaceBinding(ctx, store.CreateWorkspaceBindingParams{
				WorkspaceID: ws.ID, GroupID: b.GroupID, OrgID: o.ID, Role: string(b.Role),
			}); err != nil {
				return err
			}
		}
		for _, suffix := range n.Namespaces {
			if _, err := q.CreateWorkspaceNamespace(ctx, store.CreateWorkspaceNamespaceParams{
				ID: id.New(id.Namespace), WorkspaceID: ws.ID, ClusterID: ws.ClusterID,
				Name: ws.K8sName + "-" + suffix, Source: SourcePortal, State: nsDesired,
			}); err != nil {
				return err
			}
		}
		if _, err := q.ResyncGroupMembers(ctx, groupIDs); err != nil {
			return err
		}
		op, err := newOperation(ctx, q, ws, "workspace.create", userID, n.CorrelationID)
		if err != nil {
			return err
		}
		if err := enqueueApply(ctx, q, op, ws); err != nil {
			return err
		}
		out = Created{Workspace: ws, Operation: op}
		return audit.Record(ctx, q, audit.Event{
			Actor: userID, OrgID: o.ID, Action: audit.WorkspaceCreate, TargetType: "Workspace", TargetID: ws.ID,
			Detail: map[string]any{
				"slug": ws.Slug, "clusterId": ws.ClusterID, "computeMode": ws.ComputeMode, "sharedPoolId": pool,
				"namespaces": n.Namespaces, "bindings": bindingDetail(n.Bindings), "operationId": op.ID,
			},
		})
	})
	if c, ok := store.UniqueViolation(err); ok && c == "workspaces_k8s_name_uq" {
		return Created{}, &apperr.Error{Code: apperr.SlugTaken, Message: "the slug is already in use", Details: map[string]any{"field": "slug"}}
	}
	return out, err
}

// lockBoundGroups holds the bound groups FOR SHARE and checks each is a live
// group of the organization that Keycloak already has. It returns their ids.
func (s *Service) lockBoundGroups(ctx context.Context, q *store.Queries, orgID string, bindings []Binding) ([]string, error) {
	ids := make([]string, len(bindings))
	for i, b := range bindings {
		ids[i] = b.GroupID
	}
	groups, err := q.ShareLockGroups(ctx, store.ShareLockGroupsParams{OrgID: orgID, Ids: ids})
	if err != nil {
		return nil, err
	}
	synced := map[string]bool{}
	for _, g := range groups {
		synced[g.ID] = g.SyncStatus == "Synced"
	}
	for _, gid := range ids {
		ok, found := synced[gid]
		if !found {
			return nil, &apperr.Error{
				Code: apperr.Validation, Message: fmt.Sprintf("bindings: %s is not a group of the organization", gid),
				Details: map[string]any{"field": "bindings", "groupId": gid},
			}
		}
		if !ok {
			return nil, &apperr.Error{
				Code: apperr.GroupSyncPending, Message: "the group has not reached Keycloak yet; try again in a moment",
				Details: map[string]any{"groupId": gid},
			}
		}
	}
	return ids, nil
}

// check validates a create request on its own (orgSlug: the prefix of every name).
func (n *New) check(orgSlug string) error {
	if l := len(n.Slug); l < 2 || l > 20 || !slugPattern.MatchString(n.Slug) {
		return apperr.Invalid("slug", "must be 2 to 20 lowercase letters, digits and single hyphens, starting with a letter")
	}
	n.DisplayName = strings.TrimSpace(n.DisplayName)
	if n.DisplayName == "" || utf8.RuneCountInString(n.DisplayName) > maxDisplayName {
		return apperr.Invalid("displayName", fmt.Sprintf("must be 1 to %d characters", maxDisplayName))
	}
	switch n.ComputeMode {
	case ComputeShared:
		if n.SharedPoolID == "" {
			return apperr.Invalid("compute.sharedPoolId", "is required in Shared mode")
		}
	case ComputeDedicated:
		// ponytail: Dedicated needs node reservations (M8); refused until then.
		return apperr.Invalid("compute.mode", "Dedicated Workspaces are not available yet")
	default:
		return apperr.Invalid("compute.mode", "must be Shared or Dedicated")
	}
	if err := n.Resources.check(n.ComputeMode); err != nil {
		return err
	}

	if len(n.Namespaces) > int(n.Resources.NamespaceQuota) {
		return apperr.Invalid("namespaces", "more Namespaces than namespaceQuota")
	}
	prefix := len(orgSlug) + 1 + len(n.Slug) + 1
	seen := map[string]bool{}
	for _, s := range n.Namespaces {
		if !suffixPattern.MatchString(s) || prefix+len(s) > maxNamespace {
			return apperr.Invalid("namespaces", fmt.Sprintf("%q: lowercase letters, digits and hyphens, at most %d characters here", s, maxNamespace-prefix))
		}
		if seen[s] {
			return apperr.Invalid("namespaces", fmt.Sprintf("%q twice", s))
		}
		seen[s] = true
	}

	owner := false
	groups := map[string]bool{}
	for _, b := range n.Bindings {
		switch b.Role {
		case RoleTenantOwner:
			owner = true
		case RoleDeveloper, RoleViewer:
		default:
			return apperr.Invalid("bindings", "role must be TenantOwner, Developer or Viewer")
		}
		if groups[b.GroupID] {
			return apperr.Invalid("bindings", fmt.Sprintf("group %s twice", b.GroupID))
		}
		groups[b.GroupID] = true
	}
	if !owner {
		return ErrNoOwnerBinding
	}
	return nil
}

// grant is the quota in a grant's terms.
func (r Resources) grant() reservation.Resources {
	return reservation.Resources{
		RequestsCPUMilli: r.RequestsCPUMilli, RequestsMemoryBytes: r.RequestsMemoryBytes,
		LimitsCPUMilli: r.LimitsCPUMilli, LimitsMemoryBytes: r.LimitsMemoryBytes,
		Pods: r.Pods, RequestsStorageBytes: r.RequestsStorageBytes,
	}
}

// check validates a quota and fills in Dedicated limits (= requests).
func (r *Resources) check(mode string) error {
	if r.Pods < 1 {
		return apperr.Invalid("resources.pods", "must be at least 1")
	}
	if r.NamespaceQuota < 1 {
		return apperr.Invalid("resources.namespaceQuota", "must be at least 1")
	}
	if mode == ComputeDedicated {
		// Guaranteed QoS: limits equal requests (PRODUCT-PLAN §5.4).
		if (r.LimitsCPUMilli != 0 && r.LimitsCPUMilli != r.RequestsCPUMilli) ||
			(r.LimitsMemoryBytes != 0 && r.LimitsMemoryBytes != r.RequestsMemoryBytes) {
			return apperr.Invalid("resources", "limits must equal requests in Dedicated mode")
		}
		r.LimitsCPUMilli, r.LimitsMemoryBytes = r.RequestsCPUMilli, r.RequestsMemoryBytes
		return nil
	}
	if r.LimitsCPUMilli < r.RequestsCPUMilli {
		return apperr.Invalid("resources.limitsCpu", "must be at least requestsCpu")
	}
	if r.LimitsMemoryBytes < r.RequestsMemoryBytes {
		return apperr.Invalid("resources.limitsMemory", "must be at least requestsMemory")
	}
	return nil
}

func bindingDetail(bs []Binding) []map[string]string {
	out := make([]map[string]string, len(bs))
	for i, b := range bs {
		out[i] = map[string]string{"groupId": b.GroupID, "role": string(b.Role)}
	}
	return out
}
