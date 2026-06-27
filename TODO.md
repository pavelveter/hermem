# Hermem Senior Review (Part 5 — Post-Refactoring) — TODO

## P0

- [ ] **P0-47. RetrievalService is still an orchestration object**
  The service has become cleaner, but it is still responsible for embedding, vector search, graph retrieval, formatting, and error policy. This means it still owns multiple reasons to change. Continue moving toward a true retrieval pipeline where orchestration becomes declarative rather than procedural. This is no longer urgent, but remains the largest architectural opportunity.

## P1

- [ ] **P1-48. SQLBuilder API leaks abstraction**
  `q.Where("1=1")` is an implementation artifact rather than business intent. The builder should understand the empty WHERE state itself. The caller should never have to care whether the first predicate exists. If "1=1" appears in user code, the abstraction is leaking.

- [ ] **P1-49. SearchEpisodes mixes querying and ranking**
  The SQL portion retrieves candidates. The second half performs semantic reranking. Conceptually these are two separate stages. Split into `LoadCandidates()` → `SemanticRank()` → `Return`. This would make alternative ranking strategies significantly easier.

- [ ] **P1-50. Add property tests for core ranking logic**
  Tests are becoming integration-heavy (good). Add smaller property tests for core ranking logic:
  - Ranking is deterministic
  - Duplicate IDs never appear
  - Graph expansion never exceeds MaxDepth
  - Score ordering is stable
  These tests survive refactoring much longer than scenario-based tests.

- [ ] **P1-51. Monitor constructor parameter growth**
  Public constructors continue to grow (`New(...)`, `NewPlaybackService(...)`, `NewTimelineService(...)`, `NewRetrievalService(...)`). Nothing is wrong today. Monitor constructor growth — if parameter lists exceed 4–5 arguments, introduce dependency bundles or factories.

## P2

- [ ] **P2-52. Continue trimming implementation comments**
  Comments are much better. Continue trimming comments that explain implementation history rather than API contracts. The code is now readable enough that several comments could disappear completely.

- [ ] **P2-53. Consider package-level sentinel errors**
  Error prefixes are consistent (`retrieval:`, `episodic:`, `query:`). Eventually consider introducing package-level sentinel errors where callers need to branch on behavior instead of parsing strings. Not urgent.

- [ ] **P2-54. Optimize allocations after profiling**
  Allocation style is excellent (`make([]Episode, 0)` instead of `var out []Episode`). If future profiling shows predictable result sizes, capacity hints could reduce allocations further. Don't optimize before measuring.

- [ ] **P2-55. Test retrieval scoring invariants mathematically**
  ScoreComponents tests are excellent. Go further — test invariants instead of examples:
  - Similarity is always in [0,1]
  - Recency never becomes negative
  - Increasing similarity never decreases total score (holding other variables constant)
  - BuildScoreBreakdown always matches ComputeCompositeScore
  These tests specify the algorithm mathematically.

## Architecture

- [ ] **P0-56. Reduce coupling between retrieval stages**
  The codebase no longer looks "unfinished". The next level is reducing coupling between retrieval stages rather than adding functionality. The retrieval pipeline should gradually evolve toward independently testable stages with explicit inputs and outputs. This will make future work (hybrid search, rerankers, GraphRAG, caching, experimentation) dramatically easier.

# Overall Impression

This is a noticeable improvement over the previous revision. Most previous structural concerns have been addressed. The remaining observations are primarily about long-term evolution rather than correctness.
