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

[ ] Remove ActiveSchema() singleton completely
[ ] Inject SchemaConfig through all services
[ ] Remove all remaining global mutable state
[ ] Add CI check preventing new ActiveSchema() usage
[ ] Audit all package-level variables
[ ] Ensure all services are dependency-injected
[ ] Verify concurrent safety with go test -race
[ ] Eliminate remaining old-architecture code paths
[ ] Finish migration from legacy package structure
[ ] Document dependency graph between services

==================================================
P0 — ENTITY MODEL REFACTOR
==================================================

[ ] Audit current Entity structure
[ ] Identify mixed responsibilities inside Entity
[x] Introduce Fact model
[x] Introduce Evidence model
[x] Introduce Episode model
[x] Introduce Task model
[x] Introduce Goal model
[x] Introduce Belief model
[x] Keep backward compatibility layer
[ ] Add tests for all model conversions
[ ] Document domain model boundaries

==================================================
P0 — CONTRADICTION ENGINE 2.0
==================================================

[ ] Extract contradiction detection behind interface
[ ] Create ContradictionDetector interface
[ ] Implement LexicalDetector
[ ] Implement EmbeddingDetector
[ ] Implement LLMDetector
[ ] Add detector composition pipeline
[ ] Add contradiction confidence scoring
[ ] Add contradiction explanation output
[ ] Add contradiction benchmark dataset
[ ] Add contradiction evaluation metrics
[ ] Add contradiction regression tests

==================================================
P0 — RETRIEVAL EXPLAINABILITY
==================================================

[ ] Create ScoreBreakdown structure
[ ] Add VectorScore component
[ ] Add RecencyScore component
[ ] Add TemporalScore component
[ ] Add CentralityScore component
[ ] Add PathScore component
[ ] Add DepthPenalty component
[ ] Add FinalScore component
[ ] Return score breakdown from retrieval API
[ ] Log retrieval score explanations
[ ] Add retrieval explanation tests

==================================================
P1 — EVALUATION FRAMEWORK
==================================================

[ ] Create evaluation package
[ ] Create retrieval benchmark dataset
[ ] Create contradiction benchmark dataset
[ ] Create memory benchmark dataset
[ ] Create reranker benchmark dataset
[ ] Implement Recall@K metrics
[ ] Implement Precision@K metrics
[ ] Implement MRR metrics
[ ] Implement NDCG metrics
[ ] Implement benchmark runner
[ ] Add benchmark reports
[ ] Add benchmark CI job

==================================================
P1 — MIGRATION SYSTEM HARDENING
==================================================

[ ] Add migration checksums
[ ] Add migration verification command
[ ] Add migration status command
[ ] Add migration rollback command
[ ] Add migration dry-run command
[ ] Add migration integrity tests
[ ] Add migration failure recovery tests
[ ] Document migration workflow

==================================================
P1 — RETRIEVAL CLEANUP
==================================================

[ ] Audit retrieval scoring logic
[ ] Remove duplicated scoring functions
[ ] Remove duplicated recency logic
[ ] Separate retrieval stages clearly
[ ] Separate reranking stage
[ ] Separate graph expansion stage
[ ] Separate temporal ranking stage
[ ] Add retrieval tracing
[ ] Add retrieval profiling
[ ] Document retrieval pipeline

==================================================
P1 — SERVICE LAYER
==================================================

[ ] Create MemoryService
[ ] Create RetrievalService
[ ] Create ContradictionService
[ ] Create TaskService
[ ] Create EpisodeService
[ ] Create GoalService
[ ] Remove business logic from HTTP handlers
[ ] Remove business logic from CLI commands
[ ] Add service-level tests
[ ] Document service boundaries

==================================================
P1 — OBSERVABILITY
==================================================

[ ] Add OpenTelemetry tracing
[ ] Add span propagation
[ ] Add Prometheus metrics
[ ] Add ingestion metrics
[ ] Add retrieval metrics
[ ] Add contradiction metrics
[ ] Add reranker metrics
[ ] Add graph metrics
[ ] Add Grafana dashboard
[ ] Add alert recommendations

==================================================
P2 — MEMORY EVOLUTION
==================================================

[ ] Add Belief abstraction
[ ] Add Evidence abstraction
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
P4 — CI/CD & ARTIFACT DISTRIBUTION
==================================================

[ ] Create GitHub Actions core workflow definition
[ ] Configure Matrix builds for target platforms (Darwin, Linux, Windows)
[ ] Configure Matrix builds for target architectures (amd64, arm64)
[ ] Set up deterministic CGO cross-compilation environment (zig cc / xgo)
[ ] Inject Git tags, commit SHAs, and build timestamps into BuildInfo via ldflags
[ ] Implement automated semantic versioning extraction from tags
[ ] Add automated linting stage (golangci-lint) with strict configuration
[ ] Add automated testing stage with race detector enabled (go test -race)
[ ] Implement build artifact caching (Go module cache & Build cache)
[ ] Implement build reproducibility and checksum verification (sha256sum)
[ ] Configure automated GitHub Release creation on tag push
[ ] Automate binary asset stripping and signing/notarization setup for macOS
[ ] Document CI/CD infrastructure and release process

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