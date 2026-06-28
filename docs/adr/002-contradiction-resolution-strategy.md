# ADR-002: Pluggable Contradiction Resolution Strategy

## Status

Accepted

## Context

The ingestion pipeline previously hardcoded a confidence threshold of 0.7 to decide whether to keep both contradictory entities or archive the existing one. This made it impossible to swap in different resolution strategies (e.g. LLM-based, cross-encoder, human-in-the-loop) without modifying the ingestion code.

## Decision

Introduce a `ContradictionResolver` interface with a single `Resolve` method:

```go
type ContradictionResolver interface {
    Resolve(existing core.Entity, incoming core.ExtractedEntity) ResolutionAction
}
```

Initial implementation: `ThresholdResolver` — replaces the hardcoded 0.7 threshold with a configurable value.

The ingest pipeline depends only on the interface; concrete resolvers are injected via `IngestionWorkerConfig.Resolver`.

## Alternatives Considered

1. **Configuration-driven threshold**: Just expose the 0.7 value in config. Simpler, but doesn't allow future strategies like LLM-based resolution. Rejected because the interface is barely more code and enables future extensibility.

2. **Strategy pattern with a factory**: A `ResolverFactory` that creates resolvers from config strings. More complex than needed for the current use case. The simple interface + nil-default pattern is sufficient.

3. **Chain of Responsibility**: Multiple resolvers tried in sequence. Over-engineered for the initial implementation; can be composed later via a `CompositeResolver` if needed.

## Trade-offs Accepted

- **Interface overhead**: One extra indirection per contradiction resolution. Negligible cost — the resolver is called once per entity pair during ingestion.
- **Default behavior**: When no resolver is configured, the pipeline falls back to `ThresholdResolver{Threshold: 0.7}`. This preserves backward compatibility while enabling opt-in customization.
