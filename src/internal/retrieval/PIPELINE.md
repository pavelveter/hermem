# Retrieval Pipeline

This document describes the retrieval pipeline that turns seed IDs
(or a user query) into a ranked, bucketed `*core.RetrievalResult`.
It is the canonical reference for what each stage does, where the
code lives, and how to observe each stage in production.

## Pipeline at a glance

```
seeds / query
     │
     ▼
┌──────────────────┐
│  expand_graph    │  SQL CTE walk from seeds up to effDepth.
│  (expand.go)     │  Returns []scannedNode (node + decoded vec).
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│ score_and_rank   │  Per-node: scorer or ComputeScoreComponents
│ (walk.go)        │  (Explain path). Collects depth-0 seeds.
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│   rank_sort      │  Stable sort by RankingScore DESC; NaN/Inf
│ (scoring.go:     │  clamped to 0 so sort never sees an undefined
 │  sortByScoreDesc)│  comparator.
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│   bucketize      │  Content-level dedup (first-write-wins) +
│  (walk.go)       │  per-category fan-out into WorldFacts /
│                  │  Opinions / Experiences / Observations.
│                  │  Explain path also propagates legacy scalar
│                  │  VectorScore / RecencyScore / DepthPenalty /
│                  │  RankingScore fields.
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│     rerank       │  Optional core.Reranker is invoked per non-
│  (walk.go)       │  empty bucket. nil Reranker = no-op. Bucket
│                  │  contents are replaced in place.
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│  explain log     │  One slog.Info("retrieval.explain", ...) per
│  (walk.go:       │  call when opts.Explain=true; per-bucket
 │  logRetrieval-  │  counts + top-ranked breakdown per bucket.
 │  Explanation)   │
└────────┬─────────┘
         │
         ▼
*core.RetrievalResult
```

## File layout

```
src/internal/retrieval/
├── expand.go      ← expand_graph stage + scannedNode type
├── scoring.go     ← defaultCompositeScorer, compositeScore,
│                    ScoreComponents, sortByScoreDesc,
│                    recencyScore, centralityScore,
│                    ComputeScoreComponents, BuildScoreBreakdown
├── temporal.go    ← expDecayHours, temporalScore (temporal ranking
│                    stage — the CreatedAt decay axis)
├── walk.go        ← RetrieveContext orchestrator + scoreAndRank +
│                    bucketize + applyReranker + logRetrievalExplanation
│                    + MultiHopRetrieveContext + topKFromResult
├── tracing.go     ← tracerFromOpts, spanFromOpts, startStageSpan
├── service.go     ← transport-agnostic Service (Search, Retrieve,
│                    Query, Response, Explain, Provenance)
├── response.go    ← GenerateResponse (embed → search → walk → markdown)
├── formatting.go  ← FormatContextMarkdown
├── walk_bench_test.go    ← per-stage benchmarks
├── walk_test.go          ← integration + stage unit tests
├── scoring_test.go       ← scoring helper tests
└── service_test.go       ← Service-level tests
```

## Stage contracts

### 1. expand_graph — `expand.go`

| Aspect | Detail |
|--------|--------|
| Inputs | `db`, `seedIDs`, `opts`, `effDepth` |
| Output | `[]scannedNode`, `error` |
| Span | `retrieval.expand_graph` |
| Failure modes | SQL query error, row scan error |

Runs the recursive CTE that walks the graph from seed IDs up to
`effDepth`. Honors `opts.MaxRetrievedNodes` as a soft cap and
`opts.TimeFrom` / `opts.TimeTo` as SQL filters. Decodes the
embedding blob inline so the scorer doesn't have to re-query the
row. Depth-0 seeds are NOT filtered here — `score_and_rank` owns
that.

### 2. score_and_rank — `walk.go`

| Aspect | Detail |
|--------|--------|
| Inputs | `[]scannedNode`, `opts`, `w`, `scorer` |
| Output | `[]rankedNode` (unsorted), `[]core.GraphNode` (seeds) |
| Span | `retrieval.score_and_rank` (attrs: ranked_count, seed_count) |
| Failure modes | none — pure function |

Per-node: applies `scorer` (non-Explain) or `ComputeScoreComponents`
+ `comps.Final(w)` (Explain). The Explain path funnels through
`ComputeScoreComponents` so sim / recency / temporal / centrality /
path are extracted exactly once and the breakdown derives from the
same intermediates as the final score. Depth-0 entries are
collected into `seeds` (with `rn.node`, not the pre-score copy, so
any attached ScoreBreakdown propagates).

### 3. rank_sort — `scoring.go:sortByScoreDesc`

| Aspect | Detail |
|--------|--------|
| Inputs | `[]rankedNode` (mutated in place) |
| Output | none |
| Span | `retrieval.rank_sort` (attrs: sorted_count) |
| Failure modes | none — pure function |

