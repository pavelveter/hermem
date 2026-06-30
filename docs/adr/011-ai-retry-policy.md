# ADR-011: Typed RetryPolicy for AI HTTP Client

## Status

Accepted

## Context

The `ResilientClient` in `internal/ai/client.go` is the single retry entrypoint for all external AI provider calls (embedder, extractor, reranker). Before C1, retry configuration was expressed as two bare public fields (`Attempts int`, `Backoffs []time.Duration`) with no type safety, no wall-clock guard, and no configurable retryable-status set. The retry loop in `Do` was a monolithic 60-line method with inline body-replay, drain, and sleep logic, making it hard to reason about individual concerns.

## Decision

1. **Typed `RetryPolicy` struct.** Replace the bare `Attempts`/`Backoffs` fields with:

   ```go
   type RetryPolicy struct {
       MaxAttempts      int              // 0 → 10
       Backoff          []time.Duration  // nil → 200ms/500ms/1s/2s
       RetryableStatus  map[int]bool     // nil → {429, 500, 502, 503, 504}
       MaxWallClock     time.Duration    // 0 → 30s, negative → disabled
   }
   ```

   A zero-value `RetryPolicy` is valid and applies sensible defaults via `resolvePolicy()`. This makes the API self-documenting and eliminates the "magic number" problem.

2. **Wall-clock guard.** `MaxWallClock` caps total retry duration independently of `ctx` deadlines. This is critical for background workers that pass `context.Background()` — without a wall-clock ceiling, retries could run indefinitely on a flaky provider. Default: 30 s.

3. **Configurable retryable-status set.** `RetryableStatus` lets callers opt in/out of specific HTTP codes. The default set (`{429, 500, 502, 503, 504}`) matches the previous hardcoded `resp.StatusCode == 429 || resp.StatusCode >= 500` but is now open for extension (e.g. retrying 502 only for a specific provider).

4. **Extracted helpers.** `Do` is decomposed into four unexported functions:
   - `prepareRequest(ctx, req)` — context check + body replay via GetBody
   - `executeOnce(ctx, req, inner)` — clone + inner.Do
   - `classifyResponse(resp, retryable)` — drain-or-return decision
   - `waitOrAbort(ctx, backoff, attempt)` — backoff sleep with cancellation

   Each helper is independently testable and the main loop reads as a clear retry state machine.

## Alternatives Considered

1. **Keep bare fields + deprecation comments** — rejected: the old API (`Attempts int`, `Backoffs []time.Duration`) lacked discoverability and had no place for `MaxWallClock` or `RetryableStatus`. A clean break is simpler for a pre-1.0 project.

2. **Functional options pattern** (`WithMaxAttempts(5)`) — rejected: overengineered for 4 fields; the struct literal is more explicit and grep-friendly.

3. **Separate `RetryConfig` per provider** — rejected: all 6 AI clients share identical retry semantics; per-provider config would duplicate logic with no behavioral difference.

## Consequences

- **Breaking change (pre-1.0).** All 8 `newHTTPClient` call-sites (embedder, extractor, reranker, tests) updated to `RetryPolicy{MaxAttempts: N}`. Zero callers outside `internal/ai`.
- **29 unit tests** (up from 5) covering: retry on 429/5xx, non-retryable 4xx, wall-clock guard, negative wall-clock (disabled), custom status map, GetBody replay, GetBody failure, classifyResponse, resolvePolicy defaults/custom.
- **Property test** (`AttemptCapInvariant`) verifies Do never exceeds MaxAttempts across 5 backoff configurations.
- **Benchmark** (`DoHappyPath`) establishes baseline: ~124 allocs/op, ~445 µs/op on Apple M1.
- **ADR-011** (this document) is the source of truth for the retry contract. The doc-comment on `ResilientClient` references it.
