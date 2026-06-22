# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added (PR8 — sqlite-vec)
- `VectorIndex` interface with two backends: `InMemoryVectorIndex` (default, zero-dependency) and `SqliteVecIndex` (sqlite-vec vec0 virtual table for indexed KNN). Selected via `[database] backend` config key.
- `SqliteVecIndex`: stores vectors in a `vec0` virtual table; search uses indexed KNN (`WHERE embedding MATCH ? ORDER BY distance LIMIT ?`), reducing search complexity from O(N) to O(log N). Entity-ID → vec0 rowid mapping via deterministic FNV-1a hash.
- `[vector] dim` config key (default `768`) sets the embedding dimension for the `vec0` virtual table. Must match the model's output dimension.
- `sqlite-vec` Go binding (`github.com/asg017/sqlite-vec-go-bindings/cgo`): statically linked, replaces hand-written `EmbeddingToBytes` serialization with `SerializeFloat32()`.
- `newVectorIndex(backend)` factory in `vector.go` dispatches on config; `currentVectorIndex` global delegates `SearchByVector` and `StoreEntityWithEmbedding` transparently.

### Changed (PR8)
- `InitDB` signature: now takes `vectorDim int` as second parameter to create the `vec0` virtual table with the correct dimension.
- `EmbeddingToBytes` now delegates to `sqlite_vec.SerializeFloat32()` for canonical binary format compatibility.
- Tests: `memDB(t)` and all direct `InitDB` calls pass the dimension parameter. Config tests cover `[database] backend` and `[vector] dim` parsing.

### Docs (PR8)
- `README.md`: updated features, config defaults table, performance section (sqlite-vec vs in-memory), dependencies.
- `USAGE.md`: new "Vector backend" section comparing `in-memory` and `sqlite-vec`, `vec_entities` schema docs, updated file-reference table.
- `hermem.ini`: `[vector]` section with `dim` example.
- `CHANGELOG.md`: this entry.


- `context.Context` propagation through `Embedder.Embed(ctx, text)`, `LLMExtractor.ExtractEntities(ctx, dialog)`, `IngestionWorker.ProcessDialog(ctx)`, `MemoryWorker(ctx)`, `AddEdgeWithAutoCreate(ctx)`, `GenerateResponse(ctx)`, `AutoLinkEdges(ctx)`. HTTP handlers pass `r.Context()` downstream.
- Graceful shutdown: `SIGINT`/`SIGTERM` → `http.Server.Shutdown` with 10-second drain timeout.
- Request-ID middleware: every HTTP response gets `X-Request-ID` header; `request_id` flows into `slog` events.
- `/metrics` endpoint: `expvar` counters (`hermem_stores_total`, `hermem_searches_total`, `hermem_retrieves_total`, `hermem_ingests_total`, `hermem_queries_total`, `hermem_edges_total`, `hermem_errors_total`).
- OpenAI-compatible extractor: `NewOpenAILLMExtractor` + `OpenAILLMExtractor`, selected via `provider = openai` in config (reuses `[embedder] provider` key for simplicity). Retry/backoff parity with Ollama extractor.
- Embedding dimension validation: `checkEmbeddingDim()` called on every `StoreEntityWithEmbedding`. Warns via `slog.Warn` on mismatch.
- `AutoLinkEdges` on `/store` HTTP endpoint (previously CLI-only). Auto-link failure is non-fatal (logged as Warn).
- `Dockerfile`: multi-stage build (`golang:1.26-alpine` → `alpine:3.21`), non-root user `hermem`, port `8420`.
- Edge example in USAGE.md fixed from `likes` to `prefers`.

### Changed (PR7b)
- `RetrievalResult` JSON tags: all top-level keys are now `snake_case` (`seed_nodes`, `world_facts`, `opinions`, `experiences`, `observations`). Breaking change for consumers reading PascalCase keys.**
- `NewOllamaLLMExtractor` signature now includes `temperature float32` as third parameter.
- `Config.NewExtractor` dispatches on `provider`: `"openai"` → `NewOpenAILLMExtractor`, default → `NewOllamaLLMExtractor`.

### Removed (PR7b)

### Fixed (PR7b)

### Notes (PR7b)
- Request-IDs flow through `slog` events via `requestIDMiddleware` → `context.WithValue`. Every middleware-wrapped handler emits `request_start` and `request_end` debug events with `request_id`, `method`, `path`, `duration_ms`.
- Embedding dimension validation is non-fatal (tolerant of heterogeneous embedding models).

