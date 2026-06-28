# ADR-003: Ranking Strategy Interface

## Status

Accepted

## Context

The retrieval engine previously used a single hardcoded set of ranking weights (`RankingWeight.WithDefaults()`). Different use cases require different ranking philosophies — a conversation memory lookup should prioritise recency, while a semantic search should prioritise vector similarity.

## Decision

Introduce a `RankingStrategy` interface:

```go
type RankingStrategy interface {
    Name() string
    Weights() core.RankingWeight
}
```

Four built-in policies:
- `DefaultRanking` — canonical weights (vector=0.7, recency=0.3)
- `FreshnessFirst` — recency-dominant (recency=0.5, vector=0.3)
- `SemanticSearch` — vector-dominant (vector=0.85, recency=0.05)
- `GraphExpansion` — centrality-dominant (centrality=0.45, vector=0.3)

The retrieval engine accepts a strategy via `RankingStrategyByName(name)` or direct injection.

## Alternatives Considered

1. **Enum-based profiles in config**: Simpler but less extensible. A future caller might need a strategy that computes weights dynamically based on query characteristics. The interface allows that.

2. **Per-request weight overrides**: More flexible but shifts complexity to callers. The strategy pattern keeps ranking philosophy in one place.

3. **Hardcoded switch on config string**: Rejected because it couples the retrieval engine to config parsing. The interface keeps the engine transport-independent.

## Trade-offs Accepted

- **Fixed set of policies**: The four built-in policies cover common use cases. Custom policies require implementing the interface — slightly more code but maximum flexibility.
- **No runtime weight tuning**: Weights are fixed per strategy. Dynamic weight adjustment based on query analysis is left to a future `AdaptiveRankingStrategy`.
