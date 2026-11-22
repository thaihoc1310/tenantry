package httpapi

import (
	"fmt"
	"math"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/thaihoc1310/tenantry/backend/internal/apperr"
	"github.com/thaihoc1310/tenantry/backend/internal/httpapi/oapi"
)

// Limits from architecture/06-security-and-identity.md §6.
const (
	loginsPerMinute = 20  // /auth/login and /auth/callback, per client IP
	writesPerMinute = 120 // writes under /api/v1, per user
)

// limiter is a token bucket per key (a user id or a client IP), in memory.
// Buckets belong to this replica: exact while the API runs one replica,
// looser with more (06 §6 names the way out).
type limiter struct {
	perMinute int
	mu        sync.Mutex
	buckets   map[string]*rate.Limiter
	swept     time.Time
}

func newLimiter(perMinute int) *limiter {
	return &limiter{perMinute: perMinute, buckets: map[string]*rate.Limiter{}}
}

// take spends one token of key's bucket, or says how long until one is free.
func (l *limiter) take(key string, now time.Time) (wait time.Duration, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// A full bucket behaves like a new one, so it can go; this keeps the
	// map to the keys seen in about the last minute.
	if now.Sub(l.swept) > time.Minute {
		for k, b := range l.buckets {
			if b.TokensAt(now) >= float64(l.perMinute) {
				delete(l.buckets, k)
			}
		}
		l.swept = now
	}
	b, found := l.buckets[key]
	if !found {
		b = rate.NewLimiter(rate.Limit(float64(l.perMinute)/60), l.perMinute)
		l.buckets[key] = b
	}
	res := b.ReserveN(now, 1)
	if d := res.DelayFrom(now); d > 0 {
		res.CancelAt(now) // a refused request costs nothing
		return d, false
	}
	return 0, true
}

// tooMany sets Retry-After (whole seconds, rounded up) and returns the error to answer with.
func tooMany(w http.ResponseWriter, wait time.Duration) error {
	secs := int(math.Ceil(wait.Seconds()))
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	return apperr.New(apperr.RateLimited, fmt.Sprintf("too many requests; retry in %d seconds", secs))
}

// limitWrites holds each user to writesPerMinute writes. Reads are free.
func limitWrites(l *limiter) oapi.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
			default:
				if wait, ok := l.take(userID(r.Context()), time.Now()); !ok {
					writeError(w, r, tooMany(w, wait))
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// limitLogins holds each client IP to loginsPerMinute requests of h, a
// browser navigation, so the refusal is a page.
func limitLogins(l *limiter, trusted []netip.Prefix, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if wait, ok := l.take(clientIP(r, trusted), time.Now()); !ok {
			loginFailed(w, r, tooMany(w, wait))
			return
		}
		h(w, r)
	}
}

// clientIP is the address the request came from. Behind a trusted proxy
// (Traefik) it is read from X-Forwarded-For: the rightmost address that is
// not a trusted proxy, since everything left of it was written by the client
// and can be anything. From anyone else the header is ignored.
func clientIP(r *http.Request, trusted []netip.Prefix) string {
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	ip := ap.Addr().Unmap()
	isTrusted := func(a netip.Addr) bool {
		for _, p := range trusted {
			if p.Contains(a) {
				return true
			}
		}
		return false
	}
	if !isTrusted(ip) {
		return ip.String()
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break // garbage: the last proxy we trust is the best we know
		}
		ip = a.Unmap()
		if !isTrusted(ip) {
			break
		}
	}
	return ip.String()
}
