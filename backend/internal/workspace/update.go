package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/audit"
	"github.com/thaihoc1310/tenantry/backend/internal/reservation"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// Patch holds what a PATCH changes; nil keeps the current value
// (PRODUCT-PLAN §17.2). Resources replaces the whole quota.
type Patch struct {
	DisplayName   *string
	Resources     *Resources
	CorrelationID string
}

// Changed is a Workspace after a PATCH and the workspace.update operation
// carrying it to the cluster; Operation is nil when nothing changed.
type Changed struct {
	Workspace store.Workspace
	Operation *store.Operation
}

// Update changes a Workspace's display name, quota and namespaceQuota
// (OrgOwner or OrgAdmin). A resize takes its new share of the grant under
// the locks of a create, in their order (02-data-model §4), needs the
// cluster Ready, and goes below neither what the Pods use nor the
// Namespaces the Workspace has.
func (s *Service) Update(ctx context.Context, userID, wsID string, p Patch) (Changed, error) {
	ws, a, err := s.access(ctx, userID, wsID)
	if err != nil {
		return Changed{}, err
	}
	if !a.Manages() {
		return Changed{}, ErrForbidden
	}
	if p.DisplayName != nil {
		v := strings.TrimSpace(*p.DisplayName)
		if v == "" || utf8.RuneCountInString(v) > maxDisplayName {
			return Changed{}, apperr.Invalid("displayName", fmt.Sprintf("must be 1 to %d characters", maxDisplayName))
		}
		p.DisplayName = &v
	}
	if p.Resources != nil {
		if err := p.Resources.check(ws.ComputeMode); err != nil {
			return Changed{}, err
		}
	}

	var out Changed
	err = store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		resize := p.Resources != nil && *p.Resources != current(ws)
		var grant store.OrganizationClusterGrant
		if resize {
			cluster, err := q.ShareLockCluster(ctx, ws.ClusterID)
			if err != nil {
				return err
			}
			if cluster.Status != reservation.ClusterReady {
				return apperr.New(apperr.ClusterNotReady, "the cluster is not Ready: resizing waits for it")
			}
			if grant, err = q.LockClusterGrant(ctx, store.LockClusterGrantParams{OrgID: ws.OrgID, ClusterID: ws.ClusterID}); err != nil {
				return err
			}
		}
		cur, err := lockForChange(ctx, q, wsID)
		if err != nil {
			return err
		}
		next := store.UpdateWorkspaceSpecParams{
			ID: cur.ID, DisplayName: cur.DisplayName,
			RequestsCpuMilli: cur.RequestsCpuMilli, RequestsMemoryBytes: cur.RequestsMemoryBytes,
			LimitsCpuMilli: cur.LimitsCpuMilli, LimitsMemoryBytes: cur.LimitsMemoryBytes, Pods: cur.Pods,
			RequestsStorageBytes: cur.RequestsStorageBytes, NamespaceQuota: cur.NamespaceQuota,
		}
		if p.DisplayName != nil {
			next.DisplayName = *p.DisplayName
		}
		if resize {
			r := *p.Resources
			if err := checkResize(ctx, q, grant, cur, r); err != nil {
				return err
			}
			next.RequestsCpuMilli, next.RequestsMemoryBytes = r.RequestsCPUMilli, r.RequestsMemoryBytes
			next.LimitsCpuMilli, next.LimitsMemoryBytes, next.Pods = r.LimitsCPUMilli, r.LimitsMemoryBytes, r.Pods
			next.RequestsStorageBytes, next.NamespaceQuota = r.RequestsStorageBytes, r.NamespaceQuota
		}
		if next.DisplayName == cur.DisplayName && !resize {
			out.Workspace = cur
			return nil
		}
		updated, err := q.UpdateWorkspaceSpec(ctx, next)
		if err != nil {
			return err
		}
		op, err := beginUpdate(ctx, q, updated, "workspace.update", userID, p.CorrelationID)
		if err != nil {
			return err
		}
		if out.Workspace, err = q.GetWorkspace(ctx, cur.ID); err != nil {
			return err
		}
		out.Operation = &op
		return audit.Record(ctx, q, audit.Event{
			Actor: userID, OrgID: cur.OrgID, Action: audit.WorkspaceUpdate, TargetType: "Workspace", TargetID: cur.ID,
			Detail: map[string]any{"before": specDetail(cur), "after": specDetail(out.Workspace), "operationId": op.ID},
		})
	})
	return out, err
}

