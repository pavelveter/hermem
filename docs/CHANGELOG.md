# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

- **P2 — SEMANTIC COMPRESSION**: `feat/semantic-compression` completes all 9 P2 SEMANTIC COMPRESSION items via the new `src/internal/compression/` package and migration `012_create_summary_nodes.sql`. All 909 tests pass race-safe. All 9 TODO.md items now [x].
  - **feat(compression)**: `SummaryNode` domain type + `summary` category registered in `DefaultSchemaConfig`. Conversion methods: `AsEntity()` and `EntityAsSummaryNode()`.
  - **feat(compression)**: `Clusterer` with greedy cosine-similarity clustering (configurable threshold/min/max); loads embeddings from SQLite entities table directly.
  - **feat(compression)**: `Compressor` with `Compress`/`CompressCluster`/`Recompress`/`Regenerate` methods. Uses `core.LLMExtractor` for LLM-backed summary generation.
  - **feat(compression)**: Recursive summarization with `Recompress` (Generation++, provenance chain preserved, max depth = 3 guard).
  - **feat(compression)**: Provenance preservation: `CompressedFrom` carries original entity IDs + old summary ID forward on recompression; `SupersededBy` edge tracks replacement.
  - **feat(compression)**: `Regenerate` refreshes summary content at same generation (same source set, updated content).
  - **migration**: `012_create_summary_nodes.sql` — `summary_nodes` table with content, compressed_from (JSON), compressed_at, confidence, provenance, generation, extractor_model, superseded_by FK, regenerated_at.
  - **test(compression)**: 4 cluster tests, 6 generate tests (compress, recompress, regenerate, provenance), 1 integration test (10 entities → cluster → compress → recompress → regenerate), 3 benchmarks (cluster/compress/recompress).
  - **feat(compression)**: Zero-dep `Metrics` struct with atomic counters for compress/recompress/regenerate calls, compressed entities, cluster sizes, duration tracking.
  - **docs**: TODO.md 9 items flipped + sub-agent-12 provenance comment added; CHANGELOG.md updated; USAGE.md `## Compression API` section added.

- **P2 — EPISODIC MEMORY**: `feat/episodic-memory` completes all 10 P2 EPISODIC MEMORY items via the new `src/internal/episodic/` package and migration `011_create_episodes_events.sql`. All 105 tests pass race-safe. All 10 TODO.md items now [x].
  - **feat(episodic)**: migration 011 — `episodes` (session/conversation FKs, title, summary, started_at, ended_at, JSON metadata), `events` (CHECK-constrained type: message | action | observation | system), `episode_memories` junction (with role), `episode_tasks` junction.
  - **feat(episodic)**: `Episode` domain type + `Service` (Create/Get/ListBySession/UpdateSummary/EndEpisode) — flat-package + stateless pattern.
  - **feat(episodic)**: `SessionService` (CreateSession/GetSession/EndSession/ListSessions) — adds the Go layer over migration 004's `sessions` table.
  - **feat(episodic)**: `EventService` with typed `EventType` enum (message | action | observation | system) — validates at the Go layer, SQL CHECK is the authoritative guard.
  - **feat(episodic)**: `LinkService` (LinkMemory/UnlinkMemory/ListMemoriesForEpisode/ListEpisodesForMemory) — idempotent via ON CONFLICT DO NOTHING; slim MemoryRef/EpisodeRef projections.
  - **feat(episodic)**: `TaskLinkService` (LinkTask/UnlinkTask/ListTasksForEpisode) — same idempotent pattern, no role column.
  - **feat(episodic)**: `TimelineService.ReconstructTimeline` merges events + linked memories + linked tasks into a single chronologically sorted feed; stable tiebreak on (kind, source_id).
  - **feat(episodic)**: `RetrievalService.SearchEpisodes` with filter struct (SessionID, TimeFrom, TimeTo, HasSummary, HasLinkedMemories, Limit) + optional semantic rerank via a local Embedder interface (cosine against `metadata.embedding`).
  - **feat(episodic)**: `Summarizer` stitches events + memories into a dialog, hands to `core.LLMExtractor`, formats extracted entities as a bulleted summary, persists via `UpdateSummary`.
  - **feat(episodic)**: `PlaybackService` renders the chronological feed as `[]PlaybackFrame` with per-kind Actor context; exports JSON, Markdown, and plain text.
  - **test(episodic)**: 5 integration tests (full pipeline, empty-episode, missing-LLM, parallel subtests, search-with-filters) using SQLite shared in-memory + MaxOpenConns(1) for -race safety.
  - **docs**: USAGE.md ## Episodic Memory section added; TODO.md 10 items flipped + sub-agent-11 provenance comment added.

- **P2 — MEMORY EVOLUTION (C3–C10)**: `feat/memory-evolution-rest` completes the remaining 8 sub-items of the P2 memory evolution track via the new `src/internal/evolution/` package and migration `010_add_belief_history.sql`. All 29 tests pass race-safe with store.MemDBRandom(). C3–C10 now [x] in TODO.md.
  - C3 — `evolution.PropagateConfidence` aggregates evidence by polarity (support/refute strength ratio) to update belief confidence.
  - C4 — `evolution.AggregateEvidence` with Sum/Avg/Min selectors for evidence strength aggregation.
  - C5 — `evolution.TrustScore` formula: confidence × sourceWeight × recencyFactor; configurable TrustWeights with defaults.
  - C6 — `evolution.TraceRevisions` uses recursive CTE (N+1-safe, MaxChainDepth=32) to walk parent_chain_id backward.
  - C7 — `evolution.ListActiveBeliefs` filters beliefs to Active-only by default, with includeSuperseded opt-in flag.
  - C8 — `evolution.GetSupportRefute` counts evidence rows by polarity in a single SQL query.
  - C9 — `evolution.RecordHistory` / `ListHistory` append-only belief_history table (migration 010).
  - C10 — `evolution.GetSupersededBy` / `StateAt` evolution queries using JOIN-style SQL (N+1-safe) + benchmark.
  - Migration: `010_add_belief_history.sql` — idempotent CREATE TABLE IF NOT EXISTS + two indexes.

- docs: TODO.md audit pass flips `[x] Add Grafana dashboard` and `[x] Add alert recommendations` to reflect the opencode merge at `81a624c`. The duplicate `[x] Add Belief abstraction` line in the P2 MEMORY EVOLUTION section is removed (single source of truth).
- **P1 — MIGRATION SYSTEM HARDENING**: Hardens the SQLite migration subsystem with dry-run mode, out-of-order detection, content-drift detection, concurrent-apply guards, schema snapshotting, down-migration test harness, and race-safe concurrent tests. New exported functions: `RunDry`, `DetectOutOfOrder`, `DetectContentDrift`, `CaptureSchemaHash`, `MigrationFiles`, `applyOneMigration`. Tests: +14 unit/race cases in `migration_test.go`.

- **P2 — MEMORY EVOLUTION (C1)**: `feat/memory-evolution` introduces the first-class `belief` package (`src/internal/memory/belief/`) plus migration `008_add_beliefs_table.sql`. `Belief.Service` exposes `CreateBelief`, `GetBelief`, `ListBeliefs`, `UpdateConfidence`, `MarkSuperseded` against a dedicated `beliefs` SQLite table (id, content, confidence [0,1], source_kind, source_id, status enum, timestamps, FK columns for superseded_by / parent_chain_id). The legacy thin `core.Belief` projection off `core.Entity` is preserved untouched for backward compatibility. Race-safe unit tests (8 cases) in `belief_test.go` exercise happy path, nil/empty rejection, default confidence, ordered listing, confidence bounds + ErrNotFound + id<=0 short-circuit, transactional MarkSuperseded + missing-target rollback.


### Fixed

