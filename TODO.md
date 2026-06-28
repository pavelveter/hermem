# TODO: Retrieval Engine Improvements

This document tracks architectural improvements that should be implemented after the current stabilization phase.

The goal is to improve flexibility, explainability and long-term maintainability without introducing unnecessary complexity.

---

## P0 — Explainable Retrieval

- [x] **Explain Mode** — Added `ScoreBreakdown` value object with all scoring components (`VectorScore`, `RecencyScore`, `TemporalScore`, `CentralityScore`, `PathScore`, `DepthPenalty`, `FinalScore`) and `Weights` for full explainability. Populate when `Explain=true`.

---

## P0 — Contradiction Resolution Strategy

- [x] **Pluggable Interface** — Added `ContradictionResolver` interface with `Resolve(existing, incoming) ResolutionAction`. Ingest pipeline depends only on the interface.
- [x] **ThresholdResolver** — Initial implementation replaces hardcoded 0.7 threshold with configurable value.

---

## P0 — Ranking Strategy

- [x] **RankingStrategy Interface** — Added `RankingStrategy` interface with `Name()` and `Weights()`.
- [x] **Built-in Policies** — `DefaultRanking`, `FreshnessFirst`, `SemanticSearch`, `GraphExpansion`.
- [x] **Selection** — `RankingStrategyByName(name)` for config-driven policy selection.

---

## P1 — Exponential Depth Decay

- [x] **Replace Linear Penalty** — Depth penalty changed from `w.DepthPenalty * pathWeight` (linear) to `2^(-depth)` (exponential). `DepthPenalty` field now represents `1 - decay`.

---

## P1 — Adaptive Auto-Link Threshold

- [x] **Infrastructure** — Added `AdaptiveLinkThreshold` function that scales threshold by local graph density. Disabled by default, ready for production metrics.

---

## P1 — Confidence Lifecycle

- [x] **TTL-based Cleanup** — Added `ConfidenceLifecycle` service that archives low-confidence entities after configurable TTL.
- [x] **Audit Logging** — `slog.Info`/`slog.Debug` logging for archived entities.
- [x] **Optional** — Disabled by default via `ConfidenceLifecycleConfig.Enabled`.

---

## P1 — Property-Based Tests

- [x] **Ranking Invariants** — Deterministic ordering, identical inputs → identical scores.
- [x] **Scoring Invariants** — Similarity ∈ [0,1], recency ≥ 0, BuildScoreBreakdown matches compositeScore.
- [x] **Graph Invariants** — MaxDepth respected, no duplicate IDs.

---

## P1 — Ranking Benchmarks

- [x] **Per-Strategy Benchmarks** — `BenchmarkCompositeScore_Default/FreshnessFirst`, `BenchmarkComputeScoreComponents`, `BenchmarkBuildScoreBreakdown`, `BenchmarkDepthDecay`, `BenchmarkSortByScoreDesc_100`.

---

## P2 — Configuration Profiles

- [x] **RetrievalProfile** — Named profiles bundling ranking weights + retrieval tuning: `Default`, `FreshnessFirst`, `SemanticSearch`, `GraphExpansion`, `ConversationMemory`.
- [x] **Selection** — `RetrievalProfileByName(name)` for config-driven profile selection.

---

## P2 — Config Hot Reload

- [x] **AtomicConfig** — `AtomicConfig` wrapper using `atomic.Pointer` for lock-free reads and atomic replacement. Methods: `Load()`, `Store()`, `Swap()`.

---

## P2 — Score Normalization Research

- [x] **ScoreNormalizer Interface** — `Normalize(raw) float32` for [0,1] output.
- [x] **Implementations** — `LinearNormalizer`, `LogNormalizer`, `SigmoidNormalizer`, `TanhNormalizer`.
- [x] **Factory** — `NormalizerByName(name, min, max)` for config-driven selection.

---

## P2 — sqlite-vec Backend

- [x] **SQLiteVecIndex** — Stub implementation of `core.VectorIndex` backed by sqlite-vec extension. Falls back to in-memory when unavailable.
- [x] **Architecture** — `NewIndex(backend, db, dim)` supports "sqlite-vec" backend with graceful fallback.

---

## P3 — Retrieval Pipeline

- [x] **Pipeline Type** — Explicit `Pipeline` struct with pluggable stages: `CandidateRetrievalStage`, `RankingStage`, `ContextAssemblyStage`, `RenderingStage`.
- [x] **Default Implementations** — Delegate to existing package-level functions.
- [x] **Composition** — `SetExpand()`, `SetRank()`, `SetAssembly()`, `SetRender()` for stage replacement.

---

## P3 — ADR Improvements

- [x] **ADR-001** — Explainable Retrieval via ScoreBreakdown.
- [x] **ADR-002** — Pluggable Contradiction Resolution Strategy.
- [x] **ADR-003** — Ranking Strategy Interface.
- [x] **ADR-004** — Exponential Depth Decay.

Each ADR includes: Context, Decision, Alternatives Considered, Trade-offs Accepted.

---

## Explicitly Deferred

The following ideas are intentionally postponed until profiling demonstrates a measurable need:

- sync.Pool
- worker-based ingest pipeline
- replacing recursive CTE with Go BFS
- aggressive interface extraction
- premature micro-optimizations

Hermem should remain simple, deterministic and maintainable until real workloads justify additional complexity.
