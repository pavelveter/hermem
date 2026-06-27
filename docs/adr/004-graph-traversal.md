# ADR-004: Graph Traversal Strategy

## Status

Accepted

## Context

The retrieval system needs to traverse the entity graph from seed nodes to discover related entities. The traversal must be bounded to prevent infinite loops and excessive computation.

## Decision

Use recursive CTE (Common Table Expression) with depth limiting:

1. Start from seed IDs
2. Follow `related_to` and `blocked_by` edges
3. Stop at `MaxDepth` (default: 2)
4. Respect `DepthCeiling` as a hard upper bound
5. Honor `MaxRetrievedNodes` as a soft cap

## Rationale

1. **Recursive CTE is native to SQLite** — no need for application-level BFS/DFS.
2. **Depth limiting prevents** runaway traversal on large graphs.
3. **MaxRetrievedNodes provides** a safety valve for very dense graphs.
4. **Default depth of 2** balances discovery with performance — most relevant entities are within 2 hops.

## Consequences

- Traversal is SQL-native, leveraging SQLite's query optimizer.
- Depth and node count are configurable per query.
- The CTE handles cycle detection automatically.
- Performance degrades gracefully with larger depths.
