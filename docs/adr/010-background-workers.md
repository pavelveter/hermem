# ADR-010: Background Worker Lifecycle

## Status

Accepted

## Context

The application runs long-lived background workers (GC retention, ingestion processing) alongside the HTTP server. These workers need coordinated startup/shutdown with the main server.

## Decision

Use a `lifecycle.LifecycleManager` that registers typed components:

```go
mgr := lifecycle.NewLifecycleManager()
mgr.Register(components.NewHTTPComponent(httpSrv))
mgr.Register(components.NewGCComponent(gcSvc, cfg.Retention))
```

Components implement a simple interface:
- `Start(ctx)` — begin work
- `Stop(ctx)` — graceful shutdown

The lifecycle manager starts components in registration order and stops them in reverse order. The HTTP server blocks on `ctx.Done()` (SIGINT/SIGTERM), then drains.

Workers are constructed per-concern (not per-request) and share the same `*sql.DB` and `core.VectorIndex`.

## Alternatives Considered

1. **Goroutine-per-worker with WaitGroup** — rejected: no coordinated shutdown ordering.
2. **Actor model** — rejected: overengineered for 2-3 workers.

## Consequences

- Startup order is explicit and deterministic.
- Shutdown is graceful and ordered (HTTP drains before GC stops).
- Adding a new background worker = one `Register()` call.
- Worker errors propagate through the lifecycle manager.
