// Tests for the HTTP rate-limit middleware: verify the 429 path,
// header math, /health bypass, key-by-IP isolation, key-by-api-key
// fallback, and the no-op behavior when nothing is wired.
package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// okHandler is a no-op inner handler that just answers 204.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
})

// TestMiddleware_BurstThenReject — first burst requests pass with
// 204, then a 429 with Retry-After is returned.
func TestMiddleware_BurstThenReject(t *testing.T) {
	l := New(Config{RPS: 1, Burst: 3})
	ref := NewLimiterRef(l)
	h := Middleware(Options{
		LimiterRef: ref,
		KeyFunc:    KeyByGlobal(),
	})(okHandler)

	for i := 0; i < 3; i++ {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest("GET", "/x", nil))
		if rr.Code != http.StatusNoContent {
			t.Fatalf("request %d: want 204, got %d", i, rr.Code)
		}
		if got := rr.Header().Get("X-RateLimit-Limit"); got != "3" {
			t.Errorf("request %d X-RateLimit-Limit: want 3, got %q", i, got)
		}
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/x", nil))
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("over-limit: want 429, got %d (body=%q)", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "rate_limited") {
		t.Errorf("over-limit body: want contains rate_limited, got %q", rr.Body.String())
	}
	if got := rr.Header().Get("Retry-After"); got == "" {
		t.Error("over-limit: want Retry-After header set, got empty")
	} else if n, err := strconv.Atoi(got); err != nil || n < 1 {
		t.Errorf("Retry-After: want positive int, got %q", got)
	}
	if got := rr.Header().Get("X-RateLimit-Remaining"); got != "0" {
		t.Errorf("over-limit X-RateLimit-Remaining: want 0, got %q", got)
	}
}

// TestMiddleware_HeadersOn200 — successful requests also carry
// X-RateLimit-* headers so clients can self-throttle.
func TestMiddleware_HeadersOn200(t *testing.T) {
	l := New(Config{RPS: 1, Burst: 10})
	ref := NewLimiterRef(l)
	h := Middleware(Options{LimiterRef: ref, KeyFunc: KeyByGlobal()})(okHandler)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/x", nil))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d", rr.Code)
	}
	if got := rr.Header().Get("X-RateLimit-Limit"); got != "10" {
		t.Errorf("X-RateLimit-Limit: want 10, got %q", got)
	}
	if got := rr.Header().Get("X-RateLimit-Remaining"); got != "9" {
		t.Errorf("X-RateLimit-Remaining: want 9 (1 consumed), got %q", got)
	}
	if got := rr.Header().Get("X-RateLimit-Reset"); got == "" {
		t.Errorf("X-RateLimit-Reset: want set, got empty")
	}
}

// TestMiddleware_BypassHealth — /health, /health/live, /health/ready
// are not rate-limited even when the global bucket is empty.
func TestMiddleware_BypassHealth(t *testing.T) {
	l := New(Config{RPS: 1, Burst: 1})
	ref := NewLimiterRef(l)
	h := Middleware(Options{
		LimiterRef:   ref,
		KeyFunc:      KeyByGlobal(),
		ShouldBypass: BypassHealthAndMetrics(),
	})(okHandler)

	// Drain the global bucket first.
	if !l.Allow("global").Allowed {
		t.Fatal("setup: want allowed")
	}
	if l.Allow("global").Allowed {
		t.Fatal("setup: global bucket should be empty")
	}

	for _, path := range []string{"/health", "/health/live", "/health/ready", "/health/startup"} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest("GET", path, nil))
		if rr.Code != http.StatusNoContent {
			t.Errorf("%s: want 204 (bypass), got %d", path, rr.Code)
		}
		// No X-RateLimit-* headers should be set on bypass paths.
		if got := rr.Header().Get("X-RateLimit-Limit"); got != "" {
			t.Errorf("%s: bypass should not set headers, got X-RateLimit-Limit=%q", path, got)
		}
	}
}

// TestMiddleware_BypassMetrics — /metrics is exempt for Prometheus.
func TestMiddleware_BypassMetrics(t *testing.T) {
	l := New(Config{RPS: 1, Burst: 1})
	ref := NewLimiterRef(l)
	h := Middleware(Options{
		LimiterRef:   ref,
		KeyFunc:      KeyByGlobal(),
		ShouldBypass: BypassHealthAndMetrics(),
	})(okHandler)
	l.Allow("global") // drain
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/metrics", nil))
	if rr.Code != http.StatusNoContent {
		t.Errorf("/metrics: want 204, got %d", rr.Code)
	}
}

// TestMiddleware_KeyByIPIsolation — different RemoteAddr endpoints
// have separate buckets. httptest.NewRequest defaults to "192.0.2.1:1234"
// unless we override.
func TestMiddleware_KeyByIPIsolation(t *testing.T) {
	l := New(Config{RPS: 1, Burst: 1})
	ref := NewLimiterRef(l)
	h := Middleware(Options{LimiterRef: ref, KeyFunc: KeyByIP()})(okHandler)

	// Exhaust IP "A".
	rrA := httptest.NewRecorder()
	reqA := httptest.NewRequest("GET", "/x", nil)
	reqA.RemoteAddr = "10.0.0.1:5000"
	h.ServeHTTP(rrA, reqA)
	if rrA.Code != http.StatusNoContent {
		t.Fatalf("A first: want 204, got %d", rrA.Code)
	}
	rrA2 := httptest.NewRecorder()
	h.ServeHTTP(rrA2, reqA)
	if rrA2.Code != http.StatusTooManyRequests {
		t.Fatalf("A second (same IP): want 429, got %d", rrA2.Code)
	}

	// IP "B" should still have a full bucket.
	rrB := httptest.NewRecorder()
	reqB := httptest.NewRequest("GET", "/x", nil)
	reqB.RemoteAddr = "10.0.0.2:5001"
	h.ServeHTTP(rrB, reqB)
	if rrB.Code != http.StatusNoContent {
		t.Errorf("B (different IP): want 204, got %d", rrB.Code)
	}
}

