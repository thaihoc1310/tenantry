package httpapi

import (
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

func TestClientIP(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("192.168.0.0/16"), netip.MustParsePrefix("fd00::/8")}
	cases := []struct {
		name, remote string
		xff          []string
		want         string
	}{
		{"direct, no header", "203.0.113.5:4000", nil, "203.0.113.5"},
		{"direct client cannot pick its address", "203.0.113.5:4000", []string{"198.51.100.1"}, "203.0.113.5"},
		{"through the proxy", "192.168.211.79:5000", []string{"198.51.100.7"}, "198.51.100.7"},
		{"what the client wrote is skipped", "192.168.211.79:5000", []string{"10.0.0.1, 198.51.100.7"}, "198.51.100.7"},
		{"split over several headers", "192.168.211.79:5000", []string{"10.0.0.1", "198.51.100.7, 192.168.1.1"}, "198.51.100.7"},
		{"proxy without the header", "192.168.211.79:5000", nil, "192.168.211.79"},
		{"garbage stops the walk", "192.168.211.79:5000", []string{"198.51.100.7, nonsense"}, "192.168.211.79"},
		{"IPv6 client", "[fd00::1]:5000", []string{"2001:db8::7"}, "2001:db8::7"},
		{"IPv4-mapped proxy address", "[::ffff:192.168.211.79]:5000", []string{"198.51.100.7"}, "198.51.100.7"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/auth/login", nil)
		r.RemoteAddr = c.remote
		for _, v := range c.xff {
			r.Header.Add("X-Forwarded-For", v)
		}
		if got := clientIP(r, trusted); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

func TestLimiter(t *testing.T) {
	l := newLimiter(60) // one token a second, 60 at once
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	for i := range 60 {
		if _, ok := l.take("a", now); !ok {
			t.Fatalf("request %d refused within the budget", i+1)
		}
	}
	wait, ok := l.take("a", now)
	if ok || wait != time.Second {
		t.Fatalf("over the budget: ok=%v wait=%s, want a 1s wait", ok, wait)
	}
	// A refused request costs nothing: one second later one token is back.
	if _, ok := l.take("a", now.Add(time.Second)); !ok {
		t.Fatal("token not back after its interval")
	}
	if _, ok := l.take("b", now); !ok {
		t.Fatal("one key's budget spent another's")
	}

	// Buckets full again are dropped on the next sweep, others are kept.
	l.take("c", now.Add(59*time.Second))
	l.take("d", now.Add(2*time.Minute))
	if _, kept := l.buckets["a"]; kept {
		t.Error("full bucket a kept")
	}
	if _, kept := l.buckets["d"]; !kept {
		t.Error("bucket in use dropped")
	}
}
