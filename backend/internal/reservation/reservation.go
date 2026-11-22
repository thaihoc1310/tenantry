// Package reservation owns what an organization may reserve on a cluster:
// the cluster registry, cluster grants and the shared pools they list
// (IMPLEMENTATION-PLAN §2). M3 creates a grant when an organization is
// approved; editing grants and the capacity math come in M5 and M7.
//
// The functions that take a *store.Queries run inside the caller's
// transaction.
package reservation

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/id"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

const ClusterReady = "Ready"

var ErrClusterNotReady = apperr.New(apperr.ClusterNotReady, "the cluster is not Ready, so it cannot be granted now")

// Resources are a grant's quantities in stored units. CPU, memory and pods
// bound Shared Workspaces only; storage bounds every Workspace (PRODUCT-PLAN §5.2).
// The JSON names are for the audit trail.
type Resources struct {
	RequestsCPUMilli     int64 `json:"requestsCpuMilli"`
	RequestsMemoryBytes  int64 `json:"requestsMemoryBytes"`
	LimitsCPUMilli       int64 `json:"limitsCpuMilli"`
	LimitsMemoryBytes    int64 `json:"limitsMemoryBytes"`
	Pods                 int32 `json:"pods"`
	RequestsStorageBytes int64 `json:"requestsStorageBytes"`
}

// GrantSpec is what a grant gives an organization on one cluster.
type GrantSpec struct {
	ClusterID             string    `json:"clusterId"`
	MaxWorkspaces         int32     `json:"maxWorkspaces"`
	MaxNamespacesTotal    int32     `json:"maxNamespacesTotal"`
	Resources             Resources `json:"resources"`
	SharedPoolIDs         []string  `json:"sharedPoolIds"`
	NetworkPoolIDs        []string  `json:"networkPoolIds"`
	AllowedStorageClasses []string  `json:"allowedStorageClasses"`
}

type Grant struct {
	ID        string `json:"id"`
	OrgID     string `json:"orgId"`
	IsDefault bool   `json:"isDefault"`
	GrantSpec
}

// storageClassName is a Kubernetes object name (DNS subdomain).
var storageClassName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)

// check validates what needs no database. Field names are the API's.
func (g GrantSpec) check() error {
	r := g.Resources
	for _, c := range []struct {
		field string
		bad   bool
	}{
		{"maxWorkspaces", g.MaxWorkspaces < 0},
		{"maxNamespacesTotal", g.MaxNamespacesTotal < 0},
		{"requestsCpu", r.RequestsCPUMilli < 0},
		{"requestsMemory", r.RequestsMemoryBytes < 0},
		{"pods", r.Pods < 0},
		{"requestsStorage", r.RequestsStorageBytes < 0},
	} {
		if c.bad {
			return apperr.Invalid(c.field, "must not be negative")
		}
	}
	if r.LimitsCPUMilli < r.RequestsCPUMilli {
		return apperr.Invalid("limitsCpu", "must be at least requestsCpu")
	}
	if r.LimitsMemoryBytes < r.RequestsMemoryBytes {
		return apperr.Invalid("limitsMemory", "must be at least requestsMemory")
	}
	// Every Workspace takes its CIDR from one of the grant's network pools.
	if len(g.NetworkPoolIDs) == 0 {
		return apperr.Invalid("networkPoolIds", "must list at least one network pool")
	}
	for field, ids := range map[string][]string{
		"sharedPoolIds": g.SharedPoolIDs, "networkPoolIds": g.NetworkPoolIDs, "allowedStorageClasses": g.AllowedStorageClasses,
	} {
		if dup := duplicate(ids); dup != "" {
			return apperr.Invalid(field, fmt.Sprintf("lists %q twice", dup))
		}
	}
	for _, sc := range g.AllowedStorageClasses {
		if len(sc) > 253 || !storageClassName.MatchString(sc) {
			return apperr.Invalid("allowedStorageClasses", fmt.Sprintf("%q is not a StorageClass name", sc))
		}
	}
	return nil
}

func duplicate(ids []string) string {
	s := slices.Clone(ids)
	slices.Sort(s)
	for i := 1; i < len(s); i++ {
		if s[i] == s[i-1] {
			return s[i]
		}
	}
	return ""
}

