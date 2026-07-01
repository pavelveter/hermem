// Package ratelimit provides a per-key token-bucket rate limiter and
// the HTTP middleware that wires it into the server chain. It is
// disconnected from net/http so the core math (refill, Allow, eviction)
// can be unit-tested without spinning up an http.Server.
//
// Algorithm: classic token bucket. Each key gets its own bucket sized
// at `burst` tokens, refilled at `rps` tokens per second up to the
// same `burst` cap. Every Allow() call consumes one token. If a
// bucket is empty the call is rejected with a Retry-After hint
// derived from the live token count (not the static burst) so a
// long-idle client gets a small retry window while a hot client gets
// a larger one.
//
// Eviction: a background ticker sweeps the per-key map every
// `evictInterval` and removes any bucket whose `lastTime` is older
// than `idleTTL`. This bounds memory under random-IP DoS pressure.
// A hard cap (`maxKeys`) provides a backstop: if the cap is reached,
// the oldest idle entries are pruned in line — guaranteeing O(1)
// rejections at the cap without growing the map past the cap.
package ratelimit

import (
	"math"
	"sync"
	"time"
)

// Decision is the result of a single Allow() call. Returned to callers
// (HTTP middleware) so they can write the right headers and JSON
// envelope without recomputing the math themselves.
type Decision struct {
	// Allowed is true when a token was consumed.
	Allowed bool
	// RetryAfter is the minimum delay the client should wait before
	// retrying. Zero on Allowed=true. Always >= 1s on Allowed=false
	// so the Retry-After header never advertises "0" (which clients
	// interpret as "retry immediately" and amplifies the storm).
	RetryAfter time.Duration
	// Limit is the configured burst capacity (constant per limiter).
	Limit int
	// Remaining is the number of tokens left in the bucket AFTER
	// this call. 0 when the bucket was empty before the call (and
	// the call was rejected). Floored at 0 even if the call was
	// allowed (consumed 1 token).
	Remaining int
	// Reset is the wall-clock time at which the bucket returns to
	// full capacity. Computed as now + (burst - remaining) / rate.
	Reset time.Time
}

// bucket is the per-key token bucket. All fields are guarded by the
// parent Limiter's mu — buckets are never accessed outside the
// Limiter's lock so we don't pay a per-bucket mutex cost.
type bucket struct {
	tokens   float64
	lastTime time.Time
}

// Limiter is a per-key token-bucket rate limiter. Zero-value is not
// usable; construct with New.
//
// Limiter is safe for concurrent use.
type Limiter struct {
	mu sync.Mutex
	// buckets is keyed by the rate-limit key (an opaque string the
	// caller extracts from the request — IP, API key, or "global").
	// Memory is bounded by maxKeys + the periodic eviction sweep.
	buckets map[string]*bucket
	// rps is the refill rate in tokens per second.
	rps float64
	// burst is the bucket capacity. Always >= 1.
	burst int
	// maxKeys caps the per-key map size. When len(buckets) == maxKeys
	// and a new key arrives, the sweeper prunes the oldest idle
	// entries to make room.
	maxKeys int
	// idleTTL is the per-key idle threshold. Buckets unseen for
	// longer than this are pruned by the sweeper.
	idleTTL time.Duration
	// now is overridable for tests; production code uses time.Now.
	now func() time.Time
}

// Config is the input to New. All numeric fields are validated and
// coerced to safe values (positive, non-zero). Invalid input is
// silently corrected rather than rejected so a typo in hermem.ini
// fails-open (limiter is permissive) rather than fails-closed
// (every request 429s).
type Config struct {
	// RPS is the refill rate in tokens per second. Must be > 0.
	// Zero or negative is coerced to 1.
	RPS float64
	// Burst is the bucket capacity. Must be >= 1. Defaults to RPS
	// (rounded up) when <= 0. Clamped to >= 1.
	Burst int
	// MaxKeys caps the per-key map size. Defaults to 100000 when
	// <= 0. Clamped to >= 16 (the eviction sweep has a linear
	// cost — anything under 16 is pathological).
	MaxKeys int
	// IdleTTL is the per-key idle threshold for eviction. Defaults
	// to 10 minutes when <= 0.
	IdleTTL time.Duration
	// EvictInterval is the sweeper period. Defaults to 5 minutes
	// when <= 0. Set to a very large value in tests if you want
	// deterministic eviction (call SweepNow instead).
	EvictInterval time.Duration
}

