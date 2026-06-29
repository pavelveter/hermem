# ADR-006: Retrieval Pipeline Architecture

## Status

Accepted

## Context

`RetrieveContext()` was accumulating responsibilities — embedding, search, graph walk, scoring, ranking, bucketizing, and rendering all lived in a single function. This made the function hard to test, modify, and reason about.

## Decision

Introduce a stage-based pipeline architecture:

```go
type PipelineStage interface {
    Name() string
}

type Pipeline struct {
    expand   CandidateRetrievalStage
    rank     RankingStage
    assemble ContextAssemblyStage
    render   RenderingStage
}
```

Four stages execute in sequence:
1. **expandGraph** — recursive CTE graph walk from seed IDs
2. **scoreAndRank** — composite scorer per node; sorts by RankingScore DESC
3. **bucketize** — content-level dedup + per-category fan-out
4. **render** — convert RetrievalResult to string (Markdown/PlainText/JSON)

`RetrieveContext()` remains the package-level entry point, orchestrating stages inline for backward compatibility. The `Pipeline` struct provides an alternative, fully pluggable entry point for tests and advanced use.

## Alternatives Considered

1. **Handler chain (middleware pattern)** — rejected: stages need typed inputs/outputs, not generic `http.Request`.
2. **Single monolithic function** — rejected: the whole point is separation.
3. **Plugin system** — rejected: overengineered for 4 stages.

## Consequences

- Each stage is independently testable with mock inputs.
- Stages can be swapped via `Pipeline.SetExpand()`, `SetRank()`, etc.
- `RetrieveContext()` continues to work unchanged for existing callers.
- New stages (e.g., reranking, dedup) can be added without modifying existing stages.
