# ADR-005: Recency Scoring

## Status

Accepted

## Context

The retrieval system needs to score entities by how recently they were updated. Recent entities are generally more relevant, but the decay rate must be carefully chosen.

## Decision

Use exponential decay with a configurable half-life (default: 720 hours = 30 days).

```go
func recencyScore(updatedAt *time.Time, halfLifeHours float32) float32 {
    if updatedAt == nil || updatedAt.IsZero() || halfLifeHours <= 0 {
        return 1  // nil/zero = "as fresh as possible"
    }
    return expDecayHours(*updatedAt, halfLifeHours)
}
```

## Rationale

1. **Exponential decay** provides smooth, continuous scoring.
2. **720-hour half-life** means an entity loses half its recency score every 30 days.
3. **Nil/zero timestamps score 1** — entities without explicit timestamps are treated as "fresh" (never updated = potentially still relevant).
4. **Configurable half-life** allows tuning for different use cases.

## Consequences

- Recent entities score higher than older ones.
- The decay rate can be tuned via `RankingWeight.RecencyHalfLifeHours`.
- Nil timestamps are handled gracefully (score = 1).
- The formula is consistent with temporal scoring.
