# ADR-004: Exponential Depth Decay

## Status

Accepted

## Context

The retrieval engine previously used a linear depth penalty (`w.DepthPenalty * pathWeight`) to penalise nodes further from the seed. This meant a node at depth 3 was penalised 3× as much as a node at depth 1, which doesn't match the empirical observation that semantic relevance drops off exponentially with graph distance.

## Decision

Replace the linear depth penalty with exponential decay: `2^(-depth)`. The composite score formula becomes:

```
score = (vector_weight * sim + recency_weight * recency + ...) * 2^(-depth)
```

Depth is derived from `pathWeight` (cumulative edge weight, typically 1.0 per hop). The `DepthPenalty` field in `ScoreBreakdown` now represents `1 - 2^(-depth)` (the fraction subtracted).

## Alternatives Considered

1. **Configurable decay base (e.g. `base^(-depth)`)**: More flexible but adds a tuning parameter with no clear default. The base-2 exponential is a natural choice for binary tree-like graph traversal. Rejected for simplicity.

2. **Keep linear penalty with configurable coefficient**: Simpler, but doesn't match the empirical decay pattern. The exponential formula is barely more code and produces better rankings.

3. **Gaussian decay**: `exp(-depth² / (2σ²))`. More parameters to tune, harder to reason about. The exponential is sufficient for the current use case.

## Trade-offs Accepted

- **Non-linear scaling**: Nodes at depth 0 are unchanged (decay=1.0). Nodes at depth 1 lose 50%, depth 2 lose 75%, depth 3 lose 87.5%. This is aggressive but matches the intuition that graph-walk quality degrades quickly.
- **`DepthPenalty` field semantics changed**: The field now represents `1 - decay` instead of `weight * path`. Callers relying on the old semantics will see different values. The `ScoreBreakdown.Weights` field provides full reproducibility.
