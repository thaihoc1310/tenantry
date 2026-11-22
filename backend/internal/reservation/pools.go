package reservation

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/netip"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/audit"
	"github.com/thaihoc1310/tenantry/backend/internal/id"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// Shared pools, network pools, the nodes of a cluster and dedicated nodes as
// a Platform Admin manages them (PRODUCT-PLAN §6.2–6.3, §7.1). In M5 a change
// is written to the database only, with no Operation and no outbox event:
// from M6 the provisioning worker builds PlatformSharedPool and
// PlatformNodeGrant from these rows (IMPLEMENTATION-PLAN M5 step 4).

const (
	// SystemPoolID is the label value of the system pool: no shared pool may take it.
	SystemPoolID = "system"
	maxNodes     = 200
)

var (
	ErrNoSuchPool  = apperr.New(apperr.NotFound, "pool not found")
	ErrPoolIDTaken = &apperr.Error{Code: apperr.Conflict, Message: "a pool with this id exists (pool ids are unique across clusters)", Details: map[string]any{"field": "id"}}
	ErrOrgGone     = apperr.New(apperr.NotFound, "organization not found")
	ErrOrgInactive = apperr.New(apperr.OrgNotActive, "the organization is not approved yet")
	ErrNoGrant     = apperr.New(apperr.NotFound, "the organization has no grant on this cluster")
)

// SharedPool is what an Admin sets on a shared pool. RatioHundredths is the
// allocation overcommit ratio × 100 (3.0 is 300); SystemReserve is per node.
type SharedPool struct {
	ID, Name        string
	RatioHundredths int64
	SystemReserve   Capacity
	Nodes           []string
}

// PoolView is a shared pool with its nodes and capacity (architecture/05 §4).
// Operation is the sharedpool.update a change of nodes started, if any.
type PoolView struct {
	store.SharedComputePool
	RatioHundredths int64
	Nodes           []string
	Reservable      Capacity
	Ceiling         Capacity
	Allocated       Capacity
	Operation       *store.Operation
}

func (p *SharedPool) check() error {
	if p.Name == "" || len(p.Name) > 80 {
		return apperr.Invalid("name", "must be 1 to 80 characters")
	}
	if p.RatioHundredths < 100 || p.RatioHundredths > 9999 {
		return apperr.Invalid("allocationOvercommitRatio", "must be between 1.00 and 99.99")
	}
	r := p.SystemReserve
	if r.CPUMilli < 0 || r.MemoryBytes < 0 || r.Pods < 0 {
		return apperr.Invalid("systemReserve", "must not be negative")
	}
	if len(p.Nodes) > maxNodes {
		return apperr.Invalid("nodes", fmt.Sprintf("at most %d", maxNodes))
	}
	if dup := duplicate(p.Nodes); dup != "" {
		return apperr.Invalid("nodes", fmt.Sprintf("lists %q twice", dup))
	}
	if p.Nodes == nil {
		p.Nodes = []string{}
	}
	return nil
}

// SharedPools lists a cluster's shared pools with their capacity.
func (s *Service) SharedPools(ctx context.Context, clusterID string) ([]PoolView, error) {
	if _, err := s.q.ShareLockCluster(ctx, clusterID); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoSuchCluster
	} else if err != nil {
		return nil, err
	}
	return PoolViews(ctx, s.q, clusterID)
}

