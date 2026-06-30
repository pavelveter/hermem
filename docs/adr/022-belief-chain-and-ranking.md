# ADR-022: Belief Chain Depth and Ranking Weights

## Status
Accepted

## Context
Two sets of magic constants govern belief propagation and retrieval ranking. Without documentation, their purpose and safe ranges are unclear.

## Decision

### Belief Chain Depth

| Constant | Value | Rationale |
|----------|-------|-----------|
| `MaxChainDepth` | 32 | Caps belief revision chain traversal in `evolution/chains.go`. Prevents unbounded recursion on circular or deeply nested belief chains. 32 is sufficient for realistic trust chains (typically 3–5 levels) while preventing stack issues. |

### Ranking Weights (default values from `RankingWeight.WithDefaults()`)

| Field | Default | Rationale |
|-------|---------|-----------|
| `VectorWeight` | 0.7 | Semantic similarity is the primary relevance signal. |
| `RecencyWeight` | 0.3 | Temporal freshness provides a secondary boost. |
| `DepthPenalty` | 0.05 | Mild penalty for deeper graph hops — discourages irrelevant far-away nodes. |
| `RecencyHalfLifeHours` | 720 (30 days) | Facts older than 30 days decay to half relevance. |
| `TemporalWeight` | 0.0 | Disabled by default; opt-in via config for time-sensitive domains. |
| `TemporalHalfLifeHours` | 720 | Same as recency when enabled. |
| `CentralityWeight` | 0.0 | Disabled by default; opt-in for authority-biased retrieval. |

All ranking weights are configurable via `hermem.ini` `[ranking]` section or the `/admin/stats` runtime endpoint. The `RankingWeight` struct uses zero-means-unset semantics — zero fields are filled by `WithDefaults()`.

## Consequences
- `MaxChainDepth` is a safety limit, not a tuning knob.
- Ranking weights are tuning knobs — the defaults are conservative and biased toward semantic similarity.
- Production deployments should experiment with `FreshnessFirst` or `GraphExpansion` profiles for domain-specific tuning.
