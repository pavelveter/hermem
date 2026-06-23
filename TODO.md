# TODO: State-on-Graph — Graph-Based Task Execution State

## Batch 9 — State Machine on Hermem Graph

Theme: turn the graph memory into a crash-resilient task execution state machine.

### Context

Hermem already stores entities + edges with category/relation allowlists. We extend the
existing `extra_categories` / `extra_relation_types` config path (Batch 7 commit `a814929`)
and add a `status` text field on `entities` so steps can be tracked without new tables.

### #19 State-on-Graph core schema + API — HIGH (lead item)

Files: `src/db.go`, `src/config.go`, `src/main.go`, `src/server.go`, tests.

Approach:
1. Add `status` column to `entities` via schema migration in `InitDB`:
   `ALTER TABLE entities ADD COLUMN status TEXT DEFAULT 'pending'`.
   Guard the migration so it is idempotent (catch `duplicate column` error).
2. Add `UpdateTaskStatus(db, id, status)` helper in `src/db.go` (only updates rows where
   `category = 'task'` to avoid touching memory/opinion nodes).
3. Confgure `AllowedCategories` and `AllowedRelations` maps in `src/extractor.go` to
   include `"task"`, `"blocked_by"`, `"recovers_via"`. These should be driven by the same
   config keys already used for user-defined allowlists so the change is additive, not
   a hard fork.
4. Expose HTTP handlers:
   - `POST /task/status` `{ "id": "...", "status": "pending|running|completed|failed" }`
     -> 204 on success, 400 if entity not found or wrong category.
   - `POST /task/executable?goal_id=...` -> JSON list of executable tasks whose
     `blocked_by` dependencies are all `completed` and whose own status is `pending`.
5. Wire the new routes and any needed flags into `cmd` layer; keep `--no-decor` and
   existing auth / logging semantics.

### #20 Recursive CTE task walk — HIGH

Files: `src/retrieval.go`, `src/retrieval_test.go`.

Approach:
1. `GetExecutableTasks(db, goalID)` — recursive CTE starting from the supplied goal id
   (or from all pending task nodes when goalID is empty), walking `blocked_by` edges in
   reverse and pruning any branch that contains a dependency with `status != 'completed'`.
   Return only leaf-ready tasks: `pending` tasks with zero remaining blockers.
2. `FindRollbackTask(db, failedTaskID)` — look up `recovers_via` edge; return the target
   id or empty if no recovery arc is wired.
3. Tests:
   - Fixture: seeds tasks A ->blocks B ->blocks C, mark A completed => B executable, C not.
   - Fixture: mark B completed => C executable.
   - Fixture: `recovers_via` edge exists => returns target; no edge => empty.
   - All new tests must run under `go test -race`.

### #21 Embedding + API polish — MEDIUM

Files: `src/retrieval.go`, `src/server.go`, `src/embedder.go` if needed.

Approach:
1. Embed task nodes on creation same as any other category; no special-case embedding code.
2. Add `task` to any prompt-template or category-doc examples in docs so operators know it
   can be treated like first-class memory.
3. `POST /store` already supports `AutoLinkEdges`; ensure the new relations are surfaced
   in the API docs (`USAGE.md` or `README.md` add a small State Machine section).

### Execution order

1. Schema + config allowlist expansion (#19)
2. CTE walk + rollback lookup (#20)
3. Embedding / API / docs polish (#21)

### Validation

- `gofmt -w src/*.go`
- `go vet ./src/...`
- `go test -count=1 -race -timeout 180s ./src/...`
- Manual smoke:
  - `./hermem --no-decor` then
  - `POST /store` with `category=task`, then
  - `POST /task/status id=... status=running`, then
  - `POST /task/executable` -> expect JSON
- `go test -bench=BenchmarkRetrieveContextStar -benchtime=10x -run='^$' ./src/...`
  (regression guard for retrieval benches)

### Out of scope

- Locking / distributed coordination across multiple hermem instances — keep single-process
  SQLite semantics for now.
- Auto-retry loop — this batch only adds state + executable queries; execution loop is
  operator / agent code above hermem.