// PoolViews lists a cluster's shared pools with their capacity.
func PoolViews(ctx context.Context, q *store.Queries, clusterID string) ([]PoolView, error) {
	rows, err := q.ListSharedPools(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	out := make([]PoolView, 0, len(rows))
	for _, r := range rows {
		v, err := poolView(ctx, q, r.SharedComputePool, r.Nodes)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func poolView(ctx context.Context, q *store.Queries, p store.SharedComputePool, nodes []string) (PoolView, error) {
	caps, err := q.PoolNodeCapacities(ctx, &p.ID)
	if err != nil {
		return PoolView{}, err
	}
	per := make([]Capacity, len(caps))
	for i, c := range caps {
		per[i] = Capacity{CPUMilli: c.AllocatableCpuMilli, MemoryBytes: c.AllocatableMemoryBytes, Pods: int64(c.AllocatablePods)}
	}
	ratio := hundredths(p.AllocationOvercommitRatio)
	r := PoolReservable(per, poolReserve(p))
	a, err := q.PoolAllocation(ctx, &p.ID)
	if err != nil {
		return PoolView{}, err
	}
	return PoolView{SharedComputePool: p, RatioHundredths: ratio, Nodes: nonNil(nodes), Reservable: r, Ceiling: PoolCeiling(r, ratio),
		Allocated: Capacity{CPUMilli: a.CpuMilli, MemoryBytes: a.MemoryBytes, Pods: a.Pods}}, nil
}

func poolReserve(p store.SharedComputePool) Capacity {
	return Capacity{CPUMilli: p.SystemReserveCpuMilli, MemoryBytes: p.SystemReserveMemoryBytes, Pods: int64(p.SystemReservePods)}
}

// CreateSharedPool adds a shared pool to a cluster, with free nodes.
func (s *Service) CreateSharedPool(ctx context.Context, adminID, correlationID, clusterID string, p SharedPool) (PoolView, error) {
	if !dnsLabel.MatchString(p.ID) {
		return PoolView{}, apperr.Invalid("id", "must be a DNS label: lowercase letters, digits and hyphens, at most 63")
	}
	if p.ID == SystemPoolID {
		return PoolView{}, apperr.Invalid("id", `"system" is the label of the system pool`)
	}
	if err := p.check(); err != nil {
		return PoolView{}, err
	}
	var out PoolView
	err := store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := q.LockCluster(ctx, clusterID); errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSuchCluster
		} else if err != nil {
			return err
		}
		row, err := q.CreateSharedPool(ctx, store.CreateSharedPoolParams{
			ID: p.ID, ClusterID: clusterID, Name: p.Name, AllocationOvercommitRatio: numeric(p.RatioHundredths),
			SystemReserveCpuMilli: p.SystemReserve.CPUMilli, SystemReserveMemoryBytes: p.SystemReserve.MemoryBytes,
			SystemReservePods: int32(p.SystemReserve.Pods),
		})
		if err != nil {
			return err
		}
		if err := setPoolNodes(ctx, q, clusterID, p.ID, p.Nodes); err != nil {
			return err
		}
		if out, err = poolView(ctx, q, row, p.Nodes); err != nil {
			return err
		}
		op, err := enqueuePool(ctx, q, clusterID, p.ID, adminID, correlationID)
		if err != nil {
			return err
		}
		out.Operation = &op
		return audit.Record(ctx, q, audit.Event{
			Actor: adminID, Action: audit.SharedPoolCreate, TargetType: "SharedPool", TargetID: p.ID,
			Detail: map[string]any{"after": poolDetail(out), "operationId": op.ID},
		})
	})
	if c, ok := store.UniqueViolation(err); ok && c == "shared_compute_pools_pkey" {
		return PoolView{}, ErrPoolIDTaken
	}
	return out, err
}

// UpdateSharedPool changes a pool's name, ratio, system reserve and nodes,
// never bringing its ceiling below what Shared Workspaces hold
// (COMPUTE_POOL_CAPACITY_EXCEEDED).
func (s *Service) UpdateSharedPool(ctx context.Context, adminID, correlationID, clusterID, poolID string, p SharedPool) (PoolView, error) {
	if err := p.check(); err != nil {
		return PoolView{}, err
	}
	var out PoolView
	err := store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := q.LockCluster(ctx, clusterID); errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSuchCluster
		} else if err != nil {
			return err
		}
		cur, err := q.LockSharedPool(ctx, poolID)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && cur.ClusterID != clusterID) {
			return ErrNoSuchPool
		}
		if err != nil {
			return err
		}
		before, err := poolNodes(ctx, q, cur)
		if err != nil {
			return err
		}
		row, err := q.UpdateSharedPool(ctx, store.UpdateSharedPoolParams{
			ID: poolID, Name: p.Name, AllocationOvercommitRatio: numeric(p.RatioHundredths),
			SystemReserveCpuMilli: p.SystemReserve.CPUMilli, SystemReserveMemoryBytes: p.SystemReserve.MemoryBytes,
			SystemReservePods: int32(p.SystemReserve.Pods),
		})
		if err != nil {
			return err
		}
		if err := setPoolNodes(ctx, q, clusterID, poolID, p.Nodes); err != nil {
			return err
		}
		if out, err = poolView(ctx, q, row, p.Nodes); err != nil {
			return err
		}
		// Fewer nodes, a lower ratio or a larger reserve: never below what
		// Shared Workspaces hold (PRODUCT-PLAN §6.2).
		if err := PoolFits(out, Capacity{}); err != nil {
			return err
		}
		detail := map[string]any{"before": poolDetail(before), "after": poolDetail(out)}
		// Only the nodes reach the cluster: the operator labels them.
		if !sameSet(before.Nodes, out.Nodes) {
			op, err := enqueuePool(ctx, q, clusterID, poolID, adminID, correlationID)
			if err != nil {
				return err
			}
			out.Operation, detail["operationId"] = &op, op.ID
		}
		return audit.Record(ctx, q, audit.Event{
			Actor: adminID, Action: audit.SharedPoolUpdate, TargetType: "SharedPool", TargetID: poolID, Detail: detail,
		})
	})
	return out, err
}

