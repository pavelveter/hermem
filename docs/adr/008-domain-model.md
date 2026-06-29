# ADR-008: Domain Model Evolution

## Status

Accepted

## Context

The codebase initially used a generic `Entity` struct for all domain objects. As the domain grew (facts, episodes, evidence, beliefs, tasks, goals), the single `Entity` type became a bottleneck — it carried fields irrelevant to most use cases and forced type assertions at domain boundaries.

## Decision

Introduce typed domain objects alongside the persistence-level `Entity`:

| Type | Package | Purpose |
|------|---------|---------|
| `Fact` | `core` | World knowledge with confidence scoring |
| `Episode` | `episodic` | Temporal event sequences |
| `Evidence` | `memory/evidence` | Supporting evidence for beliefs |
| `Belief` | `memory/belief` | Confidence-weighted assertions |
| `Task` | `task` | Lifecycle-managed work items |
| `Goal` | `goal` | Top-level objectives |

Each type has a dedicated domain service package that operates on it. `Entity` remains the persistence representation — domain services convert to/from Entity at boundaries.

## Alternatives Considered

1. **Full DDD with aggregates** — rejected: overengineered for a single-binary embedded system.
2. **Keep Entity-only** — rejected: growing field sprawl and type assertions.

## Consequences

- Domain services operate on typed objects, not generic Entity.
- Entity remains the SQLite row representation.
- Conversion between domain types and Entity is explicit and testable.
- New domain concepts get their own type without polluting Entity.
