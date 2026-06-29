# Hermem — Runbook

Production operations guide. Covers operational notes, common pitfalls,
admin operations, profiling, observability, architecture, database schema,
and diagnostics.

For CLI reference, see [CLI.md](CLI.md). For server endpoints, see
[SERVER.md](SERVER.md). For configuration, see [USAGE.md](USAGE.md).

---

## Operational notes

- **Config is binary-directory relative.** `hermem.ini` is resolved via
  `os.Executable()`, not `os.Getwd()`, so `~/.hermes/bin/hermem store` works
  the same from any working directory. The ini lives next to the binary.
- **Concurrency.** The HTTP server is fine for dozens of
  concurrent requests, but the underlying SQLite write path is
  serialised by SQLite itself. For high-write workloads consider
  batching through `/ingest` instead of N×`/store`.
- **Slog output.** Logs go to stderr via `log/slog`. The exact field
  set per event is:
  - `server_ready`            — `port`
  - `ingest_failed`           — `err`, `dialog_len`
  - `ollama_call`             — `model`, `attempts_used`, `outcome` (emitted at Debug; pre-retry `ollama_attempt_retry` Warn lines surface only mid-loop)
  - `retrieval_complete`      — `seed_count`, `total_ranked`, `effective_depth`, `cap_active` (emitted at Debug — the level filter is the throttle)
  No `entity_id` / `embedding_dim` / `cost_ms` fields are emitted yet
  (TODO §5). Pipe stderr to a JSON-aware log shipper.