func poolNodes(ctx context.Context, q *store.Queries, p store.SharedComputePool) (PoolView, error) {
	caps, err := q.PoolNodeCapacities(ctx, &p.ID)
	if err != nil {
		return PoolView{}, err
	}
	names := make([]string, len(caps))
	for i, c := range caps {
		names[i] = c.NodeName
	}
	return poolView(ctx, q, p, names)
}

func poolDetail(v PoolView) map[string]any {
	return map[string]any{"name": v.Name, "allocationOvercommitRatio": float64(v.RatioHundredths) / 100,
		"systemReserve": poolReserve(v.SharedComputePool), "nodes": v.Nodes}
}

// setPoolNodes makes names the pool's nodes. A node that joins must be free:
// a known worker of the cluster, Ready, in no other pool and dedicated to
// nobody (PRODUCT-PLAN §6.2). The nodes are locked first.
func setPoolNodes(ctx context.Context, q *store.Queries, clusterID, poolID string, names []string) error {
	rows, err := q.LockClusterNodes(ctx, store.LockClusterNodesParams{ClusterID: clusterID, Names: names})
	if err != nil {
		return err
	}
	known := map[string]store.LockClusterNodesRow{}
	for _, r := range rows {
		known[r.ClusterNode.NodeName] = r
	}
	for _, name := range names {
		r, ok := known[name]
		n := r.ClusterNode
		switch {
		case !ok:
			return apperr.Invalid("nodes", fmt.Sprintf("cluster %s has no node %q", clusterID, name))
		case n.SharedPoolID != nil && *n.SharedPoolID == poolID:
			continue // already in
		case n.Kind != "Worker":
			return apperr.Invalid("nodes", fmt.Sprintf("%s is a %s node, not a worker", name, n.Kind))
		case n.SharedPoolID != nil:
			return apperr.Invalid("nodes", fmt.Sprintf("%s is in pool %s", name, *n.SharedPoolID))
		case r.DedicatedOrgID != nil:
			return apperr.Invalid("nodes", fmt.Sprintf("%s is dedicated to an organization", name))
		case !n.Ready:
			return apperr.Invalid("nodes", fmt.Sprintf("%s is not Ready", name))
		}
	}
	if err := q.LeavePool(ctx, store.LeavePoolParams{PoolID: &poolID, Keep: names}); err != nil {
		return err
	}
	return q.JoinPool(ctx, store.JoinPoolParams{PoolID: &poolID, ClusterID: clusterID, Names: names})
}

// numeric is hundredths as a numeric(4,2).
func numeric(h int64) pgtype.Numeric { return pgtype.Numeric{Int: big.NewInt(h), Exp: -2, Valid: true} }

// hundredths reads a numeric as hundredths.
func hundredths(n pgtype.Numeric) int64 {
	if !n.Valid || n.Int == nil {
		return 0
	}
	v := new(big.Int).Set(n.Int)
	switch e := n.Exp + 2; {
	case e > 0:
		v.Mul(v, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(e)), nil))
	case e < 0:
		v.Quo(v, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-e)), nil))
	}
	return v.Int64()
}

// NodeView is a node of a cluster, with the organization it is dedicated to.
type NodeView struct {
	store.ClusterNode
	DedicatedOrgID, DedicatedOrgSlug, DedicatedOrgName *string
}

// Nodes lists a cluster's nodes as last reported.
func (s *Service) Nodes(ctx context.Context, clusterID string) ([]NodeView, error) {
	if _, err := s.q.ShareLockCluster(ctx, clusterID); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoSuchCluster
	} else if err != nil {
		return nil, err
	}
	rows, err := s.q.ListClusterNodes(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	out := make([]NodeView, len(rows))
	for i, r := range rows {
		out[i] = NodeView{ClusterNode: r.ClusterNode, DedicatedOrgID: r.DedicatedOrgID, DedicatedOrgSlug: r.DedicatedOrgSlug, DedicatedOrgName: r.DedicatedOrgName}
	}
	return out, nil
}

