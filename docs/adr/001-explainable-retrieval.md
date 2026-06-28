# ADR-001: Explainable Retrieval via ScoreBreakdown

## Status

Accepted

## Context

The retrieval engine previously returned only a final composite ranking score for each result. Callers had no visibility into *why* a particular node was ranked higher than another — whether it was due to vector similarity, recency, centrality, or graph distance.

## Decision

Introduce a `ScoreBreakdown` value object that decomposes the ranking score into its constituent components. When `Explain=true` is set on `RetrieveContextOptions`, every `GraphNode` and `RetrievedFact` carries a `*ScoreBreakdown` containing:

- `VectorScore` — cosine similarity to query
- `RecencyScore` — exponential decay on UpdatedAt
- `TemporalScore` — exponential decay on CreatedAt
- `CentralityScore` — log10(1 + Degree)
- `PathScore` — cumulative edge weight from seed
- `DepthPenalty` — the fraction subtracted by exponential depth decay
- `FinalScore` — the composite ranking score
- `Weights` — the ranking weights used (for full reproducibility)

JSON serialization is owned by the API layer via `json` tags; retrieval logic remains transport-independent.

## Alternatives Considered

1. **Return only final score + a log line**: Simpler, but callers can't programmatically inspect scoring reasons. Rejected because the `/query/explain` endpoint needs structured output.

2. **Score explanation as a separate API call**: Adds latency and requires re-computation. Rejected in favor of inline breakdown that piggybacks on the existing scoring pass.

3. **Machine-readable scoring rules (e.g. JSON Logic)**: Over-engineered for the current use case. The fixed linear combination with exponential decay is sufficient and more predictable.

## Trade-offs Accepted

- **Memory**: Each explained node carries an extra ~48 bytes (the ScoreBreakdown struct). Acceptable because Explain is opt-in.
- **Backward compatibility**: The legacy scalar fields (`VectorScore`, `RecencyScore`, `DepthPenalty`, `RankingScore`) on `RetrievedFact` are preserved for callers predating ScoreBreakdown. Will be deprecated in a future release.
