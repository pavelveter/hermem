# ADR-037: Retrieval Read-Path Modularization

## Status

Proposed (scale trigger: >1M nodes, hybrid-search demand, per-tenant ranking, sub-100ms p99 SLOs)

## Context

The read path today (`retrieval/`):

- A `Pipeline` stage abstraction exists (`pipeline.go`: expand → rank →
  assemble → render) but **`NewPipeline()` is only referenced by tests**.
  Production (`Service`, `app.Application`) calls package-level
  functions (`RetrieveContext`, `GenerateResponse`) directly — the
  pipeline is a veneer with two parallel implementations to keep in
  sync.
- Graph expansion is a single recursive CTE (`expand.go`) with
  `char(31)`-delimited visited-set cycle detection; it loads the
  embedding BLOB of every walked node (6 KB × N nodes), then truncates
  to `MaxRetrievedNodes` **after** the full walk executes.
- Ranking is a hand-rolled composite scorer (vector/recency/temporal/
  centrality/depth, ADR-022) with a 15-field `RetrieveContextOptions`
  grab-bag as the only tuning surface.
- No result cache, no query-embedding cache, no cursor/pagination
  (top-K only), no hybrid lexical channel (FTS5 is absent; there's no
  BM25 anywhere — `evaluation` has NDCG/MRR harnesses but retrieval has
  one vector-only channel).
- Errors are deliberately swallowed on hot paths (`Explain`: `embedding,
  _ := ...`) — silent degradation with no signal.

## Why the current design breaks

1. **Frontier explosion.** The CTE walks edges in both directions; on a
   1M-node graph with hub nodes, a depth-3 walk from a popular seed can
   materialize hundreds of thousands of rows before the soft cap — per
   query, per user, on the single SQLite connection.
2. **One retrieval channel.** Pure vector recall fails on exact-keyword,
   rare-term, and ID lookups (support tickets, error codes, names).
   Every mature memory/search system converges on hybrid (vector +
   BM25 + RRF) — there is no seam to add a second channel.
3. **No caching.** Identical agent queries (agents repeat themselves
   constantly) re-embed and re-walk every time; there is no
   invalidation-aware cache layer anywhere.
4. **Ranking is frozen in code.** Per-tenant weights, A/B experiments,
   or learning-to-rank require a fork. `CompositeScorer` is injectable
   per-request, but the production HTTP path never injects it — the
   seam exists and is unused, like `Pipeline`.
5. **Veneer abstractions.** `Pipeline` + `SetRank/SetExpand` suggest
   extensibility that production never exercises — a trap for
   contributors who extend the wrong side.

## Decision

Rebuild the read path as an explicit, wired pipeline (make `Pipeline`
the only implementation, delete the package-level entry points):

1. **Stage SPI** (`retrieval/stage`):
   `SeedResolver`, `Channel` (vector | fts | graph-walk), `Ranker`,
   `Assembler`, `Renderer` — each registered (ADR-029) and composed in
   `app.Application`, not hardcoded in `NewPipeline()`.
2. **Hybrid channels + fusion:** add an FTS5 lexical channel (SQLite
   built-in — no new dependency) producing BM25-ish scores; fuse with
   vector hits via RRF before the graph walk. Graph walk becomes one
   *optional expansion stage* with a strict frontier budget enforced
   **inside** the walk (LIMIT per depth, hub-degree cutoffs), not after.
3. **Frontier budgeting:** `expand` enforces per-depth row limits and
   stops materializing embeddings (scores come from VectorIndex v2 —
   ADR-028 — not BLOB re-decode).
4. **Caching layer:** two caches behind a tiny `Cache` interface —
   (a) query-embedding cache (key: model + normalized text), (b) result
   cache (key: tenant + query + knobs, invalidated by write-version
   stamp bumped on any write in the tenant's subgraph). In-memory LRU
   default; interface allows Redis later.
5. **Pagination:** cursor tokens (opaque, keyset over `(score, id)`) on
   search/retrieve endpoints; SDKs expose iterators.
6. **Fail loud, degrade explicit:** swallowed errors become structured
   `degradations: [...]` entries in the response envelope — clients and
   evals can see them.
7. **Ranking config per tenant/profile:** weights come from a named
   profile in config (ADR-030 overlays) referenced by `?profile=`,
   replacing the 15-field options struct over time.

## Alternatives considered

1. **Keep package funcs, fix walk internals only.** Rejected: leaves
   the two-implementation drift, no channel seam, no cache seam.
2. **External search engine (OpenSearch/Typesense) as the retrieval
   core.** Rejected as default: breaks single-binary identity; becomes
   possible later as a `Channel` implementation for the server tier.
3. **Full LTR (learning-to-rank) model.** Deferred: needs training
   data and an offline eval loop; `evaluation/` harnesses are the
   foundation, but the composite ranker with profiles ships first.
4. **Push retrieval into the DB engine (stored procedures / single
   mega-query).** Rejected: unmaintainable, unportable (ADR-027), and
   unprofileable.

## Tradeoffs

- **FTS5 index adds write cost + storage** (~20–30% table size) and must
  be kept in sync via triggers or the ingestion pipeline; accepted as
  the price of hybrid recall.
- **Cache invalidation by write-version** can over-invalidate on hot
  subgraphs (a busy hub flushes many cached queries); bounded by TTL +
  per-key tags, tuned operationally.
- **Cursor tokens** require stable sort keys — ties broken by ID, so
  ranking must be deterministic (it is, modulo float equality; we add
  ID tiebreak).
- **Deleting package-level entry points** is a breaking change for any
  in-repo callers — mechanical, one release with deprecated shims.

## Consequences

- Retrieval latency becomes budget-driven (frontier caps + caches)
  instead of graph-shape-driven.
- Hybrid recall (vector + lexical) closes the exact-match gap without
  new infrastructure.
- Ranking becomes operable: profiles, per-tenant overrides, A/B via
  `?profile=`.
- The pipeline SPI is real — stages are registered, composed, and
  tested as the only path, ending veneer-drift.