// NetworkPool is what an Admin sets on a network pool (PRODUCT-PLAN §7.1).
type NetworkPool struct {
	ID, OrgID      string // OrgID "": a pool any organization's grant may list
	CIDRs          []netip.Prefix
	DefaultPrefix  int32
	AllowedPrefixs []int32
}

func (p *NetworkPool) checkPrefixes() error {
	if len(p.AllowedPrefixs) == 0 {
		return apperr.Invalid("allowedPrefixLengths", "must list at least one prefix length")
	}
	if dup := duplicate(intsToStrings(p.AllowedPrefixs)); dup != "" {
		return apperr.Invalid("allowedPrefixLengths", "lists "+dup+" twice")
	}
	longest := 0
	for _, c := range p.CIDRs {
		longest = max(longest, c.Bits())
	}
	for _, l := range p.AllowedPrefixs {
		// A Workspace block fits in every range, and leaves room for Pods.
		if int(l) < longest || l > 28 {
			return apperr.Invalid("allowedPrefixLengths", fmt.Sprintf("/%d: each must be between the pool's longest prefix (/%d) and /28", l, longest))
		}
	}
	if !slices.Contains(p.AllowedPrefixs, p.DefaultPrefix) {
		return apperr.Invalid("defaultPrefixLength", "must be one of allowedPrefixLengths")
	}
	return nil
}

func intsToStrings(v []int32) []string {
	out := make([]string, len(v))
	for i, x := range v {
		out[i] = fmt.Sprint(x)
	}
	return out
}

func (s *Service) NetworkPools(ctx context.Context, clusterID string) ([]store.NetworkPool, error) {
	if _, err := s.q.ShareLockCluster(ctx, clusterID); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoSuchCluster
	} else if err != nil {
		return nil, err
	}
	return s.q.ListNetworkPools(ctx, clusterID)
}

// otherRanges is every range of the cluster's other network pools.
func otherRanges(ctx context.Context, q *store.Queries, clusterID, except string) (map[string][]netip.Prefix, error) {
	pools, err := q.NetworkPoolRangesOfCluster(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	out := map[string][]netip.Prefix{}
	for _, p := range pools {
		if p.ID != except {
			out[p.ID] = p.Cidrs
		}
	}
	return out, nil
}

// CreateNetworkPool adds a network pool to a cluster. Under the cluster's
// lock, its ranges overlap neither another pool of the cluster nor the
// cluster's reserved ranges (02-data-model §3).
func (s *Service) CreateNetworkPool(ctx context.Context, adminID, clusterID string, p NetworkPool) (store.NetworkPool, error) {
	if !dnsLabel.MatchString(p.ID) {
		return store.NetworkPool{}, apperr.Invalid("id", "must be a DNS label: lowercase letters, digits and hyphens, at most 63")
	}
	var out store.NetworkPool
	err := store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		c, err := q.LockCluster(ctx, clusterID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSuchCluster
		}
		if err != nil {
			return err
		}
		others, err := otherRanges(ctx, q, clusterID, "")
		if err != nil {
			return err
		}
		if err := CheckPoolCIDRs(p.CIDRs, others, c.ReservedCidrs); err != nil {
			return err
		}
		if err := p.checkPrefixes(); err != nil {
			return err
		}
		var org *string
		if p.OrgID != "" {
			o, err := q.LockOrganization(ctx, p.OrgID)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && o.Status != "Active") {
				return apperr.Invalid("organizationId", "no Active organization "+p.OrgID)
			}
			if err != nil {
				return err
			}
			org = &o.ID
		}
		if out, err = q.CreateNetworkPool(ctx, store.CreateNetworkPoolParams{
			ID: p.ID, ClusterID: clusterID, OrgID: org, Cidrs: p.CIDRs,
			DefaultPrefixLength: p.DefaultPrefix, AllowedPrefixLengths: p.AllowedPrefixs,
		}); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Event{
			Actor: adminID, OrgID: p.OrgID, Action: audit.NetworkPoolCreate, TargetType: "NetworkPool", TargetID: p.ID,
			Detail: map[string]any{"after": networkDetail(out)},
		})
	})
	if c, ok := store.UniqueViolation(err); ok && c == "network_pools_pkey" {
		return store.NetworkPool{}, ErrPoolIDTaken
	}
	return out, err
}

