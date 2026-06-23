---
name: hermem
description: Lightweight graph memory for Hermes — store facts, search by vector similarity, retrieve connected context
version: 0.1.0
metadata:
  hermes:
    tags: [memory, graph, vector-search, sqlite, task-state]
    category: memory
    config:
      - key: hermem.url
        description: Hermem server URL
        default: "http://localhost:8420"
        prompt: "Hermem server URL (e.g. http://localhost:8420)"
required_environment_variables:
  - name: HERMEM_URL
    help: "Optional: override Hermem base URL for HTTP/server mode"
    required_for: HTTP/server mode only
---

# Hermem — Graph Memory Skill

Lightweight graph memory via the Hermem CLI binary. No server is required for normal use.

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

### Critical working-directory rule

The Go binary resolves `./hermem.ini` from the **current working directory**, not from the binary's location. Therefore, if the binary is invoked from any directory other than `~/.hermes`, it will fall back to built-in defaults and can create a stray `hermem.db` in that unrelated directory. When running the binary, use the `~/.hermes` working directory explicitly: `cd ~/.hermes && ~/.hermes/bin/hermem ...`. This is also true when rebuilding from the hermem source tree in `/Users/pavelveter/Projects/labs/hermem`.

## Memory Categories

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

### 1. Store a fact

Use `hermem_store` tool:

```
hermem_store(content="User prefers dark mode in all editors", category="opinion")
hermem_store(content="Project uses Go 1.22 with Chi router", category="world")
hermem_store(content="Deployed v2.1 to production on 2026-01-15", category="experience")
```

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

## State Machine on Graph (Batch 9)

Hermem supports graph-based task execution state.

### New relation types

- `blocked_by` — B is blocked until A completes
- `recovers_via` — B offers a recovery/rollback path when A fails

### Schema

`entities.status` is `TEXT DEFAULT 'pending'`, backfilled. The `category` CHECK now accepts `task`. Status is validated: `pending`, `running`, `completed`, `failed`.

### Task API

CLI:

```bash
# update status
echo '{"id":"step-1","status":"running"}' | ~/.hermes/bin/hermem task-status

# list executable (global)
echo '{}' | ~/.hermes/bin/hermem task-executable

# alias
echo '{"goal_id":"goal-1"}' | ~/.hermes/bin/hermem task-next

# filter by status / goal
echo '{"status":"pending"}' | ~/.hermes/bin/hermem task-list
echo '{"goal_id":"goal-1"}' | ~/.hermes/bin/hermem task-list

# show task + relations
echo '{"id":"step-1"}' | ~/.hermes/bin/hermem task-show

# manage dependency
echo '{"source_id":"step-1","target_id":"step-0","add":true}' | ~/.hermes/bin/hermem task-dep

# find rollback task
echo '{"id":"step-1"}' | ~/.hermes/bin/hermem task-rollback
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
- Default URL is `http://localhost:8420` — set `HERMEM_URL` env var if different
- Entities with similar content (>88% cosine similarity) are merged automatically
- Graph walk depth defaults to 2 hops — increase via `max_depth` parameter
- `store` auto-links the new entity to up to 3 existing entities with cosine similarity > 0.85 using relation type `related_to`. Explicit graph edges still use `edge` with `source_id`, `target_id`, and `relation_type`. `ingest` remains the path that extracts entities and relations from dialog text.
- `store` and `ingest` are NOT interchangeable. `ingest` is the only path that produces both entities and graph edges from dialog text, because it runs the LLM extractor to discover relationships. Do not run `store` and then manually `INSERT INTO edges` in SQLite — that skips validation, entity resolution, and the dedup layer, and it creates dirty state the CLI does not know about.
- If `ingest` fails because the LLM extractor times out, fix the root cause — usually by raising `ollamaRequestTimeout` in `extractor.go` and rebuilding — then re-run `ingest`. Never cure Ollama latency by hand-inserting into `edges`.
- CLI `store` does not guarantee an embedding was generated. Always verify retrieval with `hermem_query`/`hermem_search` after storing facts.
- If `query` returns `{"context":""}`, inspect the active binary path, database path, and query wording before retrying.

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

The Go binary loads `hermem.ini` from the current working directory by default. When run via Hermes, prefer setting an explicit database path so it does not depend on session cwd:

```ini
[embedder]
provider = ollama
url = http://localhost:11434
model = nomic-embed-text

[database]
path = /Users/pavelveter/.hermes/hermem.db
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

## Batch 9 Smoke Test

```bash
cd /Users/pavelveter/Projects/labs/hermem
gofmt -w src/*.go
go vet ./src/...
go test -count=1 -race -timeout 180s ./src/...
```

Manual endpoint smoke:

```bash
~/.hermes/bin/hermem --no-decor &
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

# CLI smoke
echo '{"id":"goal-1","status":"running"}' | ~/.hermes/bin/hermem task-status
echo '{"goal_id":"goal-1"}' | ~/.hermes/bin/hermem task-executable
echo '{"id":"step-1"}' | ~/.hermes/bin/hermem task-show
echo '{"status":"pending"}' | ~/.hermes/bin/hermem task-list
echo '{"id":"step-1"}' | ~/.hermes/bin/hermem task-rollback
```