- **ci**: Zig pin in `.github/workflows/{ci,release}.yml` repaired. `mlugg/setup-zig@v2` was pinned to `version: v2.12.2` (Zig 2.x has never shipped — every mirror responded 404) and `release.yml` used `version: latest` (silently drift-prone). Both files now pin to stable `0.16.0` via a shared `env.ZIG_VERSION` (mirrors the existing `env.GO_VERSION` precedent) and request `use-tool-cache: true` so the toolchain tarball is cached across matrix jobs. No code change; CI / release builds resume on the next push.
- **cross-compile**: Windows build targets fixed. `src/internal/health/probes.go` referenced `syscall.Statfs_t` / `syscall.Statfs` (Unix-only — undefined on `GOOS=windows`), which broke `windows/amd64` and `windows/arm64` matrix builds. Split into per-OS files mirroring the existing `src/internal/vector/cosine{,_darwin}.go` convention: `disk_unix.go` (`//go:build !windows`) keeps the `syscall.Statfs` path; `disk_windows.go` (`//go:build windows`) returns `nil` with `Severity: "warning"` so `/health/ready` stays operational. No new dependency (verified `golang.org/x/sys` absence in `go.mod` / `go.sum`). Both Windows builds resume.

### Added

- **P0 — Architecture hardening**: completed all critical architecture tasks.
  - **refactor(auth)**: `RequiredScopes` map made unexported (`requiredScopes`); added `RequiredScopesMap()` getter returning a safe copy to prevent external mutation.
  - **ci**: added `Architecture Guardrails` CI job that greps for `ActiveSchema()` and exported `RequiredScopes` usage.
  - **ci**: added `forbidigo` linter to `.golangci.yml` to catch `ActiveSchema()` at lint time.
  - **docs**: added `docs/package-level-audit.md` — full audit of all package-level variables across `src/`, categorised as safe/mutable-fixed/by-design.
  - **docs**: added `docs/service-dependencies.md` — complete dependency graph for all 12 domain services, HTTP shell wiring matrix, data flow diagram, and architectural properties.
  - **test(auth)**: added tests for `ScopeForPath` and `RequiredScopesMap`.

- **P0 — Retrieval explainability (ScoreBreakdown)**: full feature breakdown on every retrieved node and fact. `/query/explain` and any caller setting `opts.Explain=true` now get a `score_breakdown` object on each `GraphNode` and `RetrievedFact` carrying the seven canonical components (`vector_score`, `recency_score`, `temporal_score`, `centrality_score`, `path_score`, `depth_penalty`, `final_score`) so callers can understand *why* a node ranked where it did. Non-explain paths stay byte-compatible (breakdown omitted).
  - **feat(core)**: `core.ScoreBreakdown` struct + `ScoreBreakdown *ScoreBreakdown` field on `GraphNode` and `RetrievedFact` (omitempty).
  - **feat(retrieval)**: `ComputeScoreComponents` / `BuildScoreBreakdown` helpers in `retrieval/scoring.go` — single-pass feature arithmetic, NaN/Inf clamp preserved.
  - **feat(retrieval)**: walk.go attaches breakdown on the Explain path; fixed SeedNode copy to propagate breakdown into the seeds slice.
  - **feat(retrieval)**: one structured `slog.Info("retrieval.explain", ...)` per explain call — per-bucket counts + top-ranked breakdown per bucket (final/vector/recency/temporal/centrality/path/depth_penalty). Default path emits no log.
  - **test(retrieval)**: 9 new tests — breakdown field mapping, depth-penalty arithmetic, NaN clamp, non-explain backward compat, log emission / non-emission contracts.
  - **docs**: README, USAGE, TODO updated; CHANGELOG (this entry).

- **P1 — Retrieval cleanup**: the retrieval subsystem is now organised around five named pipeline stages, each file-isolated, each tracing-spanned, each benchmark-able. The dead `core.Reranker` field is wired in; tracing emits per-stage spans; benchmarks let operators measure per-stage cost.
  - **refactor(retrieval)**: `RetrieveContext` is now a 10-line orchestrator that calls `expandGraph` → `scoreAndRank` → `sortByScoreDesc` → `bucketize` → `applyReranker`. Behaviour-preserving.
  - **refactor(retrieval)**: `defaultCompositeScorer` delegates to `ComputeScoreComponents`; raw feature extraction lives in exactly one place. `ScoreComponents.Final(w)` lets `walk.go` derive the final score from already-computed components.
  - **refactor(retrieval)**: extracted `expDecayHours` helper so `recencyScore` and `temporalScore` share the decay math; temporal moved to its own file.
  - **feat(retrieval)**: `applyReranker` stage invokes `opts.Reranker.Rerank` per non-empty bucket. `core.Reranker` was previously plumbed through config / env / serverstate but never called from `RetrieveContext`.
  - **refactor(retrieval)**: graph expansion moved to its own file (`expand.go`) along with the internal `scannedNode` handoff type. Pipeline stages are now visible at the file level.
  - **refactor(retrieval)**: temporal scoring isolated in `temporal.go` (the CreatedAt decay axis); recency stays in `scoring.go` as the primary ranker signal.
  - **feat(retrieval)**: tracing spans per stage — `retrieval.expand_graph`, `retrieval.score_and_rank`, `retrieval.rank_sort`, `retrieval.bucketize`, `retrieval.rerank` (when `opts.Reranker` is set). `NoopTracer` zero-overhead contract preserved.
  - **feat(retrieval)**: per-stage benchmarks — `BenchmarkRetrieveContext`, `BenchmarkExpandGraph`, `BenchmarkScoreAndRank`, `BenchmarkBucketize`; `go test -bench=. -benchmem ./src/internal/retrieval/`.
  - **docs(retrieval)**: new `src/internal/retrieval/PIPELINE.md` — canonical pipeline reference (diagram, file layout, per-stage contract tables, observability, profiling, configuration knobs).
  - **test(retrieval)**: 11 new tests covering stage refactor + rerank + tracing + backward compat.
  - **test(retrieval)**: 6 new rerank unit tests + 3 tracing tests pin the per-stage observability contract.

- **P1 — Observability (Prometheus sprint, C1–C6)**:
  - **feat(observability)**: `prometheus/client_golang` v1.21.0 wired against hermem-owned `*prometheus.Registry` (not the global default); four domain duration histograms — `hermem_ingest_duration_seconds`, `hermem_retrieval_duration_seconds`, `hermem_contradiction_duration_seconds`, `hermem_rerank_duration_seconds` — exposed on `/metrics` in Prometheus text exposition format alongside the existing `expvar` JSON.
  - **feat(observability)**: `hIngest` → `*HistogramVec` labeled by `category` (`observation`, `world`, `task`, `edge`); pre-warmed with the `_init` sentinel so cold-start scrapes are zero-missing. `TestHermemPrefixContract_KnownCategoriesSet` regression test guards against label-domain drift.
  - **feat(observability)**: `hRetrieval` → `*HistogramVec` labeled by `mode` (`vector_only`, `vector_plus_graph`, `vector_plus_rerank`, `vector_plus_graph_plus_rerank`); guarded by `TestHermemPrefixContract_KnownModesSet`.
  - **feat(observability)**: `hContradiction` → `*HistogramVec` labeled by `detector` (`lexical`, `composite`; `semantic` reserved for phase 2.4). Guarded by `TestHermemPrefixContract_KnownDetectorsSet`.
  - **feat(observability)**: `hRerank` → `*HistogramVec` labeled by `strategy` (`llm_openai`, `llm_ollama`, `noop`); bound by `RerankerTimeout` (default 30 s). Guarded by `TestHermemPrefixContract_KnownStrategiesSet`.
  - **feat(server)**: `/metrics` registered by `Server.mount()` directly (after the AdminService dissolution); `X-API-Key` auth still applies when `[server] api_key` is set.
  - **feat(docs)**: `docs/USAGE.md` § 19.1 documents the Prometheus endpoint, the four labeled series with their bounded label sets, a sample scrape config, and the per-Vec regression-test recipe.
  - **ObserveRerankDuration(seconds)** signature is now **`ObserveRerankDuration(seconds, strategy)`** — the contract break is intentional and tracked alongside the other three `ObserveXxxDuration` calls.

## [v0.3.0] - 2026-06-25

Six production-ready groups land together: scoped multi-key auth,
offline admin ops, OpenTelemetry tracing slice, opt-in pprof profiling,
SHA-256 migration hardening, and an evaluation framework. The CLI
surface gains `admin`, `adminops`, and `profile` groups; the HTTP API
gains per-dependency health endpoints and admin-keys management.