// NetworkPoolPatch holds the fields to change; nil keeps the current value.
type NetworkPoolPatch struct {
	CIDRs          *[]netip.Prefix
	DefaultPrefix  *int32
	AllowedPrefixs *[]int32
}

// UpdateNetworkPool changes a pool's ranges or prefix lengths: a range added
// is checked like a new pool's, and only the database changes (the CIDR is
// handed out by the API, M9).
// Its ranges keep every block Workspaces hold in it (CONFLICT otherwise);
// prefix lengths only bind blocks given from now on.
func (s *Service) UpdateNetworkPool(ctx context.Context, adminID, clusterID, poolID string, p NetworkPoolPatch) (store.NetworkPool, error) {
	var out store.NetworkPool
	err := store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		c, err := q.LockCluster(ctx, clusterID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSuchCluster
		}
		if err != nil {
			return err
		}
		cur, err := q.LockNetworkPool(ctx, poolID)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && cur.ClusterID != clusterID) {
			return ErrNoSuchPool
		}
		if err != nil {
			return err
		}
		next := NetworkPool{ID: cur.ID, CIDRs: cur.Cidrs, DefaultPrefix: cur.DefaultPrefixLength, AllowedPrefixs: cur.AllowedPrefixLengths}
		if p.CIDRs != nil {
			next.CIDRs = *p.CIDRs
		}
		if p.DefaultPrefix != nil {
			next.DefaultPrefix = *p.DefaultPrefix
		}
		if p.AllowedPrefixs != nil {
			next.AllowedPrefixs = *p.AllowedPrefixs
		}
		others, err := otherRanges(ctx, q, clusterID, poolID)
		if err != nil {
			return err
		}
		if err := CheckPoolCIDRs(next.CIDRs, others, c.ReservedCidrs); err != nil {
			return err
		}
		if err := next.checkPrefixes(); err != nil {
			return err
		}
		held, err := q.PoolAllocatedCIDRs(ctx, poolID)
		if err != nil {
			return err
		}
		for _, b := range held {
			if !Within(b, next.CIDRs) {
				return &apperr.Error{Code: apperr.Conflict,
					Message: fmt.Sprintf("a Workspace holds %s, outside the new ranges", b), Details: map[string]any{"field": "cidrs", "cidr": b.String()}}
			}
		}
		if out, err = q.UpdateNetworkPool(ctx, store.UpdateNetworkPoolParams{
			ID: poolID, Cidrs: next.CIDRs, DefaultPrefixLength: next.DefaultPrefix, AllowedPrefixLengths: next.AllowedPrefixs,
		}); err != nil {
			return err
		}
		org := ""
		if cur.OrgID != nil {
			org = *cur.OrgID
		}
		return audit.Record(ctx, q, audit.Event{
			Actor: adminID, OrgID: org, Action: audit.NetworkPoolUpdate, TargetType: "NetworkPool", TargetID: poolID,
			Detail: map[string]any{"before": networkDetail(cur), "after": networkDetail(out)},
		})
	})
	return out, err
}

func networkDetail(p store.NetworkPool) map[string]any {
	cidrs := make([]string, len(p.Cidrs))
	for i, c := range p.Cidrs {
		cidrs[i] = c.String()
	}
	return map[string]any{"organizationId": p.OrgID, "cidrs": cidrs, "defaultPrefixLength": p.DefaultPrefixLength,
		"allowedPrefixLengths": p.AllowedPrefixLengths}
}

// DedicatedNode is a node granted to an organization, with its capacity
// (architecture/04 §5).
type DedicatedNode struct {
	store.DedicatedNodeGrant
	Allocatable, SystemReserve, Reservable, Reserved, Remaining Capacity
	Workspaces                                                  []string // slugs of the Workspaces holding a reservation
}

