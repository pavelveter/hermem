# ADR-001: Ranking Formula

## Status

Accepted

## Context

The retrieval system needs a ranking formula to order retrieved entities by relevance. The formula must balance multiple signals:

- **Vector similarity**: How semantically similar is the entity to the query?
- **Recency**: How recently was the entity updated?
- **Temporal**: When was the entity created?
- **Centrality**: How connected is the entity in the graph?
- **Depth penalty**: How far from the seed node was this entity discovered?

## Decision

Use a linear combination of weighted features with a depth penalty:

```
score = VectorWeight * sim + RecencyWeight * recency + TemporalWeight * temporal + CentralityWeight * centrality - DepthPenalty * pathWeight
```

Default weights:
- VectorWeight: 0.7
- RecencyWeight: 0.3
- TemporalWeight: 0 (disabled by default)
- CentralityWeight: 0.05
- DepthPenalty: 0.05

## Rationale

1. **Vector similarity dominates** (0.7) because semantic relevance is the primary signal for most queries.
2. **Recency matters** (0.3) because newer information is generally more relevant.
3. **Centrality provides a small boost** (0.05) for well-connected entities, but doesn't overwhelm primary signals.
4. **Depth penalty discourages** walking too far from seeds, preferring nearby entities.
5. **Temporal is disabled by default** because creation time is less useful than update time for most use cases.

## Consequences

- Weights are configurable via `RankingWeight` struct.
- `WithDefaults()` provides safe zero-value handling.
- The formula is applied consistently across all retrieval paths.
- Explain mode exposes per-component scores for debugging.
