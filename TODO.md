# Technical Task: Architecture Hardening & Domain Decomposition

## Goal

Continue evolving Hermem from a well-structured application into a maintainable long-term platform.

The objective is **not** to add new features, but to reduce architectural risk, improve separation of concerns, and make future development easier.

---

# General Rules

After completing **every individual task** (including sub-tasks):

- [ ] Run the complete test suite.
- [ ] Run all linters and static analysis.
- [ ] Fix every warning or failure before proceeding.
- [ ] Ensure there is no behavior regression.
- [ ] Create a separate Git commit with a meaningful commit message.
- [ ] Mark every sub-task as done with [x].

Do **not** continue to the next task until the current one is green.

---

# Priority P0 (Highest)

## [x] P0.1 Split Store into Domain Repositories

### Problem

The `store` package is steadily becoming a God Repository responsible for every persistence concern.

As the project grows, this will become increasingly difficult to maintain.

### Tasks

- [ ] Identify logical persistence domains.
- [ ] Split persistence code into dedicated repository files (or packages if appropriate).

Suggested structure:

```
store/
    entity_repository.go
    graph_repository.go
    retrieval_repository.go
    provenance_repository.go
    task_repository.go
    migration_repository.go
    retention_repository.go
```

- [ ] Move SQL queries close to their owning repository.
- [ ] Eliminate unrelated responsibilities from each repository.
- [ ] Keep public API backward compatible whenever possible.
- [ ] Preserve transaction behavior.
- [ ] Preserve error semantics.

Acceptance Criteria

- [ ] No repository owns unrelated domains.
- [ ] Files remain reasonably sized.
- [ ] Responsibilities are clearly separated.

---

## [x] P0.2 Introduce Retrieval Pipeline

### Problem

`RetrieveContext()` continues accumulating responsibilities.

It should become an orchestration layer rather than containing retrieval logic.

### Tasks

Create a pipeline architecture.

Suggested interface:

```go
type Stage interface {
    Run(ctx *PipelineContext) error
}
```

Possible stages:

- [ ] Candidate collection
- [ ] Graph expansion
- [ ] Temporal expansion
- [ ] Score calculation
- [ ] Score normalization
- [ ] Reranking
- [ ] Deduplication
- [ ] Rendering
- [ ] Explainability

Pipeline:

```
RetrieveContext
    ↓
Pipeline
    ↓
Stage 1
Stage 2
Stage 3
...
```

Acceptance Criteria

- [ ] RetrieveContext primarily orchestrates stages.
- [ ] Stages have single responsibilities.
- [ ] Stages are independently testable.
- [ ] Existing behavior remains unchanged.

---

# Priority P1

## [x] P1.1 Introduce Application Container

### Problem

Dependency wiring is becoming increasingly complex.

The server currently depends on many individual services.

### Tasks

Introduce a root application container.

Example:

```go
type Application struct {
    Retrieval ...
    Memory ...
    Graph ...
    Tasks ...
    ...
}
```

Responsibilities:

- [ ] Construct dependency graph.
- [ ] Own application lifecycle.
- [ ] Initialize services.
- [ ] Hide wiring complexity.

Acceptance Criteria

- [ ] Server receives one root dependency instead of many.
- [ ] Dependency construction is centralized.
- [ ] Initialization remains deterministic.

---

## [x] P1.2 Move Bootstrap Logic

### Tasks

Create a dedicated bootstrap layer.

Suggested package:

```
internal/app
```

or

```
internal/bootstrap
```

Move into it:

- [ ] Dependency construction
- [ ] Configuration loading
- [ ] Database initialization
- [ ] Service creation
- [ ] Router construction
- [ ] Background workers

Acceptance Criteria

- [ ] main.go becomes minimal.
- [ ] Startup logic is isolated.

---

## [x] P1.3 Simplify Renderers

### Problem

Renderers duplicate traversal logic.

### Tasks

Extract common rendering infrastructure.

Instead of multiple implementations repeating iteration logic:

```
Markdown
Plain Text
JSON
```

share common traversal and formatting components.

Acceptance Criteria

- [ ] No duplicated traversal loops.
- [ ] Rendering formats differ only by formatting behavior.
- [ ] JSON rendering uses encoding/json whenever practical.

---

# Priority P2

## [x] P2.1 Strengthen Domain Model

### Problem

The project still relies heavily on the generic Entity abstraction.

The domain model should become more explicit.

### Tasks

Gradually move toward typed domain objects.

Examples:

- [ ] Fact
- [ ] Episode
- [ ] Evidence
- [ ] Belief
- [ ] Observation
- [ ] Task

Entity should increasingly become a persistence representation rather than the primary domain abstraction.

Acceptance Criteria

- [ ] Domain services operate on domain types whenever practical.
- [ ] Entity is no longer the default business object.

---

## [x] P2.2 Add Property-Based Tests

Introduce property-based testing for graph algorithms and retrieval.

Potential invariants:

### Graph

- [ ] Every node belongs to exactly one connected component.
- [ ] Traversal never produces duplicate nodes.
- [ ] Community detection never loses nodes.

### Retrieval

- [ ] Returned IDs are unique.
- [ ] Scores remain finite.
- [ ] Sorting order is deterministic.
- [ ] Empty database never panics.
- [ ] Random graph generation never crashes retrieval.

Acceptance Criteria

- [ ] Property tests pass consistently.
- [ ] Randomized inputs expose no panics.

---

## [x] P2.3 Remove Known-Bug Test Exceptions

### Tasks

Find tests marked as:

- skipped
- known bug
- TODO
- temporary workaround

For each case:

- [ ] Determine root cause.
- [ ] Fix implementation if practical.
- [ ] Otherwise create a documented tracking issue.
- [ ] Remove obsolete skips.

Acceptance Criteria

- [ ] No unexplained skipped tests remain.

---

# Priority P3

## [x] P3.1 Expand ADR Documentation

Create Architecture Decision Records for major architectural choices.

Suggested topics:

- [ ] Retrieval pipeline
- [ ] Repository decomposition
- [ ] Domain model
- [ ] Graph architecture
- [ ] Ranking strategy
- [ ] Memory lifecycle
- [ ] Background workers
- [ ] Dependency injection

Each ADR should explain:

- [ ] Context
- [ ] Problem
- [ ] Decision
- [ ] Alternatives considered
- [ ] Consequences

---

## [x] P3.2 Review Package Boundaries

Perform a full architectural review.

For every package ask:

- [ ] Does it own exactly one responsibility?
- [ ] Does it expose the smallest possible public API?
- [ ] Does it depend only on lower architectural layers?
- [ ] Can responsibilities be split further?

Acceptance Criteria

- [ ] Reduced coupling.
- [ ] Improved cohesion.
- [ ] Cleaner dependency graph.

---

## [x] P3.3 Reduce Architectural Debt

Perform a final cleanup pass.

Look for:

- [ ] God objects
- [ ] Large files
- [ ] Large functions
- [ ] Circular dependencies
- [ ] Duplicate code
- [ ] Manual object wiring
- [ ] Hidden coupling
- [ ] Excessive package exports

Acceptance Criteria

- [ ] Overall architecture becomes simpler.
- [ ] No unnecessary complexity is introduced.
- [ ] Public APIs remain stable.

---

# Final Validation

Before considering this work complete:

- [ ] All tests pass.
- [ ] All linters pass.
- [ ] Static analysis passes.
- [ ] Benchmarks show no significant regressions.
- [ ] Public API remains backward compatible.
- [ ] No new architectural warnings are introduced.
- [ ] Every completed task has its own Git commit.
- [ ] Documentation is updated where necessary.