func dedicatedView(ctx context.Context, q *store.Queries, g store.DedicatedNodeGrant) (DedicatedNode, error) {
	alloc := Capacity{CPUMilli: g.AllocatableCpuMilli, MemoryBytes: g.AllocatableMemoryBytes, Pods: int64(g.AllocatablePods)}
	res := Capacity{CPUMilli: g.SystemReserveCpuMilli, MemoryBytes: g.SystemReserveMemoryBytes, Pods: int64(g.SystemReservePods)}
	r := Reservable(alloc, res)
	reserved, err := NodeReserved(ctx, q, g.ID)
	if err != nil {
		return DedicatedNode{}, err
	}
	rows, err := q.NodeGrantWorkspaces(ctx, []string{g.ID})
	if err != nil {
		return DedicatedNode{}, err
	}
	slugs := []string{}
	for _, w := range rows {
		slugs = append(slugs, w.Slug)
	}
	return DedicatedNode{DedicatedNodeGrant: g, Allocatable: alloc, SystemReserve: res, Reservable: r, Reserved: reserved,
		Remaining: Remaining(r, reserved), Workspaces: slugs}, nil
}

// NodeReserved is what the Workspaces on a node grant hold of it.
func NodeReserved(ctx context.Context, q *store.Queries, grantID string) (Capacity, error) {
	s, err := q.NodeReserved(ctx, grantID)
	return Capacity{CPUMilli: s.CpuMilli, MemoryBytes: s.MemoryBytes, Pods: s.Pods}, err
}

// ReserveOnNode checks that more fits on a locked node grant
// (PRODUCT-PLAN §6.3): the node Ready, not Overcommitted, and what its
// Workspaces hold plus more within reservable. Hard: no overcommit.
func ReserveOnNode(ctx context.Context, q *store.Queries, g store.DedicatedNodeGrant, more Capacity) error {
	if g.NodeState != "Ready" {
		return &apperr.Error{Code: apperr.DedicatedNodeNotReady, Message: fmt.Sprintf("node %s is %s", g.NodeName, g.NodeState),
			Details: map[string]any{"node": g.NodeName, "nodeState": g.NodeState}}
	}
	v, err := dedicatedView(ctx, q, g)
	if err != nil {
		return err
	}
	have, want, room := v.Reserved.dims(), more.dims(), v.Reservable.dims()
	names := [3]string{"requestsCpu", "requestsMemory", "pods"}
	formats := [3]func(int64) string{cpuQ, bytesQ, count}
	for i := range have {
		if want[i] > 0 && have[i]+want[i] > room[i] {
			return &apperr.Error{
				Code: apperr.DedicatedNodeInsufficientCapacity,
				Message: fmt.Sprintf("node %s can hold %s %s; its Workspaces hold %s and this asks %s more",
					g.NodeName, formats[i](room[i]), names[i], formats[i](have[i]), formats[i](want[i])),
				Details: map[string]any{"node": g.NodeName, "resource": names[i], "reservable": formats[i](room[i]),
					"reserved": formats[i](have[i]), "requested": formats[i](want[i])},
			}
		}
	}
	return nil
}

// RefreshNodeCapacity records a node grant's capacity state from what is
// reserved on it (04-state-machines §5.1).
func RefreshNodeCapacity(ctx context.Context, q *store.Queries, grantID string) error {
	g, err := q.GetNodeGrant(ctx, grantID)
	if err != nil {
		return err
	}
	v, err := dedicatedView(ctx, q, g)
	if err != nil {
		return err
	}
	return q.SetNodeCapacityState(ctx, store.SetNodeCapacityStateParams{ID: g.ID, CapacityState: CapacityState(v.Reservable, v.Reserved)})
}

// NodeRequest is one node an Admin dedicates, with its system reserve.
type NodeRequest struct {
	Name          string
	SystemReserve Capacity
}

// DedicatedNodes lists the nodes an organization holds on a cluster.
func (s *Service) DedicatedNodes(ctx context.Context, orgID, clusterID string) ([]DedicatedNode, error) {
	return DedicatedNodesOf(ctx, s.q, orgID, clusterID)
}

