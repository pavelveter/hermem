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

	// defaultMaxWallClock is the hard ceiling on total retry duration
	// when RetryPolicy.MaxWallClock is zero. 30 s accommodates 4
	// explicit backoff slots (2.9 s cumulative) plus extrapolation
	// without blocking the caller indefinitely.
	defaultMaxWallClock = 30 * time.Second
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
//   - MaxAttempts   → defaultMaxAttempts (10)
//   - Backoff       → DefaultBackoffs() (200 ms / 500 ms / 1 s / 2 s)
//   - RetryableStatus → {429, 500, 502, 503, 504}
//   - MaxWallClock  → defaultMaxWallClock (30 s)
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

	// MaxWallClock caps the total wall-clock duration of all retries
	// (from the first call to Do) regardless of ctx deadlines.
	// Once exceeded, Do returns the last error even if attempts remain.
	// 0 → defaultMaxWallClock (30 s). Negative → no guard.
	MaxWallClock time.Duration
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
	if out.MaxWallClock == 0 {
		out.MaxWallClock = defaultMaxWallClock
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
//  7. Wall-clock guard. MaxWallClock caps the total retry duration
//     independently of ctx deadlines. Once exceeded, Do returns the
//     last error even if attempts remain. Default: 30 s. Negative
//     value disables the guard (ctx is then the only ceiling).
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
// error after attempts are exhausted or the wall-clock guard fires.
//
// See the ResilientClient type comment for the full contract
// (idempotency, body-replay, ctx propagation, body-draining).
func (c *ResilientClient) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	inner := c.Inner
	if inner == nil {
		inner = http.DefaultClient
	}
	p := resolvePolicy(c.Policy)

	var deadline time.Time
	if p.MaxWallClock >= 0 {
		deadline = time.Now().Add(p.MaxWallClock)
	}

	var lastErr error
	for i := 0; i < p.MaxAttempts; i++ {
		if i > 0 {
			if err := prepareRequest(ctx, req); err != nil {
				return nil, err
			}
		}
		resp, err := executeOnce(ctx, req, inner)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if p.MaxWallClock >= 0 && time.Now().After(deadline) {
				return nil, lastErr
			}
			if !waitOrAbort(ctx, p.Backoff, i) {
				return nil, ctx.Err()
			}
			continue
		}
		if retry, rerr := classifyResponse(resp, p.RetryableStatus); retry {
			lastErr = rerr
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if p.MaxWallClock >= 0 && time.Now().After(deadline) {
				return nil, lastErr
			}
			if !waitOrAbort(ctx, p.Backoff, i) {
				return nil, ctx.Err()
			}
			continue
		}
		return resp, nil
	}
	return nil, lastErr
}

// prepareRequest checks for context cancellation and, on retries
// (attempt > 0), refreshes the request body via GetBody. Returns a
// non-nil error only when ctx is cancelled or GetBody fails.
func prepareRequest(ctx context.Context, req *http.Request) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return fmt.Errorf("retry: get body: %w", err)
		}
		req.Body = body
	}
	return nil
}

// executeOnce clones req with ctx and issues it via inner.Do. The
// clone ensures the per-call ctx override is attached while the
// original req's URL/method/headers are preserved for retries.
func executeOnce(ctx context.Context, req *http.Request, inner *http.Client) (*http.Response, error) {
	clone := req.Clone(ctx)
	return inner.Do(clone)
}

// classifyResponse checks whether resp.StatusCode is retryable. If so,
// it drains and closes the body (returning to keep-alive pool) and
// returns (true, wrappedError). Otherwise it returns (false, nil) and
// the caller should return the response to its user.
func classifyResponse(resp *http.Response, retryable map[int]bool) (bool, error) {
	if !retryable[resp.StatusCode] {
		return false, nil
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return true, fmt.Errorf("HTTP %d (transient)", resp.StatusCode)
}

// waitOrAbort sleeps for the backoff duration at attempt index i (plus
// small jitter), or returns false immediately if ctx is cancelled.
func waitOrAbort(ctx context.Context, backoff []time.Duration, attempt int) bool {
	return backoffSleep(ctx, backoffFor(backoff, attempt))
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