// checkResize checks a new quota against grant, locked, and against what
// the Workspace uses (PRODUCT-PLAN §5.4).
func checkResize(ctx context.Context, q *store.Queries, grant store.OrganizationClusterGrant, ws store.Workspace, r Resources) error {
	holds, err := reservation.Holds(ctx, q, grant.ID)
	if err != nil {
		return err
	}
	shared := ws.ComputeMode == ComputeShared
	old := reservation.Share(shared, ws.NamespaceQuota, current(ws).grant())
	more := reservation.Share(shared, r.NamespaceQuota, r.grant()).Minus(old)
	// Only what grows asks the grant for more; a smaller number frees room.
	if err := reservation.GrantFits(reservation.FromRow(grant, nil, nil).GrantSpec, holds, positive(more)); err != nil {
		return err
	}
	if !shared {
		// A larger reservation is taken now, under the node grant's lock; a
		// smaller one keeps the old numbers until the cluster has the new
		// quota, so the database never says less than the node enforces
		// (IMPLEMENTATION-PLAN §3.2).
		res, err := q.GetNodeReservation(ctx, ws.ID)
		if err != nil {
			return err
		}
		g, err := q.LockNodeGrant(ctx, store.LockNodeGrantParams{ID: res.NodeGrantID, OrgID: ws.OrgID, ClusterID: ws.ClusterID})
		if err != nil {
			return err
		}
		held := reservation.Capacity{CPUMilli: res.ReservedCpuMilli, MemoryBytes: res.ReservedMemoryBytes, Pods: int64(res.ReservedPods)}
		next := r.onNodes()
		grow := reservation.Capacity{
			CPUMilli: max(next.CPUMilli-held.CPUMilli, 0), MemoryBytes: max(next.MemoryBytes-held.MemoryBytes, 0), Pods: max(next.Pods-held.Pods, 0),
		}
		if grow != (reservation.Capacity{}) {
			if err := reservation.ReserveOnNode(ctx, q, g, grow); err != nil {
				return err
			}
		}
		if err := q.SetReservationAmount(ctx, store.SetReservationAmountParams{
			WorkspaceID: ws.ID, ReservedCpuMilli: max(held.CPUMilli, next.CPUMilli),
			ReservedMemoryBytes: max(held.MemoryBytes, next.MemoryBytes), ReservedPods: int32(max(held.Pods, next.Pods)),
		}); err != nil {
			return err
		}
		if err := reservation.RefreshNodeCapacity(ctx, q, g.ID); err != nil {
			return err
		}
	}
	if shared && ws.SharedPoolID != nil {
		pool, err := reservation.LockedPool(ctx, q, *ws.SharedPoolID)
		if err != nil {
			return err
		}
		grow := positive(more).Resources
		if err := reservation.PoolFits(pool, reservation.Capacity{CPUMilli: grow.RequestsCPUMilli, MemoryBytes: grow.RequestsMemoryBytes, Pods: int64(grow.Pods)}); err != nil {
			return err
		}
	}

	used := map[string]string{}
	if err := json.Unmarshal(ws.UsageSnapshot, &used); err != nil {
		return fmt.Errorf("workspace %s usage: %w", ws.ID, err)
	}
	for _, c := range []struct {
		key, field string
		next       int64
		milli      bool
	}{
		{"requests.cpu", "requestsCpu", r.RequestsCPUMilli, true},
		{"requests.memory", "requestsMemory", r.RequestsMemoryBytes, false},
		{"limits.cpu", "limitsCpu", r.LimitsCPUMilli, true},
		{"limits.memory", "limitsMemory", r.LimitsMemoryBytes, false},
		{"pods", "pods", int64(r.Pods), false},
		{"requests.storage", "requestsStorage", r.RequestsStorageBytes, false},
	} {
		v, ok := used[c.key]
		if !ok {
			continue
		}
		qty, err := resource.ParseQuantity(v)
		if err != nil {
			return fmt.Errorf("workspace %s usage %s=%q: %w", ws.ID, c.key, v, err)
		}
		u := qty.Value()
		if c.milli {
			u = qty.MilliValue()
		}
		if c.next < u {
			return belowUsage(c.field, v)
		}
	}
	n, err := q.CountWorkspaceNamespaces(ctx, ws.ID)
	if err != nil {
		return err
	}
	if r.NamespaceQuota < n {
		return belowUsage("namespaceQuota", fmt.Sprint(n))
	}
	return nil
}

func belowUsage(field, used string) error {
	return &apperr.Error{
		Code:    apperr.WorkspaceQuotaBelowUsage,
		Message: fmt.Sprintf("%s cannot go below %s, what the Workspace uses now", field, used),
		Details: map[string]any{"field": "resources." + field, "used": used},
	}
}

// positive keeps the dimensions that grow.
func positive(a reservation.Allocation) reservation.Allocation {
	p := func(v int64) int64 { return max(v, 0) }
	r := a.Resources
	return reservation.Allocation{
		Workspaces: int32(p(int64(a.Workspaces))), Namespaces: int32(p(int64(a.Namespaces))),
		Resources: reservation.Resources{
			RequestsCPUMilli: p(r.RequestsCPUMilli), RequestsMemoryBytes: p(r.RequestsMemoryBytes),
			LimitsCPUMilli: p(r.LimitsCPUMilli), LimitsMemoryBytes: p(r.LimitsMemoryBytes),
			Pods: int32(p(int64(r.Pods))), RequestsStorageBytes: p(r.RequestsStorageBytes),
		},
	}
}

// current is a Workspace's quota as a Resources.
func current(ws store.Workspace) Resources {
	return Resources{
		RequestsCPUMilli: ws.RequestsCpuMilli, RequestsMemoryBytes: ws.RequestsMemoryBytes,
		LimitsCPUMilli: ws.LimitsCpuMilli, LimitsMemoryBytes: ws.LimitsMemoryBytes,
		Pods: ws.Pods, RequestsStorageBytes: ws.RequestsStorageBytes, NamespaceQuota: ws.NamespaceQuota,
	}
}

func specDetail(ws store.Workspace) map[string]any {
	return map[string]any{"displayName": ws.DisplayName, "resources": current(ws)}
}