- **Graceful shutdown.** Server drains in-flight requests on `SIGINT`/`SIGTERM` with a 10-second timeout, then exits cleanly. Use `kill <pid>` or `systemctl stop hermem`.
- **Backups.** The DB is a single SQLite file. `sqlite3 hermem.db
  ".backup hermem.db.bak"` while the server is running is safe
  (SQLite's online backup API). Plain `cp hermem.db hermem.db.bak`
  while writers are active is **not** safe.

---

## Common pitfalls

- **Stale ini.** Edited `hermem.ini` but didn't restart the server.
  Send `SIGHUP` to the server process (`kill -HUP <pid>`) to
  reload `hermem.ini` without restart. Schema, categories, and
  relations update atomically across all in-flight handlers.
- **`store` saying "id required" when you have an id.** Almost
  always a shell-escaping bug in single-quoted JSON. Pipe through a
  file (`./hermem store < req.json`) or use `jq -c … | hermem…`.
- **`search` always returns the same top result.** Either the
  embedder model is misconfigured (check `slog` for
  `embedding_dim`), or the DB has fewer than K similar entities.
- **`/query` returns an empty markdown.** No seed-match → no graph
  walk → empty buckets. Verify with `/search` first that the query
  actually matches stored content.
- **Two concatenations in one body** (`{...}{...}`). Now rejected
  with `trailing_data` (PR7a); pre-PR7a would silently accept the
  first object. If a legacy client still relies on the old behavior,
  simplify its request shape.
- **Embedding dimension drift.** Storing 768-dim and 1536-dim
  entities in the same DB corrupts cosine similarity silently.
  See [USAGE.md](USAGE.md) §11.
- **CLI exit 1 with no log line.** Almost always a stdin read
  failure (Ctrl-D on an empty stdin in a script). Capture stderr.

---

## Database schema

A single SQLite file with two (or three) tables. The schema lives in
`db.go → InitDB`; below is the field-by-field reference.

### `entities`

| Column          | SQLite type | Notes                                                 |
|-----------------|-------------|-------------------------------------------------------|
| `id`            | TEXT PK     | Stable identifier.                                    |
| `category`      | TEXT        | One of the categories defined in `[schema] allowed_categories` (defaults: `world`/`opinion`/`experience`/`observation`). |
| `content`       | TEXT        | Free text.                                            |
| `embedding`     | BLOB        | `len(embedding) * 4` raw little-endian float32 bytes. |
| `updated_at`    | DATETIME    | `CURRENT_TIMESTAMP` default; refreshed on each upsert.|
| `last_accessed_at` | DATETIME | `CURRENT_TIMESTAMP` default; GC uses this for TTL.    |
| `archived`      | INTEGER     | 0 = active, 1 = excluded from graph walks by GC.      |
| `degree`        | INTEGER     | `0` default; auto-maintained by SQL triggers on edges INSERT/DELETE. Powers `log10(1+degree)` centrality scoring. |
| `priority`      | INTEGER     | `0` default; `task/list` + `task/executable` + `ExecutionPlan` order by `priority DESC`. Added in migration `007_task_priorities.sql`. |

Entity IDs are primary keys — repeated `store` calls upsert (the row
is replaced; edges are not deleted). The DB is configured with
`PRAGMA journal_mode = WAL` and `PRAGMA synchronous = NORMAL` in
`InitDB` for better concurrent performance. Re-storing the same `id`
overwrites the embedded content; cosine math remains valid because
the float32 stride does not change.

### `edges`

| Column          | SQLite type | Notes                                               |
|-----------------|-------------|-----------------------------------------------------|
| `source_id`     | TEXT        | FK → `entities.id` (cascade on delete).             |
| `target_id`     | TEXT        | FK → `entities.id` (cascade on delete).             |
| `relation_type` | TEXT        | Relation label from `[schema] allowed_relations` (defaults: `prefers`, `uses`, `mentions`, `related_to`, `part_of`, `causes`, `contradicts`, `blocked_by`, `recovers_via`). Unknown values are rejected with HTTP 422. |
| `weight`        | REAL        | `1.0` default; added in migration `006_weighted_edges.sql`. Used by CTE `path_weight` accumulator and the ranker's `compositeScore`. Read with `COALESCE(weight, 1.0)`. |

Composite PK `(source_id, target_id, relation_type)` means duplicate
edges auto-dedupe on insert. `weight` defaults to `1.0` for every
write path (auto-link `related_to` >0.85 cosine from `/store`, bulk
merge edges from `ProcessDialogWithProvenance`, manual `memory edge`).
All reads use `COALESCE(weight, 1.0)` so a hand-edited legacy row never
crashes a multiplier path. Edge provenance is recovered via
`RetrievedFact.parent_id` / `relation_type` from the graph walk.

### `vec_entities` (when `[database] backend = sqlite-vec`)

| Column       | SQLite type | Notes                                                 |
|--------------|-------------|-------------------------------------------------------|
| `rowid`      | INTEGER PK  | Mapped via `id_map` table (AUTOINCREMENT).             |
| `embedding`  | FLOAT32[n]  | Vector stored in `vec0` virtual table for KNN search. |
| `entity_id`  | TEXT        | Maps back to `entities.id`.                            |

This is a `vec0` virtual table managed by the `sqlite-vec` extension.
It enables indexed KNN search via `WHERE embedding MATCH ? ORDER BY distance`.
Only created when `[database] backend = sqlite-vec`; the default
`in-memory` backend reads directly from `entities.embedding`.

### Migrations

Hermem uses a versioned migration system (`schema_migrations` table).
Each embedded SQL migration in `src/migrations/` is tracked with an
applied-at timestamp. `InitDB` applies unapplied files in ordered
transactions at startup; `hermem db migrate` shows status for operator
visibility. To change the schema, write a new migration file and
re-build.

For full embedder-model switches, write a new `hermem.db` and
re-ingest (`hermem ingest` against every persisted dialog is
sufficient; the embedded text regenerates).

---

## Admin Operations

The `hermem ops` group provides offline database diagnostics and
maintenance — no HTTP server required.

### Commands

| Command             | Description                                         |
|---------------------|-----------------------------------------------------|
| `hermem ops stats`  | Print entity/edge/contradiction counts + embedding coverage |
| `hermem ops integrity` | Run integrity checks: missing embeddings, dangling edges, archive consistency |
| `hermem ops vacuum` | Reclaim disk space via SQLite VACUUM               |
| `hermem ops rebuild-index` | Re-generate embeddings and re-index entities |

### Examples

```bash
# Statistics
hermem ops stats                    # tabular output
hermem ops stats --json             # machine-readable JSON

# Integrity checks
hermem ops integrity                # exit 0 (OK), 1 (critical issue)
hermem ops integrity --json         # machine-readable JSON
hermem ops integrity --fail-on-warning  # exit 2 on warnings

# Vacuum (SQLite VACUUM)
hermem ops vacuum                   # with progress bar
hermem ops vacuum --no-progress     # silent mode

# Rebuild vector index
hermem ops rebuild-index --dry-run                          # preview only
hermem ops rebuild-index --category fact                    # by entity category
hermem ops rebuild-index --since 2026-01-01                 # recent only
hermem ops rebuild-index --only-archived                    # archived entities only
```

### Integrity issue codes

| Code                  | Level    | Meaning                                         |
|-----------------------|----------|-------------------------------------------------|
| `MISSING_EMBEDDING`   | warning  | Few entities without embeddings (<10)            |
| `MISSING_EMBEDDING`   | critical | ≥10 entities without embeddings                 |
| `DANGLING_EDGE`       | critical | Edge references a non-existent entity           |
| `ARCHIVE_CONSISTENCY` | warning  | Archived entity still has embedding in vector index |

### Exit codes

| Code | Meaning                          |
|------|----------------------------------|
| 0    | OK (no critical issues)          |
| 1    | Critical integrity issue found   |
| 2    | Warning-level issue (--fail-on-warning) |

### Cron recommendation

Run `hermem ops integrity` weekly to catch drift early:

```bash
0 3 * * 1 cd /opt/hermem && hermem ops integrity --fail-on-warning
```

---

## Diagnose Command

`hermem diagnose` runs a self-check of the database and memory subsystem
and emits a structured health report.

### Usage

```bash
# Human-readable output (default)
hermem diagnose

# Machine-readable JSON (for ops dashboards / CI gates)
hermem diagnose --json
```

### Checks performed

| Check           | What it inspects                                          |
| --------------- | --------------------------------------------------------- |
| Schema          | PRAGMA foreign_key_check, orphan edge count, PRAGMA integrity_check |
| Vector index    | id_map row count, embedding dimension consistency (meta vs configured) |
| Memory          | entity count, embedding density overall and by category, beliefs table by status |
| Retention       | archived entity count and percentage                      |
| Recent errors   | best-effort tail (currently returns a note — no persisted error log) |

### JSON output shape (top-level fields)

```json
{
  "schema":      { "foreign_keys_ok": true, "orphan_edges": 0, "integrity_ok": true },
  "vector":      { "total_rows": 1500, "config_dim": 768, "stored_dim": 768, "dim_mismatch": false },
  "memory":      { "total_entities": 1500, "entities_with_embedding": 1200, "embedding_density_pct": 80.0, "density_by_category": {...}, "belief_counts_by_status": {...} },
  "retention":   { "archived_entities": 50, "total_entities": 1500, "archived_pct": 3.33 },
  "errors":      { "note": "slog ERROR entries are not persisted to a queryable store..." }
}
```

### Exit codes

| Exit code | Meaning                   |
| --------- | ------------------------- |
| 0         | All checks passed         |
| 1         | One or more checks failed |

---

## Runtime Profiling

Profiling is opt-in and off by default — Hermem does not third-party sidecars
by default. Two surfaces share the same `runtime/pprof` backend.

**Server mode** — set `HERMEM_PPROF_ENABLED=1` to mount the stdlib
profile endpoints behind the same `http.ServeMux`:

```bash
HERMEM_PPROF_ENABLED=1 hermem serve --port 8420
curl http://localhost:8420/debug/pprof/             # human-readable index
curl -o cpu.pprof http://localhost:8420/debug/pprof/profile?seconds=30
go tool pprof cpu.pprof
```

The env-var match is exact (`"1"` only) — typos like `true`/`yes`/`on`
fail to enable so a misconfiguration cannot accidentally expose process
internals. Endpoints include `/debug/pprof/profile`, `/heap`,
`/goroutine`, `/symbol`, `/cmdline`, and `/trace`.

**CLI mode** — `hermem profile …` works against the running process or
in a one-off CLI invocation. Default duration is 10 seconds; override
with the positional arg or `--seconds`.

```bash
hermem profile cpu 30          | go tool pprof -      # 30 s CPU profile, protobuf
hermem profile heap                            # → /tmp/hermem-heap.pprof
hermem profile goroutine                       # text dump → stdout
hermem profile trace 5                         # → /tmp/hermem-trace.out
go tool trace /tmp/hermem-trace.out
```

Analyzing a heap snapshot:

```bash
hermem profile heap                            # text index written to /tmp/hermem-heap.pprof
go tool pprof -alloc_objects /tmp/hermem-heap.pprof
go tool pprof -top /tmp/hermem-heap.pprof
```

---

## Observability — OpenTelemetry Tracing

Hermem v0.3.0 ships a tracing slice in `src/internal/tracing` —
transport-agnostic instrumentation for retrieval, ingestion, and
memory-store pipelines. By default Hermem is invisible: no exporters
attached, no SDK import side-effects, no spans emitted.

```bash
TRACING_EXPORTER=otlp hermem serve --port 8420    # enable OTLP/gRPC export
unset TRACING_EXPORTER && hermem memory query ...   # noop fallback, zero overhead
```

When `TRACING_EXPORTER=otlp` is set, Hermem uses the OpenTelemetry
gRPC OTLP exporter and reads `[tracing]` from `hermem.ini` for the
endpoint, headers, and protocol knobs:

```ini
[tracing]
endpoint = http://localhost:4317   # OTLP gRPC collector
; headers = x-api-key=secret       # comma-separated `key=value` pairs
; sample_ratio = 1.0               # 0.0–1.0; default 1.0 (always-on when enabled)
```

The tracing SDK is gated at `main.go` startup: if the SDK is unavailable
in the build (e.g. CGO_ENABLED=0 build dropped it), Hermem falls back to
`NoopTracer` and logs a single warning — the rest of the request flow
runs unchanged. Spans are propagated through context via
`tracing.WithSpan` / `tracing.SpanFrom`, so handlers, services, and
SQL-layer code all pick the right parent.

```bash
hermes agent --request-id abc123 …  # any caller setting X-Request-ID joins into the same trace
curl -H 'X-Request-ID: my-trace' http://localhost:8420/store …
```

For local development without a collector, point `endpoint` to
[otel-desktop](https://github.com/SigNoz/otel-desktop) or any
local-only OTLP receiver. PromQL/Grafana dashboards can then filter
by `service.name=hermem` to see retrieval latency histograms,
ingestion batch sizes, and dependency-failure counts.

### Prometheus /metrics endpoint

In addition to the legacy `expvar` JSON at `/metrics`, the server now exposes
`/metrics` in Prometheus exposition format (text/plain; version=0.0.4). The
endpoint is registered by `Server.mount()` directly (see
`src/internal/server/server.go`) and is reachable without authentication when
`[server] api_key` is unset; when it is set, the same `X-API-Key` middleware
applies to `/metrics` as to the rest of the surface.

`prometheus/client_golang` v1.21.0 is wired against hermem's own
`*prometheus.Registry` (not the global default) so hermem metrics are isolated
from any third-party library that might also register with the default. The
exposition handler is exposed via `Metrics.PrometheusHandler()`.

#### Duration histograms with bounded labels

Four domain `*HistogramVec` series carry the request cost of each pipeline stage.
Each is pre-warmed at `New()` with an `_init` sentinel series so cold-start
scrapes are zero-missing; bounded label sets guard against unbounded cardinality.
All four share the same bucket boundaries defined as `durationBuckets` in
`src/internal/metrics/metrics.go`:

```go
var durationBuckets = []float64{0.05, 0.1, 0.5, 1, 2, 5, 10, 15, 30, 60}
```

| Series                              | Label      | Known values (`known[X]` in metrics.go)                                |
| ----------------------------------- | ---------- | ---------------------------------------------------------------------- |
| `hermem_ingest_duration_seconds`    | `category` | `observation`, `world`, `task`, `edge` (plus `_init` sentinel)         |
| `hermem_retrieval_duration_seconds` | `mode`     | `search`, `retrieve`, `query`, `response`, `query_explain`, `provenance` (plus `_init`) |
| `hermem_contradiction_duration_seconds` | `detector` | `lexical`, `composite` |
| `hermem_rerank_duration_seconds`    | `strategy` | `llm_openai`, `llm_ollama`, `noop` (plus `_init`)                       |

Adding a new label value is intentionally not free: extend the matching
`knownCategories` / `knownModes` / `knownDetectors` / `knownStrategies` slice
and the corresponding `TestHermemPrefixContract_KnownCategoriesSet` /
`_KnownModesSet` / `_KnownDetectorsSet` / `_KnownStrategiesSet` regression test.
Cardinality stays bounded by the slice length, and the matching regression
test fails closed if any of the slice drifts.

Plus domain counters (inc/dec) for stores, searches, retrieves, queries, edges,
errors, task creates/pend/complete/delete, and contradiction hits/misses. The
exact series cardinality is bounded by `sum(1 + len(knownX))` for the four
`HistogramVec`s plus the inc/dec counter families; see
`TestHermemPrefixContract_AllHermemMetricsPresent` in
`src/internal/metrics/metrics_test.go` for the assertion envelope and the
`prometheus.collectorCount` gauge for a runtime reading.

#### Sample Prometheus scrape config

```yaml
scrape_configs:
  - job_name: hermem
    scrape_interval: 15s
    static_configs:
      - targets: ['localhost:8080']
    metrics_path: /metrics
    # If [server] api_key is set:
    authorization:
      type:       X-API-Key
      credentials_file: /etc/hermem/api_key   # one-line secret
```

#### Local development

```bash
# binary exposes /metrics on the configured port.
curl -s http://localhost:8080/metrics | head -40

# helper script records a snapshot to a file for offline inspection.
hermem metrics --format=text > /tmp/hermem-metrics.txt
```

#### Migration / regression guards

Any future commit that adds a new label value must:

1. Append the value to the corresponding `known[...]` slice in
   `src/internal/metrics/metrics.go`.
2. Update the matching `TestHermemPrefixContract_KnownCategoriesSet` /
   `KnownModesSet` / `KnownDetectorsSet` / `KnownStrategiesSet` regression test.

`slog` continues to be the structured-logging surface; Prometheus is the
metrics surface. Both are independent and avoid double-counting.

---

## Architecture & Dependency Injection

All 12 domain services use **constructor injection** — no singletons,
no global mutable state, no service locators. Dependencies are passed
as parameters at construction time in `cli/serve.go`:

```go
memSvc    := memdomain.New(env.DB, env.VI, env.Embedder, env.Extractor)
retSvc    := retdomain.NewService(env.DB, env.VI, env.Embedder)
taskSvc   := taskdomain.NewService(env.DB, env.Embedder, env.VI)
cndSvc    := contradictdomain.NewService(env.DB)
edgeSvc   := edgedomain.New(env.DB, env.VI, env.Embedder)
// ... 7 more services
```

**Key properties:**

- **No circular dependencies** — each domain package imports only `core`, `store`, `vector`, and `config`.
- **Schema per-call** — `SchemaConfig` is passed per call, not held as state. SIGHUP reload swaps `serverstate.Ref` atomically; handlers always see a consistent snapshot.
- **No global mutable state** — `ActiveSchema()` singleton removed; `RequiredScopes` map made unexported; all package-level variables audited (see `docs/package-level-audit.md`).
- **CI guardrails** — `forbidigo` linter + grep-based CI job prevent `ActiveSchema()` and exported mutable state regressions.

For the full dependency graph, HTTP shell wiring matrix, and data flow
diagram, see **[docs/service-dependencies.md](docs/service-dependencies.md)**.

---

## Where to look in the code

| Concern                         | File                              |
|---------------------------------|-----------------------------------|
| INI parsing, defaults, Validate | `src/internal/config/ini.go` + `src/internal/config/config.go` (defaults) |
| Schema, embedding serialisation | `src/internal/store/migration.go` (migrations runner) + `src/internal/store/init.go` (DSN + PRAGMAs) |
| VectorIndex interface, search backends (InMemory / SqliteVec) | `src/internal/vector/index.go` (interface) + `src/internal/vector/inmemory.go` + `src/internal/vector/quantize.go` |
| Graph walk, ranking, formatting | `src/internal/retrieval/{walk,scoring,formatting,response,tasks}.go` |
| Background worker, dedup, edges | `src/internal/ingestion/worker.go` (IngestionWorker) + `src/internal/ingestion/dialog.go` (ProcessDialog) |
| Contradiction detection        | `src/internal/contradiction/` (domain Service) + HTTP shell in `src/internal/server/contradiction/` |
| Community detection (Louvain)  | `src/internal/store/community.go` |
| Background re-embedding        | `src/internal/reembed/` (domain Service) + HTTP shell in `src/internal/server/reembed/` |
| Graph verify (integrity check) | `src/internal/graph/service.go::Verify` |
| Agent loop + execution plan    | `src/internal/orchestrator/service.go::AgentLoop` + `::ExecutionPlan` |
| Ollama / OpenAI HTTP (ResilientClient-wrapped) | `src/internal/ai/{client,embedder,extractor,reranker}.go` |
| HTTP handlers, strict decoder   | `src/internal/server/server.go` (mux shell) + `src/internal/server/middleware.go` + `src/internal/httputil/httputil.go::DecodeStrict` |
| Config state, hot reload        | `src/internal/serverstate/state.go` (`atomic.Pointer[State]`) + `src/internal/cli/env/env.go::EnvManager` |
| CLI dispatch (Cobra root)       | `src/internal/cli/root.go`        |
| CLI helpers, runtime Env        | `src/internal/cli/env/env.go`     |
| CLI subcommand groups          | `src/internal/cli/{memory,task,graph,time,agent,db}/<sub>.go` |
| Top-level CLI (`serve`, `health`, `metrics`, `version`) | `src/internal/cli/{serve,health,metrics,version}.go` |
| Binary entry-point              | `src/main.go`                     |
| Retention GC loop               | `src/internal/retention/` (domain Service) + `src/internal/server/retention/` (HTTP) |
| Health probes                   | `src/internal/health/` (domain Service) + `src/internal/server/health/` (HTTP) |
| Accelerate SIMD cosine (darwin) | `src/internal/vector/cosine_darwin.go` (build-tag `darwin && cgo`) |
| Pure-Go cosine fallback         | `src/internal/vector/cosine.go`   (build-tag `!darwin || !cgo`) |
| Coch-Granger cyclic-task safe scheduler   | `src/internal/store/task.go::BuildNode` (iterative work-stack DFS) |
| NaN/Inf-safe embedding read     | `src/internal/store/codec.go::BytesToFloat32Safe` |
| Per-package tests               | `src/**/*_test.go`                |
| Per-domain model projection contracts | `src/internal/core/{fact,evidence,episode,task,goal,belief}_test.go` (4 each) + `compose_test.go` (4) + `pairs_test.go` (35 subtests) — `go test -race ./src/internal/core/...` (64 total) |

---

## E2E Testing

### Running E2E Tests

```bash
# Run all E2E tests
make test-e2e

# Or directly with go test
go test ./tests/e2e/... -v -timeout 5m
```

### Test Structure

```
tests/e2e/
├── cli/                    # CLI command tests
│   ├── memory_test.go      # memory store, search, query, edge, ingest
│   ├── task_test.go        # task create, status, list, show, dep, tree
│   ├── graph_test.go       # graph verify, components, communities
│   ├── db_test.go          # db migrate, schema, verify, dry-run
│   └── top_test.go         # version, health, metrics, diagnose
├── http/                   # HTTP endpoint tests
│   ├── health_test.go      # /health, /health/live, /health/ready
│   ├── memory_test.go      # /store, /search, /query, /retrieve, /edge, /ingest
│   ├── persistence_test.go # Data persistence across restarts
│   └── auth_test.go        # API key authentication
├── helpers/                # Test helpers
│   ├── workspace.go        # Temporary workspace creation
│   ├── server.go           # Server startup/shutdown
│   ├── http.go             # HTTP client wrapper
│   ├── cli.go              # CLI command wrapper
│   ├── json.go             # JSON comparison utilities
│   └── scenario.go         # YAML scenario runner
└── fixtures/               # Test data fixtures
```

### YAML Scenario Runner

Scenarios in `testdata/scenarios/` define cross-interface tests:

```bash
# Run a specific scenario
go test ./tests/e2e/scenarios/ -run TestBasicMemoryScenario -v

# Run all scenarios
go test ./tests/e2e/scenarios/ -run TestAllScenarios -v
```

Available scenarios:
- `basic_memory.yaml` — Store, search, query, edge creation
- `task_planner.yaml` — Task lifecycle: create → status → dependencies → executable
- `contradictions.yaml` — Ingest contradicting facts, verify contradicts edges
- `provenance.yaml` — Store with provenance, query by conversation_id/message_id
- `retrieval.yaml` — Graph traversal, depth limits, explain mode
- `timeline.yaml` — Timeline ordering, temporal filtering
- `communities.yaml` — Connected components, community detection

### Writing New Tests

Every test starts from a clean temporary directory:

```go
func TestMyFeature(t *testing.T) {
    dir, _ := helpers.TempWorkspace(t)
    helpers.WriteConfig(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
    
    // CLI test
    cli := helpers.NewCLI(helpers.BinaryPath(t), dir)
    result := cli.Run(t, "memory", "store", `{"id":"e1","category":"world","content":"test"}`)
    result.MustSucceed(t)
    
    // HTTP test
    srv := helpers.StartServer(t, dir)
    client := helpers.NewHTTPClient(srv.URL)
    resp := client.Post(t, "/store", map[string]interface{}{
        "id": "e2", "category": "world", "content": "test2",
    })
    helpers.MustStatus(t, resp, 200)
}
```
