# TODO: E2E Test Suite + Public Developer Interfaces

---

## P0 — E2E Test Suite (CLI + HTTP API)

### Goal

Implement a comprehensive end-to-end (E2E) test suite covering every public user-facing interface of Hermem.

### Requirements

- Test every CLI command.
- Test every HTTP endpoint.
- Verify successful and failure scenarios.
- Verify JSON schemas where applicable.
- Verify exit codes.
- Verify HTTP status codes.
- Verify persistence across process restarts.
- Verify compatibility between CLI and HTTP (same database).

### General Principles

- Tests must use only public interfaces.
- No calls into internal Go packages.
- Every test starts from a clean temporary directory.
- Every test creates its own `hermem.ini` and SQLite database.
- Tests must be deterministic.
- Tests must be runnable in CI.
- Tests must support Linux and macOS.

### Test Layout

```
tests/
    e2e/
        cli/
        http/
        fixtures/
        helpers/
        snapshots/
```

### Helpers

Helpers should provide:

- temporary workspace creation
- temporary config generation
- server startup/shutdown
- free-port allocation
- HTTP client
- CLI wrapper
- JSON comparison
- polling utilities
- snapshot helpers

### CLI Coverage

#### Top level

- `serve`
- `health`
- `metrics`
- `version`
- `diagnose`
- `bench`

#### Admin

- `admin keys list`
- `admin keys add`
- `admin keys rotate`
- `admin keys revoke`

#### Operations

- `ops stats`
- `ops integrity`
- `ops vacuum`
- `ops rebuild-index`

#### Profiling

- `profile cpu`
- `profile heap`
- `profile goroutine`
- `profile trace`

#### Memory

- `memory store`
- `memory search`
- `memory retrieve`
- `memory query`
- `memory response`
- `memory ingest`
- `memory edge`
- `memory explain`
- `memory re-embed`
- `memory quantize`

#### Task

- `task create`
- `task status`
- `task list`
- `task show`
- `task dep`
- `task tree`
- `task rollback`
- `task executable`
- `task next`

#### Graph

- `graph plan`
- `graph recovery-plan`
- `graph components`
- `graph communities`
- `graph verify`
- `graph contradictions`
- `graph provenance`

#### Temporal

- `time temporal`
- `time timeline`

#### Agent

- `agent loop`

#### Database

- `db migrate`
- `db dry-run`
- `db rollback`
- `db verify`
- `db schema`

### HTTP Coverage

#### GET

- `/health`
- `/health/live`
- `/health/ready`
- `/metrics`
- `/timeline`
- `/provenance`
- `/contradictions`
- `/connected-components`
- `/communities`
- `/recovery-plan`
- `/graph/verify`

#### POST

- `/store`
- `/search`
- `/retrieve`
- `/query`
- `/query/explain`
- `/query/temporal`
- `/response`
- `/ingest`
- `/edge`
- `/task/create`
- `/task/status`
- `/task/list`
- `/task/show`
- `/task/dep`
- `/task/tree`
- `/task/rollback`
- `/task/executable`
- `/task/next`
- `/admin/re-embed`

### Positive Scenarios

- valid requests
- persistence
- graph traversal
- vector search
- ingestion
- deduplication
- contradiction detection
- temporal filtering
- provenance
- state transitions
- dependency resolution
- recovery plans
- graph analytics
- explain mode
- authentication enabled
- authentication disabled

### Negative Scenarios

- malformed JSON
- missing required fields
- unknown fields
- invalid category
- invalid relation
- invalid task state
- invalid transition
- missing entity
- invalid IDs
- invalid API key
- duplicate edges
- invalid timestamps
- invalid configuration
- database locked
- corrupted database
- unsupported vector dimension

### Cross-Interface Scenarios

- store via CLI → query via HTTP
- store via HTTP → query via CLI
- ingest via HTTP → retrieve via CLI
- edge via CLI → retrieve via HTTP
- restart server → data still exists
- concurrent CLI + HTTP access

### Persistence Scenarios

- restart process
- WAL recovery
- migration on existing database
- empty database
- populated database

### Authentication

- API key required
- API key missing
- API key invalid
- API key valid

### Performance Sanity

- repeated queries
- repeated inserts
- repeated graph walks
- repeated searches

### Assertions

- exit code
- HTTP status
- JSON schema
- expected fields
- database contents
- graph integrity
- idempotency where expected
- deterministic output where applicable

### CI

The complete E2E suite must run automatically in GitHub Actions and fail the build on any regression.

### Success Criteria

Every documented CLI command and every documented HTTP endpoint is exercised by at least one positive and one negative end-to-end test, providing confidence that the published interface remains stable across future releases.

---

