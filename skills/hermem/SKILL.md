---
name: hermem
description: Lightweight graph memory for Hermes — store facts, search by vector similarity, retrieve connected context
version: 0.3.0
metadata:
  hermes:
    tags: [memory, graph, vector-search, sqlite, task-state]
    category: memory
    config:
      - key: hermem.url
        description: Hermem server URL
        default: "http://localhost:8420"
        prompt: "Hermem server URL (e.g. http://localhost:8420)"
required_environment_variables: []
---

# Hermem — Graph Memory Skill

Lightweight graph memory via the Hermem CLI binary. No server is required for normal use.

## CLI surface (Cobra grouped grammar)

> **Breaking change (commit `8f0bf71`)** — the previously-flat 26-name
> surface is gone. There are **no back-compat aliases**; commands
> missing here should be invoked using the grouped grammar below.

```bash
# Top-level
hermem serve [--port 8420]               HTTP server (SIGHUP reloads hermem.ini)
hermem health                            DB ping (mirrors /health/ready, exit 1 on fail)
hermem metrics                           Prometheus exposition
hermem version                           ldflags build metadata

# `hermem memory …` — knowledge CRUD + retrieval
hermem memory store         < req.json   Upsert entity
hermem memory search        < req.json   Top-K cosine neighbours
hermem memory retrieve      < req.json   Graph walk from explicit seed_ids
hermem memory query         < req.json   embed → search → walk → markdown
hermem memory response      < req.json   Full pipeline + LLM response
hermem memory edge          < req.json   Add typed edge (opt body.auto_create)
hermem memory ingest        < req.json   LLM-extract + dedup-merge
hermem memory explain       < req.json   Retrieval with score breakdown
hermem memory re-embed      [--batch-size N] [--model M]   Batch re-embed all
hermem memory quantize      < req.json   Scalar int8 roundtrip + stats

# `hermem task …` — task lifecycle (`next` aliases `executable`)
hermem task status          < req.json   Update task status
hermem task list            < req.json   Filter by status / goal_id
hermem task show            < req.json   task + blocked_by + recovers_via
hermem task dep             < req.json   Add/remove a dependency edge
hermem task tree            < req.json   ASCII tree under goal_id
hermem task create          < req.json   Auto-embed + stateful category
hermem task rollback        < req.json   Find recovers_via companion
hermem task next / executable [{}]       Executable tasks

# `hermem graph …` — graph analytics
hermem graph plan           < req.json   Topo-sorted plan
hermem graph recovery-plan  < req.json   recovers_via chain
hermem graph components                   Connected components
hermem graph communities                  Louvain + global modularity
hermem graph verify                       Integrity check (exit 1)
hermem graph contradictions [entity-id]   Optional positional ID filter
hermem graph provenance [--conversation …] [--message …] [--source …] [--limit N]

# `hermem time …`
hermem time temporal        < req.json   Time-windowed retrieval
hermem time timeline                     Recent 50 entities

# `hermem agent …`
hermem agent loop           < req.json   algo.AgentLoop on a goal_id

# `hermem db …`
hermem db migrate                       Migration status
hermem db rollback                      Roll back most-recent (--target=N for any range)
hermem db dry-run                       List pending migrations without applying
hermem db verify                        Checksum integrity check (per-migration SHA-256)
hermem db schema                        Stored vs current schema fingerprint

# `hermem admin …` — multi-key scoped API-key management
hermem admin keys list                  List API keys (masked) + their scopes/labels
hermem admin keys add [--scope S]       Generate a new key (32-byte CSPRNG → 64 hex)
hermem admin keys rotate <id>           Issue a new value, retain label/scope
hermem admin keys revoke <id>           Remove a key from hermem.ini
hermem admin keys show <header>         Decode in-flight X-API-Key → label/scope

# `hermem adminops …` (registered as `ops`) — offline database diagnostics
hermem adminops stats                   Node/edge counts, embedding coverage, last GC
hermem adminops integrity [--fix-hint]  Exit 1 on integrity issues, list critical/warning
hermem adminops vacuum                  VACUUM with progress + bytes-reclaimed report
hermem adminops rebuild-index           [--category C] [--since D] [--only-archived] [--dry-run]

# `hermem profile …` — opt-in runtime profiling (default: disabled)
hermem profile cpu     [N]              CPU profile (default 10s) → protobuf via stdout
hermem profile heap                     Heap snapshot → /tmp/hermem-heap.pprof
hermem profile goroutine                Goroutine dump (text) → stdout
hermem profile trace   [N]              Execution trace (default 10s) → /tmp/hermem-trace.out
# In server mode, set HERMEM_PPROF_ENABLED=1 to mount /debug/pprof/* (off by default).
```