Stable sort by `score` DESC. NaN/Inf scores are clamped to 0 before
the sort so `sort.SliceStable`'s comparator never sees undefined
ordering (which would otherwise produce a run-time panic in the
comparator).

### 4. bucketize — `walk.go:bucketize`

| Aspect | Detail |
|--------|--------|
| Inputs | `[]rankedNode`, `seeds`, `w`, `explain` |
| Output | `*core.RetrievalResult` |
| Span | `retrieval.bucketize` (attrs: world_facts, opinions, experiences, observations) |
| Failure modes | none — pure function |

Content-level dedup (first-write-wins on the sorted ranked slice)
plus per-category fan-out. Explain path also propagates the legacy
scalar `VectorScore` / `RecencyScore` / `DepthPenalty` /
`RankingScore` fields on each fact for backward compat with callers
predating `ScoreBreakdown`.

### 5. rerank — `walk.go:applyReranker`

| Aspect | Detail |
|--------|--------|
| Inputs | `*core.RetrievalResult`, `core.Reranker`, `ctx`, `query` |
| Output | `error` |
| Span | `retrieval.rerank` (only when `opts.Reranker != nil`) |
| Failure modes | `Reranker.Rerank` error (wrapped with bucket name) |

`nil` Reranker is a no-op pass-through so pipeline composition
stays uniform across callers. Per-bucket invocation preserves
category semantics — the Reranker only re-orders facts within
their bucket. Empty buckets are skipped (no wasted round-trip).

### 6. explain log — `walk.go:logRetrievalExplanation`

| Aspect | Detail |
|--------|--------|
| Inputs | `*core.RetrievalResult`, `seedCount`, `effDepth` |
| Output | none |
| Span | none (it's the observability sink itself) |
| Failure modes | none |

One structured `slog.Info("retrieval.explain", ...)` per call
when `opts.Explain=true`. Carries per-bucket counts and the top-
ranked breakdown per bucket (vector / recency / temporal /
centrality / path / depth_penalty / final). Non-explain calls
emit no log so log volume stays flat for the common path.

## Observability

Every stage opens a span via `tracing.startStageSpan`. Spans nest
under the outer `/retrieve` handler span (when the HTTP layer adds
one) and carry per-stage attributes for the OTLP exporter.

With no `Tracer` in `opts.Ctx` the pipeline uses `NoopTracer`
through `tracing.TracerFrom`, so adding spans adds zero overhead
when tracing is off. See `walk_test.go:TestRetrieveContext_*Tracing*`
for the contract.

## Profiling

Per-stage benchmarks live in `walk_bench_test.go`:

```
go test -bench=. -benchmem ./src/internal/retrieval/
```

| Benchmark | Stage under test | Setup cost dropped |
|-----------|------------------|--------------------|
| `BenchmarkRetrieveContext` | full pipeline | `b.ResetTimer()` after `benchSetup` |
| `BenchmarkExpandGraph` | SQL CTE walk | same |
| `BenchmarkScoreAndRank` | composite scorer pass | pre-built `[]scannedNode` |
| `BenchmarkBucketize` | dedup + per-category fan-out | pre-built `[]rankedNode` |

Each benchmark seeds a 50-node ring fixture once via `benchSetup`.
`benchtime=N` lets you control sample size; `-benchmem` adds
allocation tracking.

## Multi-hop

`MultiHopRetrieveContext` interleaves shallow walks with vector
similarity jumps to cross topological gaps. It calls
`RetrieveContext` per hop (for the walk) plus one final call
for the union-of-seeds subgraph. The span tree reflects this:
each hop opens its own `retrieval.expand_graph` /
`retrieval.score_and_rank` / etc. span nested under the multi-hop
driver.

## Configuration knobs (from `core.RetrieveContextOptions`)

| Field | Default | Effect |
|-------|---------|--------|
| `MaxDepth` | 2 | Graph-walk depth (clamped by `DepthCeiling`). |
| `DepthCeiling` | 0 (no clamp) | Hard upper bound on `MaxDepth`. |
| `MaxRetrievedNodes` | 0 (no cap) | Soft cap on distinct IDs walked. |
| `RankingWeight` | `WithDefaults()` | Vector / recency / temporal / centrality / depth weights. |
| `CompositeScorer` | `defaultCompositeScorer` | Override the ranker; receives (node, vec, query, norm). |
| `Reranker` | nil | Per-bucket post-rank rerank (LLM call when set). |
| `Explain` | false | Populate `ScoreBreakdown` on every node + fact. |
| `TimeFrom` / `TimeTo` | zero | SQL filter on `entities.created_at`. |
| `MultiHopCount` | 2 (when called from `MultiHopRetrieveContext`) | Number of discovery iterations. |
