package ratelimit

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
)

// KeyFunc extracts the rate-limit key from a request. Returning ""
// means "use the global bucket" (one bucket for the whole server).
type KeyFunc func(r *http.Request) string

// Options configures Middleware.
type Options struct {
	// Limiter is the underlying token-bucket state. Required.
	Limiter *Limiter
	// KeyFunc extracts the per-request key. Required.
	// Pre-built helpers: KeyByIP, KeyByAPIKey, KeyByGlobal.
	KeyFunc KeyFunc
	// ShouldBypass returns true for paths that must NOT be rate-
	// limited (e.g. /health/* for K8s probes, /metrics for
	// Prometheus). Returning true short-circuits to the next
	// handler with no header writes. If nil, nothing is bypassed.
	ShouldBypass func(r *http.Request) bool
}

// Middleware returns an http.Handler middleware that consults the
// limiter for every request, sets X-RateLimit-* headers, and
// short-circuits with 429 + Retry-After when a request is rejected.
//
// The middleware writes headers on BOTH 200 and 429 responses. The
// X-RateLimit-Remaining / X-RateLimit-Reset headers on 200 responses
// let well-behaved clients throttle themselves before the server
// has to.
func Middleware(opts Options) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if opts.ShouldBypass != nil && opts.ShouldBypass(r) {
				next.ServeHTTP(w, r)
				return
			}
			key := opts.KeyFunc(r)
			dec := opts.Limiter.Allow(key)
			writeRateLimitHeaders(w, dec)
			if !dec.Allowed {
				w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(dec.RetryAfter.Seconds()))))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":"rate_limited"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// writeRateLimitHeaders sets the X-RateLimit-Limit / Remaining / Reset
// triplet per the de-facto convention used by GitHub, Stripe, etc.
// Reset is the Unix timestamp (seconds) at which the bucket returns
// to full. Limit and Remaining are integer token counts.
func writeRateLimitHeaders(w http.ResponseWriter, dec Decision) {
	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(dec.Limit))
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(dec.Remaining))
	w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(dec.Reset.Unix(), 10))
}

// BypassHealthAndMetrics returns a ShouldBypass that exempts /health/*
// (K8s liveness/readiness/startup probes) and /metrics (Prometheus
// scrapes). Exported so the server can pass it to Middleware.Options.
func BypassHealthAndMetrics() func(r *http.Request) bool {
	return func(r *http.Request) bool {
		p := r.URL.Path
		return p == "/health" || strings.HasPrefix(p, "/health/") || p == "/metrics"
	}
}

// KeyByIP returns a KeyFunc that keys on the client IP extracted from
// r.RemoteAddr. Uses net.SplitHostPort so the bucket is shared across
// every connection from the same client (otherwise every TCP socket
// gets its own bucket and the limiter is effectively disabled).
//
// V1 deliberately does NOT trust X-Forwarded-For. Trusting XFF
// without a trusted-proxy CIDR allowlist lets a client spoof its key
// and trivially bypass per-IP limits. A future enhancement can add
// a TRUSTED_PROXIES config that whitelists which IPs' XFF is honored.
//
// Edge case: an empty RemoteAddr (e.g. unix domain sockets, very old
// Go versions, or tests that construct a request without a real
// connection) maps SplitHostPort to an error and falls through to
// returning r.RemoteAddr verbatim — typically "". All such callers
// share one bucket, which is acceptable for the test/unix case but
// should be a known property for anyone deploying hermem behind a
// non-TCP listener.
func KeyByIP() KeyFunc {
	return func(r *http.Request) string {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return r.RemoteAddr
		}
		return host
	}
}

// KeyByAPIKey returns a KeyFunc that keys on the X-API-Key header
// when present and falls back to the client IP when absent. The
// fallback prevents an unauthenticated deployment from collapsing
// every request into a single "empty" bucket.
func KeyByAPIKey() KeyFunc {
	ipFn := KeyByIP()
	return func(r *http.Request) string {
		if k := r.Header.Get("X-API-Key"); k != "" {
			return "key:" + k
		}
		return "ip:" + ipFn(r)
	}
}

// KeyByGlobal returns a KeyFunc that always returns the same key —
// every request shares one global bucket. Useful for protecting
// the server against unbounded request rates regardless of source.
func KeyByGlobal() KeyFunc {
	return func(r *http.Request) string { return "global" }
}

// ResolveKeyFunc maps the human-readable config string ("ip",
// "api_key", "global") to a concrete KeyFunc. Used by the server
// to translate hermem.ini's rate_limit_key_by setting.
func ResolveKeyFunc(name string) KeyFunc {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "api_key", "apikey", "key":
		return KeyByAPIKey()
	case "global":
		return KeyByGlobal()
	case "ip", "":
		return KeyByIP()
	default:
		// Unknown key: fall through to IP rather than panic. The
		// Validate() check should have caught the bad value, but
		// this is the last line of defense.
		return KeyByIP()
	}
}
