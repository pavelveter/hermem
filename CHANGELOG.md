# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `extraction.provider` / `extraction.url` / `extraction.key` config keys with fallback to `[embedder]` values.
- `PRAGMA auto_vacuum = INCREMENTAL` in `InitDB` — `vacuumAfter()` now works.
- Auth middleware: `server.api_key` config key, validated via `X-API-Key` header (empty = disabled).
- `RetrieveContextOptions.Ctx` for request-id propagation through `withReqID`.
- `id_map` table in core schema (replaces per-backend `vec_id_map`).
- Retention config parsing: `retention.observation_ttl`, `retention.run_interval`, `retention.batch_size`.
- `InMemoryVectorIndex.flatMatrix` — pre-built row-major matrix, maintained incrementally on Store/Remove; eliminates per-search matrix rebuild.

### Changed
- All `.go` files moved to `src/` — build path is now `./src`.
- `InMemoryVectorIndex.Search` uses snapshot pattern (RLock → local vars → unlock before compute) for concurrent safety without serializing searches.
- `SearchBatch` reuses `flatMatrix` for all queries in a batch.

### Removed
- `cosine_darwin.go` — Apple Accelerate CGO path removed. Pure Go `BatchDotProducts` provides identical throughput without CGO overhead.

### Fixed
- `cblas_sdot` deprecation warning silenced via `-DACCELERATE_NEW_LAPACK` CGo flag.
- `out.txt` added to `.gitignore`, removed from tracking.

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