// TestMiddleware_KeyByIPStripsPort — net.SplitHostPort must extract
// just the host so :5000 and :5001 share a bucket. Without the
// strip, every TCP connection gets a separate bucket and IP
// limiting is a no-op.
func TestMiddleware_KeyByIPStripsPort(t *testing.T) {
	l := New(Config{RPS: 1, Burst: 1})
	ref := NewLimiterRef(l)
	h := Middleware(Options{LimiterRef: ref, KeyFunc: KeyByIP()})(okHandler)

	r1 := httptest.NewRequest("GET", "/x", nil)
	r1.RemoteAddr = "10.0.0.5:11111"
	h.ServeHTTP(httptest.NewRecorder(), r1)

	r2 := httptest.NewRequest("GET", "/x", nil)
	r2.RemoteAddr = "10.0.0.5:22222"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r2)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("same host different port: want 429 (port stripped), got %d", rr.Code)
	}
}

// TestMiddleware_KeyByAPIKey — X-API-Key is the bucket key when
// present.
func TestMiddleware_KeyByAPIKey(t *testing.T) {
	l := New(Config{RPS: 1, Burst: 2})
	ref := NewLimiterRef(l)
	h := Middleware(Options{LimiterRef: ref, KeyFunc: KeyByAPIKey()})(okHandler)

	r := httptest.NewRequest("GET", "/x", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-API-Key", "secret-a")
	h.ServeHTTP(httptest.NewRecorder(), r)
	h.ServeHTTP(httptest.NewRecorder(), r)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("secret-a over-limit: want 429, got %d", rr.Code)
	}

	// secret-b from the same IP should still be allowed.
	r2 := httptest.NewRequest("GET", "/x", nil)
	r2.RemoteAddr = "10.0.0.1:1234"
	r2.Header.Set("X-API-Key", "secret-b")
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, r2)
	if rr2.Code != http.StatusNoContent {
		t.Errorf("secret-b: want 204 (separate bucket), got %d", rr2.Code)
	}
}

// TestMiddleware_KeyByAPIKeyFallsBackToIP — when no X-API-Key is
// present, key by IP. Prevents all-no-auth traffic collapsing into
// one bucket.
func TestMiddleware_KeyByAPIKeyFallsBackToIP(t *testing.T) {
	// Burst=1 so the very NEXT request from the same IP is 429.
	l := New(Config{RPS: 1, Burst: 1})
	ref := NewLimiterRef(l)
	h := Middleware(Options{LimiterRef: ref, KeyFunc: KeyByAPIKey()})(okHandler)

	r := httptest.NewRequest("GET", "/x", nil)
	r.RemoteAddr = "10.0.0.7:1234"
	// No X-API-Key.
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("first: want 204, got %d", rr.Code)
	}
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, r)
	if rr2.Code != http.StatusTooManyRequests {
		t.Errorf("no-auth from same IP: want 429 (shared bucket), got %d", rr2.Code)
	}

	// Different IP, no auth: separate bucket — full quota available.
	r2 := httptest.NewRequest("GET", "/x", nil)
	r2.RemoteAddr = "10.0.0.8:1234"
	rr3 := httptest.NewRecorder()
	h.ServeHTTP(rr3, r2)
	if rr3.Code != http.StatusNoContent {
		t.Errorf("no-auth different IP: want 204 (separate bucket), got %d", rr3.Code)
	}
}

// TestResolveKeyFunc — the string-to-KeyFunc dispatcher in the
// config layer.
func TestResolveKeyFunc(t *testing.T) {
	cases := []struct {
		in       string
		wantSame bool // if true, two same-IP reqs share; if false, separate
	}{
		{"ip", true},
		{"IP", true},
		{"", true},         // default
		{"whoknows", true}, // unknown falls back to ip
		{"api_key", true},  // two no-auth reqs share (IP fallback)
		{"global", false},  // two reqs share same bucket → first hits, second 429
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			kf := ResolveKeyFunc(tc.in)
			got1 := kf(httptest.NewRequest("GET", "/x", nil))
			got2 := kf(httptest.NewRequest("GET", "/x", nil))
			if tc.wantSame && got1 != got2 {
				t.Errorf("%q: want same key for two empty requests, got %q vs %q", tc.in, got1, got2)
			}
		})
	}
}

// TestKeyByGlobal — every request shares one bucket.
func TestKeyByGlobal(t *testing.T) {
	kf := KeyByGlobal()
	r1 := httptest.NewRequest("GET", "/x", nil)
	r1.RemoteAddr = "10.0.0.1:1"
	r2 := httptest.NewRequest("GET", "/x", nil)
	r2.RemoteAddr = "10.0.0.2:2"
	if kf(r1) != kf(r2) {
		t.Errorf("global: want same key for different IPs, got %q vs %q", kf(r1), kf(r2))
	}
}
