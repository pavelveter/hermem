# Middleware Audit

Date: 2026-06-30

## Finding: Middleware Chain Is Already Centralized

The global middleware stack is defined in `server/server.go::Serve()` (outer→inner):

1. RecoveryMiddleware
2. TimeoutMiddleware(120s)
3. RuntimeMiddleware
4. APIVersionMiddleware + RequestIDMiddleware + AuthMiddleware
5. SlogMiddleware
6. MaxBytesMiddleware
7. SafeBodyCloseMiddleware

**All** sub-shell handlers pass through this stack. No sub-shell applies additional middleware.

## Per-Handler Wrapping (`shared.Wrap`)

Each sub-shell embeds `shared.BaseHTTPService` and uses `Wrap(handler)` to convert
`func(w, r) error` → `http.HandlerFunc`. On error, `Wrap` calls `Metrics.IncErr()`
and maps the error to an HTTP status via `mapStatus()`.

This is **not** middleware — it's a handler adapter. The two are distinct:

| Concept | Location | Purpose |
|---------|----------|---------|
| Middleware | `server.go` Serve() | Cross-cutting: logging, auth, panic recovery, timeouts |
| Wrap | `shared/base.go` | Per-handler: error → HTTP status mapping |

## Exceptions (Deliberate)

| Route | Why Not Wrapped |
|-------|----------------|
| `/provenance` | Pre-3.2 contract maps ALL errors to 400 (not 500). `mapStatus` would break wire bytes. |
| `/task/status` | Pre-3.2 contract maps non-NotFound errors to 422 (semantic: "unknown state"). |
| `/admin/retention/run` | Writes body on error, returns nil. `Wrap` no-ops on nil error. |

## Conclusion

H3's original premise ("adding a middleware requires editing all twelve sub-shells")
is **incorrect** — the middleware chain is already in one place (`server.go`). The
`Wrap` pattern is a handler adapter, not middleware. No refactor needed.