---

### Added
- Per-package unit tests (TODO §4) covering 40+ test functions + 6 benchmarks across 6 new files: `config_test.go`, `vector_test.go`, `retrieval_test.go`, `ingestion_test.go`, `extractor_test.go`, plus the shared `helpers_test.go`. stdlib-only mocks: `stubExtractor` / `stubEmbedder` / `statefulExtractor` implement `LLMExtractor` and `Embedder` interfaces; `httptest.NewServer` mocks `/api/chat` for retry semantics; `:memory:` SQLite via `InitDB(":memory:")` for speed (verify_test.go keeps file-based DBs for the timing test). §4 items satisfied: cycle-injection test on `RetrieveContext` (contract-level: finite, bounded result on A↔B graph), mock for `LLMExtractor` so `IngestionWorker` can be tested without Ollama, dedup-threshold + content-append merge path tests, and benchmarks (`BenchmarkSearchByVector{100,1000,5000}`, `BenchmarkRetrieveContext{depth=1,2,3}`). JSON parse errors + 5xx retry + 4xx-no-retry are all covered for the Ollama HTTP path.
- `Relation` named type in `extractor.go` for typed extraction edges.
- `[ingestion] dedup_threshold` config key (default `0.88`, cosine-similarity unit; documented inline in `hermem.ini`).
- Per-section / INI-key coverage test (`config_test.go`) that writes a hermem.ini with every known key and asserts `LoadConfig` populates every `Config` field, plus a parse-failure test for `ingestion.dedup_threshold`.
- Doc comments on the extraction contract types (`Relation`, `ExtractedEntity`, `ExtractionResult`, `LLMExtractor`).
- `log.Printf` warning on `ingestion.dedup_threshold` parse failure so misconfigured deployments are visible rather than silently reverting to the default.
- `log/slog` package-internal logger replaces direct `fmt.Printf` for error and status events. Per-call `event` field for filtering: `ingest_failed`, `server_ready`, `ollama_attempt_retry`, `retrieval_complete`. Per-row graph telemetry is suppressed by default — one summary event per retrieval acts as the level-based throttle for TODO §5.3.
- `decodeStrict` helper in `server.go`: validates inbound JSON via `encoding/json.Decoder.DisallowUnknownFields`, classifies rejections into 5 codes (`empty_body`, `unknown_field`, `invalid_type`, `bad_json`, `trailing_data`) so clients can route strict-decode failures without parsing prose. Used by `/store`, `/search`, `/retrieve`, `/ingest`, `/query` (+ the same wire contract on CLI JSON-stdin via `bytes.NewReader`). `ErrorResponse` extended with `Code` and `Field` (both `omitempty`) so pre-PR7 callers see no schema change for non-strict rejections; new `writeErrorWithCode` helper populates the structured fields. `server_test.go` (new): 6 endpoint tests + method-gate test, 35+ subtests covering valid / `unknown_field` / `invalid_type` / missing-required / `empty_body` / malformed JSON / `trailing_data` (concatenated JSON + trailing garbage).
- `USAGE.md`: operator-facing runbook covering build, configuration, both run modes (CLI and HTTP server) side-by-side, request/response reference, the strict-decode error model with curl + CLI examples per code, full DB schema (entities + edges column tables), embedding-model & dimension notes with migration recipe, operational notes (working dir, slog fields, shutdown, backups), common-pitfalls catalogue, and a file ↔ concern map. `README.md` gets a Documentation section pointing to USAGE.md.
- Plugin compatibility verified (`plugins/memory/hermem/__init__.py`) against the new strict decoder: every outbound payload (`{"query": …, "top_k": …}` for `/search`+`/query`, `{"id": …, "category": …, "content": …}` for `/store`, `{"dialog": …}` for `/ingest`) uses only fields on the accept-list. `hermem_store` handler's deterministic `mem-<hash>` id is an upsert-stabiliser, not a payload-shape concern. **No plugin changes required; existing installs continue to work after PR7a.**