// CreateGrant gives orgID the grant g. The cluster must be Ready; every pool
// must be on that cluster, and a network pool must be shared or orgID's own
// (02-data-model §3).
func CreateGrant(ctx context.Context, q *store.Queries, orgID string, isDefault bool, g GrantSpec) (Grant, error) {
	if err := g.check(); err != nil {
		return Grant{}, err
	}
	cluster, err := q.ShareLockCluster(ctx, g.ClusterID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Grant{}, apperr.Invalid("clusterId", fmt.Sprintf("no cluster %q", g.ClusterID))
	}
	if err != nil {
		return Grant{}, err
	}
	if cluster.Status != ClusterReady {
		return Grant{}, ErrClusterNotReady
	}

	shared, err := q.GetSharedPools(ctx, g.SharedPoolIDs)
	if err != nil {
		return Grant{}, err
	}
	sharedCluster := map[string]string{}
	for _, p := range shared {
		sharedCluster[p.ID] = p.ClusterID
	}
	for _, pid := range g.SharedPoolIDs {
		if c, ok := sharedCluster[pid]; !ok || c != g.ClusterID {
			return Grant{}, apperr.Invalid("sharedPoolIds", fmt.Sprintf("no shared pool %q on cluster %s", pid, g.ClusterID))
		}
	}
	network, err := q.GetNetworkPools(ctx, g.NetworkPoolIDs)
	if err != nil {
		return Grant{}, err
	}
	usable := map[string]bool{}
	for _, p := range network {
		usable[p.ID] = p.ClusterID == g.ClusterID && (p.OrgID == nil || *p.OrgID == orgID)
	}
	for _, pid := range g.NetworkPoolIDs {
		if !usable[pid] {
			return Grant{}, apperr.Invalid("networkPoolIds",
				fmt.Sprintf("no network pool %q on cluster %s that this organization may use", pid, g.ClusterID))
		}
	}

	r := g.Resources
	row, err := q.CreateClusterGrant(ctx, store.CreateClusterGrantParams{
		ID: id.New(id.ClusterGrant), OrgID: orgID, ClusterID: g.ClusterID, IsDefault: isDefault,
		MaxWorkspaces: g.MaxWorkspaces, MaxNamespacesTotal: g.MaxNamespacesTotal,
		RequestsCpuMilli: r.RequestsCPUMilli, RequestsMemoryBytes: r.RequestsMemoryBytes,
		LimitsCpuMilli: r.LimitsCPUMilli, LimitsMemoryBytes: r.LimitsMemoryBytes,
		Pods: r.Pods, RequestsStorageBytes: r.RequestsStorageBytes,
		AllowedStorageClasses: nonNil(g.AllowedStorageClasses),
	})
	if err != nil {
		return Grant{}, err
	}
	if err := q.AddGrantSharedPools(ctx, store.AddGrantSharedPoolsParams{GrantID: row.ID, PoolIds: nonNil(g.SharedPoolIDs)}); err != nil {
		return Grant{}, err
	}
	if err := q.AddGrantNetworkPools(ctx, store.AddGrantNetworkPoolsParams{GrantID: row.ID, PoolIds: g.NetworkPoolIDs}); err != nil {
		return Grant{}, err
	}
	g.SharedPoolIDs, g.AllowedStorageClasses = nonNil(g.SharedPoolIDs), nonNil(g.AllowedStorageClasses)
	return Grant{ID: row.ID, OrgID: orgID, IsDefault: isDefault, GrantSpec: g}, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// Policy is the active onboarding policy (PRODUCT-PLAN §5.2.1): what an
// approval proposes. The templates are JSON documents in the API's shape,
// passed to the Admin as they are; approving checks what comes back.
type Policy struct {
	DefaultClusterID    string
	EntitlementTemplate []byte
	GrantTemplate       []byte
}

// ActivePolicy returns the active onboarding policy; ok is false when there is none.
func ActivePolicy(ctx context.Context, q *store.Queries) (p Policy, ok bool, err error) {
	row, err := q.GetActiveOnboardingPolicy(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return Policy{}, false, nil
	}
	if err != nil {
		return Policy{}, false, err
	}
	return Policy{DefaultClusterID: row.DefaultClusterID, EntitlementTemplate: row.EntitlementTemplate, GrantTemplate: row.GrantTemplate}, true, nil
}

// Grants returns orgID's grants, the default first.
func Grants(ctx context.Context, q *store.Queries, orgID string) ([]Grant, error) {
	rows, err := q.ListClusterGrants(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]Grant, 0, len(rows))
	for _, r := range rows {
		g := r.OrganizationClusterGrant
		out = append(out, Grant{ID: g.ID, OrgID: g.OrgID, IsDefault: g.IsDefault, GrantSpec: GrantSpec{
			ClusterID: g.ClusterID, MaxWorkspaces: g.MaxWorkspaces, MaxNamespacesTotal: g.MaxNamespacesTotal,
			Resources: Resources{
				RequestsCPUMilli: g.RequestsCpuMilli, RequestsMemoryBytes: g.RequestsMemoryBytes,
				LimitsCPUMilli: g.LimitsCpuMilli, LimitsMemoryBytes: g.LimitsMemoryBytes,
				Pods: g.Pods, RequestsStorageBytes: g.RequestsStorageBytes,
			},
			SharedPoolIDs: nonNil(r.SharedPoolIds), NetworkPoolIDs: nonNil(r.NetworkPoolIds),
			AllowedStorageClasses: nonNil(g.AllowedStorageClasses),
		}})
	}
	return out, nil
}

// Allocation is what an organization's Workspaces hold of one grant, in the
// grant's scopes (PRODUCT-PLAN §5.2): CPU, memory and pods count Shared
// Workspaces only.
type Allocation struct {
	GrantID    string
	Workspaces int32
	Namespaces int32
	Resources  Resources
}

// Allocations returns, for each grant, what its Workspaces hold.
//
// ponytail: zero until the workspaces table exists (M7); then the sums go here.
func Allocations(grants []Grant) []Allocation {
	out := make([]Allocation, len(grants))
	for i, g := range grants {
		out[i] = Allocation{GrantID: g.ID}
	}
	return out
}
