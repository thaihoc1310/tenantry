// Package backoff is the retry schedule every worker loop shares
// (IMPLEMENTATION-PLAN §5.1–5.3): 1s, 2s, 4s, ... capped at 5 minutes.
package backoff

import (
	"math"
	"time"
)

const (
	first   = time.Second
	ceiling = 5 * time.Minute
)

// After is the wait after the given number of consecutive failures.
func After(failures int32) time.Duration {
	if failures < 1 {
		return 0
	}
	d := first * time.Duration(math.Pow(2, float64(min(failures-1, 30))))
	return min(d, ceiling)
}