### Changed
- `LLMExtractor`, `ExtractionResult`, `ExtractedEntity` unified in `extractor.go`; `ingestion.go` only consumes them. Single source of truth for the extraction contract.
- `NewIngestionWorker`, `NewServer`, and `MemoryWorker` accept `dedupThreshold float32` from `Config` instead of hardcoding `0.88`.
- `MemoryWorker` channel parameter moved to the trailing position so future tunables can be appended without reshuffling callers.
- Parser-extracted JSON decode errors now carry a (truncated) preview of the raw LLM content so log lines stay bounded.
- `GET /search` JSON output: `SearchResult` keys are now `entity` and `similarity` (snake_case) instead of the Go-default PascalCase. Same payload, stable wire shape.
- `GET /retrieve` JSON output: per-category buckets are now objects of `{content, parent_id, relation_type, depth}` rather than plain strings — the struct type is `RetrievedFact`. Seed nodes additionally carry composite `ranking_score` (0.7*cosine + 0.3*recency, half-life 30d) and empty `parent_id`/`relation_type` (omitempty). `empty` buckets render as `[]`, not absent. `FormatContextMarkdown` output for `/query` is unchanged. **Wire-shape caveat:** `RetrievalResult` has no explicit `json:"..."` tags, so the top-level keys currently marshal in Go-default PascalCase (`SeedNodes`, `WorldFacts`, `Opinions`, `Experiences`, `Observations`) — unlike the snake_case `/search` shape. A future PR will introduce snake_case (`seed_nodes`, `world_facts`, `opinions`, `experiences`, `observations`) and flag the breaking change in `### Changed` at that time.
- `verify_test.go`'s `TestTiming` now seeds a **small-world edge topology** alongside entities so the SQLite recursive-CTE walk in `retrieval.go` sees real fan-out and retrieval cost scales with N via the JOIN cost over edges (not just CTE setup, as in the prior no-edges baseline). Each entity gets ~8 edges on average: 5 forward chain `(i+1..i+5)` targets when `< n` (relation_type `next`) plus 3 hash-based long-range targets `((i+1)*{7,11,13}) % n` (relation_type `long-range`). The CTE walks edges bidirectionally (`source_id = gw.id OR target_id = gw.id`), so forward-only edges suffice. Per-cohort `DELETE FROM edges` + full rebuild keeps long-range targets reflecting the cohort's `n`, not the historical smaller cohort — the earlier incremental-by-cohort design left long-range pools stale at intermediate cohorts (effectively capping fan-out below N). README `## Performance` is now anchored in the actual measured output: N=1000 → search 1.4 ms / retrieve 43 ms; N=3000 → 4.5 ms / 111 ms; N=6000 → 8.9 ms / 237 ms (Apple M-class; rerun `go test -v -run TestTiming -count=1 ./...` to refresh). The strict < 5ms regression gate stays at N=1000 only; larger-N rows are informational.

### Removed
- Ad-hoc hash fallback in `OllamaLLMExtractor`: when the LLM returns non-JSON, callers previously received a synthetic single-`world` entity with an integer-hashed id. The synthetic path is gone; parse failures are surfaced as errors. **`hermem ingest` now exits non-zero on parse failures and `POST /ingest` returns HTTP 500 — update any external automation that was relying on the silent fallback.**
- `hashString` helper in `extractor.go` (was only used by the removed fallback).

### Fixed
- `RetrieveContext` (recursive CTE) now deduplicates the result set by entity id so cyclic graphs no longer inflate node counts. This is in addition to the pre-existing content-level dedup.
- Config (`hermem.ini`) and SQLite DB path are now resolved relative to the binary executable via `os.Executable()` rather than the process working directory. A deployed `~/.hermes/bin/hermem` therefore finds its config the same way from `~`, from `/tmp`, or any other CWD, and a stray `hermem.db` no longer leaks into a transient CWD. New helpers in `config.go`: `LoadConfigFromBinaryDir` (production entry point), `LoadConfigFromDir(dir)` (test-injectable; the stdlib doesn't allow mocking `os.Executable()` at test time), and `resolveDBPath(p)` which honours absolute `[database] path` overrides (only relative paths are joined to the binary's directory). Two new `config_test.go` cases cover the binary-dir copy and the missing-INI default path; the `os.IsNotExist` → defaults policy is preserved, so deployments without a hermem.ini still boot cleanly. Acceptance criterion `~/.hermes/bin/hermem store works when run from any directory` is satisfied; an empty CWD no longer creates a stray `hermem.db`.

### Notes
- Per-section / INI-key coverage test enforces config contract — adding a Config field without an INI key (or vice versa) fails `go test ./...`.
- Request-id propagation from HTTP handlers through DB calls to LLM traces (TODO §5.4) is deliberately deferred — log events have no shared correlation ID yet; downstream log aggregation must not assume cross-event linkage.