### P1 — Auth hardening (multi-key scoped API keys, June 2026)

Scoped multi-key authentication with admin CLI, middleware enforcement,
and constant-time key comparison.

- **feat(auth)**: `Scope`, `Key`, `Authenticator` interface — `Authorize(raw, required) (*Key, bool, error)`.
- **feat(auth)**: `CanAccess` hierarchy (admin > write > read) + `ScopeForPath` URL-prefix routing; unmatched paths default to `ScopeWrite`.
- **feat(auth)**: `StaticAuthenticator` — constant-time key lookup via `subtle.ConstantTimeCompare`.
- **feat(config)**: `api_keys` INI parsing (`key:scope:label` comma-separated format); `api_key` single-key fallback with `ScopeAdmin`.
- **feat(config)**: `AddKeyToFile`, `RemoveKeyFromFile`, `RotateKeyInFile` — raw INI text manipulation for admin CLI.
- **feat(server)**: `AuthMiddleware()` — parameterless, health bypass, 401/403 JSON errors.
- **feat(server)**: `Serve()` uses `AuthMiddleware()` instead of `APIKeyMiddleware(cfg.APIKey)`.
- **feat(cli)**: `hermem admin keys {list,add,rotate,revoke}` with `GenerateKey` (32-byte CSPRNG → 64 hex).
- **test(auth)**: 11 scope tests, 7 authenticator tests, 9 admin-cli tests, 8 middleware enforcement tests.
- **docs**: USAGE §16 (API Authentication) documented key format, scopes, CLI, response codes, health bypass.
- **fix(server)**: `ErrInsufficientScope` check moved before generic `!ok` fallback to correctly return 403 instead of 401.

### P1 — Admin CLI (ops group, June 2026)

Offline database diagnostics and maintenance via the `hermem ops` command group.

- **feat(admin)**: `Stats`/`Issue`/`IntegrityReport` types for operational snapshots.
- **feat(admin)**: `StatsCollector` — parallel count queries (entity/edge/contradiction/archived/embedding-coverage) with single-flight caching.
- **feat(admin)**: `IntegrityChecker` — three checks: missing embeddings (critical ≥10), dangling edges (critical), and archived entities with stale vector-index entries (warning).
- **feat(admin)**: `VacuumRunner` — SQLite VACUUM with progress callback and bytes-reclaimed report.
- **feat(admin)**: `RebuildIndex` — selective vector-index rebuild with Category/Since/OnlyArchived/DryRun filters.
- **feat(cli)**: `hermem ops {stats,integrity,vacuum,rebuild-index}` command group (registered as `ops` to avoid collision with auth key management).
- **test(admin)**: 17 unit tests across admin package (Stats, Integrity, Vacuum, RebuildIndex).
- **test(cli)**: 6 CLI integration tests for all ops subcommands.
- **docs**: USAGE §18 documents all four commands, exit codes, issue codes, and examples.

### P1 — Observability (tracing slice, June 2026)

OpenTelemetry tracing scaffold with noop fallback, OTLP exporter gate,
context propagation, and instrumentation wrappers for retrieval, ingestion,
and memory pipelines.

- **feat(tracing)**: define `Tracer`/`Span` interfaces + `NoopTracer`/`NoopSpan` defaults.
- **feat(tracing)**: `NewTracerFromEnv()` — OTLP/gRPC exporter behind `TRACING_EXPORTER=otlp` env, falls back to `NoopTracer` when SDK unavailable.
- **feat(tracing)**: `WithSpan` / `SpanFrom` / `WithRequestID` / `WithTracer` / `TracerFrom` context helpers.
- **refactor(retrieval)**: `tracing.go` — `tracerFromOpts` helper via `core.RetrieveContextOptions.Ctx`.
- **refactor(ingestion)**: `tracing.go` — `ProcessDialogWithTracing` / `ProcessDialogWithProvenanceWithTracing` wrappers.
- **refactor(memory)**: `store_tracing.go` — `StoreWithTracing` / `StoreAndLinkWithTracing` wrappers.
- **feat(runtime)**: `Env.Tracer` field + `main.go` initialization from `TRACING_EXPORTER`.
- **test(tracing)**: 8 interface-compliance + round-trip tests.
- **smoke**: `TRACING_EXPORTER=otlp hermem version` logs and gracefully degrades; unset runs clean.

### P1 — Profiling suite (June 2026)

Opt-in runtime profiling without third-party sidecars. Two surfaces
share the same `runtime/pprof` backend, both off by default — zero
production-surface change unless the operator flips an env flag or
invokes the new CLI group.

- **`HERMEM_PPROF_ENABLED=1` mounts `/debug/pprof/*`** — `server.RegisterPprof(mux)` wires the stdlib handlers (Index, Cmdline, Profile, Symbol, Trace) when the env var is exactly `"1"`. Exact-match so a typo (`true`, `yes`, `on`, `TRUE`, `enabled`) cannot accidentally expose process internals. Off by default → endpoints return 404. Wired in `Server.mount()`.
- **`hermem profile {cpu,heap,goroutine,trace}`** — new top-level CLI group. CPU profile (seconds, protobuf → stdout), heap snapshot (→ `/tmp/hermem-heap.pprof`), goroutine dump (text → stdout), execution trace (seconds → `/tmp/hermem-trace.out`). Default duration 10s, overridable via positional arg or `--seconds`.
- **Tests** — `pprof_test.go` covers the env gate (disabled default, wrong-value rejection, enabled smoke) and a gated integration check that verifies the rendered `/debug/pprof/` index lists all eight profile names.
- **Docs** — `docs/profiling.md` documents both surfaces, the security model, and the `go tool pprof` / `go tool trace` analysis workflow.

### P1 — Migration system hardening (June 2026)

Eight-task migration hardening sprint adding SHA-256 checksums, dry-run,
extended rollback with `--target=N`, per-migration checksum display in
`migrate` status, enhanced `verify` output, integrity and recovery tests,
and documented workflow.

- **feat(migration)**: add SHA-256 migration checksums (`MigrationChecksumSHA256`,
  `checksum_sha256` column in `migration_checksums`, verify compares SHA-256).
- **feat(db)**: `hermem db migrate` shows per-migration SHA-256 and match/mismatch status.
- **feat(db)**: `hermem db dry-run` — lists pending migrations without applying.
- **feat(db)**: `hermem db rollback --target=N` — roll back all migrations after version N.
- **feat(db)**: `hermem db verify` — per-mismatch breakdown with stored/current checksums.
- **test(migration)**: 4 integrity tests (deterministic hash, tamper detection, null backfill).
- **test(migration)**: 3 recovery tests (empty-DB rollback, partial-apply recovery, target rollback).
- **docs**: `docs/migration-workflow.md` documents the hardened migration workflow.

### P1 — Evaluation Framework (June 2026)

- **Evaluation package** — `src/internal/evaluation/` with four information-retrieval metrics and a benchmark runner.
- **Recall@K** — `Recall(qrels, results, k) float64`. Fraction of relevant docs found in top-K across all queries.
- **Precision@K** — `Precision(qrels, results, k) float64`. Fraction of top-K results that are relevant, averaged across queries.
- **MRR** — `MRR(qrels, results) float64`. Mean Reciprocal Rank: average 1/rank of first relevant result.
- **NDCG@K** — `NDCG(qrels, results, k) float64`. Normalized Discounted Cumulative Gain with binary relevance.
- **Benchmark Runner** — `Runner.Run(ctx, dataset, retrievalFn) (Report, error)`. Executes a retrieval function against a dataset, computes all four metrics, returns a typed Report.
- **Report** — `Report{Dataset, Recall, Precision, MRR, NDCG, TotalQueries, K, RunAt}` with `Format() string` (human-readable) and `JSON() []byte` (indented JSON).

### PHASE 3.1–3.10 — God-object dissolution + flat-domain-pkg refactoring (June 2026)

Ten-phase architectural refactoring that dismantled the AdminService god-object,
dissolved the `algo/` package, and established a flat per-domain package
structure with a paired transport shell pattern.

