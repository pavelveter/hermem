package ai

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"time"
)

// defaultBackoffs is the exponential backoff ladder applied by
// ResilientClient when Backoffs is left empty. 200ms / 500ms / 1s / 2s
// matches the spec in TODO.md 5.4 — tight enough to fail fast in
// interactive paths, long enough to ride out a model-load spike.
var defaultBackoffs = []time.Duration{
	200 * time.Millisecond,
	500 * time.Millisecond,
	1 * time.Second,
	2 * time.Second,
}

// DefaultBackoffs returns a copy of the default backoff ladder.
func DefaultBackoffs() []time.Duration {
	dst := make([]time.Duration, len(defaultBackoffs))
	copy(dst, defaultBackoffs)
	return dst
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
//     either set Attempts=1 or accept at-least-once semantics.
//  2. Body replay. http.NewRequest does NOT set req.GetBody for arbitrary
//     io.Reader bodies; it does for *bytes.Buffer, *bytes.Reader, and
//     *strings.Reader. Callers passing custom readers MUST attach a
//     GetBody closure or the second attempt will silently replay an empty
//     body. Missing GetBody on a request that needs a retry is a caller
//     bug, not a runtime error — Do does not detect it.
//  3. What triggers a retry: network error from inner.Do, HTTP 429, or
//     HTTP 5xx. Other 4xx are returned to the caller verbatim.
//  4. Context propagation. ctx is attached to every retry-attempt's
//     cloned request AND every inter-attempt sleep. A ctx cancel
//     mid-sleep aborts the loop immediately and returns ctx.Err(). A
//     ctx cancel between the inner Do error and the next sleep also
//     short-circuits.
//  5. Body draining. On a retried 5xx/429 response, the resp.Body is
//     drained to io.Discard and closed so the underlying TCP connection
//     can return to the keep-alive pool instead of being RST.
//  6. Default policy. A zero-value ResilientClient{} is valid: Inner
//     falls back to http.DefaultClient, Attempts to len(Backoffs)+1,
//     Backoffs to DefaultBackoffs() (200ms/500ms/1s/2s).
//  7. No wall-clock guard yet — see C1.4. The parent ctx deadline is
//     today's only ceiling on total retry duration.
//  8. Thread-safety. ResilientClient is stateless after construction so
//     a single instance can be shared across goroutines. Backoffs is
//     copied on Default fallback, never mutated in place.
type ResilientClient struct {
	Inner    *http.Client    // nil → http.DefaultClient
	Attempts int             // 0 → len(Backoffs)+1
	Backoffs []time.Duration // nil/empty → DefaultBackoffs
}

// NewResilientClient is the only constructor callers should use. The
// zero-value ResilientClient{} also works (defaults kick in on Do) but
// prefer this for explicit intent at the call site.
func NewResilientClient(inner *http.Client, attempts int, backoffs []time.Duration) *ResilientClient {
	return &ResilientClient{Inner: inner, Attempts: attempts, Backoffs: backoffs}
}

// Do issues req with ctx attached and applies the configured backoff
// ladder on 5xx / 429 / network errors. Returns the first 2xx/3xx/4xx
// response (other than 429) or the last error after attempts are
// exhausted.
//
// See the ResilientClient type comment for the full contract
// (idempotency, body-replay, ctx propagation, body-draining).
func (c *ResilientClient) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	inner := c.Inner
	if inner == nil {
		inner = http.DefaultClient
	}
	backoffs := c.Backoffs
	if len(backoffs) == 0 {
		backoffs = DefaultBackoffs()
	}
	attempts := c.Attempts
	if attempts <= 0 {
		attempts = len(backoffs) + 1
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			} // Refresh the body before retrying. Without GetBody we
			// can't replay a consumed Body, so callers must supply
			// a GetBody closure on their *http.Request.
			if req.GetBody != nil {
				body, err := req.GetBody()
				if err != nil {
					return nil, fmt.Errorf("retry: get body: %w", err)
				}
				req.Body = body
			}
		}
		// Clone + WithContext each attempt so a per-call ctx override
		// (e.g. a tighter parent deadline) takes effect while still
		// preserving the original req's URL/method/headers.
		c := req.Clone(ctx)
		resp, err := inner.Do(c)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if !backoffSleep(ctx, backoffFor(backoffs, i)) {
				return nil, ctx.Err()
			}
			continue
		}
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			// Drain so the underlying TCP connection is returned to
			// the keep-alive pool instead of being reset on Close.
			// Reading only 256 bytes (the previous behaviour) left the
			// remainder on the wire and forced a RST on the next retry.
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d (transient)", resp.StatusCode)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if !backoffSleep(ctx, backoffFor(backoffs, i)) {
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
