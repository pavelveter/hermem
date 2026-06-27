# Hermem Senior Review (Part 2) — TODO

## P0

- [ ] **P0-11. RetrievalService knows too much**
  Split the retrieval pipeline into separate components:
  `Resolver → CandidateRetriever → GraphExpander → Ranker → ContextAssembler → Renderer`
  Each step should be an independent component. Current RetrievalService is a use-case engine with too high fan-in (embedding, graph traversal, vector retrieval, reranking, markdown rendering, scoring, logging, cancellation).

- [ ] **P0-12. Signs of "transaction script" pattern**
  Several services follow `validate → load → transform → save → publish → log` sequentially. Code is becoming procedural. Move behavior closer to domain objects instead of keeping it in service methods. Domain invariants should live separately from orchestration.

- [ ] **P0-24. No clear boundary between Domain and Application**
  Services are becoming both domain and orchestration simultaneously. Define what constitutes the true domain model in hermem. If the answer is "Service" — that's a warning signal. Domain invariants must live separately from orchestration.

## P1

- [ ] **P1-13. Too many DTOs**
  Watch for DTO proliferation: `Episode`, `EpisodeDTO`, `EpisodeSummary`, `EpisodeResponse`, `EpisodeRecord`, `EpisodeMetadata`. Monitor that DTOs don't become copies of each other. One field change shouldn't require updating 7 structs.

- [ ] **P1-14. Logging mixed with business logic**
  Many places have `logger.Debug(...) → if err != nil → logger.Warn(...) → return err`. After several screens of logs, the algorithm is hard to see. Rule: if a log doesn't change program decisions, it shouldn't outnumber business code.

- [ ] **P1-15. Inconsistent error wrapping**
  Mix of `fmt.Errorf("...: %w", err)`, `return err`, and `errors.New(...)`. Adopt consistent style:
  - New context → `%w`
  - Passthrough → `return err`
  - Error without reason → `errors.New`

- [ ] **P1-16. Context used as "mandatory argument"**
  Some functions accept `ctx context.Context` but only do one SQL operation and never use ctx elsewhere. Each `ctx` should actively participate in cancellation or deadlines.

- [ ] **P1-17. Retrieval package becoming a "mini-framework"**
  Split retrieval into subpackages before it grows unmanageable:
  ```
  retrieval/
    search/
    ranking/
    graph/
    render/
    pipeline/
  ```

- [ ] **P1-21. Too many temporary slices in retrieval**
  Pipeline creates new collections at each step: `nodes → filteredNodes → expandedNodes → rankedNodes → renderedNodes`. Reuse memory where possible instead of allocating new slices at each step.

- [ ] **P1-22. append without pre-allocated capacity**
  Several places use `out := []T{} → for ... append(...)`. When upper bound is known, use `make([]T, 0, len(...))` to reduce allocations.

## P2

- [ ] **P2-18. Too many small methods**
  Functions like `buildSeed()`, `buildCandidate()`, `buildPrompt()` are 4 lines each. Go values reading locality over micro-functions. Sometimes 15-20 lines inline is better than jumping between files.

- [ ] **P2-19. Some names are too abstract**
  Names like `Manager`, `Engine`, `Provider`, `Processor`, `Handler` say almost nothing. Prefer concrete names: `EmbeddingIndex`, `EpisodeWriter`, `GraphRetriever`, `MemoryAssembler`. The more specific the name, the less you need to open the file.

- [ ] **P2-20. Lifecycle gradually becoming Service Locator**
  If lifecycle can provide almost any dependency, it stops being lifecycle and becomes a service locator. Watch this very carefully.

- [ ] **P2-23. map[string]any usage**
  Prefer typed structs over `map[string]any`. Typed structs are faster, clearer, and easier to refactor.
