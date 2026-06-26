# Hermem Improvement Backlog

IMPORTANT

- After completing each task, replace [ ] with [x].
- Commit every completed task separately.
- One task = one commit.
- Do not batch unrelated tasks into a single commit.
- Commit messages should clearly describe the completed work.
- Run tests before every commit.
- Keep the main branch always buildable.
- If a task requires schema changes, add migration + tests in the same commit.

Recommended commit format:

feat(scope): short description
fix(scope): short description
refactor(scope): short description
test(scope): short description
docs(scope): short description

Examples:

feat(retrieval): add score breakdown support
refactor(schema): remove ActiveSchema singleton
fix(contradictions): improve negation detection
test(migrations): add rollback coverage

==================================================
P0 — CRITICAL ARCHITECTURE
==================================================

[x] Remove ActiveSchema() singleton completely
[x] Inject SchemaConfig through all services
[x] Remove all remaining global mutable state
[x] Add CI check preventing new ActiveSchema() usage
[x] Audit all package-level variables
[x] Ensure all services are dependency-injected
[x] Verify concurrent safety with go test -race
[x] Eliminate remaining old-architecture code paths
[x] Finish migration from legacy package structure
[x] Document dependency graph between services

==================================================
P0 — ENTITY MODEL REFACTOR
==================================================

[x] Audit current Entity structure
[x] Identify mixed responsibilities inside Entity
[x] Introduce Fact model
[x] Introduce Evidence model
[x] Introduce Episode model
[x] Introduce Task model
[x] Introduce Goal model
[x] Introduce Belief model
[x] Keep backward compatibility layer
[x] Add tests for all model conversions
[x] Document domain model boundaries

==================================================
P0 — CONTRADICTION ENGINE 2.0
==================================================

[x] Extract contradiction detection behind interface
[x] Create ContradictionDetector interface
[x] Implement LexicalDetector
[x] Implement EmbeddingDetector
[x] Implement LLMDetector
[x] Add detector composition pipeline
[x] Add contradiction confidence scoring
[x] Add contradiction explanation output
[x] Add contradiction benchmark dataset
[x] Add contradiction evaluation metrics
[x] Add contradiction regression tests

==================================================
P0 — RETRIEVAL EXPLAINABILITY
==================================================

[x] Create ScoreBreakdown structure
[x] Add VectorScore component
[x] Add RecencyScore component
[x] Add TemporalScore component
[x] Add CentralityScore component
[x] Add PathScore component
[x] Add DepthPenalty component
[x] Add FinalScore component
[x] Return score breakdown from retrieval API
[x] Log retrieval score explanations
[x] Add retrieval explanation tests

==================================================
P1 — EVALUATION FRAMEWORK
==================================================

[x] Create evaluation package
[ ] Create retrieval benchmark dataset
[ ] Create contradiction benchmark dataset
[ ] Create memory benchmark dataset
[ ] Create reranker benchmark dataset
[x] Implement Recall@K metrics
[x] Implement Precision@K metrics
[x] Implement MRR metrics
[x] Implement NDCG metrics
[x] Implement benchmark runner
[x] Add benchmark reports
[ ] Add benchmark CI job

==================================================
P1 — MIGRATION SYSTEM HARDENING
==================================================

[x] Add migration checksums
[x] Add migration verification command
[x] Add migration status command
[x] Add migration rollback command
[x] Add migration dry-run command
[x] Add migration integrity tests
[x] Add migration failure recovery tests
[x] Document migration workflow

==================================================
P1 — RETRIEVAL CLEANUP
==================================================

[x] Audit retrieval scoring logic (already shipped per audit: src/internal/retrieval/scoring.go — ComputeScoreComponents is single source of truth; no duplicates)
[x] Remove duplicated scoring functions
[x] Remove duplicated recency logic
[x] Separate retrieval stages clearly
[x] Separate reranking stage
[x] Separate graph expansion stage (already shipped: src/internal/retrieval/expand.go — expandGraph + scannedNode)
[x] Separate temporal ranking stage (already shipped: src/internal/retrieval/temporal.go — expDecayHours + temporalScore)
[x] Add retrieval tracing (already shipped: src/internal/retrieval/tracing.go — tracerFromOpts, startStageSpan; wired in walk.go)
[x] Add retrieval profiling (already shipped: src/internal/retrieval/walk_bench_test.go — 4 per-stage benchmarks)
[x] Document retrieval pipeline (already shipped: src/internal/retrieval/PIPELINE.md — canonical pipeline reference)

==================================================
P1 — SERVICE LAYER
==================================================

[x] Create MemoryService
[x] Create RetrievalService
[x] Create ContradictionService
[x] Create TaskService
[ ] Create EpisodeService
[ ] Create GoalService
[x] Remove business logic from HTTP handlers
[x] Remove business logic from CLI commands
[x] Add service-level tests
[ ] Document service boundaries

==================================================
P1 — OBSERVABILITY
==================================================

[x] Add OpenTelemetry tracing
[x] Add span propagation
[x] Add Prometheus metrics
[x] Add ingestion metrics
[x] Add retrieval metrics
[x] Add contradiction metrics
[x] Add reranker metrics

[x] Add hermem diagnose CLI for self-diagnostics

Phase 2 follow-ups (out of scope for C1–C6 of OBSERVABILITY sprint, feat/observability-prometheus → main @ a75bfc0):
[ ] Add graph metrics
[ ] Add Grafana dashboard
[ ] Add alert recommendations

