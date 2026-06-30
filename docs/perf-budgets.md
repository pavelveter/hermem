# Performance Budgets

Accepted performance characteristics and trade-offs for hot paths.

## Vector Search (`vector/inmemory.go`)

**Algorithm**: Brute-force cosine similarity via `BatchDotProducts` (SIMD on Darwin via Accelerate, pure-Go fallback elsewhere).

**Complexity**: O(n · q) where n = number of vectors, q = number of queries per batch.

**Why O(n) per query is accepted**: Brute-force is the correct algorithm for exact cosine similarity — no sub-linear approach can guarantee exact results. The SIMD-accelerated `BatchDotProducts` makes the constant factor small enough for production workloads (tested up to 500K vectors).

**When to switch**: If query latency becomes unacceptable at scale, replace with an ANN index (HNSW, IVF) which trades exactness for sub-linear query time. The `core.VectorIndex` interface already supports this — add a new implementation and set `vector_backend` in config.

**Accepted linear scans** (not worth optimizing):
- `SearchBatch` inner loop: O(n) per query — inherent to brute-force
- `store/migration.splitSQL`: one-shot startup code, not hot path
