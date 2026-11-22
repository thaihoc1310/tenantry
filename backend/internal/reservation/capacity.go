package reservation

import (
	"fmt"
	"net/netip"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
)

// Capacity is an amount of node capacity in stored units, one value per
// dimension the platform reserves (architecture/05 §4).
type Capacity struct {
	CPUMilli    int64 `json:"cpuMilli"`
	MemoryBytes int64 `json:"memoryBytes"`
	Pods        int64 `json:"pods"`
}

func (c Capacity) dims() [3]int64 { return [3]int64{c.CPUMilli, c.MemoryBytes, c.Pods} }

func fromDims(d [3]int64) Capacity { return Capacity{CPUMilli: d[0], MemoryBytes: d[1], Pods: d[2]} }

// each applies f to every dimension of a and b.
func each(a, b Capacity, f func(x, y int64) int64) Capacity {
	x, y := a.dims(), b.dims()
	var out [3]int64
	for i := range out {
		out[i] = f(x[i], y[i])
	}
	return fromDims(out)
}

func (c Capacity) Add(o Capacity) Capacity {
	return each(c, o, func(x, y int64) int64 { return x + y })
}

// Reservable is what one node offers tenants: allocatable less the system
// reserve, never below zero.
func Reservable(allocatable, systemReserve Capacity) Capacity {
	return each(allocatable, systemReserve, func(x, y int64) int64 { return max(x-y, 0) })
}

// Remaining is what is left to reserve, never below zero.
func Remaining(reservable, reserved Capacity) Capacity {
	return each(reservable, reserved, func(x, y int64) int64 { return max(x-y, 0) })
}

// Fits says whether more fits next to reserved within ceiling, in every dimension.
func Fits(ceiling, reserved, more Capacity) bool {
	c, r, m := ceiling.dims(), reserved.dims(), more.dims()
	for i := range c {
		if r[i]+m[i] > c[i] {
			return false
		}
	}
	return true
}

// Capacity states of a dedicated node (architecture/04 §5.1).
const (
	Available          = "Available"
	PartiallyAllocated = "PartiallyAllocated"
	FullyAllocated     = "FullyAllocated"
	Overcommitted      = "Overcommitted"
)

// CapacityState folds the three dimensions into one state: Overcommitted if
// any dimension holds more than it may, FullyAllocated if any is exactly
// full, Available if nothing is reserved, PartiallyAllocated otherwise.
func CapacityState(reservable, reserved Capacity) string {
	full, used := false, false
	r, d := reservable.dims(), reserved.dims()
	for i := range r {
		switch {
		case d[i] > r[i]:
			return Overcommitted
		case d[i] == r[i] && d[i] > 0:
			full = true
		}
		used = used || d[i] > 0
	}
	switch {
	case full:
		return FullyAllocated
	case !used:
		return Available
	}
	return PartiallyAllocated
}

// PoolReservable is what a shared pool's nodes offer tenants: the sum, over
// its nodes, of allocatable less the per-node system reserve.
func PoolReservable(nodes []Capacity, systemReserve Capacity) Capacity {
	var sum Capacity
	for _, n := range nodes {
		sum = sum.Add(Reservable(n, systemReserve))
	}
	return sum
}

// PoolCeiling is how much the Workspaces of a shared pool may reserve in
// total: ratio × reservable (PRODUCT-PLAN §5.3). The ratio comes in
// hundredths, as stored (numeric(4,2)), so the math stays exact.
func PoolCeiling(reservable Capacity, ratioHundredths int64) Capacity {
	return each(reservable, Capacity{}, func(x, _ int64) int64 { return x * ratioHundredths / 100 })
}

// CheckPoolCIDRs checks the address ranges of a network pool: network
// addresses, IPv4, no two overlapping each other, none overlapping another
// pool of the same cluster (others, by pool id) or the cluster's reserved
// ranges (02-data-model §3). An overlap is CIDR_OVERLAP naming what it hits.
func CheckPoolCIDRs(cidrs []netip.Prefix, others map[string][]netip.Prefix, reserved []netip.Prefix) error {
	if len(cidrs) == 0 {
		return apperr.Invalid("cidrs", "must list at least one range")
	}
	for i, c := range cidrs {
		if !c.Addr().Is4() || c != c.Masked() {
			return apperr.Invalid("cidrs", fmt.Sprintf("%s is not an IPv4 network address", c))
		}
		for _, d := range cidrs[:i] {
			if c.Overlaps(d) {
				return overlap(c, d, "this pool")
			}
		}
		for _, r := range reserved {
			if c.Overlaps(r) {
				return overlap(c, r, "the cluster's own ranges")
			}
		}
		for pool, ranges := range others {
			for _, o := range ranges {
				if c.Overlaps(o) {
					return overlap(c, o, "network pool "+pool)
				}
			}
		}
	}
	return nil
}

func overlap(c, with netip.Prefix, where string) error { return overlapWith("cidrs", c, with, where) }

func overlapWith(field string, c, with netip.Prefix, where string) error {
	return &apperr.Error{
		Code:    apperr.CIDROverlap,
		Message: fmt.Sprintf("%s overlaps %s of %s", c, with, where),
		Details: map[string]any{"field": field, "cidr": c.String(), "conflictsWith": with.String(), "in": where},
	}
}
