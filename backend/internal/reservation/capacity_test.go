package reservation

import (
	"net/netip"
	"testing"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
)

const gi = int64(1) << 30

// The dedicated node of the M8 demo: fdp-k8s-wk3, 3 CPU / 11.5Gi allocatable,
// system reserve 0.5 CPU / 1.5Gi → 2.5 CPU / 10Gi reservable.
var (
	wk3        = Capacity{CPUMilli: 3000, MemoryBytes: 23 * gi / 2, Pods: 110}
	wk3Reserve = Capacity{CPUMilli: 500, MemoryBytes: 3 * gi / 2, Pods: 10}
)

func TestReservableAndRemaining(t *testing.T) {
	r := Reservable(wk3, wk3Reserve)
	if r != (Capacity{CPUMilli: 2500, MemoryBytes: 10 * gi, Pods: 100}) {
		t.Fatalf("reservable %+v", r)
	}
	if got := Reservable(Capacity{CPUMilli: 400}, Capacity{CPUMilli: 500}); got.CPUMilli != 0 {
		t.Fatalf("a reserve above allocatable leaves %d, want 0", got.CPUMilli)
	}
	water := Capacity{CPUMilli: 1000, MemoryBytes: 4 * gi, Pods: 10}
	two := water.Add(water)
	if got := Remaining(r, two); got != (Capacity{CPUMilli: 500, MemoryBytes: 2 * gi, Pods: 80}) {
		t.Fatalf("remaining %+v", got)
	}
	if Fits(r, two, Capacity{CPUMilli: 1000}) {
		t.Fatal("a third 1 CPU Workspace fits in 0.5 CPU")
	}
	if !Fits(r, two, Capacity{CPUMilli: 500, MemoryBytes: 2 * gi}) {
		t.Fatal("exactly the rest does not fit")
	}
	if got := Remaining(r, r.Add(water)); got != (Capacity{}) {
		t.Fatalf("remaining of an overcommitted node %+v, want zero", got)
	}
}

func TestCapacityState(t *testing.T) {
	r := Capacity{CPUMilli: 2500, MemoryBytes: 10 * gi, Pods: 100}
	cases := []struct {
		name     string
		reserved Capacity
		want     string
	}{
		{"nothing reserved", Capacity{}, Available},
		{"some of each", Capacity{CPUMilli: 1000, MemoryBytes: 4 * gi, Pods: 10}, PartiallyAllocated},
		{"CPU full, memory left", Capacity{CPUMilli: 2500, MemoryBytes: 4 * gi}, FullyAllocated},
		{"everything full", r, FullyAllocated},
		{"memory over after the snapshot shrank", Capacity{CPUMilli: 1000, MemoryBytes: 11 * gi}, Overcommitted},
		{"over beats full", Capacity{CPUMilli: 2500, MemoryBytes: 11 * gi}, Overcommitted},
	}
	for _, c := range cases {
		if got := CapacityState(r, c.reserved); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	// A node not reported yet (allocatable 0) with nothing on it is Available.
	if got := CapacityState(Capacity{}, Capacity{}); got != Available {
		t.Errorf("unreported node: %s", got)
	}
	// The transition to Overcommitted: the same reservations, a smaller snapshot.
	reserved := Capacity{CPUMilli: 2000, MemoryBytes: 8 * gi, Pods: 20}
	before := CapacityState(Reservable(wk3, wk3Reserve), reserved)
	shrunk := wk3
	shrunk.MemoryBytes = 9 * gi
	after := CapacityState(Reservable(shrunk, wk3Reserve), reserved)
	if before != PartiallyAllocated || after != Overcommitted {
		t.Fatalf("snapshot shrink: %s → %s, want PartiallyAllocated → Overcommitted", before, after)
	}
}

// The example of architecture/05 §4: two nodes, 24 CPU / 96Gi / 330 pods in
// total, reserve 1 CPU / 4Gi / 15 pods each, ratio 3.0.
func TestPoolCeiling(t *testing.T) {
	nodes := []Capacity{{CPUMilli: 12000, MemoryBytes: 48 * gi, Pods: 165}, {CPUMilli: 12000, MemoryBytes: 48 * gi, Pods: 165}}
	r := PoolReservable(nodes, Capacity{CPUMilli: 1000, MemoryBytes: 4 * gi, Pods: 15})
	if r != (Capacity{CPUMilli: 22000, MemoryBytes: 88 * gi, Pods: 300}) {
		t.Fatalf("reservable %+v", r)
	}
	if c := PoolCeiling(r, 300); c != (Capacity{CPUMilli: 66000, MemoryBytes: 264 * gi, Pods: 900}) {
		t.Fatalf("ceiling %+v", c)
	}
	if c := PoolCeiling(Capacity{CPUMilli: 1000}, 250); c.CPUMilli != 2500 {
		t.Fatalf("ratio 2.5: %d", c.CPUMilli)
	}
	if PoolReservable(nil, Capacity{CPUMilli: 1}) != (Capacity{}) {
		t.Fatal("an empty pool offers something")
	}
}

func TestCheckPoolCIDRs(t *testing.T) {
	p := netip.MustParsePrefix
	reserved := []netip.Prefix{p("192.168.0.0/16"), p("10.96.0.0/12")}
	others := map[string][]netip.Prefix{"onboarding-pod-network": {p("10.240.0.0/18")}}
	ok := [][]netip.Prefix{
		{p("10.240.64.0/18")},
		{p("10.240.128.0/18"), p("10.240.192.0/18")},
		{p("172.16.0.0/16")},
	}
	for _, c := range ok {
		if err := CheckPoolCIDRs(c, others, reserved); err != nil {
			t.Errorf("%v: %v", c, err)
		}
	}
	bad := []struct {
		cidrs []netip.Prefix
		code  apperr.Code
	}{
		{nil, apperr.Validation},
		{[]netip.Prefix{p("10.240.64.1/18")}, apperr.Validation},                     // not a network address
		{[]netip.Prefix{p("fd00::/64")}, apperr.Validation},                          // IPv6
		{[]netip.Prefix{p("10.240.0.0/16")}, apperr.CIDROverlap},                     // covers another pool
		{[]netip.Prefix{p("10.240.32.0/24")}, apperr.CIDROverlap},                    // inside another pool
		{[]netip.Prefix{p("192.168.10.0/24")}, apperr.CIDROverlap},                   // the Pod CIDR
		{[]netip.Prefix{p("10.100.0.0/16")}, apperr.CIDROverlap},                     // inside the Service CIDR
		{[]netip.Prefix{p("172.16.0.0/16"), p("172.16.8.0/24")}, apperr.CIDROverlap}, // its own two ranges
	}
	for _, c := range bad {
		err := CheckPoolCIDRs(c.cidrs, others, reserved)
		if e, ok := apperr.As(err); !ok || e.Code != c.code {
			t.Errorf("%v: %v, want %s", c.cidrs, err, c.code)
		}
	}
	err := CheckPoolCIDRs([]netip.Prefix{p("10.240.32.0/24")}, others, reserved)
	if e, _ := apperr.As(err); e.Details["conflictsWith"] != "10.240.0.0/18" || e.Details["in"] != "network pool onboarding-pod-network" {
		t.Fatalf("details %+v", e.Details)
	}
}
