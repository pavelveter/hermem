# ADR-003: Auto-Link Threshold

## Status

Accepted

## Context

When a new entity is stored, the system automatically links it to similar existing entities via the `related_to` edge. The threshold determines how similar two entities must be to be linked.

## Decision

Use a fixed threshold of 0.85 for cosine similarity.

```go
if r.Similarity <= 0.85 {
    continue
}
```

Limit to 3 auto-links per entity.

## Rationale

1. **0.85 is high enough** to avoid false positives — only very similar entities are linked.
2. **3 links max** prevents excessive linking that would create noise.
3. **Auto-linking is a convenience** — users can manually add more specific links.
4. **The threshold is conservative** — better to miss some links than to create incorrect ones.

## Consequences

- Entities with cosine similarity > 0.85 are automatically linked.
- The link count is limited to 3 to prevent graph explosion.
- Users can override by manually adding/removing edges.
- The threshold is hardcoded but could be made configurable.
