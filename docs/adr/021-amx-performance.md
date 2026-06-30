# ADR-021: AMX Performance Constants

## Status
Accepted

## Context
The Apple AMX (Apple Matrix coprocessor) guard test in `vector/cosine_amx_guard_test.go` uses hardcoded matrix dimensions to verify that `BatchDotProducts` stays within the performance budget on Apple Silicon.

## Decision
| Constant | Value | Rationale |
|----------|-------|-----------|
| `amxHotRows` | 1024 | Matches the §10 retrieval hot path: 1K vectors × 768-dim embeddings (typical for all-MiniLM-L6-v2). |
| `amxHotCols` | 768 | Standard embedding dimension for the default model. |
| `amxPerCallThreshold` | 2ms | Wall-time ceiling for one 1024×768 dot-product batch on M1. Exceeding this indicates AMX is not being utilized. |

These constants exist ONLY in the test file — they are not exported. The test fails if `BatchDotProducts` exceeds the threshold, catching AMX regressions at test time.

## Consequences
- AMX utilization is verified per CI run on macOS runners.
- If the default embedding model changes dimension, update `amxHotCols`.
- The threshold may need adjustment on slower CI runners — currently 2ms is ~10x the observed time on M1.