`hermem <group> --help` prints only that group's commands. Every command
that reads structured input consumes JSON on stdin (or empty `{}` for
`task next` / `task executable`).

## When to Use

- User asks to remember something for future sessions
- User asks "what do you know about X?"
- You need to recall past conversations or facts
- You want to store structured knowledge (facts, opinions, experiences, observations, tasks)

## Default Mode: CLI

Use the installed binary:

```bash
~/.hermes/bin/hermem ...
```

### Binary-directory resolution

Hermem reads `hermem.ini` from the binary's **own directory** via `os.Executable()`, not from the current working directory. A `~/.hermes/bin/hermem` binary will pick up `~/.hermes/hermem.ini` regardless of where it was launched from — `cd ~/.hermes && ~/.hermes/bin/hermem …` is not required. Older SKILL revisions warned about stray `hermem.db` files from a transient CWD; that bug is fixed in the current binary.

## Memory Categories

Categories are **config-driven** via `[schema]` in `hermem.ini`. Classic defaults:

| Category | What to store |
|----------|---------------|
| `world` | Facts, definitions, objective knowledge |
| `opinion` | User preferences, beliefs, subjective views |
| `experience` | Past events, interactions, what happened |
| `observation` | Patterns noticed, anomalies, derived insights |
| `task` | Actionable work items, steps, to-dos with status tracking |

## Procedure

Always verify retrieval before considering a memory write complete:
1. store or ingest
2. re-run the same query with natural subject language
3. treat empty `{"context":""}` as “not yet retrievable,” not as success
4. Task status lifecycle: completed/failed/etc should be reflected via `~/.hermes/bin/hermem task status` against task entities (`category=task`). Verify status changes with `task list`.

### 1. Store a fact

Use `hermem_store` tool. Under the hood this shells out to
`hermem memory store`:

```
hermem_store(content="User prefers dark mode in all editors", category="opinion")
hermem_store(content="Project uses Go 1.22 with Chi router", category="world")
hermem_store(content="Deployed v2.1 to production on 2026-01-15", category="experience")
```

The HTTP server (`POST /store`) and the shell CLI (`echo '…' |
~/.hermes/bin/hermem memory store`) accept the same JSON payload and
serve identical results.

### 2. Search memory

Use `hermem_search` for vector similarity search:

```
hermem_search(query="Что пользователь предпочитает из текстовых редакторов?")
hermem_search(query="deployment history", limit=5)
```

Prefer natural subject language matching the user's language for subject-specific queries.

### 3. Full context retrieval

Use `hermem_query` for the complete pipeline (search + graph walk + markdown):

```
hermem_query(query="Tell me about the user's preferences")
```

This returns markdown-formatted context grouped by category (WORLD, OPINION, EXPERIENCE, OBSERVATION) — ready to inject into your response.

### 4. Ingest conversations

After each conversation turn, the `sync_turn` function automatically sends dialog to Hermem for entity extraction. No manual action needed if the memory provider plugin is active.

## State Machine on Graph (Declarative Schema)

Hermem supports config-driven graph-based execution state.
Categories, relation types, valid states, and FSM relation names
are defined in `[schema]` in `hermem.ini`. The classic defaults
map to `task` with `blocked_by`/`recovers_via`/`pending|running|completed|failed`.

### Default relation types

- `blocked_by` — B is blocked until A completes
- `recovers_via` — B offers a recovery/rollback path when A fails

### Schema

`entities.status` is `TEXT DEFAULT NULL` (backward compatible).
Stateful categories auto-init to the first `valid_states` value.
Status is validated against `valid_states` from config.

### Task API

CLI (Cobra grouped grammar — `hermem task <sub>` instead of the
previous flat `hermem task-<sub>`):

```bash
# update status
echo '{"id":"step-1","status":"running"}' | ~/.hermes/bin/hermem task status

# list executable (global)
echo '{}' | ~/.hermes/bin/hermem task next

# alias (same handler dispatched under a friendlier name)
echo '{"goal_id":"goal-1"}' | ~/.hermes/bin/hermem task executable

# filter by status / goal
echo '{"status":"pending"}' | ~/.hermes/bin/hermem task list
echo '{"goal_id":"goal-1"}'  | ~/.hermes/bin/hermem task list

# show task + relations
echo '{"id":"step-1"}' | ~/.hermes/bin/hermem task show

# manage dependency
echo '{"source_id":"step-1","target_id":"step-0","add":true}' \
  | ~/.hermes/bin/hermem task dep

# create task with auto-linked context
echo '{"content":"New task","context_ids":["step-0"]}' \
  | ~/.hermes/bin/hermem task create

# find rollback task
echo '{"id":"step-1"}' | ~/.hermes/bin/hermem task rollback

# ASCII tree under a goal
echo '{"goal_id":"goal-1"}' | ~/.hermes/bin/hermem task tree
```

