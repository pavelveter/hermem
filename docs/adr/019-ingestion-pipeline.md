# ADR-019: Ingestion Pipeline

## Status
Accepted

## Context
The ingestion pipeline (`ingestion.dialog`) had the highest cognitive complexity in the codebase (cog=70). The core issue was code duplication: `MemoryWorkerResilient` and `MemoryWorkerResilientFromConfig` contained ~100 lines of identical dispatch→drain→checkpoint logic. The actual processing pipeline (Extract→Embed→Dedup→Contradict→Persist) was already well-structured in `ProcessDialogWithProvenance` and `processOneItemOnce`.

## Decision
1. **Extract `resilientLoop`** — Shared dispatch/drain/checkpoint loop extracted into a single `resilientLoop(cfg, ch)` helper. Both `MemoryWorkerResilient` and `MemoryWorkerResilientFromConfig` now delegate to it with a `resilientConfig` struct.
2. **Pipeline already clean** — `ProcessDialogWithProvenance` implements the canonical pipeline: Extract → Embed → SearchBatch → Normalize → ProcessEachItem. Each item goes through: findMatch (dedup) → handleContradiction → mergeOrCreate → executeItemTx → applyVIOps.
3. **Retry decorator** — `processOneItem` wraps `processOneItemOnce` with SQLITE_BUSY retry (max 5 attempts, exponential backoff). Non-busy errors propagate immediately.
4. **No further decomposition needed** — The individual stages are already separate methods with cognitive complexity ≤ 10 each.

## Consequences
- `MemoryWorkerResilient` and `MemoryWorkerResilientFromConfig` are now thin wrappers (~15 lines each) over the shared loop.
- The deprecated `MemoryWorkerResilient` will be removed once all callers migrate to `MemoryWorkerResilientFromConfig`.
- Pipeline stages are independently testable via the existing `IngestionWorker` methods.
