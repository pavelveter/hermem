# Service Dependency Graph

## Overview

All services use constructor injection — dependencies are passed as parameters, never
read from globals. The wiring lives in `cli/serve.go` (HTTP server boot) and
`cli/env/env.go` (lazy DB init for CLI commands).

## Construction order (cli/serve.go)

```
cfg ──→ Embedder (cfg.NewEmbedder())
    ──→ Extractor (cfg.NewExtractor())
    ──→ Reranker (cfg.NewReranker())
    ──→ Metrics (metrics.New())
    ──→ Tracer (tracing.NewTracerFromEnv())

env.DB ──→ (lazy via EnsureDB: store.InitDB + RunMigrations)
env.VI ──→ (lazy via EnsureDB: vector.NewIndex)
env.Worker ──→ (lazy via EnsureDB: metrics.InitMetricsWorker)
```

## Service dependency matrix

| Service | db | vi | embedder | extractor | reranker | metrics | refs |
|---------|:--:|:--:|:--------:|:---------:|:--------:|:-------:|:----:|
| memory.Service | ✓ | ✓ | ✓ | ✓ | | | |
| retrieval.Service | ✓ | ✓ | ✓ | | | | |
| task.Service | ✓ | ✓ | ✓ | | | | |
| contradiction.Service | ✓ | | | | | | |
| edge.Service | ✓ | ✓ | ✓ | | | | |
| ingest.Service | ✓ | ✓ | ✓ | ✓ | | | |
| timeline.Service | ✓ | | | | | | |
| graph.Service | ✓ | | | | | | |
| migration.Service | ✓ | | | | | | |
| retention.Service | ✓ | ✓ | | | | | |
| reembed.Service | ✓ | ✓ | ✓ | | | | |
| health.Service | | | | | | | |

## HTTP shell wiring (server/*.HTTPService)

Each HTTP shell wraps a domain Service + Metrics + (optionally) serverstate.Ref:

```
HTTPService(domainSvc, metrics, refs?, extraParams...)
  └─→ Routes() map[string]http.HandlerFunc
```

| HTTP shell | Domain Service | Metrics | Refs | Extra |
|------------|---------------|:-------:|:----:|-------|
| server/retrieval | retrieval.Service | ✓ | ✓ | |
| server/task | task.Service | ✓ | ✓ | |
| server/memory | memory.Service | ✓ | ✓ | dedupThreshold |
| server/edge | edge.Service | ✓ | ✓ | |
| server/timeline | timeline.Service | ✓ | | |
| server/ingest | ingest.Service | ✓ | ✓ | dedupThreshold |
| server/contradiction | contradiction.Service | ✓ | | |
| server/graph | graph.Service | ✓ | ✓ | vectorDim |
| server/migration | migration.Service | ✓ | ✓ | |
| server/retention | retention.Service | ✓ | ✓ | retentionPolicy |
| server/reembed | reembed.Service | ✓ | | |
| server/health | health.Service | | | |

## Data flow (request lifecycle)

```
HTTP Request
  → AuthMiddleware (auth scope check)
  → TimeoutMiddleware (ctx deadline)
  → RequestIDMiddleware
  → SlogMiddleware (logging)
  → MaxBytesMiddleware
  → SafeBodyCloseMiddleware
  → Handler
      ├─→ serverstate.Ref.Load() (atomic config snapshot)
      ├─→ DomainService.Method(ctx, args...)
      │     └─→ store.* (SQL operations)
      │     └─→ vector.* (in-memory cosine search)
      │     └─→ core.Embedder.Embed() (external LLM)
      └─→ Metrics.Inc*() (atomic counters)
```

## Key architectural properties

1. **No circular dependencies** — each domain package imports only `core`, `store`, `vector`, and `config`
2. **Schema via per-call args** — `SchemaConfig` is passed per call, not held as state; SIGHUP reload swaps `serverstate.Ref` atomically
3. **Serverstate pattern** — `serverstate.Ref` (atomic.Pointer) gives handlers a consistent config snapshot without locking
4. **Lazy DB init** — `env.EnsureDB()` opens SQLite + runs migrations on first command that needs DB; `--help` skips it
5. **EnvManager** — atomic hot-reload of config + handles without restarting the process