HTTP:

```bash
# status update → 204
curl -X POST http://localhost:8420/task/status \
  -H 'Content-Type: application/json' \
  -d '{"id":"step-1","status":"running"}'

# executable tasks (global)
curl -X POST http://localhost:8420/task/executable \
  -H 'Content-Type: application/json' \
  -d '{}'

# executable tasks scoped to goal
curl -X POST "http://localhost:8420/task/executable?goal_id=goal-1" \
  -H 'Content-Type: application/json' \
  -d '{}'

# next alias
curl -X POST http://localhost:8420/task/next \
  -H 'Content-Type: application/json' \
  -d '{}'

# list by status
curl -X POST http://localhost:8420/task/list \
  -H 'Content-Type: application/json' \
  -d '{"status":"pending"}'

# show task + relations
curl -X POST http://localhost:8420/task/show \
  -H 'Content-Type: application/json' \
  -d '{"id":"step-1"}'

# add dependency
curl -X POST http://localhost:8420/task/dep \
  -H 'Content-Type: application/json' \
  -d '{"source_id":"step-1","target_id":"step-0","add":true}'

# create task
curl -X POST http://localhost:8420/task/create \
  -H 'Content-Type: application/json' \
  -d '{"content":"New task","context_ids":["step-0"]}'

# rollback lookup
curl -X POST http://localhost:8420/task/rollback \
  -H 'Content-Type: application/json' \
  -d '{"id":"step-1"}'
```

### DB helpers (from `src` package)

- `UpdateTaskStatus(db, id, status) error`
- `GetTaskStatus(db, id) (string, error)`
- `GetExecutableTasks(db, goalID string) ([]Entity, error)`
- `FindRollbackTask(db, failedTaskID string) (string, error)`
- `ListTasks(db, status, goalID string) ([]Entity, error)`
- `GetTaskWithRelations(db, taskID string) (Entity, []Edge, []Edge, error)`
- `AddEdge(db, sourceID, targetID, relationType string) error`
- `DeleteEdge(db, sourceID, targetID, relationType string) error`

### Pitfalls

- A task with no `blocked_by` dependencies is executable immediately; use `goal_id` to scope to a subtree.
- `GetExecutableTasks` excludes `archived=1` entities automatically.
- Very deep DAGs (>500 recursion depth) may hit SQLite recursion limits; task planning stays well below this today.
- `POST /task/executable` accepts an empty body; omit/empty JSON object means “return global executable set.”

## API Reference

If you need to call Hermem directly via HTTP:

### Health check
```bash
curl http://localhost:8420/health
```

### Store entity
```bash
curl -X POST http://localhost:8420/store \
  -H "Content-Type: application/json" \
  -d '{"id":"my-fact","category":"world","content":"Paris is the capital of France"}'
```

### Vector search
```bash
curl -X POST http://localhost:8420/search \
  -H "Content-Type: application/json" \
  -d '{"query":"capital of France","top_k":5}'
```

### Full query (search + graph walk)
```bash
curl -X POST http://localhost:8420/query \
  -H "Content-Type: application/json" \
  -d '{"query":"Tell me about France"}'
```

### Ingest dialog
```bash
curl -X POST http://localhost:8420/ingest \
  -H "Content-Type: application/json" \
  -d '{"dialog":"User: What is Go?\nAssistant: Go is a statically typed language."}'
```

## Pitfalls

- Hermem server must be running before using the tools
- Default server URL is `http://localhost:8420`; the plugin reads this from its own `hermem.url` config (see YAML frontmatter), not from a shell env var
- Entities with similar content (>88% cosine similarity) are merged automatically
- Graph walk depth defaults to 2 hops — increase via `max_depth` parameter
- `hermem memory store` auto-links the new entity to up to 3 existing entities with cosine similarity > 0.85 using relation type `related_to`. Explicit graph edges still use `hermem memory edge` with `source_id`, `target_id`, and `relation_type`. `hermem memory ingest` remains the path that extracts entities and relations from dialog text.
- `hermem memory store` and `hermem memory ingest` are NOT interchangeable. `ingest` is the only path that produces both entities and graph edges from dialog text, because it runs the LLM extractor to discover relationships. Do not run `store` and then manually `INSERT INTO edges` in SQLite — that skips validation, entity resolution, and the dedup layer, and it creates dirty state the CLI does not know about.
- If `hermem memory ingest` fails because the LLM extractor times out, raise `[extraction] timeout` in `hermem.ini` (Go duration, default `5m` / `300s`), then re-run `ingest`. Never cure Ollama latency by hand-inserting into `edges`.
- CLI `hermem memory store` does not guarantee an embedding was generated. Always verify retrieval with `hermem memory query` / `hermem memory search` after storing facts.
- If `hermem memory query` returns `{"context":""}`, inspect the active binary path, database path, and query wording before retrying.
- **CLI grammar is grouped:** `hermem task status` (not `hermem task-status`), `hermem memory ingest` (not `hermem ingest`), `hermem db rollback` (not `hermem migration-rollback`), `hermem graph provenance` (not `hermem provenance`). The flat names are no longer registered; cobra's `--help` is authoritative.