// DedicatedNodesOf lists the nodes an organization holds on a cluster, with their capacity.
func DedicatedNodesOf(ctx context.Context, q *store.Queries, orgID, clusterID string) ([]DedicatedNode, error) {
	rows, err := q.ListLiveDedicatedNodes(ctx, store.ListLiveDedicatedNodesParams{OrgID: orgID, ClusterID: clusterID})
	if err != nil {
		return nil, err
	}
	out := make([]DedicatedNode, len(rows))
	for i, r := range rows {
		if out[i], err = dedicatedView(ctx, q, r); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// SetDedicatedNodes makes nodes the set of nodes the organization holds on
// the cluster, under its grant there: a new node must be a free worker of the
// cluster (no pool, nobody else's) and starts NotReady until the operator has
// checked it (M8); a node left out is revoked; a node kept gets the system
// reserve given.
//
// ponytail: refusing to revoke a node that still holds reservations comes
// with Dedicated Workspaces (M8).
func (s *Service) SetDedicatedNodes(ctx context.Context, adminID, correlationID, orgID, clusterID string, nodes []NodeRequest) ([]DedicatedNode, []store.Operation, error) {
	names := make([]string, len(nodes))
	for i, n := range nodes {
		names[i] = n.Name
		r := n.SystemReserve
		if r.CPUMilli < 0 || r.MemoryBytes < 0 || r.Pods < 0 {
			return nil, nil, apperr.Invalid("nodes", "systemReserve must not be negative")
		}
	}
	if dup := duplicate(names); dup != "" {
		return nil, nil, apperr.Invalid("nodes", fmt.Sprintf("lists %q twice", dup))
	}
	if len(nodes) > maxNodes {
		return nil, nil, apperr.Invalid("nodes", fmt.Sprintf("at most %d", maxNodes))
	}
	var out []DedicatedNode
	var ops []store.Operation
	err := store.InTx(ctx, s.db, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		o, err := q.LockOrganization(ctx, orgID)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && o.Status == "Deleted") {
			return ErrOrgGone
		}
		if err != nil {
			return err
		}
		if o.Status != "Active" {
			return ErrOrgInactive
		}
		if _, err := q.LockClusterGrant(ctx, store.LockClusterGrantParams{OrgID: orgID, ClusterID: clusterID}); errors.Is(err, pgx.ErrNoRows) {
			return ErrNoGrant
		} else if err != nil {
			return err
		}
		locked, err := q.LockClusterNodes(ctx, store.LockClusterNodesParams{ClusterID: clusterID, Names: names})
		if err != nil {
			return err
		}
		known := map[string]store.LockClusterNodesRow{}
		for _, r := range locked {
			known[r.ClusterNode.NodeName] = r
		}
		current, err := q.ListLiveDedicatedNodes(ctx, store.ListLiveDedicatedNodesParams{OrgID: orgID, ClusterID: clusterID})
		if err != nil {
			return err
		}
		held := map[string]store.DedicatedNodeGrant{}
		for _, g := range current {
			held[g.NodeName] = g
		}
		before := make([]string, 0, len(current))
		for _, g := range current {
			before = append(before, g.NodeName)
		}
		for _, n := range nodes {
			r := n.SystemReserve
			if g, ok := held[n.Name]; ok {
				alloc := Capacity{CPUMilli: g.AllocatableCpuMilli, MemoryBytes: g.AllocatableMemoryBytes, Pods: int64(g.AllocatablePods)}
				reserved, err := NodeReserved(ctx, q, g.ID)
				if err != nil {
					return err
				}
				// A larger system reserve may leave the node Overcommitted: its
				// Workspaces run on, it takes no new reservation (04 §5.3).
				if _, err := q.UpdateDedicatedNodeReserve(ctx, store.UpdateDedicatedNodeReserveParams{
					ID: g.ID, SystemReserveCpuMilli: r.CPUMilli, SystemReserveMemoryBytes: r.MemoryBytes, SystemReservePods: int32(r.Pods),
					CapacityState: CapacityState(Reservable(alloc, r), reserved),
				}); err != nil {
					return err
				}
				continue
			}
			k, ok := known[n.Name]
			node := k.ClusterNode
			switch {
			case !ok:
				return apperr.Invalid("nodes", fmt.Sprintf("cluster %s has no node %q", clusterID, n.Name))
			case node.Kind != "Worker":
				return apperr.Invalid("nodes", fmt.Sprintf("%s is a %s node, not a worker", n.Name, node.Kind))
			case node.SharedPoolID != nil:
				return apperr.Invalid("nodes", fmt.Sprintf("%s is in pool %s: take it out of the pool first", n.Name, *node.SharedPoolID))
			case k.DedicatedOrgID != nil:
				return apperr.Invalid("nodes", fmt.Sprintf("%s is dedicated to another organization", n.Name))
			}
			alloc := Capacity{CPUMilli: node.AllocatableCpuMilli, MemoryBytes: node.AllocatableMemoryBytes, Pods: int64(node.AllocatablePods)}
			g, err := q.CreateDedicatedNodeGrant(ctx, store.CreateDedicatedNodeGrantParams{
				ID: id.New(id.NodeGrant), OrgID: orgID, ClusterID: clusterID, NodeName: n.Name,
				AllocatableCpuMilli: alloc.CPUMilli, AllocatableMemoryBytes: alloc.MemoryBytes, AllocatablePods: int32(alloc.Pods),
				SystemReserveCpuMilli: r.CPUMilli, SystemReserveMemoryBytes: r.MemoryBytes, SystemReservePods: int32(r.Pods),
				CapacityState: CapacityState(Reservable(alloc, r), Capacity{}),
			})
			if err != nil {
				return err
			}
			op, err := enqueueNodeGrant(ctx, q, g, false, adminID, correlationID)
			if err != nil {
				return err
			}
			ops = append(ops, op)
		}
		for _, g := range current {
			if !slices.Contains(names, g.NodeName) {
				held, err := q.NodeGrantWorkspaces(ctx, []string{g.ID})
				if err != nil {
					return err
				}
				if len(held) > 0 {
					return &apperr.Error{Code: apperr.Conflict,
						Message: fmt.Sprintf("Workspaces of the organization run on %s: they go before the node does", g.NodeName),
						Details: map[string]any{"node": g.NodeName, "workspaces": len(held)}}
				}
				if err := q.RevokeDedicatedNodeGrant(ctx, g.ID); err != nil {
					return err
				}
				op, err := enqueueNodeGrant(ctx, q, g, true, adminID, correlationID)
				if err != nil {
					return err
				}
				ops = append(ops, op)
			}
		}
		rows, err := q.ListLiveDedicatedNodes(ctx, store.ListLiveDedicatedNodesParams{OrgID: orgID, ClusterID: clusterID})
		if err != nil {
			return err
		}
		out = make([]DedicatedNode, len(rows))
		after := make([]map[string]any, len(rows))
		for i, r := range rows {
			if out[i], err = dedicatedView(ctx, q, r); err != nil {
				return err
			}
			after[i] = map[string]any{"node": r.NodeName, "systemReserve": out[i].SystemReserve}
		}
		return audit.Record(ctx, q, audit.Event{
			Actor: adminID, OrgID: orgID, Action: audit.DedicatedNodesUpdate, TargetType: "ClusterGrant", TargetID: clusterID,
			Detail: map[string]any{"before": before, "after": after, "operationIds": opIDs(ops)},
		})
	})
	if err != nil {
		return nil, nil, err
	}
	return out, ops, nil
}

