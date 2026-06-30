# ADR-023: BLAS Fallback for Cosine Similarity

## Status
Accepted

## Context
`vector/cosine_darwin.go` uses Apple Accelerate (cblas) for SIMD-accelerated dot products on macOS. Linux ARM (Graviton) and Linux x86_64 fall back to the pure-Go loop in `cosine.go`. The pure-Go path is correct but ~5-15× slower for large batch operations on the hot path.

## Decision
**Stay with pure-Go as the default on non-Darwin platforms.** Build-tagged BLAS (gonum/cblas/OpenBLAS) is NOT added as a dependency.

### Rationale
1. **Dependency cost**: gonum/cblas requires CGO + system BLAS library (OpenBLAS). This adds a build dependency that conflicts with the single-binary distribution model.
2. **Marginal gain**: The hot path (`SearchBatch`) is O(n·q) brute-force cosine. On Graviton ARM64, the pure-Go loop with compiler auto-vectorization achieves ~70% of BLAS throughput for the embedding dimensions used (384-768). The bottleneck is memory bandwidth, not compute.
3. **Existing mitigation**: The `sqlite-vec` backend (`vector_backend = "sqlite-vec"`) provides ANN search that avoids brute-force entirely. For production workloads needing sub-linear query time, this is the correct path — not BLAS.
4. **Future option**: If BLAS acceleration becomes critical, add a `//go:build blasm` tagged file that imports gonum/blas. The `core.VectorIndex` interface is backend-agnostic — no caller changes needed.

### Pure-Go Performance Budget
- `CosineSimilarity`: < 100ns for 768-dim vectors on Graviton c7g (measured).
- `BatchDotProducts` (1024×768): < 2ms on Graviton c7g (measured via benchmark).
- These are within the retrieval latency budget (total query < 50ms).

## Alternatives Considered
- **gonum/blas with build tag**: rejected — adds CGO dependency, marginal gain.
- **OpenBLAS via syscall**: rejected — complex linkage, no significant benefit over compiler auto-vectorization.
- **SIMD intrinsics via asm**: rejected — platform-specific maintenance burden for <2× gain.

## Consequences
- Non-Darwin platforms use pure-Go cosine. Performance is acceptable for current scale.
- If production deployment on Graviton shows cosine as a bottleneck, revisit with gonum/blas build tag.
- Documented benchmarks in `docs/perf-budgets.md`.
