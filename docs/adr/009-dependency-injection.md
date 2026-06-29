# ADR-009: Dependency Injection via Centralized Wiring

## Status

Accepted

## Context

As the number of services grew (12 HTTP shells + domain services + infrastructure), dependency construction was scattered across `main.go` and CLI commands, making it hard to reason about the full dependency graph.

## Decision

Centralize all dependency construction in `cli/wiring.go` via a single `wireAll()` function:

```go
func wireAll(env *clienv.Env, refs *serverstate.Ref) *server.Server
```

`wireAll()` constructs all domain services and HTTP shells, then passes them to `server.NewServerFromDeps(ServerDeps{...})`.

`main.go` remains minimal: config load → env setup → CLI execute.

The `ServerDeps` struct acts as the application container, making the full dependency graph explicit in one type.

## Alternatives Considered

1. **Wire by interface (fx/dig)** — rejected: adds dependency on DI framework; overengineered for the current scale.
2. **Lazy initialization** — rejected: makes dependency graph implicit; harder to test.

## Consequences

- Adding a new service requires changes in only `wireAll()` + `ServerDeps`.
- `main.go` stays under 140 lines.
- Dependency construction is deterministic and testable.
- No runtime surprises — all dependencies are wired at startup.