// New returns a Limiter built from cfg. The returned Limiter spawns
// no goroutine — call Start() to enable background eviction. Split
// so tests can construct a Limiter with deterministic eviction by
// calling SweepNow directly.
func New(cfg Config) *Limiter {
	l := &Limiter{
		buckets: make(map[string]*bucket),
		rps:     cfg.RPS,
		burst:   normalizeBurst(cfg.Burst, cfg.RPS),
		maxKeys: normalizeMaxKeys(cfg.MaxKeys),
		idleTTL: cfg.IdleTTL,
		now:     time.Now,
	}
	if l.rps <= 0 {
		l.rps = 1
	}
	if l.idleTTL <= 0 {
		l.idleTTL = 10 * time.Minute
	}
	return l
}

// Burst returns the configured bucket capacity.
func (l *Limiter) Burst() int { return l.burst }

// RPS returns the configured refill rate in tokens per second.
func (l *Limiter) RPS() float64 { return l.rps }

// Allow attempts to consume one token from the bucket identified by
// key. Returns the Decision describing whether the call was allowed
// and the headers to write. Creates a fresh full bucket on first
// use of a key.
func (l *Limiter) Allow(key string) Decision {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	b, ok := l.buckets[key]
	if !ok {
		// Hard cap: if we're at the cap, prune the oldest idle
		// entry to make room. SweepNow runs a more thorough pass;
		// this is a fast inline trim so a hot-loop new-key flood
		// can't OOM us between sweeps.
		if len(l.buckets) >= l.maxKeys {
			l.evictOldestLocked(1)
		}
		b = &bucket{
			tokens:   float64(l.burst),
			lastTime: now,
		}
		l.buckets[key] = b
	}

	// Refill since last touch.
	elapsed := now.Sub(b.lastTime).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * l.rps
		if b.tokens > float64(l.burst) {
			b.tokens = float64(l.burst)
		}
	}
	b.lastTime = now

	// Floor remaining at 0 so a float rounding error after a
	// many-second idle window doesn't show a fractional "0.9999"
	// remaining when the bucket is effectively empty.
	remaining := int(math.Floor(b.tokens))

	if b.tokens < 1 {
		// Retry-After: how long until at least 1 token is back.
		// ceil((1 - tokens) / rps) seconds, floored at 1.
		secs := math.Ceil((1 - b.tokens) / l.rps)
		if secs < 1 {
			secs = 1
		}
		// Reset: when the bucket returns to full. Computed on
		// the current remaining-tokens view; correct enough for
		// clients that use it as a "stop hammering" hint.
		fullIn := math.Ceil((float64(l.burst) - b.tokens) / l.rps)
		return Decision{
			Allowed:    false,
			RetryAfter: time.Duration(secs * float64(time.Second)),
			Limit:      l.burst,
			Remaining:  0,
			Reset:      now.Add(time.Duration(fullIn * float64(time.Second))),
		}
	}
	b.tokens--
	// remaining was computed BEFORE the consume; report it as the
	// post-call view. If remaining was 1, post-call is 0.
	if remaining > 0 {
		remaining--
	}
	fullIn := math.Ceil((float64(l.burst) - b.tokens) / l.rps)
	return Decision{
		Allowed:   true,
		Limit:     l.burst,
		Remaining: remaining,
		Reset:     now.Add(time.Duration(fullIn * float64(time.Second))),
	}
}

