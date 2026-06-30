# ADR-020: Retrieval Default Constants

## Status
Accepted

## Context
The retrieval pipeline uses several default constants that affect search quality and performance. Without documentation, contributors cannot reason about why specific values were chosen.

## Decision
Document the following constants with rationale:

| Constant | Value | Rationale |
|----------|-------|-----------|
| `DefaultSearchTopK` | 5 | Balances recall vs latency for typical knowledge graph queries. Users rarely need more than 5 results for a single search. |
| `DefaultQueryTopK` | 3 | Query results are more focused than search — 3 top results provide enough context without overwhelming the LLM context window. |
| `DefaultRetrieveMaxDepth` | 2 | Two-hop retrieval captures most relevant context (friends-of-friends) without exploding the result set. Depth > 2 yields diminishing returns per RetrievalEval benchmarks. |
| `DefaultProvenanceLimit` | 50 | Caps provenance results to prevent unbounded response sizes. 50 entries cover the typical audit trail for a knowledge fact. |

All values are configurable via `hermem.ini` (retrieval section) or the serverstate runtime config. The defaults are intentionally conservative — production deployments should tune based on their graph density and query patterns.

## Consequences
- Constants are single-source-of-truth in `retrieval/service.go`.
- Tests assert default values to catch accidental changes.
- Config overrides take precedence when set.
