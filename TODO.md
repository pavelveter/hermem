# TODO: Dynamic Graph Schema & State Machine

## Overview
Replace hardcoded category/relation allowlists and hardcoded state-machine logic with a fully declarative, configuration-driven harness. Hermem must become a programming-free orchestrator: schema, validation, and FSM transition rules are defined in `hermem.ini` under `[schema]` and interpreted at runtime.

## 1. Config parser: declarative schema section
Files: `src/config.go`, `src/main.go`

- Add a `[schema]` block parser in `src/config.go` (or extend existing config loader).
- Load into maps for O(1) runtime checks:
  - `allowed_categories`
  - `allowed_relations`
  - `stateful_categories`
  - `valid_states`
  - `relation_blocking`
  - `state_unblocking`
  - `relation_recovery`
- Fallbacks when `[schema]` is absent:
  - categories: `world,opinion,experience,observation`
  - relations: existing allowlists
  - state machine features disabled (no `status` enforcement, no execution engine)
- Validate the config at startup (unknown keys = fatal with line number).

## 2. Database: optional status without breaking old nodes
Files: `src/db.go`

- `entities.status` is `TEXT DEFAULT NULL` (backward compatible).
- Non-stateful nodes keep `status = NULL`.
- On insert: if category ∈ `stateful_categories`, set `status = first(valid_states)` (i.e., `pending`).
- Migrations must be idempotent (`duplicate column` is benign).
- Add helpers:
  - `SetStatus(db, id, status) error`
  - `GetStatus(db, id) (string, error)`
  - `GetExecutableNodes(db, goalID string) ([]Entity, error)`
  - `FindRollbackNode(db, failedID string) (string, error)`

## 3. Extractor / validator: config-driven rejections
Files: `src/extractor.go`, `src/server.go`

- Replace hardcoded `allowedCategories` / `allowedRelations` with lookups into the loaded config maps.
- New rules:
  - unknown category → `422 Unprocessable Entity`
  - unknown relation → `422 Unprocessable Entity`
  - state transitions outside `valid_states` for stateful nodes → `422`
- `ingest` path must also call `ValidateCategory` / `ValidateRelation` before emitting edges.

## 4. Topological execution engine
Files: `src/retrieval.go`, `src/server.go`

- `GetExecutableNodes(db, goalID)` must use a **dynamic** recursive CTE:
  - blocking relation name comes from config: `relation_blocking` (default `blocked_by`).
  - unblocking state comes from config: `state_unblocking` (default `completed`).
  - stateful categories limit the search space.
  - prune branches where any blocker is not in unblocking state.
- `FindRollbackNode(db, failedID)` uses `relation_recovery` from config (default `recovers_via`).

## 5. HTTP + CLI wiring
Files: `src/server.go`, `src/main.go`, `src/banner.go`, `USAGE.md`, `README.md`, `skills/hermem/SKILL.md`

- Keep existing routes; add/extend:
  - `POST /task/status` with strict schema validation.
  - `POST /task/executable` with optional `goal_id` query.
  - `POST /task/rollback` with dynamic relation name.
- CLI commands map 1:1 to HTTP endpoints.
- Banner/help text updated.

## 6. Tests and docs
Files: `src/config_test.go`, `src/db_test.go`, `src/retrieval_test.go`, `src/server_test.go`, `USAGE.md`, `README.md`, `skills/hermem/SKILL.md`

- Config tests:
  - missing `[schema]` → classic defaults loaded.
  - malformed value → fatal with actionable message.
  - invalid key → rejected.
- DB tests:
  - NULL status on non-stateful nodes; `pending` on stateful nodes.
  - idempotent migration re-run.
- Retrieval tests:
  - dynamic CTE with custom `relation_blocking` and `state_unblocking`.
  - rollback edge reads `relation_recovery` correctly.
- Server tests:
  - 422 on unknown category / relation.
  - 200 on valid executable query.
- Smoke (manual):
  - custom config with `stateful_categories = task,milestone`; `relation_blocking = causes`; `state_unblocking = done`; verify executable list behaves accordingly.

## Execution order
1. Config: `[schema]` parsing + fallback defaults (#1)
2. DB: status column + stateful auto-init (#2)
3. Extractor: dynamic validation (#3)
4. Retrieval: config-driven CTE + rollback (#4)
5. HTTP/CLI + docs + skills (#5)
6. Tests + smoke (#6)

## Validation
- `gofmt -w src/*.go`
- `go vet ./src/...`
- `go test -count=1 -race -timeout 180s ./src/...`
- Manual smoke with a custom `hermem.ini` that changes relation/state names.

## Out of scope
- Distributed locking across multiple Hermem instances.
- Auto-execution loop (operator/agent code above Hermem).
- Schema migration tooling beyond the single optional `status` column.
