# TODO: Review-Driven Hardening Batches

## Batch 7 — Shipped (rounds 5–7)

All 7 items from the original review-driven hardening batch are
landed. Commit history (most recent at top):

```
1841faa  chore: format-only normalization across docs, db, embedder, metrics
9fa0f2c  chore: debug-log DBPath symlink resolution divergence (#1)
a814929  feat: extra_categories / extra_relation_types config-driven allowlists (#2)
40cbb11  fix: path-tracker cycle guard in graph_walk CTE, char(31) delimiter (#7)
91200e1  fix: vector-index write before SQLite commit in StoreEntityWithEmbedding (#10)
e1d98b4  fix: createEdges bulk multi-VALUES INSERT, chunked via DB var ceiling (#6)
9dc8250  feat: ini.v1 parser + embedder/extract timeouts (round 5)
63e11f2  fix: chunked SQLite IN-clause exec + INI helper (round 4)
```

### Items that remain deferred (NOT this batch)
- #4 SmartCosineSimilarity — needs M-series SIMD benchmarks before
  committing to a hybrid SIMD/naive threshold. Not picked up here.
- #5 BEGIN IMMEDIATE — theoretical; code does not currently use
  any transaction wrappers.
- #9 Tick stacking — false positive. Go's `time.Ticker` already
  drops excess ticks.
- #15 InMemory vs sqlite-vec — architectural; revisit when N>100k.

---

## Batch 8 — Lost-in-the-Middle Composite Scoring

Theme: Quality-of-results for graph walk retrieval. hermem's value
proposition is being a semantic memory store; sorting returned
context for downstream LLM prompts matters more than micro-SIMD
gains. Two production-pragma items + one architecture item.

### Why this batch over a SIMD pass

The codebase already has a non-trivial composite scorer:

- `computeRankingScore(queryEmbedding, nodeEmbedding, updatedAt)`
  in `src/retrieval.go` returns
  `0.7*cosine(query, node) + 0.3*exp(-age/30d)`.
- It runs in the row loop, attaches `RankingScore` to each
  `GraphNode`, then `sort.SliceStable` orders category buckets
  by score desc.

What it does NOT model:
- **Depth distance.** Mid-depth nodes (depth 2 in a depth-4 walk)
  are penalized equally — the classical "Lost in the Middle"
  retrieval pathology (Liu et al., 2023): relevant items near the
  ends of an ordering get more attention than those in the middle.
  For hermem, the equivalent is mid-depth graph-walk results
  scoring lower than both seeds and far-periphery nodes, so the
  downstream prompt sees the seeds + periphery first and the
  actually-relevant mid-context last (or trimmed if MaxRetrievedNodes
  kicks in).
- **Composable scoring.** Operators today cannot supply a custom
  ranking function without forking the binary; embedding a
  function field in `RetrieveContextOptions` is the smallest
  extension that unlocks A/B scoring.
- **Query-embedding overhead.** Each row decodes the node's
  embedding bytes and computes cosine. The query norm is constant
  per call but recomputed at every row.

### #16 CompositeScorer extension — MEDIUM (lead item)

**Files:** `src/retrieval.go`, tests `src/retrieval_test.go`.

**Approach:**
1. Add `CompositeScorer func(retrievedNode ScoringInput) float32`
   to `RetrieveContextOptions`. nil → use the existing weighted
   default (`sim*0.7 + recency*0.3`).
2. Define `ScoringInput` as a small read-only struct holding the
   fields the scorer needs: `Depth int`, `Embedding []float32`
   (decoded bytes for the node), `UpdatedAt time.Time`,
   `QueryEmbedding []float32` (from opts). Score is computed on
   the caller-supplied closure; nil closure falls back.
3. Refactor `computeRankingScore` → `defaultCompositeScorer` as
   the package-level default scorer implementation. Behaviour
   unchanged for callers that don't set `opts.CompositeScorer`.
