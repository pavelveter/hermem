# Hermem TODO — production readiness checklist

Status legend: `[ ]` pending · `[>]` in progress · `[x]` done

---

## 1. Code quality and maintainability

- [ ] Unify type/contract duplication in `extractor.go` and `ingestion.go`. Decide single source of truth for `LLMExtractor`, `ExtractionResult`, `ExtractedEntity`; other file must import only.
- [ ] Enforce naming consistency: config keys in code (`ExtractModel`) must match INI keys (`extraction.model`) — add contract test or validation at startup.
- [ ] Remove magic dedup threshold (0.88) from `ingestDialog`; make it from config with a sane default and doc comment explaining cosine-unit math.
- [ ] Replace ad-hoc hash fallback in `extractor.go` with structured error propagation; ingester should decide policy, not the LLM helper.
- [ ] Build with `-vet` and add a `just lint` / `make lint` target that runs it on every CI run.
- [ ] Add `CHANGELOG.md` and follow keep-a-changelog style for future commits.

## 2. Extraction pipeline robustness

- [ ] Enforce JSON Schema for Ollama extraction output (exact fields, allowlists for `category`).
- [ ] Add retry with backoff for Ollama `/api/chat` (suggest: 3 attempts, exponential, context budget aware).
- [ ] Add circuit / timeout guard: fail-fast when Ollama is unavailable rather than blocking the whole serve path.
- [ ] Extract relation labels schema with allowlist (`prefers`, `uses`, `mentions`, `related_to`, …) to prevent graph pollution.

## 3. Retrieval correctness and safety

- [x] Add cycle-visibility guard in `RetrieveContext` by tracking visited entity IDs inside the CTE result set to prevent duplicate node inflation on cyclic graphs.
- [ ] Configurable `max_depth` hard ceiling and soft pagination when depth budget is large.
- [ ] Add a deterministic re-ranking layer after graph traversal: `(vector_similarity × 0.7) + (recency × 0.3)` instead of bare depth ordering.
- [ ] Surface `parent_id` + `relation_type` in the final retrieval result shape consumed by the response generator.

## 4. Tests and CI

- [x] Add per-package unit tests: `retrieval_test.go`, `ingestion_test.go`, `vector_test.go`, `config_test.go`, `extractor_test.go`.
- [x] Add property-based or table-driven tests for `StoreEntityWithEmbedding` (null embedding handling, upsert semantics).
- [x] Add cycle injection test for `RetrieveContext`: build a diamond/cycle graph and assert result is finite and bounded.
- [x] Add mock for `LLMExtractor` so `IngestionWorker` can be tested without Ollama binary.
- [ ] Add `go test ./...` to CI (GitHub Actions) and require green before merge.
- [x] Add benchmarks for vector search (`BenchmarkSearchByVector`) and graph retrieval (`BenchmarkRetrieveContext`).

## 5. Structured logging and observability

- [ ] Replace `fmt.Println`/bare errors with `slog` (Go 1.21+ stdlib).
- [ ] Emit structured fields for every candidate: `event`, `entity_id`, `depth`, `cost_ms`, `model_name`, `embedding_dim`.
- [ ] Expensive-function guard: key expensive calls with `level = slog.LevelDebug` and add sampler/throttle.
- [ ] Expose `/metrics` (`promauto`) with histograms: `ingest_latency_seconds`, `search_latency_seconds`, `extraction_tokens_total`, `edges_created_total`.
- [ ] Add request-id propagation from HTTP handlers down to DB calls and LLM traces for end-to-end debugging.

## 6. Security, hardening, packaging

- [ ] Add `go mod tidy`, `go mod verify` and pin Ollama API base URL via config (no hardcoded localhost).
- [x] Validate all inbound JSON shapes and reject fields that do not belong (strict decoder or a DTO layer).
- [ ] Add rate limiting to `/api/chat` wrapper when used as a backend.
- [ ] Dockerfile should run non-root, use distroless final stage, and ship the CLI binary in image for operator use.

## 7. Documentation and DX

- [ ] README must show: 1) `go build`, 2) `hermem init`, 3) CLI-first flow, 4) SQLite schema with annotations, 5) Operational notes (Ollama, GPU, vectors).
- [ ] Document the DB path and embedding dimensions required by the configured model.
- [x] Add a `USAGE.md` with server-mode and CLI-mode runbooks side by side.

---

## Work order (suggested execution)

1. Unify contracts in `ingestion.go` + `extractor.go` (unblocks tests and lint).
2. Extraction validation + retry + timeout (stops bad data from entering the graph).
3. Cycle guard + re-ranking (retrieval correctness).
4. Tests + benchmarks (raise confidence for the rest).
5. Structured logging + metrics (observability over all the new paths).
6. Security + Docker + README (ship-ready polish).
