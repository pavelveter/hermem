# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

  Snapshot (macOS, darwin/arm64, Accelerate cblas_sdot, GOOS=darwin):

  | bench                                 | ns/op         | B/op      | allocs/op |
  |---------------------------------------|---------------|-----------|-----------|
  | BenchmarkRetrieveContextStarPrecompute | 277_712_844   | 5_339_679 | 11_595    |
  | BenchmarkRetrieveContextStarRecompute  | 324_184_298   | 5_339_601 | 11_594    |

  The `Precompute` row pays one sqrt per row (normB only); the `Recompute` row pays two (query + node). Wall-clock figures vary by host; relative gap is stable. Re-running the bench refreshes both rows.

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

