# ADR-028: Vector Abstraction v2 — Scored Search, Filters, Namespaces

## Status

Proposed (scale trigger: >500k vectors, ANN requirement, second embedding model)

## Context

`core.VectorIndex` today:

```go
Search(ctx, vec, limit) ([]string, error)      // IDs only — no scores
SearchBatch(ctx, vecs, limit) ([][]string, error)
Store(ctx, id, vec) error                      // no payload/metadata
Remove(ctx, ids) error
```

Two implementations exist: `InMemoryVectorIndex` (brute-force, the only
working one) and `SQLiteVecIndex` (**a stub — every method returns
"not yet implemented"**, `vector/sqlitevec.go`).

Observed behavior of the in-memory index (`vector/inmemory.go`):

- `load()` pulls **every** non-archived embedding from SQLite at startup
  (O(N) RAM; 500k × 1536-dim × 4 B ≈ 3 GB, stored twice — `entries[]` and
  `flatMatrix`).
- `Search` computes all N dot products then **fully sorts N results**
  (O(N log N) per query) instead of a top-K heap.
- LRU eviction at 500k uses `lastAccess`, which is only written on
  `Store`/`load` and **never updated by `Search`** — eviction is
  effectively FIFO, and evicted entities silently vanish from search
  recall while remaining in the DB.
- `SearchByVector` discards index-side scores (interface can't return
  them) and **recomputes cosine from DB BLOBs** after hydration.

Embeddings are duplicated: `entities.embedding` BLOB (hot OLTP rows —
every graph-walk row scan drags 6 KB per node) **and** inside the index.

## Why the current design breaks

1. **O(N) brute force per query.** At 1M+ vectors a single search is a
   ~1.5 GFLOP scan plus a full sort — 50–200 ms CPU per query, per
   deployment, before any graph work. ANN (HNSW/IVF) is table stakes at
   this scale.
2. **No scores in the contract.** Every ANN backend computes similarity
   internally; dropping it forces the wasteful recompute-from-BLOB
   pattern and blocks hybrid-score fusion.
3. **No metadata filters.** `Search(vec, k)` cannot express
   `WHERE category='task' AND archived=0 AND tenant=?`. Filtered ANN
   (pre-filter or post-filter with over-fetch) is how every real vector
   DB works; without it, multi-tenancy (ADR-030) and category-scoped
   search require full-scan + post-filter.
4. **No namespaces / model versioning.** Changing embedding model or dim
   requires a flag-day re-embed (`CheckMeta` hard-fails boot on dim
   mismatch). Blue/green re-embedding (serve old index while building
   new) is inexpressible.
5. **Silent recall loss.** Eviction + never-touched `lastAccess` means
   search results depend on insert order and process uptime — a
   correctness bug at exactly the scale this ADR targets.
6. **Dual source of truth.** BLOB in `entities` vs vector in index drift
   apart (`vi_drift` warnings in ingestion are the tell).

## Decision

The public type shape is frozen by **ADR-031** as `pkg/spi.VectorStore`.
This ADR owns the vector behavior and backend obligations; it must not define
an alternate `vectorindex.Index` or provider-specific public type.

The canonical operations are scored search, batch upsert, delete, and
namespace statistics. The contract has `Hit{ID, Score}`, a small typed filter,
an opaque namespace, and no arbitrary `map[string]any` payload. Domain
hydration remains outside the vector backend.

- `namespace` is an opaque application-owned partition key. ADR-030 may map
  tenant and model identity into it without changing this interface.
- `Score` is cosine similarity with higher-is-better semantics. Backends using
  another native metric must normalize it or wait for a versioned metric
  contract.
- Filters are equality/range predicates and must be applied logically before
  the result limit.
- Upsert is idempotent by `(namespace, id)`; partial failures identify failed
  records for retry.
- Dimension mismatch and capacity exhaustion are explicit errors.
- Silent eviction and silent recall degradation are forbidden.

Implementation options remain: (a) finished sqlite-vec, (b) pure-Go HNSW,
and (c) remote gRPC/HTTP adapters for Qdrant/pgvector/Milvus. All implement
`pkg/spi.VectorStore`; none becomes the public contract.

**Single source of truth:** vectors live only in the index; the
`entities.embedding` BLOB column is dropped by a migration (rebuild path:
re-embed from `content`). The operational migration and rollback procedure
belongs to ADR-033, not to the SPI.

The old `core.VectorIndex` remains frozen and is adapted during migration.
No methods are added to it in place.

## Alternatives considered

1. **Keep v1, add an external vector DB per deployment.** Rejected: the
   ID-only contract still wastes backend scores and can't filter;
   interface must change regardless.
2. **sqlite-vec only.** Rejected as sole option: v0 ANN maturity, CGO
   extension loading per platform, and it doesn't serve multi-node HA.
   Kept as one impl behind v2.
3. **Managed-only (Pinecone/Weaviate).** Rejected: hermem's identity is
   embedded/single-binary; a managed-only path breaks the offline CLI.
4. **Extend v1 in place (add Scores() method).** Rejected: additive
   methods on the old interface keep every old assumption (no ns, no
   filter) and double the migration surface.

## Tradeoffs

- **HNSW memory + build time:** ~1.3–1.5× raw vector RAM, index build on
  load. Accepted: bounded by namespace sharding and quantization
  (`vector/quantize.go` already exists for scalar quantization).
- **Dropping the BLOB column** is a one-way data migration; rollback
  requires re-embed. Mitigated by ADR-033's data-migration framework and
  a kept-backup window.
- **Filter language** must stay deliberately small (equality + range) so
  every backend can implement it; richer predicates belong in the
  retrieval layer as post-filter.
- **Interface churn:** v1→v2 touches `retrieval`, `ingestion`, `memory`,
  `edge`, `reembed`, `compression`, `health`. One-time, mechanical.

## Consequences

- ANN search at 10M+ vectors becomes possible without a rewrite.
- Blue/green embedding-model upgrades become an operational procedure
  instead of a flag day.
- Removes the dual-write drift class (`vi_drift`) — index is the truth.
- Prerequisite for per-tenant vector isolation (ADR-030) and for the
  retrieval read-path work (ADR-037).
