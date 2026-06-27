# Hermem Senior Review (Part 3) — TODO

## P0

- [ ] **P0-25. Make retrieval algorithms explicitly replaceable**
  Retrieval embeds algorithmic decisions directly into execution flow (ranking weights, contradiction thresholds, graph expansion logic, effective depth calculation). Extract into dedicated strategy objects: `Ranker`, `ExpansionPolicy`, `ContradictionPolicy`. The goal is to make experimentation cheap — compare multiple retrieval strategies without rewriting the pipeline.

- [ ] **P0-26. Add property-based testing**
  Most tests validate individual scenarios. Add invariant-based tests:
  - `Expand()` never returns duplicate node IDs
  - `Rank()` always produces a monotonically ordered result
  - Compression never increases the number of tokens
  - Graph traversal never exceeds the configured depth
  - Retrieval always respects cancellation
  These catch regressions that traditional unit tests miss.

- [ ] **P0-27. Introduce a complexity budget**
  Retrieval code is becoming one of the largest parts of the project. Enforce cyclomatic complexity limits in CI using `cyclop` or `gocyclo`. A limit of 15–20 prevents large orchestration functions from slowly accumulating responsibilities.

## P1

- [ ] **P1-28. Avoid multiple boolean parameters**
  As APIs evolve, functions accumulate boolean flags. Instead of `Query(..., includeEdges, includeDeleted, allowWeak)`, prefer `Query(..., QueryOptions)`. This scales better as new options are added and produces self-documenting call sites.

- [ ] **P1-29. Introduce structured error codes**
  Different frontends (CLI, HTTP, MCP, SDK) need different error representations. Instead of relying solely on wrapped errors, introduce domain-level error codes: `ErrNotFound`, `ErrConflict`, `ErrInvalidGraph`, `ErrCorruptedIndex`. Transport layers can map these independently.

- [ ] **P1-30. Prefer immutable domain models where practical**
  Many structs are created and then modified through multiple field assignments. Where possible, construct fully initialized objects and treat them as immutable afterwards. This reduces possible object states and simplifies correctness reasoning.

- [ ] **P1-31. QueryOptions will likely become inevitable**
  Retrieval APIs tend to grow (limit, depth, threshold, rerank, timeout, token budget, filters). Introducing a `QueryOptions` struct early keeps API evolution inexpensive.

- [ ] **P1-32. Increase compile-time interface verification**
  Where interfaces are intended to be implemented externally, add compile-time assertions: `var _ Retriever = (*GraphRetriever)(nil)`. This catches accidental API drift immediately during compilation.

## P2

- [ ] **P2-33. Consider stronger value objects**
  Primitive types represent different semantic concepts (Score, Confidence, Similarity, Weight). Using dedicated named types instead of raw `float64` improves readability and prevents accidental parameter mixups.

- [ ] **P2-34. Evaluate slice pooling after profiling**
  If retrieval becomes allocation-heavy under realistic workloads, consider `sync.Pool` for frequently allocated slices (`[]Node`, `[]Edge`, `[]Candidate`). Only do this after profiling confirms allocation pressure — premature pooling increases complexity without measurable benefit.

- [ ] **P2-35. Add ADRs for algorithmic decisions**
  Several retrieval decisions use carefully chosen constants or heuristics (contradiction threshold, auto-link threshold, graph traversal strategy, recency scoring, ranking formula). These deserve short Architecture Decision Records explaining the rationale. Future contributors will appreciate the historical context.

## Architecture

- [ ] **P0-36. Move toward explicit pipeline architecture**
  Retrieval currently feels primarily imperative. Consider gradually moving toward:
  ```
  Pipeline
    ├── SeedStage
    ├── ExpansionStage
    ├── RankingStage
    ├── AssemblyStage
    └── RenderingStage
  ```
  Each stage operates only on its own input/output, without knowledge of the full execution process. Benefits: easier experimentation, easier profiling, stage-level testing, simpler algorithm replacement, lower coupling, cleaner reasoning. This pattern is common in mature search engines and retrieval systems.

# Overall Impression

Hermem's biggest future challenge is unlikely to be adding more features. The challenge will be controlling architectural complexity as retrieval capabilities continue to expand. At this stage, investing in lower coupling, stronger invariants, explicit algorithm boundaries, and pipeline-oriented design will provide a much higher return than adding new functionality.
