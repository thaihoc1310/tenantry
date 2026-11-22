package reservation

import (
	"net/netip"
	"testing"
)

func TestNextFreeAndWithin(t *testing.T) {
	p := netip.MustParsePrefix
	pool := []netip.Prefix{p("10.240.0.0/18")}
	got, ok := NextFree(pool, 24, nil, nil)
	if !ok || got != p("10.240.0.0/24") {
		t.Fatalf("first block %v %v", got, ok)
	}
	// Taken and reserved blocks are skipped; a /26 inside a taken /24 counts.
	got, ok = NextFree(pool, 24, []netip.Prefix{p("10.240.0.0/24"), p("10.240.1.64/26")}, []netip.Prefix{p("10.240.2.0/23")})
	if !ok || got != p("10.240.4.0/24") {
		t.Fatalf("after taken and reserved: %v", got)
	}
	// A larger block than the range, or a full range: none.
	if _, ok := NextFree([]netip.Prefix{p("10.240.0.0/24")}, 23, nil, nil); ok {
		t.Fatal("a /23 out of a /24")
	}
	if _, ok := NextFree([]netip.Prefix{p("10.240.0.0/23")}, 24, []netip.Prefix{p("10.240.0.0/23")}, nil); ok {
		t.Fatal("a block out of a full range")
	}
	// The second range when the first is full.
	got, ok = NextFree([]netip.Prefix{p("10.240.0.0/24"), p("10.241.0.0/24")}, 25, []netip.Prefix{p("10.240.0.0/24")}, nil)
	if !ok || got != p("10.241.0.0/25") {
		t.Fatalf("second range: %v", got)
	}
	// The top of the address space does not wrap.
	if got, ok := NextFree([]netip.Prefix{p("255.255.255.0/24")}, 25, []netip.Prefix{p("255.255.255.0/25")}, nil); !ok || got != p("255.255.255.128/25") {
		t.Fatalf("top block: %v %v", got, ok)
	}

	if !Within(p("10.240.5.0/24"), pool) || Within(p("10.241.0.0/24"), pool) || Within(p("10.240.0.0/16"), pool) {
		t.Fatal("Within")
	}
}
