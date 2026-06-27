# ADR-002: Contradiction Threshold

## Status

Accepted

## Context

When ingesting new information, the system must decide how to handle contradictions with existing entities. The decision depends on the confidence of the existing entity:

- **High confidence** (≥ 0.7): Keep both entities (user explicitly stated this)
- **Low confidence** (< 0.7): Prefer the incoming entity (user's latest statement overrides)

## Decision

Use a fixed threshold of 0.7 for the confidence boundary.

```go
if existingConf >= 0.7 {
    return contradictionKeepBoth, "", nil
}
return contradictionPreferIncoming, existing.ID, viOps
```

## Rationale

1. **0.7 is a common threshold** in decision theory for "likely true" statements.
2. **High confidence means explicit user statement** — the user clearly believes this, so we shouldn't override it.
3. **Low confidence means inferred or uncertain** — the user's latest statement is more likely correct.
4. **Binary decision** (keep both vs. prefer incoming) is simpler to reason about than gradual merging.

## Consequences

- Entities with confidence ≥ 0.7 are treated as "facts" that persist.
- Entities with confidence < 0.7 are treated as "beliefs" that can be overridden.
- The threshold is hardcoded but could be made configurable in the future.
- The decision is logged for auditability.