func opIDs(ops []store.Operation) []string {
	out := make([]string, len(ops))
	for i, op := range ops {
		out[i] = op.ID
	}
	return out
}

// sameSet tells whether a and b hold the same names, in any order.
func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, x := range a {
		if !slices.Contains(b, x) {
			return false
		}
	}
	return true
}

// PoolFits checks that more fits under the pool's ceiling next to what its
// Shared Workspaces hold: a soft limit, ratio × reservable (PRODUCT-PLAN
// §6.2). It reports the first dimension short as COMPUTE_POOL_CAPACITY_EXCEEDED.
func PoolFits(v PoolView, more Capacity) error {
	have, want, ceil := v.Allocated.dims(), more.dims(), v.Ceiling.dims()
	names := [3]string{"requestsCpu", "requestsMemory", "pods"}
	formats := [3]func(int64) string{cpuQ, bytesQ, count}
	for i := range have {
		if have[i]+want[i] > ceil[i] && (want[i] > 0 || have[i] > ceil[i]) {
			return &apperr.Error{
				Code: apperr.ComputePoolCapacityExceeded,
				Message: fmt.Sprintf("shared pool %s can take %s %s; its Workspaces hold %s and this asks %s more",
					v.ID, formats[i](ceil[i]), names[i], formats[i](have[i]), formats[i](want[i])),
				Details: map[string]any{"resource": names[i], "limit": formats[i](ceil[i]), "allocated": formats[i](have[i]),
					"requested": formats[i](want[i]), "sharedPoolId": v.ID},
			}
		}
	}
	return nil
}

// LockedPool locks a shared pool and reads its capacity, for a Workspace
// taking a share of it (lock order: 02-data-model §4).
func LockedPool(ctx context.Context, q *store.Queries, poolID string) (PoolView, error) {
	p, err := q.LockSharedPool(ctx, poolID)
	if err != nil {
		return PoolView{}, err
	}
	return poolNodes(ctx, q, p)
}