**AdminService god-object dismantled across 5 phases of route extraction:**

- **PHASE 3.1** (`graph`): `/connected-components` + `/communities` + NEW `/graph/verify` → `graph.Service` + `server/graph/` HTTP shell.
- **PHASE 3.2** (`migration`): `/db/migrate`, `/db/rollback`, `/db/verify`, `/db/schema` → `migration.Service` + `server/migration/` HTTP shell (4 NEW routes, previously CLI-only).
- **PHASE 3.3** (`retention`): POST `/admin/retention/run` + `GarbageCollector` loop → `retention.Service` + `server/retention/` HTTP shell (NEW HTTP route, previously only a background goroutine).
- **PHASE 3.4** (`ingest`): `/ingest` + NEW GET `/ingest/jobs` → `ingest.Service` + `server/ingest/` HTTP shell (extracted from `server/memory` shell).
- **PHASE 3.5** (`edge` + `timeline`): `/edge`, `/timeline` → `edge.Service` + `timeline.Service` + `server/edge/` + `server/timeline/` HTTP shells (extracted from `server/memory` shell; memory keeps only `/store`).
- **PHASE 3.6** (`reembed`): `/admin/re-embed` → `reembed.Service` + `server/reembed/` HTTP shell (`algo/reembed.go` deleted).
- **PHASE 3.7** (`health`): `/health`, `/health/live`, `/health/ready` → `health.Service` + `server/health/` HTTP shell. AdminService slimmed to `/metrics` only (one route, one field, one constructor arg).
  - Follow-up: `health.Service.Ready()` refactored to return `(bool, map)` (transport-agnostic); HTTP shell maps bool → 200/503.
- **PHASE 3.8**: AdminService dissolved entirely — `/metrics` registered directly on `Server.mount()` via `Metrics *metrics.Metrics` field. `admin_service.go` deleted. The god-object is gone.

**`algo/` package dissolved across 3 phases:**

- **PHASE 3.9**: `VerifyGraph` inlined into `graph.Service.Verify()`; `algo/cache.go` deleted (EmbeddingCache, zero callers — dead code).
- **PHASE 3.10**: `AgentLoop` + `ExecutionPlan` + `resolveExecutableTasks` extracted into new `orchestrator.Service{db}` (CLI-only, no HTTP shell). `algo/verify.go` deleted; `algo/` directory removed. The pkg is entirely gone.

**Structural result:**