## P0 — Scenario Runner (YAML-driven)

### Goal

YAML-driven scenario runner that executes the same test scenario through both CLI and HTTP interfaces, comparing expected results after each step.

### Testdata Layout

```
testdata/
    scenarios/
        basic_memory.yaml
        contradictions.yaml
        task_planner.yaml
        provenance.yaml
        retrieval.yaml
        timeline.yaml
        communities.yaml
```

### Scenario Format

Each YAML scenario defines a sequence of steps. Each step specifies an action (CLI or HTTP) with input and expected output. The runner executes every step through both interfaces and asserts correctness.

### Runner

- reads a scenario file
- runs each step via CLI
- runs each step via HTTP
- compares actual vs expected output after each step
- reports pass/fail per step with diff on failure

### Scenario Coverage

| Scenario | Purpose |
|----------|---------|
| `basic_memory.yaml` | store, search, query, edge, deduplication |
| `contradictions.yaml` | ingest contradicting facts, verify contradicts edges |
| `task_planner.yaml` | task lifecycle: create → status → dependencies → executable → rollback |
| `provenance.yaml` | store with provenance, query by conversation_id/message_id/source |
| `retrieval.yaml` | graph traversal, depth limits, score breakdown, explain mode |
| `timeline.yaml` | timeline ordering, temporal filtering, created_at DESC |
| `communities.yaml` | connected components, Louvain community detection, modularity |

---

## P0 — README & Documentation

- [x] Update `docs/USAGE.md` with E2E test section.
- [x] Add `make test-e2e` target.
- [x] Add CI job for E2E suite in GitHub Actions.

---

## Phase 1 — OpenAPI 3.1 Spec (DONE)

- [x] Create `api/openapi.go` — OpenAPI 3.1 spec as Go struct, schemas from `core/types.go`
- [x] Create `api/handler.go` — JSON/YAML spec endpoints
- [x] Create `api/openapi_test.go` — 7 tests (spec generation, path coverage, schema coverage, operationId/tags)
- [x] Register `GET /openapi.json` and `GET /openapi.yaml` in `server/server.go`

---

## Phase 2 — Go SDK (DONE)

- [x] Create `sdk/go/client.go` — `Client` with sub-clients, retry, context, API key
- [x] Create `sdk/go/types.go` — Entity, Edge, SearchResult, APIError, request/response types
- [x] Create `sdk/go/memory.go` — Store, Search, Retrieve, Query, Ingest, Edge, Explain, ReEmbed
- [x] Create `sdk/go/task.go` — Create, Status, List, Show, Dep, Tree, Rollback, Executable, Next
- [x] Create `sdk/go/graph.go` — Components, Communities, Verify, Contradictions, Provenance, Timeline, RecoveryPlan
- [x] Create `sdk/go/admin.go` — Health, Ready, MigrateStatus, Schema, VerifyDB
- [x] Separate Go module: `github.com/pavelveter/hermem/sdk/go`
- [x] 5 unit tests in `sdk/go/client_test.go`

---

## Phase 3 — Python SDK (DONE)

- [x] Create `sdk/python/hermem/types.py` — dataclass types matching Go SDK
- [x] Create `sdk/python/hermem/client.py` — Client, MemoryClient, TaskClient, GraphClient, AdminClient
- [x] Zero external dependencies (stdlib `urllib.request`)
- [x] Create `sdk/python/pyproject.toml` — PyPI package config
- [x] 9 unit tests in `sdk/python/tests/test_client.py`

---

## Phase 4 — TypeScript SDK (DONE)

- [x] Create `sdk/typescript/src/types.ts` — type definitions
- [x] Create `sdk/typescript/src/client.ts` — fetch-based client with APIError
- [x] Create `sdk/typescript/src/index.ts` — entry point
- [x] Create `sdk/typescript/package.json`, `tsconfig.json`, `vitest.config.ts`
- [x] 5 vitest tests in `sdk/typescript/test/client.test.ts`

---

## Phase 5 — MCP Server (DONE)

- [x] Create `src/internal/mcp/server.go` — MCP server using official Go SDK
- [x] Create `src/internal/mcp/tools.go` — 9 tools: memory_search, memory_store, memory_retrieve, task_create, task_list, task_status, task_show, graph_components, ingest_dialog
- [x] CLI command: `hermem mcp` (stdio transport for Claude Desktop / Claude Code)

---

## Phase 6 — Examples & CI

- [ ] `sdk/go/examples/main.go` — Go SDK usage example
- [ ] `sdk/python/examples/main.py` — Python SDK usage example
- [ ] `sdk/typescript/examples/main.ts` — TypeScript SDK usage example
- [ ] CI: GitHub Actions for SDK unit tests
