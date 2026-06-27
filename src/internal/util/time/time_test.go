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

// TestNowUTCUnixMillis_WithinSecondOfStdNow — NowUTCUnixMillis must
// return a value within 1 second of stdtime.Now().UnixMilli() and
// must be a strict multiple of 1000-aligned ms (sub-ms fractions are
// discarded; millisecond precision is the contract).
func TestNowUTCUnixMillis_WithinSecondOfStdNow(t *testing.T) {
	a := NowUTCUnixMillis()
	b := stdtime.Now().UTC().UnixMilli()
	if a < b-1000 || a > b+1000 {
		t.Fatalf("NowUTCUnixMillis=%d, stdtime.Now().UTC().UnixMilli()=%d (allowed ±1s drift)", a, b)
	}
}

// TestNowUTCUnixMillis_IncreasesAcrossCalls — monotonicity contract
// holds at ms granularity. Two calls separated by a 2ms sleep must
// differ.
func TestNowUTCUnixMillis_IncreasesAcrossCalls(t *testing.T) {
	first := NowUTCUnixMillis()
	stdtime.Sleep(2 * stdtime.Millisecond)
	second := NowUTCUnixMillis()
	if second < first {
		t.Fatalf("NowUTCUnixMillis went backwards: %d → %d", first, second)
	}
	if second == first {
		t.Fatalf("NowUTCUnixMillis stayed equal across a 2ms sleep: %d", first)
	}
}

// TestUnixMillisRoundTrip — UnixMillisFromTime and TimeFromUnixMillis
// must be inverses for a non-zero UTC time.Time, and zero for zero.
// Sub-second precision is lost on round-trip (the helpers serialize
// at ms granularity), so a 500us offset is acceptable in the
// assertion tolerance.
func TestUnixMillisRoundTrip(t *testing.T) {
	now := stdtime.Date(2026, 6, 27, 12, 34, 56, 789_000_000, stdtime.UTC)
	ms := UnixMillisFromTime(now)
	if ms != now.UnixMilli() {
		t.Fatalf("UnixMillisFromTime(%v)=%d, want %d", now, ms, now.UnixMilli())
	}
	got := TimeFromUnixMillis(ms)
	if !got.Equal(now.Truncate(stdtime.Millisecond)) {
		t.Fatalf("round-trip lost ms: got %v, want %v", got, now.Truncate(stdtime.Millisecond))
	}
	// Zero time yields zero ms, and zero ms yields zero time.Time.
	if UnixMillisFromTime(stdtime.Time{}) != 0 {
		t.Fatalf("zero time should produce 0 ms")
	}
	if !TimeFromUnixMillis(0).IsZero() {
		t.Fatalf("0 ms should produce zero time.Time")
	}
}

// TestUnixMillisFromTime_NormalisesNonUTC — A non-UTC time.Time must
// round-trip through UTC. This is the core invariant: writers cannot
// persist a non-UTC value even if they forget to call .UTC() —
// UnixMillisFromTime normalises on the way in.
func TestUnixMillisFromTime_NormalisesNonUTC(t *testing.T) {
	// 2026-06-27 14:00:00 +02:00 == 2026-06-27 12:00:00 UTC.
	loc, err := stdtime.LoadLocation("Europe/Berlin")
	if err != nil {
		// Some test environments lack tzdata; fall back to a fixed
		// +02:00 zone constructed via FixedZone so the assertion
		// still runs.
		loc = stdtime.FixedZone("UTC+2", 2*60*60)
	}
	berlin := stdtime.Date(2026, 6, 27, 14, 0, 0, 0, loc)
	want := stdtime.Date(2026, 6, 27, 12, 0, 0, 0, stdtime.UTC)
	if got := UnixMillisFromTime(berlin); got != want.UnixMilli() {
		t.Fatalf("non-UTC %v should serialise as %v ms (got %d)", berlin, want, got)
	}
	if got := TimeFromUnixMillis(want.UnixMilli()); got.UTC() != want {
		t.Fatalf("UTC round-trip lost TZ: got %v, want %v", got, want)
	}
}
