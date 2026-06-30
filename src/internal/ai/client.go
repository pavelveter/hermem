package ai

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"time"
)

const (
	// defaultMaxAttempts is the maximum number of attempts (1 initial +
	// N retries) when RetryPolicy.MaxAttempts is zero. Chosen to
	// accommodate the default backoff ladder (4 slots) with room for
	// extrapolation before the wall-clock guard (C1.4) kicks in.
	defaultMaxAttempts = 10
)

var (
	defaultBackoffs = []time.Duration{
		200 * time.Millisecond,
		500 * time.Millisecond,
		1 * time.Second,
		2 * time.Second,
	}

	defaultRetryableStatus = map[int]bool{
		429: true,
		500: true, 502: true, 503: true, 504: true,
	}
)

// RetryPolicy configures the retry behaviour of ResilientClient.Do.
// A zero-value RetryPolicy is valid and applies sensible defaults:
//
//   - MaxAttempts → defaultMaxAttempts (10)
//   - Backoff     → DefaultBackoffs() (200 ms / 500 ms / 1 s / 2 s)
//   - RetryableStatus → {429, 500, 502, 503, 504}
//
// Thread-safe: a RetryPolicy may be shared across goroutines after
// construction; the map is read-only during Do.
type RetryPolicy struct {
	// MaxAttempts is the total number of attempts (initial + retries).
	// 0 → defaultMaxAttempts.
	MaxAttempts int

	// Backoff is the ordered list of sleep durations between retries.
	// nil/empty → DefaultBackoffs(). When i ≥ len(Backoff) the last
	// value is doubled each step (exponential extrapolation).
	Backoff []time.Duration

	// RetryableStatus is the set of HTTP status codes that trigger a
	// retry alongside network errors. nil/empty → defaultRetryableStatus.
	RetryableStatus map[int]bool
}

// DefaultBackoffs returns a copy of the default backoff ladder.
func DefaultBackoffs() []time.Duration {
	dst := make([]time.Duration, len(defaultBackoffs))
	copy(dst, defaultBackoffs)
	return dst
}

// resolvePolicy returns a copy of p with zero fields replaced by
// defaults. The returned Backoff slice is always non-nil and safe to
// index; RetryableStatus is always non-nil and safe to look up.
func resolvePolicy(p RetryPolicy) RetryPolicy {
	out := p
	if out.MaxAttempts <= 0 {
		out.MaxAttempts = defaultMaxAttempts
	}
	if len(out.Backoff) == 0 {
		out.Backoff = DefaultBackoffs()
	}
	if len(out.RetryableStatus) == 0 {
		out.RetryableStatus = make(map[int]bool, len(defaultRetryableStatus))
		for k, v := range defaultRetryableStatus {
			out.RetryableStatus[k] = v
		}
	}
	return out
}

// ResilientClient wraps an *http.Client with a configurable retry
// policy and is the single retry entrypoint for every external call
// site (embedder/extractor/reranker).
//
// Contract (this is the source of truth — see ADR-011):
//
//  1. Idempotency. Do is safe ONLY for idempotent requests. The wrapper
//     replays the body and re-issues the request on transient failures;
//     callers issuing non-idempotent operations (POST that mutates) must
//     set MaxAttempts=1 or accept at-least-once semantics.
//  2. Body replay. http.NewRequest does NOT set req.GetBody for arbitrary
//     io.Reader bodies; it does for *bytes.Buffer, *bytes.Reader, and
//     *strings.Reader. Callers passing custom readers MUST attach a
//     GetBody closure or the second attempt will silently replay an empty
//     body. Missing GetBody on a request that needs a retry is a caller
//     bug, not a runtime error — Do does not detect it.
//  3. What triggers a retry: network error from inner.Do, or any HTTP
//     status code listed in RetryPolicy.RetryableStatus (default:
//     429, 500, 502, 503, 504). Other 4xx are returned to the caller
//     verbatim.
//  4. Context propagation. ctx is attached to every retry-attempt's
//     cloned request AND every inter-attempt sleep. A ctx cancel
//     mid-sleep aborts the loop immediately and returns ctx.Err(). A
//     ctx cancel between the inner Do error and the next sleep also
//     short-circuits.
//  5. Body draining. On a retried response, the resp.Body is drained
//     to io.Discard and closed so the underlying TCP connection can
//     return to the keep-alive pool instead of being RST.
//  6. Default policy. A zero-value ResilientClient{} is valid: Inner
//     falls back to http.DefaultClient, Policy fields are resolved
//     via resolvePolicy (see above).
//  7. No wall-clock guard yet — see C1.4. The parent ctx deadline is
//     today's only ceiling on total retry duration.
//  8. Thread-safety. ResilientClient is stateless after construction so
//     a single instance can be shared across goroutines. Backoff is
//     copied on resolvePolicy, never mutated in place.
type ResilientClient struct {
	Inner  *http.Client // nil → http.DefaultClient
	Policy RetryPolicy  // zero-value → resolvePolicy defaults
}

// NewResilientClient is the only constructor callers should use. The
// zero-value ResilientClient{} also works (defaults kick in on Do) but
// prefer this for explicit intent at the call site.
func NewResilientClient(inner *http.Client, policy RetryPolicy) *ResilientClient {
	return &ResilientClient{Inner: inner, Policy: policy}
}

// Do issues req with ctx attached and applies the configured backoff
// ladder on transient failures. Returns the first response whose
// status code is NOT in RetryPolicy.RetryableStatus, or the last
// error after attempts are exhausted.
//
// See the ResilientClient type comment for the full contract
// (idempotency, body-replay, ctx propagation, body-draining).
func (c *ResilientClient) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	inner := c.Inner
	if inner == nil {
		inner = http.DefaultClient
	}
	p := resolvePolicy(c.Policy)

	var lastErr error
	for i := 0; i < p.MaxAttempts; i++ {
		if i > 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if req.GetBody != nil {
				body, err := req.GetBody()
				if err != nil {
					return nil, fmt.Errorf("retry: get body: %w", err)
				}
				req.Body = body
			}
		}
		clone := req.Clone(ctx)
		resp, err := inner.Do(clone)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if !backoffSleep(ctx, backoffFor(p.Backoff, i)) {
				return nil, ctx.Err()
			}
			continue
		}
		if p.RetryableStatus[resp.StatusCode] {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d (transient)", resp.StatusCode)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if !backoffSleep(ctx, backoffFor(p.Backoff, i)) {
				return nil, ctx.Err()
			}
			continue
		}
		return resp, nil
	}
	return nil, lastErr
}

// backoffFor returns the sleep duration before the i-th retry
// (zero-based). If i exceeds len(backoffs) (more attempts than slots),
// double the last value as the final exp-backoff step.
func backoffFor(backoffs []time.Duration, i int) time.Duration {
	if i < len(backoffs) {
		return backoffs[i]
	}
	last := backoffs[len(backoffs)-1]
	return last * 2
}

// backoffSleep blocks for d plus a small jitter, OR returns false the
// instant ctx is cancelled. Returns true after a normal sleep so the
// caller keeps retrying, false on cancellation so the caller can
// propagate ctx.Err() upward.
func backoffSleep(ctx context.Context, d time.Duration) bool {
	jitter := time.Duration(rand.Int63n(int64(d)/4 + 1))
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d + jitter):
		return true
	}
}