## Configuration

This skill assumes the installed Hermem binary and plugin live at predictable Hermes paths; matching the README/manual-install instructions.

- plugin: `~/.hermes/hermes-agent/plugins/memory/hermem`
- binary: `~/.hermes/bin/hermem`
- config: `~/.hermes/hermem.ini`

If the binary is missing, build from hermem repo and copy into `~/.hermes/bin`, then:
```bash
hermes gateway restart
hermes memory
```

### hermem.ini

The Go binary resolves `hermem.ini` from its own directory via `os.Executable()` (see Binary-directory resolution above), so the path is independent of session cwd. The example below uses an explicit absolute database path so the operator is never surprised by a Linux-vs-macOS homedir mismatch:

```ini
[embedder]
provider = ollama
url = http://localhost:11434
model = nomic-embed-text

[database]
path = /Users/pavelveter/.hermes/hermem.db

[schema]
; Declarative graph schema + FSM harness.
; If absent, classic defaults are used and stateful features are disabled.
; Comma-separated entity category allowlist; unknown categories return HTTP 422.
allowed_categories = world,opinion,experience,observation,task,milestone
; Comma-separated relation type allowlist; unknown relations return HTTP 422.
allowed_relations = prefers,uses,mentions,related_to,part_of,causes,contradicts,blocked_by,recovers_via
; Categories whose nodes get a lifecycle `status` field (auto-init to first valid state).
stateful_categories = task
; Ordered valid lifecycle states for stateful nodes; invalid transitions return HTTP 422.
valid_states = pending,running,completed,failed
; Relation name used for blocking dependencies.
relation_blocking = blocked_by
; Target status that unblocks a dependency.
state_unblocking = completed
; Relation name for recovery/rollback edges.
relation_recovery = recovers_via
```

After editing config, restart the Hermes gateway if the memory provider needs to pick up changes.

## Verification

Always treat empty `{"context":""}` from `query`/`search` as “not yet retrievable.” Inspect the active directory, DB path, and query output when something returns empty.

Check Hermem is working:

```bash
# 1. Health check via memory status or direct probe
hermes memory status
curl http://localhost:8420/health

# 2. Store and retrieve in natural-language style
hermem_store(content="Пользователь любит neovim", category="opinion")
hermem_search(query="Какой текстовый редактор я люблю?")
hermem_query(query="Какой текстовый редактор я люблю?")
# Should return markdown with the stored fact
```

## Smoke Test

```bash
cd /Users/pavelveter/Projects/labs/hermem
gofmt -w src/
go vet ./src/...
go test -count=1 -race -timeout 180s ./src/...
```

Manual endpoint smoke:

```bash
~/.hermes/bin/hermem serve --port 8420 &
curl -X POST http://localhost:8420/store \
  -H 'Content-Type: application/json' \
  -d '{"id":"goal-1","category":"task","content":"Ship release"}'
curl -X POST http://localhost:8420/store \
  -H 'Content-Type: application/json' \
  -d '{"id":"step-1","category":"task","content":"Run tests"}'
curl -X POST http://localhost:8420/edge \
  -H 'Content-Type: application/json' \
  -d '{"source_id":"step-1","target_id":"goal-1","relation_type":"blocked_by"}'
curl -X POST http://localhost:8420/task/status \
  -H 'Content-Type: application/json' \
  -d '{"id":"goal-1","status":"completed"}'
curl -X POST "http://localhost:8420/task/executable?goal_id=goal-1" \
  -H 'Content-Type: application/json' \
  -d '{}'

# CLI smoke (cobra grouped grammar)
echo '{"id":"goal-1","status":"running"}' | ~/.hermes/bin/hermem task status
echo '{"goal_id":"goal-1"}' | ~/.hermes/bin/hermem task next
echo '{"id":"step-1"}'      | ~/.hermes/bin/hermem task show
echo '{"status":"pending"}' | ~/.hermes/bin/hermem task list
echo '{"id":"step-1"}'      | ~/.hermes/bin/hermem task rollback
~/.hermes/bin/hermem graph verify
~/.hermes/bin/hermem db schema
```