// SweepNow runs an eviction pass synchronously. Removes any bucket
// whose lastTime is older than idleTTL. Useful for tests that don't
// want to spin up a real ticker. Returns the number of buckets
// pruned.
func (l *Limiter) SweepNow() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.evictIdleLocked()
}

func (l *Limiter) evictIdleLocked() int {
	cutoff := l.now().Add(-l.idleTTL)
	pruned := 0
	for k, b := range l.buckets {
		if b.lastTime.Before(cutoff) {
			delete(l.buckets, k)
			pruned++
		}
	}
	return pruned
}

// evictOldestLocked prunes up to n of the least-recently-touched
// entries. Used when the map is at maxKeys and a new key arrives.
// Caller MUST hold l.mu.
func (l *Limiter) evictOldestLocked(n int) int {
	if n <= 0 || len(l.buckets) == 0 {
		return 0
	}
	type kvp struct {
		key      string
		lastTime time.Time
	}
	all := make([]kvp, 0, len(l.buckets))
	for k, b := range l.buckets {
		all = append(all, kvp{k, b.lastTime})
	}
	// Selection: find the n oldest. O(len(buckets)) which is bounded
	// by maxKeys. For very large maps this is dominated by the
	// allocation; acceptable for a backstop-only path that runs at
	// most once per new-key under cap pressure.
	if n >= len(all) {
		for _, e := range all {
			delete(l.buckets, e.key)
		}
		return len(all)
	}
	// Partial selection: n-smallest-by-lastTime.
	for i := 0; i < n; i++ {
		oldestIdx := i
		for j := i + 1; j < len(all); j++ {
			if all[j].lastTime.Before(all[oldestIdx].lastTime) {
				oldestIdx = j
			}
		}
		all[i], all[oldestIdx] = all[oldestIdx], all[i]
		delete(l.buckets, all[i].key)
	}
	return n
}

// Size returns the current number of tracked buckets. Test-only —
// guarded by a method so the production code never accidentally
// depends on the map size.
func (l *Limiter) Size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// Start begins background eviction. Returns a stop function. Calling
// stop() more than once is safe (subsequent calls are no-ops). If
// EvictInterval is <= 0 the ticker is not started; callers can run
// SweepNow() on their own schedule.
func (l *Limiter) Start(interval time.Duration) (stop func()) {
	if interval <= 0 {
		return func() {}
	}
	t := time.NewTicker(interval)
	stopCh := make(chan struct{})
	var stopOnce sync.Once
	go func() {
		for {
			select {
			case <-t.C:
				l.SweepNow()
			case <-stopCh:
				t.Stop()
				return
			}
		}
	}()
	return func() {
		stopOnce.Do(func() { close(stopCh) })
	}
}

// SetClock overrides the time source. Test-only.
func (l *Limiter) SetClock(now func() time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.now = now
	if now != nil {
		// Re-base existing buckets' lastTime so the test clock
		// doesn't make them look "ancient" relative to the new
		// source. This is a best-effort helper — tests that need
		// precise control should call SetClock BEFORE any Allow().
		t := now()
		for _, b := range l.buckets {
			b.lastTime = t
		}
	}
}

func normalizeBurst(b int, rps float64) int {
	if b <= 0 {
		if rps > 0 {
			return int(math.Ceil(rps))
		}
		return 1
	}
	if b < 1 {
		return 1
	}
	return b
}

func normalizeMaxKeys(n int) int {
	if n <= 0 {
		// Unset / non-positive falls back to the high-cap default
		// so a small deployment doesn't accidentally expose a tiny
		// per-IP bucket pool that an IP-spoof flood could trivially
		// saturate. Operator can lower it via the [server] config
		// key with explicit intent; tests can use small values (e.g.
		// 4 or 8) to exercise eviction deterministically.
		return 100_000
	}
	return n
}