4. **Add depth-soft-floor**: default scorer now also applies a
   small depth penalty when `Depth > 0` to surface mid-depth
   relevant nodes over far-periphery noise. Concretely,
   `score = w_sim*sim + w_recency*recency - depthPenalty*Depth`.
   The depth penalty starts small enough (e.g. 0.05 / depth unit)
   that pure recency dominates by the time a node is stale; the
   default weight constants live in named `const` blocks so the
   existing `TestRetrieveContext*` ordering tests that depended
   on the old shape are surfaced as failures first, then patched
   in the same PR.

**Test:** `TestCompositeScorerDefault` — fixture with three nodes
identical except depth (0, 2, 4), assert score monotonic
decrease is overridden for the depth-2 node when its cosine is
strictly higher. `TestCompositeScorerCustom` — caller-supplied
closure returns 99.0; verify the post-sort ranks the
custom-scored node first regardless of cosine.

**Risk:** Any re-rank reshape can break snapshot tests that
assert first-N ordering. Acceptable; explicit list of test
fixtures to update is in §Validation.

### #17 Query-embedding norm precompute — MICRO-OPT (bundle)

**Files:** `src/retrieval.go`.

**Approach:** When `opts.QueryEmbedding` is set and non-empty,
precompute its `VectorNorm` exactly once at the top of
`RetrieveContext` and pass it into the scorer. Each row's
`cosine(query, node)` then computes `dot / (cachedQueryNorm *
node.Norm)` instead of recomputing the query-norm every call.
For a 100-row retrieval this is ~99 SQRT calls saved per
request; not a hot-loop issue at current scale, but the wiring
becomes the natural handoff point if we ever introduce
`CompositeScorer` that needs the cached value.

**Test:** No new test. Microbench before/after (`Benchmark
RetrieveContext_NormCache`) to demonstrate the win; defer
benchmark infrastructure to #18.

**Risk:** Negligible. Pure rearranging of `computeRankingScore`'s
inputs.

### #18 Recall microbench infrastructure — MICRO-OPT (bundle)

**Files:** `src/vector_bench_test.go` (extend existing bench
file) and a new `src/retrieval_bench_test.go` (parallel to
existing bench layout).

**Approach:**
1. `BenchmarkRetrieveContextRecall`: synthesize a fixture graph
   (10 seeds, 50 entities, 3 cycles), run `RetrieveContext` at
   5/10/50/100 rows, capture `BenchmarkRetrieveContextRecall-
   N` ops/sec and allocs/op. Establishes the baseline before
   #16 lands so re-order visibility is measurable.
2. `BenchmarkCompositeScorerDispatch`: nil closure vs custom
   closure vs default-with-depth; verify custom closure path
   doesn't accidentally box into interface allocations.

These benchmarks are infrastructure-only; no production code
changes.

**Test:** None — bench files are run on demand, not in CI suite.

### Execution order

1. **#18 recall microbench** — establishes the baseline before
   any re-rank change, so #16's impact is measurable.
2. **#17 query-embedding norm precompute** — pure refactor,
   zero behaviour change, microbench delta confirms.
3. **#16 CompositeScorer extension** — the lead item; updates
   `TestRetrieveContext*` snapshots where the default-depth
   penalty changes ordering. One rollup commit covering the
   three.

### Validation plan

- `gofmt -w src/*.go`
- `go vet ./src/...`
- `go test -count=1 -race -timeout 180s ./src/...` (full suite,
  verify #16 didn't drift `TestRetrieveContext*` ordering)
- `go test -bench='BenchmarkRetrieveContext|BenchmarkCompositeScorer' -benchtime=3x -run=^$ ./src/...`
  (record #17 + #18 baseline numbers into the commit body)
- New targeted runs:
  - `go test -run TestCompositeScorerDefault ./src/...`
  - `go test -run TestCompositeScorerCustom ./src/...`

### Out of scope (deferred)

- #4 SmartCosineSimilarity — needs Apple Silicon and AVX2 hosts
  on hand to measure; revisit after Batch 8 lands.
- #19 Generic retrieval response filter / category weights —
  lifted from a stub I left in `TODO.md`; paper design until a
  user hits the issue.

---

## Tree-cleanup note (small, separate)

`main` (the stray Go test binary produced by `go build -o main`
during earlier validation) was dropped at commit `1841faa`.
Future runs should add `main` to `.gitignore` so the same
mistake doesn't reappear.
