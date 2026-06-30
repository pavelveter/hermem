# ADR-012: Application Container for Dependency Injection

## Status

Accepted

## Context

The `clienv.Env` struct (~18 KB) holds all runtime dependencies as exported fields with lazy initialization via `EnsureDB()`. This creates:

1. **Temporal coupling** — callers must call `EnsureDB()` before accessing `DB`, `VI`, or `Worker`; forgetting causes nil-deref panics.
2. **Implicit init order** — `EnsureDB` populates fields in a hardcoded sequence; the order is documented only in comments.
3. **Implicit shutdown order** — `Close()` uses `sync.Once` but the worker→DB ordering is implicit.
4. **Side-effect assignment** — `wireAll` assigns `env.Retriever = retSvc` as a post-construction side effect, making the dependency graph partially hidden.
5. **Test fragility** — tests must either construct `Env{...}` manually (risking nil fields) or call `EnsureDB` (requiring a real SQLite database).

## Decision

Introduce `app.Application` — a typed DI container where ALL fields are non-nil after construction.

```go
type Application struct {
    DB        *sql.DB
    VI        core.VectorIndex
    Worker    *metrics.AsyncMetricsWorker
    Embedder  core.Embedder
    Extractor core.LLMExtractor
    Reranker  core.Reranker
    Retriever core.Retriever
    Metrics   *metrics.Metrics
    Tracer    tracing.Tracer
    Cfg       *config.Config
    Build     BuildInfo
}
```

### Construction

`app.New(ctx, cfg, build)` constructs every dependency eagerly:

1. AI clients from `cfg.NewEmbedder/NewExtractor/NewReranker`
2. SQLite DB via `store.InitDBStrictWithOptions`
3. Metrics DB table + async worker
4. Vector index via `vector.NewIndex`
5. Tracer via `tracing.NewTracerFromEnv`
6. Retrieval service (as `core.Retriever` interface)

Returns `(*Application, error)` — error only if DB init fails.

### Lifecycle

- `Start(ctx)` — begins background operations (currently a no-op since `InitMetricsWorker` starts the goroutine internally; exists for symmetry and future use).
- `Stop(ctx)` — explicit ordered shutdown: worker drain → DB close. Idempotent via `sync.Once`.

### Scope boundary

`Application` owns the **data layer** (DB, VI, domain services, worker) and the **worker lifecycle**. The HTTP serving layer (server construction, SIGHUP hot-reload, request routing) remains in `serve.go` and `wiring.go`. This avoids entangling SIGHUP complexity into the container.

## Alternatives Considered

1. **Wire/dig/fx framework** — rejected: adds external dependency; overengineered for a flat dependency graph with zero cycles.
2. **Application owns the HTTP server** — rejected: SIGHUP reload is tightly coupled to the server lifecycle; mixing it into the container would blur the data/serving boundary.
3. **Lazy construction with `EnsureDB` kept** — rejected: defeats the purpose of eliminating temporal coupling.

## Consequences

- **Nil-deref risk eliminated** — every field is populated before `New()` returns.
- **Shutdown order explicit** — encoded in `Stop()`, not in scattered `defer` chains.
- **Side-effect assignment removed** — `Retriever` is a field, not assigned in `wireAll`.
- **Testability** — `app.NewForTest(t)` (future) can construct a test Application with mock DB; production code uses real SQLite.
- **Migration path** — C2.B/C2.C/D migrate consumers from `Env` to `Application` incrementally; `Env` is deleted after all consumers migrate.