- 12 flat domain packages: `contradiction`, `edge`, `graph`, `health`, `ingest`, `memory`, `migration`, `orchestrator`, `reembed`, `retention`, `retrieval`, `task`, `timeline`.
- 12 per-domain HTTP shells under `server/`, each a thin `{Svc, Metrics, Refs}` struct with `Routes() map[string]http.HandlerFunc`.
- `Server` struct holds 12 `*HTTPService` fields + `Metrics`; `NewServer` 14-arg.
- `algo/` pkg deleted (`cache.go` dead code → `verify.go` VerifyGraph → `graph.Service` → `verify.go` AgentLoop → `orchestrator.Service` → empty → rmdir'd).
- Zero import cycles, zero god-objects, every domain service is transport-agnostic.

### Round-9 § 3 batch — atomicity, dedup safety, single-row archive, recoverable shutdown

- **§ 3.1 IngestBatch atomicity refactor** — `ProcessDialogWithProvenance` in `src/internal/ingestion/dialog.go` removes the legacy `BulkStore` pre-store path. Every per-entity `vi.Store` / `vi.Remove` now queues as a `viOp{store|remove, id, vec}` slice built during the decision phase (before BeginTx) and drained only AFTER `itemTx.Commit()` returns nil. The contradiction-archive UPDATE is folded INTO the same itemTx so it commits / roll-backs atomically with the new entity write. `applyVIOps` is a free function (not a method) depending only on `core.VectorIndex`. 5 regression tests in `dialog_test.go` lock all four doctrine branches (NEW / MERGE / LOW-CONF / ROLLBACK) plus the post-commit vi-failure surface contract.
- **§ 3.2 EntityLocker (FNV32 striped mutex)** — `src/internal/store/locker.go` shards entity-level locks across `shardCount` buckets using FNV-1a 32-bit hash, rounded up to the next power of two. `AcquireBatch(ids)` deduplicates against concurrent callers (non-reentrant `sync.Mutex` safety); keys released in reverse order. Shard count clamps to `1<<31` before the bit-trick so hostile configs cannot overflow `uint32` truncation.
- **§ 3.3 PurgeEntity (serializable tx + sentinel error)** — `src/internal/store/edge.go::PurgeEntity` uses `BEGIN IMMEDIATE` for writer-lock up-front + `sql.LevelSerializable` → `DELETE FROM edges WHERE source_id=? OR target_id=?` → `DELETE FROM entities WHERE id=?` → `COMMIT` → `vi.Remove(post-commit only)`. Sentinel `ErrPurgeEntityNotFound` for absent targets; nil-vi branch logs `slog.Warn` with structured `entity_id` / `db_purged` fields and returns cleanly (no panic).
- **§ 3.4 GarbageCollector (single-row-archive policy)** — `src/internal/algo/gc.go::GarbageCollector` uses raw `BEGIN IMMEDIATE` / `COMMIT` / `ROLLBACK` helpers instead of `BeginTx` (DEFERRED upgrade would open a window for parallel ingest-tx). Defensive `ROLLBACK` is guarded by `errorOccurred bool` so a Commit-returned rolled-back error still gets unwound. `errorOccurred` is set on every failed `ExecContext`. `vi.Remove` is folded into the cycle ONLY when no error occurred — partial-success policy is "skip vi.Remove, log the offending row, continue the cycle" rather than "ROLLBACK the entire cycle".
- **Russian negation + stem heuristic (§ 7 + § 7.1 round-9)** — `IsIngestionContradiction` in dialog.go runs TWO scans in series: (1) substring scan against a fixed `negWords` list (preserves round-7's 14 English/Russian regression cases), (2) stem-augmented scan via inline `stemRussian` / `stemPair` (round-9 § 7.1) catching bare-particle-`не`-flip-on-verb-lemma cases like `любит` vs `не любит` / `любила` / `полюбил`. The stemmer uses a 3-character minimum stem length so short prepositions never collapse onto the same canonical form.
- **Atomicity regression tests** — `dialog_test.go` ships 5 tests pinning the round-trip: `TestProcessDialogWithProvenance_VIOpFailureDoesNotFailCommit` (vi.Store returns error injection → DB row still present), `TestProcessDialogWithProvenance_FreshEntityStoresExactlyOnce` (1×Store + 0×Remove for fresh entity), `TestProcessDialogWithProvenance_MergeComposesRemoveBeforeStore` (Remove-before-Store ordering in MERGE branch), `TestProcessDialogWithProvenance_LowConfContradictionArchivesAtomically` (archive=1 atomic with new INSERT + post-commit vi drain), `TestProcessDialogWithProvenance_RollbackSkipsVIOps` (closed db → BeginTx fails → spy.Stores + spy.Removes both empty).
- **`failingVIRecord.BulkStore` no-op stub** — compile-fix: `core.VectorIndex` still declares `BulkStore` even though § 3.1 removed it from the runtime path. Test spy carries a no-op `BulkStore` so `NewIngestionWorker` accepts it at the static interface check; the stub also acts as the canary for any future regression that re-introduces BulkStore at runtime.
- **`bufferedHandler` LIMITATION godoc** — `src/internal/store/purge_test.go` documents that `WithAttrs` / `WithGroup` are no-op pass-through (chained attrs would silently drop), so test bodies must call `With(...).Info` and accept the warning emission at the default level.
- **`applyVIOps` is a free function** — round-9 second-round nit closure: `func applyVIOps(ctx context.Context, vi core.VectorIndex, ops []viOp)` expresses the actual dependency accurately and matches the codebase style for pure-passthrough helpers (compare `IsIngestionContradiction`, `isSQLiteBusyError`).
- **Drop redundant `vector.NormalizeVector` from worker.go** — round-9 second-round nit closure: `ProcessDialogWithProvenance` already normalizes once (idempotent + fast); `createEntityInTx` / `mergeEntityInTx` now document the precondition "caller MUST pass a unit-length-normalized embedding" via godoc rather than re-normalizing internally.
- **SIGINT exit 130** — `src/main.go` maps signal-driven `context.Canceled` to exit code 130 (POSIX 128 + SIGINT 2) after `cli.NewRootCommand(&env).Execute()`, in both the err-Execute and the no-err branches. Shell wrappers can now distinguish a Ctrl-C'd invocation (130) from a normal completion (0).
- **EPIPE suppression via SIGPIPE ignore** — `signal.NotifyContext` extends to `signal.Ignore(syscall.SIGPIPE)` at startup. A piped downstream consumer (`hermem memory query ... | head -1`) closing early no longer propagates a Go stack-trace; subsequent `os.Stdout.Write` returns EPIPE which `clienv.WriteStdout` already maps to nil.
- **`Config.Validate()` fail-fast** — `src/internal/config/ini.go::Validate()` already shipped pre-round-9, called from `main.go` after `LoadConfigFromBinaryDir`. `vector.dim ≤ 0`, `extraction.timeout ≤ 0`, malformed `embedder.url`, etc., return concrete errors rather than panicking.

### Round-7 P2 batch (Russian negation, DEADCODE marker, SKILL bump)

- **Russian negation list extension (§ 7)** — `IsIngestionContradiction` in `src/internal/ingestion/dialog.go` catches bare Russian negation particles (` не `, ` нет `, ` никогда `, ` ни за что`) plus common inflections of `любить` / `ненавидеть` / `хотеть` and the idiom `не нравится`. Doc comment surfaces the trade-off (high recall on listed forms; brittleness on rarer morphology). 14-case table-driven test in `dialog_test.go` pins the English baseline + the new Russian regression traps.
- **MemoryWorker DEADCODE annotation (§ 4)** — `cli/memory/ingest.go` uses `ProcessDialog` (one-shot); the channel-based `MemoryWorker` is reserved for external batch consumers. Doc comment explicitly forbids removal absent the planned P1 § 4 checkpoint work landing.
- **SKILL.md version bump (§ 9a)** — `version: 0.1.0` → `version: 0.2.0`. Power-curated installations pinning `=0.1.0` will need to update the pin.
- **USAGE.md § 10 schema table (§ 9c)** — added the `degree` / `priority` rows under `entities` (migrations `005_centrality.sql` / `007_task_priorities.sql`) and the `weight` row under `edges` (migration `006_weighted_edges.sql`). Narrative below the table already cited these columns in adjacent prose, so no inconsistency.

### Round-8 / TODO § 4 — ingest worker checkpoint + drain

- **§ 4.1 Checkpoint partial batches on ctx cancellation** — `MemoryWorkerResilient` (new companion to the legacy `MemoryWorker`) in `src/internal/ingestion/dialog.go` writes a JSON `IngestionCheckpoint{LastCommittedIndex, LastCommittedAt, WorkerID}` per successful `ProcessDialogWithProvenance`. Atomic-counter-unique tmp filenames + POSIX-atomic `os.Rename` for crash-safe writes. Each goroutine writes a LOCAL `IngestionCheckpoint` copy so concurrent flushes never race on a shared struct field. 9-case `checkpoint_test.go` table-driven test pins round-trip, missing/corrupt fallback, atomic-rename, and 16-goroutine concurrent safety.
- **§ 4.2 Drain the channel on ctx cancel** — same function drains the unprocessed channel buffer into a JSONL side file (`pendingPath`) bounded by a 5s deadline (`defaultDrainTimeout`) so a producer that doesn't close its channel cannot stall the worker. JSONL round-trip test confirms producer-side replay fidelity.
- **MemoryWorker doc comment updated** — explicit "ZERO in-tree callers" framing + `grep -rnF MemoryWorker src/internal/ | grep -v _test.go` audit one-liner. Both functions ship side-by-side for future callers.
- **MemoryMessage JSON tags** — `json:"dialog" | json:"conversation_id" | json:"message_id"` on `src/internal/core/types.go` so the `pending.jsonl` drain file is readable by Go AND any external producer/language that consumes it on restart.

### Breaking changes (CLI surface)

- **Cobra-grouped CLI surface (commit `8f0bf71`).** The flat 26-command
  registry (`src/cmd/<name>.go` + `init()`-driven `Register`) is gone.
  Hermem now ships a single cobra command tree under `src/internal/cli/`:
  - `serve | health | metrics | version` (top-level)
  - `memory {store,search,retrieve,query,response,edge,ingest,explain,re-embed,quantize}`
  - `task {status,list,show,dep,tree,create,rollback,executable}` (alias `next`)
  - `graph {plan,recovery-plan,components,communities,verify,contradictions,provenance}`
  - `time {temporal,timeline}`
  - `agent {loop}`
  - `db {migrate,rollback,verify,schema}`
  Examples: `hermem memory store …` (was `hermem store`), `hermem task status …`
  (was `hermem task-status`), `hermem db rollback` (was `hermem migration-rollback`),
  `hermem graph components` (was `hermem connected-components`),
  `hermem serve --port 8420` (port is a real cobra flag, no longer a positional arg).
- **No back-compat aliases.** Every previously-flat command name is
  permanently removed. Any script that ran `hermem store`, `hermem ingest`,
  `hermem task-executable`, `hermem execution-plan`, `hermem re-embed`, etc.
  must be rewritten to the grouped form.
- **`hermem --help`** now renders the full cobra command tree instead of
  the legacy single-line `printUsage` block. Each group's `--help`
  prints only its own subcommands (`hermem task --help`, etc.).
- **`hermem version`** is new; prints ldflags BuildInfo (Version /
  BuildDate / GitCommit).

### Implementation notes

- `src/internal/cli/env/env.go` — new sub-package hosting the `Env`,
  `BuildInfo`, `ReadStdin`, `DecodeStdin`, `DecodeString`, `WriteJSON`
  helpers. Split out of the cli/ root package so per-group subpackages
  (`cli/memory`, `cli/task`, …) can depend on the types without forming
  an import cycle with the orchestrator (which itself depends on the
  groups for their `NewCmd(env)` factories).
- ~36 new files in `src/internal/cli/`; all of `src/cmd/` deleted
  (~1500 lines of duplicated dispatch replaced with ~1900 lines of
  focused cobra commands).
- All `log.Fatalf` calls converted to `return fmt.Errorf(...)`; cobra's
  error renderer formats them, main.go handles exit code 1.
- `os.Exit(1)` paths (`graph verify`, `db verify`) return errors
  instead of syscalling; same external exit-code behavior.
- `provenance` and `re-embed` flags now real cobra flags (no more
  manual `argTail()` parsing).
- `cli/time/*.go` aliases stdlib `time` as `stdtime` because the
  package name collides.

### Validation

`gofmt`, `go vet`, `go build`, `go test ./src/...`, `-race
./src/internal/cli/...`, `CGO_ENABLED=1 ./src/internal/cli/...` all green
post-write.

### Added
- **Sprint 4: Versioned migration system** — `schema_migrations` table tracks applied versions. `runMigrations` reads embedded SQL files from `src/migrations/` (001_initial_schema, 002_entity_metadata, 003_provenance), applies unapplied files in ordered transactions. `hermem migrate` CLI shows status. Replaces the old ad-hoc `migrateSchema`.
- **Sprint 4: Schema fingerprinting** — `HashSchema(schema)` produces deterministic SHA-256 fingerprint via JSON + sorted map keys. `CheckSchemaFingerprint` compares stored vs current on startup. `hermem schema` CLI. `SchemaConfig.Fingerprint()` method.
- **Sprint 5: Configurable ranking weights** — `[ranking]` config section with `vector_weight`, `recency_weight`, `depth_penalty`, `recency_half_life_hours`. `RankingWeight` struct threaded through `RetrieveContextOptions`. `defaultCompositeScorer` now a factory accepting weights. Zero-valued weights substituted with defaults (0.7/0.3/0.05/720h) for backward compatibility.
- **Sprint 5: Optional Reranker** — `Reranker` interface with `OllamaReranker` (cross-encoder `/api/rerank`) and `OpenAIReranker` (chat-based ranking). Follows the same `ollama`/`openai` provider convention as embedder and extractor. `Config.NewReranker()` returns nil when `[reranker].provider` is empty. Reranker fires after graph expansion; errors fall back to original order.
- **Sprint 4: Dynamic config reload via SIGHUP** — `serve` mode reloads `hermem.ini` on SIGHUP without restart. Server uses `atomic.Pointer[ServerState]` for lock-free schema reads. `Server.ReloadState` atomically swaps state across all handlers.
- **Sprint 1 refactor** — Structural overhaul: globals removed, explicit schema threading, transactional ingestion, FK enforcement, graph integrity CLI.
  - Dropped global `activeSchema` (`SetActiveSchema`/`ActiveSchema`). All functions now take explicit `schema SchemaConfig` parameter.
  - Dropped global `iniRef`. INI parser state now scoped to `LoadConfig` via local closures.
  - New `Runtime` struct (`src/runtime.go`) bundles DB, VI, Embedder, Extractor, Config — built once in `main.go`.
  - Transactional ingestion: `ProcessDialog` wraps entity INSERT + edges INSERT in a single per-item SQL transaction — no half-written graph states.
  - Foreign-key enforcement: `_fk=true` in DSN, ON DELETE CASCADE on edges, verified with post-init PRAGMA check.
  - `verify` CLI command: checks entity count, edge count, embedding count, corrupt blobs, orphan edges, invalid status, invalid relation types. Exits 0 when clean.
  - `VerifyReport` struct with `Pass()` and formatted text output; `VerifyGraph(db, schema, dim)` performs the check.
  - `NormalizeVector` called before `vi.Store` in both merge and non-merge ingestion paths; merge-path `vi.Store` deferred to post-commit.
- **Sprint 2** — Memory provenance, entity metadata, and retrieval explainability.
  - Entity metadata: `confidence`, `source`, `source_type`, `created_at`, `valid_from`, `valid_to` columns on `entities` table with ALTER TABLE migrations.
  - Memory provenance: `conversation_id`, `message_id`, `extracted_from` columns track where each entity was extracted from. `Provenance` struct threaded through `ProcessDialogWithProvenance` → `createEntityInTx` / `mergeEntityInTx`.
  - `MemoryMessage` extended with `ConversationID` and `MessageID`; `MemoryWorker` passes them through.
  - Retrieval explainability: `RetrievedFact` gains `vector_score`, `recency_score`, `depth_penalty`, `ranking_score` breakdown fields (populated when `RetrieveContextOptions.Explain = true`).
  - `/query/explain` HTTP endpoint and `explain` CLI command run the full pipeline with score breakdown per fact.
  - `orNullTime` helper for nullable timestamp columns in INSERTs.
- `extraction.provider` / `extraction.url` / `extraction.key` config keys with fallback to `[embedder]` values.
- `PRAGMA auto_vacuum = INCREMENTAL` in `InitDB` — `vacuumAfter()` now works.
- Auth middleware: `server.api_key` config key, validated via `X-API-Key` header (empty = disabled).
- `RetrieveContextOptions.Ctx` for request-id propagation through `withReqID`.
- `id_map` table in core schema (replaces per-backend `vec_id_map`).
- Retention config parsing: `retention.observation_ttl`, `retention.run_interval`, `retention.batch_size`.
- `InMemoryVectorIndex.flatMatrix` — pre-built row-major matrix, maintained incrementally on Store/Remove; eliminates per-search matrix rebuild.
- `embedder.timeout` and `extraction.timeout` config keys (default 30s / 300s).
- Vector normalization at ingest — embeddings stored as unit vectors, Search skips norm division.
- Graceful shutdown: HTTP drain → GC cancel → metrics flush → DB close, in order.
- `--help` / `-h` CLI flag short-circuits before any DB work and prints a block-glyph HERMEM banner followed by the command reference (stdout, exit 0). The no-args path now also prints the banner (stderr, exit 1). Banner is plain text everywhere — no ANSI escapes leak into piped output or test captures.
- **Schema validation compiler** (Phase 7) — `ValidateSchema()` checks duplicate states, stateful_categories requires valid_states, state_unblocking ∈ valid_states, blocking/recovery ∈ allowed_relations. Integrated into `Config.Validate()` — runs at startup and on SIGHUP reload. Fail-fast on invalid schema.
- **Health levels** (Phase 6) — `/health/live` always returns 200 (liveness probe). `/health/ready` pings DB, returns 503 with per-dependency status if degraded (readiness probe).
- **Vector index dedup** (Phase 5) — Removed `vec []float32` from `vectorEntry`; vectors live only in `flatMatrix`. ~50% RAM reduction on entries slice metadata.
- **sync.Pool for search buffers** (Phase 5) — `dotPool` + `intBufPool` reuse dot-product and index buffers across `Search`/`SearchBatch`. Lower GC pressure on hot search paths.
- **Contradiction detection** (Phase 3) — `isContradiction(existing, incoming)` heuristic (negation asymmetry, sentiment-opposite pairs via ~45 inflected-form antonym pairs). On contradiction: creates `contradicts` edge, forces separate node instead of merge. No LLM needed.
- **Temporal memory retrieval** (Phase 10) — `RetrieveContextOptions.TimeFrom/TimeTo` filters CTE graph walk by `created_at` range; time filter in both anchor and recursive arms. `/query/temporal` endpoint + `temporal` CLI.
- **Episodic memory** (Phase 10) — `sessions` + `conversations` tables via `004_episodic_sessions.sql` migration; `idx_entities_created_at` index. `/timeline[?limit=N]` endpoint + `timeline [limit]` CLI.
- **Contradiction graph** (Phase 10) — `ContradictionPair` type (snake_case JSON); `GetContradictions(db, entityID)` bidirectional filter. `/contradictions[?id=X]` endpoint + `contradictions [entity_id]` CLI.

### P1 — Graph analytics & provenance (June 2026)

- **Graph centrality scoring** — `degree INTEGER DEFAULT 0` column on entities, auto-maintained via SQL triggers on edges INSERT/DELETE. `RankingWeight.CentralityWeight` (default 0.05, INI-parsed as `centrality_weight`). `log10(1+degree)` normalisation in `defaultCompositeScorer`. `Degree` field on `Entity`, selected in graph walk CTE.
- **Weighted edges** — `weight REAL DEFAULT 1.0` column on edges (migration `006_weighted_edges.sql`). `Weight` field on `Edge` struct, `EdgeRequest`, and `AddEdge` signature (4th param). CTE `path_weight` accumulator: `COALESCE(ed.weight, 1.0)` per hop. `compositeScore` uses `pathWeight` instead of integer `depth` for penalty. `GraphNode.PathWeight` field.
- **Provenance APIs** — `GetEntitiesByProvenance(db, convID, msgID, source, limit)` returns entities by memory origin (conversation, message, source with LIMIT). `HandleProvenance` GET handler at `/provenance?conversation_id=X&message_id=Y&source=Z&limit=N`. `provenance` CLI command with `--conversation`, `--message`, `--source`, `--limit` flags.
- **Execution plan CLI** — `execution-plan` CLI command shows priority-sorted topological task plan for a goal via `ExecutionPlan(db, schema, goalID)`.

### P2 — Task management & graph algorithms (June 2026)

- **Task priorities** — `priority INTEGER DEFAULT 0` column on entities (migration `007_task_priorities.sql`). `Entity.Priority` field. `ExecutionPlan` orders by `priority DESC`. `getExecutableTasksForGoal`/`Global` order by `COALESCE(priority, 0) DESC`. `ListTasks`/`GetRootTasks` SELECT priority. `scanTaskEntities` scans priority via `sql.NullInt64`.
- **Critical path analysis** — `CriticalPath(db, schema, goalID)` CTE walks longest weighted path from leaf to goal, reconstructs path via `blocked_by` edges. Returns ordered path slice + total path weight.
- **Recovery plan generation** — `GenerateRecoveryPlan(db, schema, failedTaskID)` walks `recovers_via` chain, returns ordered recovery task sequence with cycle detection. `HandleRecoveryPlan` at `GET /recovery-plan?id=X`. `recovery-plan` CLI command.
- **Graph clustering (connected components)** — `FindConnectedComponents(db, minSize)` BFS-based, all edges/relation types. `ConnectedComponent` type with `IDs`, `Size`, `AvgDegree`. Results sorted by size descending via `sort.Slice`. `HandleConnectedComponents` at `GET /connected-components?min_size=N`. `connected-components [min_size]` CLI.
- **Community detection (Louvain)** — `DetectCommunities(db, maxIterations)` one-pass modularity optimisation. Builds symmetric adjacency from edges, iterates node moves by ΔQ, returns `[]Community` with per-community modularity + global Q. `HandleCommunities` at `GET /communities?min_size=N&max_iterations=N`. `communities` CLI with `--min-size`/`--max-iterations` flags.
- **Background re-embedding** — `NeedsReEmbed(db, configuredDim)` detects dimension drift from `meta.embedding_dim`. `ReEmbedAll(ctx, db, vi, embedder, configuredDim, batchSize, model)` batch re-embeds all entities with content, updates DB BLOBs + vector index per batch, verifies returned embedding dimension. Progress logging per batch + context cancellation support. Updates `meta.embedding_dim` on completion. `HandleReEmbed` at `POST /admin/re-embed` with `{dim, batch_size?, model?}`. `re-embed` CLI with `--batch-size`/`--model` flags.
- **Embedding cache (LRU)** — `EmbeddingCache` map + doubly-linked list with `Get`/`Put`/`Invalidate`/`Size`. Wired into `InMemoryVectorIndex` (`storeLocked` calls `cache.Put`, `Remove` calls `cache.Invalidate`). Single `sync.Mutex` for simplicity (Get mutates LRU order).
- **Vector quantization** — `QuantizeVector`/`DequantizeVector` scalar int8 compression (8+d bytes vs 4d bytes). `QuantizedEmbeddingToBytes`/`BytesToQuantizedEmbedding` BLOB serialisation. `QuantizeBatch`/`DequantizeBatch` batch helpers. `quantize` CLI for testing roundtrip + compression ratio.

### Restored
- **`src/internal/vector/cosine_darwin.go`** — Apple Accelerate (cblas) AMX/NEON fast path reinstated after the package-split refactor lost it. `VectorNorm` → `cblas_snrm2`; `NormalizeVector` → `cblas_snrm2` + `cblas_sscal`; `CosineSimilarity` and `CosineSimilarityWithNorm` → `cblas_sdot` + `cblas_snrm2`; `BatchDotProducts` → `cblas_sgemv` (row-major). Build-tag `//go:build darwin && cgo` so non-darwin or `CGO_ENABLED=0` builds fall through to pure-Go `cosine.go` (now strictly tagged `//go:build !darwin || !cgo`). Expected gain on 768-dim × N-entity batch cosine on Apple Silicon: ~5-15× kernel-level speedup over the pure-Go loop (modern Go has some Float32 autovec — cblas sgemv hits ~50-100 GFLOPS for M=200, N=768); end-to-end retrieval is smaller because SQL + allocations dominate. Bad input panics loudly via bounds-bumps, matching the pure-Go fallback's panic-on-bad-input contract. CGO already enforced by `Dockerfile` and `install.sh` (for `mattn/go-sqlite3`) — no build-infra change required.
- **`src/internal/vector/cosine_darwin_test.go`** — Cgo parity tests: cblas-driven results must agree with the pure-Go reference within `1e-4` (single-vector functions) and `5e-4` (BatchDotProducts on magnitudes up to ~50 — about 1e-5 relative). 768-dim realistic shapes, plus panic-loudness tests for short-query / short-dot.

### Changed
- `IngestionWorker` schema is now directly swappable (maps are immutable after construction).
- `Server` schema/validCategories/validRelationTypes consolidated into `atomic.Pointer[ServerState]` for SIGHUP-safe reload.
- All `.go` files moved to `src/` — build path is now `./src`.
- INI parser replaced with `gopkg.in/ini.v1` (production-grade, handles quoting, multiline, comments).
- `NewOllamaEmbedder`, `NewOpenAIEmbedder`, `NewOllamaLLMExtractor`, `NewOpenAILLMExtractor` signatures accept `timeout.Duration`.
- `InMemoryVectorIndex.Search` uses snapshot pattern (RLock → local vars → unlock before compute) for concurrent safety without serializing searches.
- `SearchBatch` reuses `flatMatrix` for all queries in a batch.

### Fixed
- `out.txt` added to `.gitignore`, removed from tracking.

### Benchmarks
- `BenchmarkRetrieveContextStarPrecompute` / `BenchmarkRetrieveContextStarRecompute` (both N=500, star graph, depth=1, dim=768, in-memory backend): post-#17 path uses `defaultCompositeScorer` (cached queryNorm via `CosineSimilarityWithNorm` — one sqrt per row), pre-#17 path uses a `CompositeScorer` override that calls `CosineSimilarity` directly (two sqrts per row). Both columns are reproducible with a single `go test -bench` invocation on the same harness. The relative delta (one-vs-two sqrts) is linear in N.

  Reproduce:
  ```
  go test -count=1 -bench='BenchmarkRetrieveContextStar' -benchtime=20x -run='^$' -benchmem ./src/...
  ```

### Plugin — agent tool surface (v0.2.0)

`plugins/memory/hermem` now exposes ten tools to Hermes Agent (was three). New schemas added: `hermem_edge`, `hermem_retrieve`, `hermem_timeline`, `hermem_contradictions`, `hermem_task_create`, `hermem_task_status`, `hermem_task_list`. The three legacy schemas (`hermem_search`, `hermem_store`, `hermem_query`) are preserved verbatim so existing installations keep working.

Plugin internals:

- `_call(path, data)` keys by HTTP path / cobra-style positional arg. Nested commands like `task/create` are split on `/` and passed as positional args to the CLI binary so the new cobra tree is reached transparently (HTTP path is the same).
- `_cli_args(path)` is the single point that translates `memory store`-style paths into `["hermem", "memory", "store"]` lists for `subprocess.run`.
- `_http` recognises 400/422 as expected rejection noise (logged at info) rather than as an unexpected error, so an LLM agent's malformed payloads don't pollute the warning stream.
- `_cli` distinguishes `TimeoutExpired` from `FileNotFoundError` from generic `Exception` and logs each at a different severity; the timeouts are exposed as module constants (`_DEFAULT_CLI_TIMEOUT_S=10`, `_DEFAULT_HTTP_TIMEOUT_S=5`).
- `_json_result(resp, default_error)` consolidates the None → error envelope + non-None pass-through pattern used by every new dispatch handler.

Excluded-from-tool-surface (operator-only, edge cases): `/admin/re-embed`, `/connected-components`, `/communities`, `/query/explain`, `/health*`, `/metrics`, `/task/rollback`, `/recovery-plan`, `agent-loop`. Rationale in the plugin source godoc.

`plugin.yaml` bumped from 0.1.0 to 0.2.0. README's "Plugin tools" table now lists all ten.

  Snapshot (macOS, darwin/arm64, Accelerate cblas_sdot, GOOS=darwin):

  | bench                                 | ns/op         | B/op      | allocs/op |
  |---------------------------------------|---------------|-----------|-----------|
  | BenchmarkRetrieveContextStarPrecompute | 277_712_844   | 5_339_679 | 11_595    |
  | BenchmarkRetrieveContextStarRecompute  | 324_184_298   | 5_339_601 | 11_594    |

  The `Precompute` row pays one sqrt per row (normB only); the `Recompute` row pays two (query + node). Wall-clock figures vary by host; relative gap is stable. Re-running the bench refreshes both rows.

### Multi-hop — iterative seed expansion

`retrieval.MultiHopRetrieveContext` is now a real multi-hop walk, not a passthrough to `RetrieveContext`.

Algorithm (Design A — Iterative Seed Expansion):

1. Shallow graph walk from the just-discovered seeds (`MaxDepth=ShallowDepth=1`).
2. Pick top-`TopKPerHop=2` facts across all retrieval buckets + seed contents, ordered by `RankingScore` desc with `Content` asc tiebreak for determinism.
3. Embed each fact's content via the supplied `Embedder`.
4. `VectorIndex.SearchBatch` for `VectorTopK=3` neighbours per embedding — the "vector jump" across topological gaps.
5. Union new IDs into the accumulated seed set; break the loop early when no new seeds, the seed set is empty, or the budget (`MaxTotalMultiHopSeeds=20`) is hit.
6. Final call: a single `RetrieveContext` over the union of all seeds owns dedup-by-content, ranking, and bucket-population.

Hardening:

- Empty-seeds short-circuit at the top: tolerates `len(seedIDs)==0` with nil vi/embedder/db.
- Three `ctx.Err()` checkpoints per iteration (loop entry, before embed round-trip, before `SearchBatch` round-trip).
- `sort.Strings(finalSeeds)` before the final call for stable SQL IN-clause ordering (Go map iteration is randomized, and the parameter order influences `ORDER BY depth ASC, category ASC` ties).
- Tuneables renamed to CamelCase (`MaxTotalMultiHopSeeds`, `TopKPerHop`, `VectorTopK`, `ShallowDepth`) and discoverable via grep; still function-local `const`s — not externally tunable.

`opts.MultiHopCount` semantics:

| Value | Behaviour |
| :--- | :--- |
| `≤0` (unset) | **NEW default: 2 hops.** Requires `vi` + `embedder`. |
| `1` | Strict passthrough to `RetrieveContext`. Nil `vi`/`embedder` allowed. |
| `≥2` | Iterative expansion; nil `vi`/`embedder` returns an error. |

BEHAVIOUR-CHANGE NOTE: callers that switch from `RetrieveContext` to `MultiHopRetrieveContext` without explicitly setting `MultiHopCount` enter the new 2-hop path and MUST supply `vi` + `embedder` or the call errors with `"multi-hop (count=N) requires non-nil VectorIndex and Embedder"`. Existing direct `RetrieveContext` callers (`GenerateResponse` in `response.go`, every `retrieval_service.go` handler) are UNAFFECTED since the migration is opt-in.

Tests:

- `TestMultiHopRetrieveContext_PassthroughOnCountOne` — `MultiHopCount=1` still delegates (back-compat path).
- `TestMultiHopRetrieveContext_DiscoversDisconnectedSubgraph` — headline test: two topologically disconnected subgraphs (`a→b`, `c→d`) with semantically identical `alpha`/`delta` vectors. Multi-hop pulls `delta` into the seed set via vector jump; the final walk then reaches `gamma` via the `c→d` edge.
- `TestSingleHopRetrieveDoesNotCrossTopologicalGap` — negative control: single-hop definitively cannot reach `delta` from `a`.
- `TestMultiHopRetrieveContext_EmptySeedsReturnsEmptyResult` — short-circuit tolerates nil vi/embedder/db.
- `TestMultiHopRetrieveContext_RequiresIndexAndEmbedderWhenCountGTE2` — `MultiHopCount≥2` with nil deps errors instead of silently degrading.

A `TODO(retrieval/tests)` breadcrumb at the end of `walk.go` flags two tracked followups: assertion coverage for the three loop-break conditions (`nextSeeds empty`, `accumulated > MaxTotalMultiHopSeeds`, `currentSeeds empty`), and the per-hop seed-content re-embed redundancy in `topKFromResult`.

### PHASE 4 P0 — Entity model refactor (June 2026)

Nine-phase per-domain projection refactor that split the 19-field
`core.Entity` into 5 typed per-domain models (Fact, Evidence, Episode,
Task, Belief) + a Goal view (no new field). Entity remains the
umbrella persistence view mapped onto the SQLite `entities` row.

**Per-domain models introduced:**
- `Fact{ID, Category, Content, Embedding}` — the semantic claim.
- `Evidence{Confidence, Source, SourceType}` — quality meta-block.
- `Episode{ConversationID, MessageID, ExtractedFrom}` — provenance.
- `Task{Status, ValidFrom, ValidTo, Priority}` — lifecycle + priority.
- `Goal` — re-views Task's shape with `Category="goal"` intent.
- `Belief{CreatedAt, UpdatedAt, LastAccessedAt, Archived, Degree}` — persistence / retention / centrality.

**Helpers:**
- `Entity.AsX()` — decompose (X ∈ {Fact, Evidence, Episode, Task, Belief, Goal}).
- `X.AsEntity()` — recompose any individual band onto Entity.
- `core.Compose(Fact, Evidence, Episode, Task, Belief) Entity` — free function field-by-field wires all 19 fields. Goal bridges via inline 4-field copy (no `Goal.AsTask()` method).

**Tests:**
- 8 test files in `src/internal/core/`: per-model contract (4 each × 6 models = 24), Compose helper (4), cross-pair projection matrix (35 subtests) = **64** tests/subtests under `-race`.

Backward-compat layer preserved — all prior tests pass unchanged.
README.md (Features + Architecture) and USAGE.md §15 (Domain Models)
document the new type landscape and mark Entity as the umbrella
persistence view.

## [PR9] — Retention, auth, id_map, CTE filters

### Added
- `last_accessed_at` and `archived` columns on `entities` + `meta` table for schema versioning.
- `RetentionPolicy` (ObservationTTL, RunInterval, DeleteBatchSize) and `GarbageCollector` loop.
- `GarbageCollector` runs hourly in `serve` mode; `archiveStale` + `incremental_vacuum` after each cycle.
- `touchAccessedBatch` updates `last_accessed_at` after vector search.
- `archived = 0` filter in CTE anchor and recursive arms.
- `withReqID` helper + nil-safe `getReqID` for structured slog with `request_id`.
- `SearchBatch` method on `VectorIndex` interface (eliminates N+1 during ingestion).
- `InMemoryVectorIndex` RAM cache (`sync.RWMutex` + `[]vectorEntry` + `map[string]int`), loaded once at startup.
- Accelerate framework via CGo: `cosine_darwin.go` uses `cblas_sdot` (NEON SIMD), build-tag isolated from `cosine.go`.

### Changed
- `FNV-1a` hash for sqlite-vec rowid replaced with `id_map` AUTOINCREMENT dict table.
- `entityRowID` removed; `ensureEntityID` in core `db.go` is the single source of truth.
- `EmbeddingToBytes` is pure stdlib (no CGO dependency).
- `sqlite_vec` isolated via build tags (`db.go` no longer imports sqlite_vec).
- `[extraction]` section in INI: `provider`, `url`, `key` override embedder values when set.

## [PR8] — sqlite-vec

### Added
- `VectorIndex` interface with two backends: `InMemoryVectorIndex` (default) and `SqliteVecIndex` (sqlite-vec vec0).
- `[database] backend` config key, `[vector] dim` config key.
- `newVectorIndex(backend)` factory dispatches on config.

### Changed
- `InitDB` signature takes `vectorDim int`.
- `EmbeddingToBytes` delegates to `sqlite_vec.SerializeFloat32()`.

## [PR7b] — OpenAI extractor, metrics, graceful shutdown, Docker

### Added
- OpenAI-compatible extractor (`NewOpenAILLMExtractor`), selected via `provider = openai`.
- `context.Context` propagation through `Embedder.Embed`, `LLMExtractor.ExtractEntities`, `IngestionWorker.ProcessDialog`, etc.
- Graceful shutdown: `SIGINT`/`SIGTERM` → `http.Server.Shutdown` with 10s drain.
- Request-ID middleware (`X-Request-ID` header → `slog`).
- `/metrics` endpoint (`expvar` counters for stores/searches/retrieves/ingests/queries/edges/errors).
- Embedding dimension validation (`checkEmbeddingDim`).
- `AutoLinkEdges` on `/store` HTTP endpoint.
- `Dockerfile`: multi-stage build, non-root user, port 8420.

### Changed
- `RetrievalResult` JSON tags → `snake_case` (breaking).
- `NewOllamaLLMExtractor` signature includes `temperature`.
- `Config.NewExtractor` dispatches on `provider`.

