# Performance Budgets

Accepted performance characteristics and trade-offs for hot paths.

## Vector Search (`src/internal/vector/provider.go`)

**Algorithm**: Brute-force cosine similarity via `BatchDotProducts` (SIMD on Darwin via Accelerate, pure-Go fallback elsewhere). Vectors are stored
contiguous per namespace — one row-major matrix plus parallel ID and metadata
slices — so a search hands the whole matrix to `BatchDotProducts` without
copying. Only the metadata-filter path copies, because a filter breaks
contiguity.

**Complexity**: O(n · q) where n = number of vectors, q = number of queries per batch.

**Why O(n) per query is accepted**: Brute-force is the correct algorithm for exact cosine similarity — no sub-linear approach can guarantee exact results. The SIMD-accelerated `BatchDotProducts` makes the constant factor small enough for production workloads (tested up to 500K vectors).

**When to switch**: If query latency becomes unacceptable at scale, replace with an ANN index (HNSW, IVF) which trades exactness for sub-linear query time. `spi.VectorStore` already supports this — add a new implementation and set `vector_backend` in config.

**Accepted linear scans** (not worth optimizing):
- `inMemoryVectorStore.Search` inner loop: O(n) per query — inherent to brute-force.
- `store/migration.splitSQL`: one-shot startup code, not hot path.
- `SELECT ... FROM entities WHERE archived = 1`: admin stats, `hermem diagnose`
  and the integrity sweep only. Measured 3.39ms at 100k rows with 40% archived;
  a partial index `ON entities(archived) WHERE archived = 1` makes it 0.62ms but
  adds ~1.1ms to every 500-row archive flip, so it is not worth a per-write tax
  until an archived lookup lands on a request path.

## Graph Reads

**Connected components** (`store.FindConnectedComponents`): entities are
addressed by a dense `int32` index and edges live in one CSR adjacency array,
walked with a `BFS` over a `[]bool` visited set. Any map-of-slices layout costs
a slice header per node plus a map bucket per adjacency insert and rehashes the
visited set per neighbour — measured at 8.88ms / 125k allocs for 5k nodes, 6.89ms
/ 105k allocs after. The residual is go-sqlite3 materializing the two row
cursors.

**Community detection** (`graph/community.DetectCommunities`): the Louvain
local-moving loop operates on integer indices resolved once per call
(`indexNeighbours`), not on `map[string]float64` adjacency. Candidate selection
is a linear scan that breaks ties by lowest community ID — the sort it replaced
existed only for deterministic tie-breaking. Measured at 3k nodes / 12k edges:
10.79ms / 34k allocs → 4.48ms / 7k allocs, identical partition.

**Archived entities in components**: `FindConnectedComponents` indexes *every*
entity and only refuses to *seed* from an archived one. Archiving is a visibility
filter, not an edge deletion, so an edge running through an archived entity still
joins the live entities on both sides. An all-archived component is never
reported. Both properties are pinned by
`src/internal/store/graph_bench_test.go`.

**Graph walk** (`retrieval.expandGraph`): the recursive CTE's step is written as
two `UNION ALL` branches (outgoing, then incoming) rather than one join with
`(ed.source_id = gw.id OR ed.target_id = gw.id)` plus a `CASE`-expression entity
join. SQLite cannot turn the `CASE` form into a primary-key lookup — it builds an
automatic partial index on `entities(archived)` and scans every live entity per
edge row, which makes the walk quadratic in graph size.

## Batched Writes

Single-statement `UPDATE ... WHERE id IN (...)` beats a per-row loop whenever the
batch is already all-or-nothing. Measured on 500 rows: 1.56ms per-row versus
0.45ms batched for the retention archive sweep. Batch size must stay well below
SQLite's `SQLITE_MAX_VARIABLE_NUMBER` (32766; `go-sqlite3` compiles at the
default). Applies to `retention.RunOnce`, `ConfidenceLifecycle.RunOnce` and
`metrics.AsyncMetricsWorker.flushLocked`.
