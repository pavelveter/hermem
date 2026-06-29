# ADR-007: Repository Decomposition

## Status

Accepted

## Context

The `store` package provides all persistence operations for the application. As the domain grew, there was a risk of it becoming a "God Repository" mixing unrelated concerns.

## Decision

Organize the `store` package into domain-specific files:

| File | Domain |
|------|--------|
| `entity.go` | Entity CRUD, status, helpers |
| `edge.go` | Edge CRUD, purge, query |
| `graph.go` | Contradictions, provenance, connected components |
| `graph_verify.go` | Orphan edges, dimension mismatches |
| `task.go` | Task CRUD, tree, blocking relationships |
| `task_executable.go` | Executable task queries, claim |
| `migration.go` | Schema migrations, checksums, drift detection |
| `recovery.go` | Recovery plans, cascade rollback |
| `community.go` | Louvain community detection |
| `schema.go` | Schema fingerprinting |
| `codec.go` | Embedding encoding/decoding |
| `locker.go` | Entity-level distributed locking |
| `init.go` | Database initialization, pragmas, schema validation |
| `query_builder.go` | SQL query builder |

All functions remain package-level (accepting `*sql.DB`) for backward compatibility. No repository structs are introduced — the package-level pattern is idiomatic for SQLite's single-writer model.

## Alternatives Considered

1. **Repository structs with methods** — rejected: adds ceremony without benefit for single-writer SQLite; breaks existing callers.
2. **Separate packages per domain** — rejected: would create import cycles and force interface abstractions prematurely.

## Consequences

- Each file owns exactly one persistence domain.
- Files remain reasonably sized (23–560 lines).
- New persistence concerns get their own file.
- No breaking changes to existing callers.
