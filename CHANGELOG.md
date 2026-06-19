# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `Relation` named type in `extractor.go` for typed extraction edges.
- `[ingestion] dedup_threshold` config key (default `0.88`, cosine-similarity unit; documented inline in `hermem.ini`).
- Per-section / INI-key coverage test (`config_test.go`) that writes a hermem.ini with every known key and asserts `LoadConfig` populates every `Config` field, plus a parse-failure test for `ingestion.dedup_threshold`.
- Doc comments on the extraction contract types (`Relation`, `ExtractedEntity`, `ExtractionResult`, `LLMExtractor`).
- `log.Printf` warning on `ingestion.dedup_threshold` parse failure so misconfigured deployments are visible rather than silently reverting to the default.

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
