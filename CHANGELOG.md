# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
- `GET /retrieve` JSON output: per-category buckets (`world_facts`, `opinions`, `experiences`, `observations`) are now objects of `{content, parent_id, relation_type, depth}` rather than plain strings. Seed nodes (`depth=0`) carry empty `parent_id`/`relation_type`, omitted via `omitempty`. `FormatContextMarkdown` output for `/query` is unchanged.
- `GET /search` JSON output: `SearchResult` keys are now `entity` and `similarity` (snake_case) instead of the Go-default PascalCase. Same payload, stable wire shape.

### Removed
- Ad-hoc hash fallback in `OllamaLLMExtractor`: when the LLM returns non-JSON, callers previously received a synthetic single-`world` entity with an integer-hashed id. The synthetic path is gone; parse failures are surfaced as errors. **`hermem ingest` now exits non-zero on parse failures and `POST /ingest` returns HTTP 500 — update any external automation that was relying on the silent fallback.**
- `hashString` helper in `extractor.go` (was only used by the removed fallback).

### Fixed
- `RetrieveContext` (recursive CTE) now deduplicates the result set by entity id so cyclic graphs no longer inflate node counts. This is in addition to the pre-existing content-level dedup.

### Notes
- Per-section / INI-key coverage test enforces config contract — adding a Config field without an INI key (or vice versa) fails `go test ./...`.
- Request-id propagation from HTTP handlers through DB calls to LLM traces (TODO §5.4) is deliberately deferred — log events have no shared correlation ID yet; downstream log aggregation must not assume cross-event linkage.