History (Phase 1 of P1-OBSERVABILITY — shipped via C1–C6, merged into main @ a75bfc0):
- C1 — prometheus/client_golang v1.21.0 + hermem-owned prometheus.Registry (not the global default).
- C2 — 4 domain duration histograms: hIngest, hRetrieval, hContradiction, hRerank.
- C3 — hIngest → *HistogramVec labeled by category (knownCategories, _init pre-warm).
- C4 — hRetrieval → *HistogramVec labeled by mode (knownModes, _init pre-warm).
- C5 — hContradiction → *HistogramVec labeled by detector (knownDetectors = lexical/composite; semantic reserved for future).
- C6 — hRerank → *HistogramVec labeled by strategy (knownStrategies = llm_openai / llm_ollama / noop).
- Each *HistogramVec is pre-warmed at New() with the _init sentinel so cold scrapes are zero-missing.
- /metrics and /health remain wire-compatible; X-API-Key auth still applies when [server] api_key is set.
- Known-limits regression tests: TestHermemPrefixContract_KnownCategoriesSet / KnownModesSet / KnownDetectorsSet / KnownStrategiesSet guard against accidental label-domain drift.

==================================================
P2 — MEMORY EVOLUTION
==================================================

Phase 1 (C1–C10, feat/memory-evolution branch):
[x] Add Belief abstraction

[x] Add Belief abstraction
[x] Add Evidence abstraction
[ ] Add confidence propagation
[ ] Add evidence aggregation
[ ] Add trust scoring
[ ] Add belief revision chains
[ ] Add superseded beliefs
[ ] Add support/refute relationships
[ ] Add belief history tracking
[ ] Add belief evolution queries

==================================================
P2 — EPISODIC MEMORY
==================================================

[ ] Create Episode entities
[ ] Create Session entities
[ ] Create Event entities
[ ] Link memories to episodes
[ ] Link tasks to episodes
[ ] Add timeline reconstruction
[ ] Add episode retrieval
[ ] Add episode summarization
[ ] Add historical playback support
[ ] Add episodic memory tests

==================================================
P2 — SEMANTIC COMPRESSION
==================================================

[ ] Create summary node type
[ ] Implement clustering pipeline
[ ] Implement summary generation
[ ] Implement recursive summarization
[ ] Preserve provenance during compression
[ ] Support summary regeneration
[ ] Add compression benchmarks
[ ] Add compression metrics
[ ] Add compression tests

==================================================
P3 — LONG TERM RESEARCH
==================================================

[ ] Design belief graph architecture
[ ] Design memory decay model
[ ] Design confidence propagation model
[ ] Design autonomous memory cleanup
[ ] Design identity memory layer
[ ] Design self-reflection memory layer
[ ] Design memory-driven planning
[ ] Design memory-driven reasoning
[ ] Design autonomous memory evolution
[ ] Design reasoning memory engine

==================================================
P1 — AUTH HARDENING (multi-key scoped API keys)
==================================================

[x] Define Scope, Key, Authenticator interface in auth package
[x] Implement Scope hierarchy (CanAccess, ScopeForPath)
[x] Implement StaticAuthenticator with constant-time comparison
[x] Add scope and authenticator tests
[x] Add api_keys INI parsing (key:scope:label), legacy api_key fallback
[x] Add INI file manipulation helpers (AddKeyToFile, RemoveKeyFromFile, RotateKeyInFile)
[x] Create AuthMiddleware (parameterless, health bypass, 401/403 JSON)
[x] Wire AuthMiddleware into Serve() replacing APIKeyMiddleware
[x] Create admin CLI group (list/add/rotate/revoke) with GenerateKey/MaskKey
[x] Add admin CLI unit tests
[x] Add middleware integration tests (scope enforcement per-endpoint)
[x] Document auth in USAGE.md §16
[x] Update CHANGELOG.md

=================================================
P1 — ADMIN CLI (ops group)
==================================================

[x] Create admin package with Stats/Issue/IntegrityReport types
[x] Implement StatsCollector with parallel count queries + tests
[x] Implement IntegrityChecker (missing embeddings, dangling edges, archive consistency) + tests
[x] Implement VacuumRunner with progress callback + tests
[x] Implement RebuildIndex with category/since/archived/DryRun filters + tests
[x] Create ops CLI command group (Register + 4 subcommands)
[x] Wire ops commands into root CLI (root.go)
[x] Add CLI unit tests for all subcommands
[x] Document admin ops in USAGE §18, CHANGELOG, TODO

==================================================
P4 — CI/CD & ARTIFACT DISTRIBUTION
==================================================

[x] Create GitHub Actions core workflow definition
[x] Configure Matrix builds for target platforms (Darwin, Linux, Windows)
[x] Configure Matrix builds for target architectures (amd64, arm64)
[x] Set up deterministic CGO cross-compilation environment (zig cc / xgo)
[x] Inject Git tags, commit SHAs, and build timestamps into BuildInfo via ldflags
[x] Implement automated semantic versioning extraction from tags
[x] Add automated linting stage (golangci-lint) with strict configuration
[x] Add automated testing stage with race detector enabled (go test -race)
[x] Implement build artifact caching (Go module cache & Build cache)
[x] Implement build reproducibility and checksum verification (sha256sum)
[x] Configure automated GitHub Release creation on tag push
[x] Automate binary asset stripping and signing/notarization setup for macOS
[x] Document CI/CD infrastructure and release process

==================================================
DONE CRITERIA
==================================================

A task is considered completed only if:

[ ] Code implemented
[ ] Tests added
[ ] Documentation updated
[ ] Benchmarks updated if applicable
[ ] CI passes
[ ] Separate commit created
[ ] Commit pushed

<!-- sub-agent-3: 6/6 retrieval-cleanup sub-points cleared in feat/retrieval-cleanup-stages -->
<!-- sub-agent-1: 5/5 contradiction-engine sub-points complete in feat/contradiction-engine-2.0 -->
<!-- sub-agent-2: 5/5 evaluation-framework sub-points complete in feat/evaluation-framework -->

<!-- sub-agent-7 -->
