package reservation

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// What Workspaces hold of a grant, checked against it (PRODUCT-PLAN §5.3,
// §5.5): a create or resize must fit in what is left; an Admin's change may
// not take the grant below what is held.

// dimension is one number of a grant, named as the API names it.
type dimension struct {
	resource     string
	limit, holds int64
	format       func(int64) string
	sharedOnly   bool // CPU, memory and pods bound Shared Workspaces only
}

func cpuQ(m int64) string   { return resource.NewMilliQuantity(m, resource.DecimalSI).String() }
func bytesQ(b int64) string { return resource.NewQuantity(b, resource.BinarySI).String() }
func count(n int64) string  { return fmt.Sprint(n) }

func dimensions(g GrantSpec, a Allocation) []dimension {
	r, h := g.Resources, a.Resources
	return []dimension{
		{"workspaces", int64(g.MaxWorkspaces), int64(a.Workspaces), count, false},
		{"namespaces", int64(g.MaxNamespacesTotal), int64(a.Namespaces), count, false},
		{"requestsCpu", r.RequestsCPUMilli, h.RequestsCPUMilli, cpuQ, true},
		{"requestsMemory", r.RequestsMemoryBytes, h.RequestsMemoryBytes, bytesQ, true},
		{"limitsCpu", r.LimitsCPUMilli, h.LimitsCPUMilli, cpuQ, true},
		{"limitsMemory", r.LimitsMemoryBytes, h.LimitsMemoryBytes, bytesQ, true},
		{"pods", int64(r.Pods), int64(h.Pods), count, true},
		{"requestsStorage", r.RequestsStorageBytes, h.RequestsStorageBytes, bytesQ, false},
	}
}

// GrantFits checks that more fits in grant g next to what Workspaces hold
// (holds). more is in the grant's scopes: its CPU, memory and pods are zero
// for a Dedicated Workspace. The first number short is reported as
// CLUSTER_GRANT_QUOTA_EXCEEDED with details.resource, limit, allocated and
// requested.
func GrantFits(g GrantSpec, holds, more Allocation) error {
	have, want := dimensions(g, holds), dimensions(g, more)
	for i, d := range have {
		add := want[i].holds
		if add > 0 && d.holds+add > d.limit {
			return &apperr.Error{
				Code: apperr.ClusterGrantQuotaExceeded,
				Message: fmt.Sprintf("the grant on %s allows %s %s; Workspaces hold %s and this asks %s more",
					g.ClusterID, d.format(d.limit), d.resource, d.format(d.holds), d.format(add)),
				Details: map[string]any{
					"resource": d.resource, "limit": d.format(d.limit), "allocated": d.format(d.holds), "requested": d.format(add),
				},
			}
		}
	}
	return nil
}

// Holds is what the Workspaces not Deleted hold of grant g.
func Holds(ctx context.Context, q *store.Queries, grantID string) (Allocation, error) {
	a, err := Allocations(ctx, q, []Grant{{ID: grantID}})
	if err != nil {
		return Allocation{}, err
	}
	return a[0], nil
}

// Share is what one Workspace takes of its grant: one Workspace, its
// namespaceQuota and storage, and CPU, memory and pods when Shared.
func Share(shared bool, namespaceQuota int32, r Resources) Allocation {
	a := Allocation{Workspaces: 1, Namespaces: namespaceQuota, Resources: Resources{RequestsStorageBytes: r.RequestsStorageBytes}}
	if shared {
		a.Resources = r
	}
	return a
}

// NotBelow checks that grant g keeps every number at least what Workspaces
// hold; else CLUSTER_GRANT_BELOW_ALLOCATION with details.resource and minimum.
func NotBelow(g GrantSpec, holds Allocation) error {
	for _, d := range dimensions(g, holds) {
		if d.limit < d.holds {
			return &apperr.Error{
				Code: apperr.ClusterGrantBelowAllocation,
				Message: fmt.Sprintf("%s cannot go below %s, what the organization's Workspaces hold; they must shrink first",
					d.resource, d.format(d.holds)),
				Details: map[string]any{"resource": d.resource, "minimum": d.format(d.holds), "requested": d.format(d.limit)},
			}
		}
	}
	return nil
}

// Minus is a less b, dimension by dimension: what is left held when a
// Workspace's old share is set aside for its new one.
func (a Allocation) Minus(b Allocation) Allocation {
	r, s := a.Resources, b.Resources
	return Allocation{GrantID: a.GrantID, Workspaces: a.Workspaces - b.Workspaces, Namespaces: a.Namespaces - b.Namespaces,
		Resources: Resources{
			RequestsCPUMilli: r.RequestsCPUMilli - s.RequestsCPUMilli, RequestsMemoryBytes: r.RequestsMemoryBytes - s.RequestsMemoryBytes,
			LimitsCPUMilli: r.LimitsCPUMilli - s.LimitsCPUMilli, LimitsMemoryBytes: r.LimitsMemoryBytes - s.LimitsMemoryBytes,
			Pods: r.Pods - s.Pods, RequestsStorageBytes: r.RequestsStorageBytes - s.RequestsStorageBytes,
		}}
}
