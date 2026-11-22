package reservation

import (
	"fmt"
	"net/netip"
	"slices"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/store"
)

// IPAM: Pod address blocks of Workspaces out of a network pool
// (PRODUCT-PLAN §7.2). Pure functions on prefixes; the caller holds the
// pool's lock and passes every block of the cluster not Released.

// overlaps tells whether p overlaps any of others.
func overlaps(p netip.Prefix, others []netip.Prefix) bool {
	for _, o := range others {
		if p.Overlaps(o) {
			return true
		}
	}
	return false
}

// NextFree is the first block of size bits in ranges, in order, that
// overlaps neither taken nor reserved; ok is false when none is left.
func NextFree(ranges []netip.Prefix, bits int, taken, reserved []netip.Prefix) (netip.Prefix, bool) {
	for _, r := range ranges {
		r = r.Masked()
		if bits < r.Bits() || bits > 32 {
			continue
		}
		// uint64: one past the top range of the address space is 2^32.
		size := uint64(1) << (32 - bits)
		start := uint64(addr4(r.Addr()))
		end := start + uint64(1)<<(32-r.Bits())
		for a := start; a < end; a += size {
			p := netip.PrefixFrom(fromUint(uint32(a)), bits)
			if !overlaps(p, taken) && !overlaps(p, reserved) {
				return p, true
			}
		}
	}
	return netip.Prefix{}, false
}

// Within tells whether p lies inside one of ranges.
func Within(p netip.Prefix, ranges []netip.Prefix) bool {
	for _, r := range ranges {
		if r.Bits() <= p.Bits() && r.Contains(p.Addr()) {
			return true
		}
	}
	return false
}

func addr4(a netip.Addr) uint32 {
	b := a.As4()
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func fromUint(u uint32) netip.Addr {
	return netip.AddrFrom4([4]byte{byte(u >> 24), byte(u >> 16), byte(u >> 8), byte(u)})
}

// Block picks a Workspace's address block out of a locked network pool
// (PRODUCT-PLAN §7.2): a requested CIDR (advanced) must lie in the pool,
// else CIDR_OUTSIDE_ALLOWED_POOL, and overlap nothing taken in the cluster
// nor its reserved ranges, else CIDR_OVERLAP (in that order: 04 §4);
// otherwise the first free block of the prefix length, the pool's default
// when bits is 0, else NETWORK_POOL_EXHAUSTED.
func Block(pool store.NetworkPool, bits int32, requested *netip.Prefix, taken, reserved []netip.Prefix) (netip.Prefix, error) {
	if bits == 0 {
		bits = pool.DefaultPrefixLength
		if requested != nil {
			bits = int32(requested.Bits())
		}
	}
	if !slices.Contains(pool.AllowedPrefixLengths, bits) {
		return netip.Prefix{}, apperr.Invalid("network.prefixLength", fmt.Sprintf("/%d is not one of the pool's lengths %v", bits, pool.AllowedPrefixLengths))
	}
	if requested != nil {
		r := requested.Masked()
		if r != *requested || int32(r.Bits()) != bits {
			return netip.Prefix{}, apperr.Invalid("network.requestedCidr", "must be a network address of the prefix length")
		}
		if !Within(r, pool.Cidrs) {
			return netip.Prefix{}, &apperr.Error{Code: apperr.CIDROutsideAllowedPool, Message: fmt.Sprintf("%s is outside pool %s", r, pool.ID),
				Details: map[string]any{"field": "network.requestedCidr", "networkPoolId": pool.ID}}
		}
		if overlaps(r, taken) || overlaps(r, reserved) {
			return netip.Prefix{}, ErrCIDROverlap(r)
		}
		return r, nil
	}
	p, ok := NextFree(pool.Cidrs, int(bits), taken, reserved)
	if !ok {
		return netip.Prefix{}, &apperr.Error{Code: apperr.NetworkPoolExhausted,
			Message: fmt.Sprintf("pool %s has no free /%d left", pool.ID, bits), Details: map[string]any{"networkPoolId": pool.ID}}
	}
	return p, nil
}

// ErrCIDROverlap is the answer to a block another holds, also when the
// exclusion constraint catches it (SQLSTATE 23P01).
func ErrCIDROverlap(p netip.Prefix) error {
	return &apperr.Error{Code: apperr.CIDROverlap, Message: fmt.Sprintf("%s overlaps a block in use in the cluster", p),
		Details: map[string]any{"field": "network.requestedCidr"}}
}

// FreeBlocks counts the blocks of size bits in ranges that overlap neither
// taken nor reserved.
func FreeBlocks(ranges []netip.Prefix, bits int, taken, reserved []netip.Prefix) int {
	n := 0
	for _, r := range ranges {
		r = r.Masked()
		if bits < r.Bits() || bits > 32 {
			continue
		}
		size := uint64(1) << (32 - bits)
		start := uint64(addr4(r.Addr()))
		end := start + uint64(1)<<(32-r.Bits())
		for a := start; a < end; a += size {
			p := netip.PrefixFrom(fromUint(uint32(a)), bits)
			if !overlaps(p, taken) && !overlaps(p, reserved) {
				n++
			}
		}
	}
	return n
}
