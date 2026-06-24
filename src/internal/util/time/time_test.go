package time

import (
	"testing"
	stdtime "time"
)

// TestNowUTCUnix_WithinOneSecondOfStdNow — NowUTCUnix must return a value
// very close to (≤1s behind) stdtime.Now().Unix(). A drift beyond that
// suggests the helper is reading a non-monotonic clock or skipping UTC.
func TestNowUTCUnix_WithinOneSecondOfStdNow(t *testing.T) {
	a := NowUTCUnix()
	b := stdtime.Now().UTC().Unix()
	if a < b-1 || a > b+1 {
		t.Fatalf("NowUTCUnix=%d, stdtime.Now().UTC().Unix()=%d (allowed ±1s drift)", a, b)
	}
}

// TestNowUTCUnix_IncreasesAcrossCalls — two consecutive calls must be
// equal or strictly increasing. Monotonicity is the core contract: any
// downstream use (epoch comparisons, cache TTLs) trusts this.
func TestNowUTCUnix_IncreasesAcrossCalls(t *testing.T) {
	first := NowUTCUnix()
	stdtime.Sleep(2 * stdtime.Millisecond)
	second := NowUTCUnix()
	if second < first {
		t.Fatalf("NowUTCUnix went backwards: %d → %d", first, second)
	}
}
