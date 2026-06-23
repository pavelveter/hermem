# Hermem — Usage

A runbook for operators. Covers build, configuration, both run modes
(CLI and HTTP server) side-by-side, request/response shapes, error
codes, schema, embedding-model notes, and common pitfalls.

For a conceptual overview and the install/Hermes-integration recipe
see [README.md](README.md). This document is concerned with *what to
do at the keyboard*.

---

## 1. TL;DR

```bash
# Build once.
go build -o hermem ./src

# CLI mode: pipe JSON into stdin. No server, no Ollama process to keep alive.
echo '{"query":"What is Go?"}' | ./hermem query

# Server mode: long-running HTTP service on :8420.
./hermem serve 8420 &
curl -s http://localhost:8420/health   # → {"status":"ok"}
```

Hermem reads `hermem.ini` from the working directory in both modes. If
the file is missing, all keys fall back to defaults (Ollama at
`http://localhost:11434`, model `nomic-embed-text`, DB at
`hermem.db`).

---

## 2. Build & install

### Prerequisites

- **Go 1.21+** — `go version` should report ≥ 1.21
- **CGO enabled** — required by `github.com/mattn/go-sqlite3`. On Linux
  install `gcc`; on macOS install Xcode CLT (`xcode-select --install`).
- **One of**:
  - [Ollama](https://ollama.com) running locally with an embedding
    model pulled (`ollama pull nomic-embed-text`), or
  - An OpenAI API key + OpenAI-compatible endpoint.

### Build the binary

```bash
go build -o hermem ./src
# or with -trimpath for reproducible builds
go build -trimpath -ldflags="-s -w" -o hermem ./src
```

Install into `$PATH`:

```bash
sudo cp hermem /usr/local/bin/    # Linux/macOS, system-wide
# or user-local:
mkdir -p ~/.local/bin && cp hermem ~/.local/bin/
```

### Smoke test

```bash
./hermem                              # prints command help
go test -count=1 ./src                # whole suite green? good
```

---

## 3. Configuration

`hermem.ini` is INI-format, three or four sections, all optional.
Missing keys fall back to defaults.

```ini
[embedder]
provider = ollama                # "ollama" | "openai"
url      = http://localhost:11434
model    = nomic-embed-text
key      =                        # only used when provider = openai
timeout  = 30s                    # HTTP request timeout (Go duration)

[extraction]
; provider, url, key — optional, fall back to [embedder]
provider    = ollama
url         = http://localhost:11434
model       = qwen2.5-coder:7b
temperature = 0.1
timeout     = 300s                 # HTTP request timeout (Go duration)

[ingestion]
dedup_threshold = 0.88           # cosine floor for merge-during-ingest

[database]
path    = hermem.db              # SQLite file; created on first store
backend = in-memory              # "in-memory" | "sqlite-vec"

[vector]
dim = 768                        # embedding dimension for vec0 table (must match model output)

[retrieval]
depth_ceiling = 5                 # hard clamp on requested max_depth
max_nodes     = 100               # soft cap on nodes per RetrieveContext

[retention]
observation_ttl = 2160h          # age beyond which observation nodes are archived (Go duration)
run_interval    = 1h              # how often the GC loop fires
batch_size      = 500             # max nodes archived per cycle (0 = no limit)

[server]
api_key =                        # X-API-Key auth (empty = disabled)

[schema]                         # optional — state machine on graph
; When absent, classic categories (world, opinion, experience, observation)
; and classic relations (prefers, uses, mentions, related_to, part_of,
; causes, contradicts, blocked_by, recovers_via) are used as defaults.
; FSM query endpoints (task-*) return empty results.
allowed_categories  = task,milestone,world
allowed_relations   = blocked_by,recovers_via,causes,heals,related_to,mentions
stateful_categories = task,milestone
valid_states        = pending,running,completed,failed
relation_blocking   = blocked_by    # relation name for dependency edges
state_unblocking    = completed     # state that unblocks dependents
relation_recovery   = recovers_via  # relation name for recovery edges
```

### Lookup order

1. `hermem.ini` next to the binary executable (resolved via
   `os.Executable()` → `filepath.Dir(exe)` → joined with
   `hermem.ini`). Both `hermem store` and `hermem serve` read from
   this location regardless of the caller's working directory, so a
   deployed `~/.hermes/bin/hermem` finds its config the same way from
   `~`, from a cron job's CWD, or from a fresh shell.
2. Built-in defaults (non-fatal when the file is absent —
   `LoadConfigFromDir` returns the defaults with `err == nil`).

`HERMEM_INI` env-var override and `--config <path>` flag are
deliberately **not wired** in this release; the binary's directory
*is* the config location. Both remain tracked as TODO items for a
future "operator portable between installs" change.

### Vector backend

Hermem supports two vector index backends, selected via `[database] backend`:

| Backend | Config value | Search | Dependency |
|---------|-------------|--------|------------|
| In-memory (default) | `in-memory` | Brute-force O(N) cosine scan | None (zero-dependency) |
| sqlite-vec | `sqlite-vec` | Indexed KNN via `vec0` virtual table | `sqlite-vec` statically linked |

The in-memory backend reads all embeddings from the `entities` table
and computes cosine similarity in Go — simple, no dependencies, good up
to ~20k entities. The sqlite-vec backend stores vectors in a `vec0`
virtual table and uses its indexed KNN query for fast O(log N) search.
Switch by setting `[database] backend = sqlite-vec` and ensuring
`[vector] dim` matches your model's output dimension.

### Embedder dimension gotcha

The SQLite BLOB column holds embeddings as raw `float32` bytes with a
fixed stride. Mixing models with different output dimensions in the
same database corrupts cosine math silently (e.g. 768-dim
`nomic-embed-text` and 1536-dim `text-embedding-3-small` cannot share
a DB). Either:

- Use **one model per DB**, or
- Migrate by writing a new DB and re-ingesting (`hermem ingest` of
  every dialog is enough).

See §11.

---

## 4. CLI mode runbook

Hermem is a single binary that reads JSON from stdin and writes JSON
to stdout. There is no REPL, no daemon, no IPC — every command is a
one-shot read-process-print.

### Commands

| Command | Reads (stdin JSON)              | Writes (stdout JSON)             |
|---------|----------------------------------|----------------------------------|
| `store` | `{id, category, content, …}`     | `{"status":"ok"}`                |
| `search`| `{query, top_k?}`                | `[{entity, similarity}, …]`      |
| `query` | `{query}`                        | `{"context": "<markdown>"}`      |
| `explain` | `{query}` | `RetrievalResult` (with score breakdown) |
| `edge`  | `{source_id, target_id, relation_type, auto_create?}` | `{"status":"ok"}` |
| `ingest`| `{dialog}`                       | `{"status":"ok"}`                |
| `task-status` | `{id, status}` | `{"status":"ok"}` |
| `task-executable` | `{goal_id?}` | `{"tasks":[{"id","category","content","status","updated_at"}, …]}` |
| `task-next` | `{goal_id?}` | `{"tasks":[{"id","category","content","status","updated_at"}, …]}` |
| `task-list` | `{status?, goal_id?}` | `{"tasks":[{"id","category","content","status","updated_at"}, …]}` |
| `task-show` | `{id}` | `{"entity":{…},"blocked_by":[…],"recovers_via":[…]}` |
| `task-dep` | `{source_id, target_id, relation_type?, add?}` | `{"status":"ok"}` |
| `task-create` | `{content, context_ids?, id?}` | `{"id":"…","status":"ok"}` |
| `task-rollback` | `{id}` | `{"rollback_task_id":"…"}` |
| `verify` | `{dim?}` | `{"status":"ok"}` (or report) |

The CLI uses the **same strict JSON contract as the HTTP server**
(`DisallowUnknownFields` etc.), so a payload that works against
`curl` will work against `echo '…' | hermem …` and vice versa.

Validation is **declarative**: categories and relation types are
enforced via the `[schema]` section. Unknown values return
`422 Unprocessable Entity`. When `[schema]` is absent, classic
defaults apply and the state machine is disabled.

### `store`

Upsert an entity. The embedder is consulted automatically if
`embedding` is omitted from the payload. After storing, edges are
automatically created to up to 3 existing entities with cosine
similarity > 0.85 (relation type `related_to`).
Unknown category → `422 Unprocessable Entity`.

```bash
echo '{
  "id": "user-likes-coffee",
  "category": "opinion",
  "content": "User drinks espresso every morning"
}' | ./hermem store
```

You can supply a pre-computed embedding to skip the embedder call
(useful in tests; must be a `float32` array in §11-correct stride):

```bash
echo '{
  "id": "f32-explicit",
  "category": "world",
  "content": "Pre-computed embedding test",
  "embedding": [0.1, 0.2, 0.3, 0.4]
}' | ./hermem store
```

### `search`

Returns the top-K entities by cosine similarity to the embedded query.

```bash
echo '{"query":"coffee preferences","top_k":3}' | ./hermem search
```

`top_k` defaults to 5 when omitted or ≤ 0. Output is a JSON array of
`{entity, similarity}` objects sorted descending by `similarity`.

### `query`

Full pipeline: embed → vector search → graph walk → markdown render.

```bash
echo '{"query":"Tell me about France"}' | ./hermem query
# → {"context":"## world\n- Paris is the capital of France\n…"}
```

`MaxDepth` for the graph walk uses the value from `[retrieval]`
(`depth_ceiling` is the hard clamp; the CLI always uses 2 by
default).

### `edge`

Create an edge between two existing entities. Optional `auto_create`
(default `false`) will auto-create missing entities as placeholder
nodes (`category=world`, `content=id`) before linking.

```bash
# Both entities must already exist:
echo '{"source_id":"user-likes-coffee","target_id":"espresso","relation_type":"prefers"}' | ./hermem edge

# Auto-create missing entities on the fly:
echo '{"source_id":"user-likes-coffee","target_id":"new-concept","relation_type":"related_to","auto_create":true}' | ./hermem edge
```

### `ingest`

Synchronous one-pass of the ingestion worker — extract entities,
embed, dedup-merge, persist.

```bash
echo '{
  "dialog": "User: What is Go?\nAssistant: Go is a statically typed language.\nUser: Who created it?\nAssistant: Rob Pike, Robert Griesemer and Ken Thompson."
}' | ./hermem ingest
```

Use this in cron/automation when you don't need the streaming worker.
For a long-running channel of messages, use the HTTP `/ingest`
endpoint or import `MemoryWorker` directly.

---

## 5. Server mode runbook

```bash
# Foreground (development):
./hermem serve 8420

# Detached (production):
nohup ./hermem serve 8420 > hermem.log 2>&1 &
echo $! > hermem.pid

# systemd unit sketch:
cat > /etc/systemd/system/hermem.service <<UNIT
[Unit]
Description=Hermem graph-memory HTTP server
After=network-online.target

[Service]
ExecStart=/usr/local/bin/hermem serve 8420
Restart=on-failure
WorkingDirectory=/var/lib/hermem
Environment=HERMEM_INI=/etc/hermem.ini

[Install]
WantedBy=multi-user.target
UNIT
```

The server uses `slog` for structured logs (`event`, `entity_id`,
`depth`, `cost_ms`, `model_name`, `embedding_dim` on relevant
paths). Pipe stderr to your log aggregator.

### Endpoints

| Method | Path | Body | Returns |
|--------|------|------|---------|
| GET | `/health` | — | `{"status":"ok"}` |
| GET | `/metrics` | — | expvar JSON |
| POST | `/store` | `StoreRequest` | `{"status":"ok"}` |
| POST | `/search` | `SearchRequest` | `[{"entity", "similarity"}]` |
| POST | `/retrieve` | `RetrieveRequest` | `RetrievalResult` (snake_case keys) |
| POST | `/edge` | `EdgeRequest` | `{"status":"ok"}` |
| POST | `/ingest` | `IngestRequest` | `{"status":"ok"}` |
| POST | `/query` | `QueryRequest` | `{"context":"..."}` |
| POST | `/task/status` | `{"id", "status"}` | `204 No Content` |
| POST | `/task/executable` | query `goal_id?` | `{"tasks":[{"id","category","content","status","updated_at"}]}` |
| POST | `/task/next` | `{"goal_id?":…}` | `{"tasks":[{"id","category","content","status","updated_at"}]}` |
| POST | `/task/list` | `{"status?", "goal_id?"}` | `{"tasks":[{"id","category","content","status","updated_at"}]}` |
| POST | `/task/show` | `{"id"}` | `{"entity":{…},"blocked_by":[…],"recovers_via":[…]}` |
| POST | `/task/dep` | `{"source_id","target_id","relation_type?","add?"}` | `{"status":"ok"}` |
| POST | `/task/rollback` | `{"id"}` | `{"rollback_task_id":"…"}` |
| POST | `/verify` | `{"dim"?}` | `VerifyReport` (text report) |

Every POST endpoint goes through a strict JSON decoder; fields not in
the request schema are rejected with `400`. See §9 for the error
shape and the full list of codes.

### `/health`

Liveness probe. No request body, no DB hit beyond the open
connection.

```bash
curl -s http://localhost:8420/health
# → {"status":"ok"}
```

### `/store`

Upsert an entity. Body must include `id`, `category`, `content`.
Optional: `embedding` (raw `float32` array — omit and the server
fills it via the configured embedder).

```bash
curl -X POST http://localhost:8420/store \
  -H 'Content-Type: application/json' \
  -d '{
    "id":"paris",
    "category":"world",
    "content":"Paris is the capital of France"
  }'
```

### `/search`

```bash
curl -X POST http://localhost:8420/search \
  -H 'Content-Type: application/json' \
  -d '{"query":"capital of France","top_k":5}'
```

`top_k` defaults to 5; `top_k > 0` is enforced.

### `/retrieve`

Walk the graph from explicit seed IDs (top-K vector search is
*not* done here — call `/search` first if you need seed selection).

```bash
curl -X POST http://localhost:8420/retrieve \
  -H 'Content-Type: application/json' \
  -d '{
    "seed_ids": ["paris", "france"],
    "max_depth": 2
  }'
```

`max_depth` is silently clamped to `[retrieval].depth_ceiling`.
Response is the full `RetrievalResult` — see §8 for the
authoritative wire shape. **Breaking change:** As of PR7b the
top-level keys are snake_case (`seed_nodes`, `world_facts`,
`opinions`, `experiences`, `observations`). Consumers reading
PascalCase keys must update.

### `/ingest`

Synchronous ingestion pipeline (extract → embed → dedup → store).

```bash
curl -X POST http://localhost:8420/ingest \
  -H 'Content-Type: application/json' \
  -d '{"dialog":"User: I love espresso.\nAssistant: Noted."}'
```

### `/query`

Full pipeline: embed query → search → retrieve → markdown render.

```bash
curl -X POST http://localhost:8420/query \
  -H 'Content-Type: application/json' \
  -d '{"query":"Tell me about France"}'
# → {"context":"## world\n- Paris is the capital of France\n..."}
```

### `/task/status`

Update execution state for a stateful entity. Valid statuses,
stateful categories, and blocking/recovery relation names are
defined in `[schema]`. Unknown statuses → `422`.

```bash
curl -X POST http://localhost:8420/task/status \
  -H 'Content-Type: application/json' \
  -d '{"id":"step-1","status":"running"}'
# → 204 No Content
```

Non-stateful entities or unknown statuses return `422`.

### `/task/executable`

List pending tasks whose `blocked_by` dependencies are all `completed`.
Omit `goal_id` for a global view, or pass `?goal_id=...` to restrict
the recursive CTE walk to a specific goal subtree.

```bash
# global
curl -X POST http://localhost:8420/task/executable \
  -H 'Content-Type: application/json' \
  -d '{}'

# goal-scoped
curl -X POST "http://localhost:8420/task/executable?goal_id=goal-1" \
  -H 'Content-Type: application/json' \
  -d '{}'
```

### `/task/next`

Alias for `/task/executable`. Same response shape.

```bash
curl -X POST http://localhost:8420/task/next \
  -H 'Content-Type: application/json' \
  -d '{}'
```

### `/task/list`

Filter tasks by `status` and optional `goal_id`.

```bash
# all pending tasks globally
curl -X POST http://localhost:8420/task/list \
  -H 'Content-Type: application/json' \
  -d '{"status":"pending"}'

# all tasks under a goal
curl -X POST http://localhost:8420/task/list \
  -H 'Content-Type: application/json' \
  -d '{"goal_id":"goal-1"}'
```

### `/task/show`

Show a single task and its `blocked_by` / `recovers_via` relations.

```bash
curl -X POST http://localhost:8420/task/show \
  -H 'Content-Type: application/json' \
  -d '{"id":"step-1"}'
```

Response:

```json
{
  "entity": {
    "id": "step-1",
    "category": "task",
    "content": "Run tests",
    "status": "pending",
    "updated_at": "2026-06-23T10:00:00Z"
  },
  "blocked_by": [
    {"id":"step-0","status":"pending"}
  ],
  "recovers_via": [
    {"id":"step-2","status":"pending"}
  ]
}
```

### `/task/dep`

Manage `blocked_by` (or other allowed) relations between tasks.

```bash
# add dependency: step-1 is blocked by step-0
curl -X POST http://localhost:8420/task/dep \
  -H 'Content-Type: application/json' \
  -d '{"source_id":"step-1","target_id":"step-0","add":true}'

# remove it
curl -X POST http://localhost:8420/task/dep \
  -H 'Content-Type: application/json' \
  -d '{"source_id":"step-1","target_id":"step-0","add":false}'
```

`relation_type` defaults to `blocked_by`. Allowed values:
`prefers`, `uses`, `mentions`, `related_to`, `part_of`, `causes`,
`contradicts`, `blocked_by`, `recovers_via`.

### `/task/rollback`

Find the task linked via `recovers_via` from a failed task.

```bash
curl -X POST http://localhost:8420/task/rollback \
  -H 'Content-Type: application/json' \
  -d '{"id":"step-1"}'
```

Response:

```json
{"rollback_task_id":"step-recover-1"}
```

If no rollback task is linked, `rollback_task_id` is empty string.

---

## 6. CLI vs. Server — side-by-side

| Task                | CLI                                                    | HTTP                                                  |
|---------------------|--------------------------------------------------------|-------------------------------------------------------|
| Store a fact        | `… \| ./hermem store`                                  | `curl -X POST …/store -d '{…}'`                       |
| Search by query     | `… \| ./hermem search`                                 | `curl -X POST …/search -d '{…}'`                      |
| Full query → md   | `… \| ./hermem query`                                  | `curl -X POST …/query -d '{…}'`                       |
| Ingest dialog       | `… \| ./hermem ingest`                                 | `curl -X POST …/ingest -d '{…}'`                      |
| Update task status  | `… \| ./hermem task-status`                            | `curl -X POST …/task/status -d '{…}'`                 |
| List executable     | `… \| ./hermem task-executable`                        | `curl -X POST …/task/executable`                       |
| Next executable     | `… \| ./hermem task-next`                              | `curl -X POST …/task/next`                             |
| List tasks          | `… \| ./hermem task-list`                              | `curl -X POST …/task/list`                             |
| Show task           | `… \| ./hermem task-show`                              | `curl -X POST …/task/show`                             |
| Task dependency     | `… \| ./hermem task-dep`                               | `curl -X POST …/task/dep`                              |
| Create task         | `… \| ./hermem task-create`                            | `curl -X POST …/task/create`                           |
| Rollback task       | `… \| ./hermem task-rollback`                          | `curl -X POST …/task/rollback`                         |
| Task tree           | `… \| ./hermem task-tree`                              | `curl -X POST …/task/tree`                             |
| Verify graph       | `… \| ./hermem verify`                                | `curl -X POST …/verify -d '{…}'`                       |
| Health              | n/a (CLI is one-shot)                                  | `curl …/health`                                       |
| Long-running        | No — one-shot per process                              | Yes — single process, multiple requests               |
| Errors              | Exit non-zero + `log.Fatalf` to stderr                 | `HTTP 400` + structured `ErrorResponse` body          |
| Embedding model     | Read from `[embedder] model`                           | Same                                                  |
| DB file             | `[database] path` from working-dir `hermem.ini`        | Same                                                  |
| Strict JSON         | Yes (`DisallowUnknownFields`)                          | Yes                                                   |

---

## 7. Request payload reference

JSON tag is the wire field name. JSON name is identical to the Go
struct field unless noted.

### `StoreRequest` (`/store`, CLI `store`)

| Field        | Type        | Required | Notes                                          |
|--------------|-------------|----------|------------------------------------------------|
| `id`         | string      | yes      | Stable identifier, used as upsert key.         |
| `category`   | string      | yes      | One of: `world`, `opinion`, `experience`, `observation`, `task`. |
| `content`    | string      | yes      | Free text.                                     |
| `embedding`  | float32[]   | no       | Pre-computed embedding; server fills if absent.|

### `SearchRequest` (`/search`, `/query`, CLI `search`, CLI `query`)

| Field    | Type   | Required | Notes                                       |
|----------|--------|----------|---------------------------------------------|
| `query`  | string | yes      | Free-text query.                            |
| `top_k`  | int    | no       | Default 5; must be > 0; ≤ 0 falls back to 5.|

### `RetrieveRequest` (`/retrieve`)

| Field        | Type     | Required | Notes                                          |
|--------------|----------|----------|------------------------------------------------|
| `seed_ids`   | string[] | yes      | IDs of the nodes to start the walk from.       |
| `max_depth`  | int      | no       | Default 2; clamped to `[retrieval].depth_ceiling`. |

### `EdgeRequest` (`/edge`, CLI `edge`)

| Field           | Type    | Required | Notes                                          |
|-----------------|---------|----------|------------------------------------------------|
| `source_id`     | string  | yes      | Source entity ID.                              |
| `target_id`     | string  | yes      | Target entity ID.                              |
| `relation_type` | string  | yes      | One of: `prefers`, `uses`, `mentions`, `related_to`, `part_of`, `causes`, `contradicts`, `blocked_by`, `recovers_via`. |
| `auto_create`   | bool    | no       | Default `false`. When `true`, missing entities are auto-created as `category=world` placeholders with embeddings. |

### `IngestRequest` (`/ingest`, CLI `ingest`)

| Field    | Type   | Required | Notes                                       |
|----------|--------|----------|---------------------------------------------|
| `dialog` | string | yes      | Free-form conversational text.             |

### `TaskStatusRequest` (`/task/status`, CLI `task-status`)

| Field    | Type   | Required | Notes                                       |
|----------|--------|----------|---------------------------------------------|
| `id`     | string | yes      | Task entity ID.                             |
| `status` | string | yes      | One of: `pending`, `running`, `completed`, `failed`. |

### `TaskExecutableRequest` (`/task/executable`, `/task/next`, CLI `task-executable`, CLI `task-next`)

HTTP: body ignored; `goal_id` comes from the query string.
CLI: pass `goal_id` in the JSON body (omit or leave empty for global view).

| Field     | Type   | Required | Notes                                          |
|-----------|--------|----------|------------------------------------------------|
| `goal_id` | string | no       | Restrict CTE walk to a specific goal subtree.  |

### `TaskListRequest` (`/task/list`, CLI `task-list`)

| Field     | Type   | Required | Notes                                          |
|-----------|--------|----------|------------------------------------------------|
| `status`  | string | no       | Filter by `pending` / `running` / `completed` / `failed`. |
| `goal_id` | string | no       | Restrict to a goal subtree.                    |

### `TaskShowRequest` (`/task/show`, CLI `task-show`)

| Field | Type   | Required | Notes                  |
|-------|--------|----------|------------------------|
| `id`  | string | yes      | Task entity ID.        |

### `TaskDepRequest` (`/task/dep`, CLI `task-dep`)

| Field           | Type    | Required | Notes                                          |
|-----------------|---------|----------|------------------------------------------------|
| `source_id`     | string  | yes      | Task that has the dependency.                  |
| `target_id`     | string  | yes      | Dependency target.                             |
| `relation_type` | string  | no       | Default `blocked_by`. Allowed values listed above. |
| `add`           | bool    | no       | Default `true`. `false` removes the edge.      |

### `TaskCreateRequest` (`/task/create`, CLI `task-create`)

| Field           | Type    | Required | Notes                                          |
|-----------------|---------|----------|------------------------------------------------|
| `id`            | string  | no       | Stable task ID; auto-generated when omitted.    |
| `content`       | string  | yes      | Task description / payload.                     |
| `context_ids`   | string[]| no       | Existing task IDs to link via `related_to`.     |

### `TaskTreeRequest` (`/task/tree`, CLI `task-tree`)

| Field   | Type   | Required | Notes                                           |
|---------|--------|----------|-------------------------------------------------|
| `goal_id` | string | no       | Root task ID; omit to render all root tasks.    |

### `TaskRollbackRequest` (`/task/rollback`, CLI `task-rollback`)

| Field | Type   | Required | Notes                  |
|-------|--------|----------|------------------------|
| `id`  | string | yes      | Failed task entity ID. |

---

## 8. Response payload reference

### `StoreResponse` / `IngestResponse`

```json
{"status":"ok"}
```

### `SearchResponse`

```json
[
  {
    "entity":  {"id":"paris","category":"world","content":"Paris is the capital of France"},
    "similarity": 0.9134
  },
  ...
]
```

### `RetrieveResponse` (`RetrievalResult`)

```json
{
  "seed_nodes": [
    {
      "entity":        {"id":"paris","category":"world","content":"Paris is the capital of France","embedding":[...], "updated_at":"..."},
      "relations":     [],
      "depth":         0,
      "parent_id":     "",
      "relation_type": "",
      "ranking_score": 0.9134
    }
  ],
  "world_facts":   [{"content":"Paris is the capital of France","parent_id":"france","relation_type":"part_of","depth":1}],
  "opinions":     [],
  "experiences":  [],
  "observations": []
}
```

Each per-category bucket (`world_facts` / `opinions` / `experiences` /
`observations`) is `[]RetrievedFact` — i.e. an array of
`{content, parent_id, relation_type, depth}` objects. Empty buckets
remain in the output as `[]` (not absent) so consumers can iterate
without nil-checking. `parent_id` and `relation_type` are `omitempty`:
seed-reached facts (`depth == 0`) emit them as empty strings; graph-walk
facts (`depth > 0`) carry them populated so the calling agent can see
why each fact was pulled in. `seed_nodes` is `[]GraphNode` with full
`entity` + composite score.

(Use `FormatContextMarkdown` server-side or the wrapper at
`/query` to render to LLM-ready markdown.)

### `QueryResponse`

```json
{"context":"## world\n- Paris is the capital of France\n..."}
```

### `TaskExecutableResponse` (`/task/executable`, `/task/next`, `/task/list`)

```json
{
  "tasks": [
    {
      "id": "step-1",
      "category": "task",
      "content": "Run tests",
      "status": "pending",
      "updated_at": "2026-06-23T10:00:00Z"
    }
  ]
}
```

### `TaskShowResponse` (`/task/show`)

```json
{
  "entity": {
    "id": "step-1",
    "category": "task",
    "content": "Run tests",
    "status": "pending",
    "updated_at": "2026-06-23T10:00:00Z"
  },
  "blocked_by": [
    {"id":"step-0","status":"pending"}
  ],
  "recovers_via": [
    {"id":"step-recover-1","status":"pending"}
  ]
}
```

### `TaskRollbackResponse` (`/task/rollback`)

```json
{"rollback_task_id":"step-recover-1"}
```

When no rollback task is found, `rollback_task_id` is `""`.

### `HealthResponse`

```json
{"status":"ok"}
```

---

## 9. Error model

All POST endpoints (HTTP) and all CLI payloads return errors in a
single shape. The wire format is strictly backwards-compatible:
the legacy `{"error":"msg"}` envelope is preserved; the new `code`
and `field` fields are optional (omitted when not applicable).

### `ErrorResponse`

```json
{
  "error": "human-readable message",
  "code":  "machine-readable code",
  "field": "offending JSON field, when known"
}
```

### Codes

| `code`         | Meaning                                              | `field` example             |
|----------------|------------------------------------------------------|-----------------------------|
| `empty_body`   | Body was empty or contained only whitespace.         | —                           |
| `bad_json`     | Body was not valid JSON, or trailing data after value.| —                          |
| `trailing_data`| A second value followed the parsed object/array.     | —                           |
| `unknown_field`| A field present in the body is not allowed.          | `top_k` (typo `topK`)       |
| `invalid_type` | A field is present but the wrong JSON type.          | `top_k` (string vs int)     |

### Examples

```bash
# Unknown field
$ curl -s -X POST localhost:8420/search -d '{"query":"x","topK":3}'
{"error":"unknown field: topK","code":"unknown_field","field":"topK"}

# Wrong type
$ curl -s -X POST localhost:8420/search -d '{"query":"x","top_k":"three"}'
{"error":"invalid type for field \"top_k\" (got string, want uint8)","code":"invalid_type","field":"top_k"}

# Empty body
$ curl -s -X POST localhost:8420/search -d ''
{"error":"request body is empty","code":"empty_body"}

# Trailing data
$ curl -s -X POST localhost:8420/store -d '{"id":"a","category":"world","content":"x"}{"id":"b","category":"world","content":"y"}'
{"error":"trailing data after JSON value","code":"trailing_data"}
```

CLI shows the human message and exits non-zero:

```bash
$ echo '{"query":"x","topK":3}' | ./hermem search
2025/... invalid request: unknown field: topK
# (exit 1)
```

---

## 10. Database schema

A single SQLite file with two (or three) tables. The schema lives in
`db.go → InitDB`; below is the field-by-field reference.

### `entities`

| Column          | SQLite type | Notes                                                 |
|-----------------|-------------|-------------------------------------------------------|
| `id`            | TEXT PK     | Stable identifier.                                    |
| `category`      | TEXT        | One of the categories defined in `[schema] allowed_categories` (defaults: `world`/`opinion`/`experience`/`observation`). |
| `content`       | TEXT        | Free text.                                            |
| `embedding`     | BLOB        | `len(embedding) * 4` raw little-endian float32 bytes. |
| `updated_at`    | DATETIME    | `CURRENT_TIMESTAMP` default; refreshed on each upsert.|
| `last_accessed_at` | DATETIME | `CURRENT_TIMESTAMP` default; GC uses this for TTL.    |
| `archived`      | INTEGER     | 0 = active, 1 = excluded from graph walks by GC.      |

Entity IDs are primary keys — repeated `store` calls upsert (the row
is replaced; edges are not deleted). The DB is configured with
`PRAGMA journal_mode = WAL` and `PRAGMA synchronous = NORMAL` in
`InitDB` for better concurrent performance. Re-storing the same `id`
overwrites the embedded content; cosine math remains valid because
the float32 stride does not change.

### `edges`

| Column          | SQLite type | Notes                                               |
|-----------------|-------------|-----------------------------------------------------|
| `source_id`     | TEXT        | FK → `entities.id` (cascade on delete).             |
| `target_id`     | TEXT        | FK → `entities.id` (cascade on delete).             |
| `relation_type` | TEXT        | Relation label from `[schema] allowed_relations` (defaults: `prefers`, `uses`, `mentions`, `related_to`, `part_of`, `causes`, `contradicts`, `blocked_by`, `recovers_via`). Unknown values are rejected with HTTP 422. |

Composite PK `(source_id, target_id, relation_type)` means duplicate
edges auto-dedupe on insert. There is no `weight` or timestamp column
on edges — weight is implicit (always 1.0 in the current model) and
edge provenance is recovered via `RetrievedFact.parent_id` /
`relation_type` from the graph walk.

### `vec_entities` (when `[database] backend = sqlite-vec`)

| Column       | SQLite type | Notes                                                 |
|--------------|-------------|-------------------------------------------------------|
| `rowid`      | INTEGER PK  | Mapped via `id_map` table (AUTOINCREMENT).             |
| `embedding`  | FLOAT32[n]  | Vector stored in `vec0` virtual table for KNN search. |
| `entity_id`  | TEXT        | Maps back to `entities.id`.                            |

This is a `vec0` virtual table managed by the `sqlite-vec` extension.
It enables indexed KNN search via `WHERE embedding MATCH ? ORDER BY distance`.
Only created when `[database] backend = sqlite-vec`; the default
`in-memory` backend reads directly from `entities.embedding`.

### Migrations

There is no migration system. To change the schema, write a new
`hermem.db` and re-ingest (`hermem ingest` against every persisted
dialog is sufficient; the embedded text regenerates).

---

## 11. Embedding model & dimensions

Hermem writes embeddings as raw `float32` bytes in BLOB. The expected
stride is whatever the configured embedder produces. Switching
models with a different output dimension against an existing DB will
silently produce wrong cosine scores.

### Dimension per common model

| Model                            | Dim |
|----------------------------------|-----|
| `nomic-embed-text`               | 768 |
| `text-embedding-3-small` (OpenAI)| 1536 |
| `text-embedding-3-large` (OpenAI)| 3072 |
| `mxbai-embed-large`              | 1024 |
| `all-minilm`                     | 384 |

### Migration: switch the embedder

1. Stop the server / exit CLI processes.
2. Rename or move `hermem.db` aside (`mv hermem.db hermem.db.v1`).
3. Update `[embedder] model` (and provider, if applicable) in
   `hermem.ini`.
4. Re-ingest every dialog you have on hand (`hermem ingest`).
5. The new `hermem.db` is consistent with the new stride.

---

## 12. Operational notes

- **Config is binary-directory relative.** `hermem.ini` is resolved via
  `os.Executable()`, not `os.Getwd()`, so `~/.hermes/bin/hermem store` works
  the same from any working directory. The ini lives next to the binary.
- **Concurrency.** The HTTP server is fine for dozens of
  concurrent requests, but the underlying SQLite write path is
  serialised by SQLite itself. For high-write workloads consider
  batching through `/ingest` instead of N×`/store`.
- **Slog output.** Logs go to stderr via `log/slog`. The exact field
  set per event is:
  - `server_ready`            — `port`
  - `ingest_failed`           — `err`, `dialog_len`
  - `ollama_call`             — `model`, `attempts_used`, `outcome` (emitted at Debug; pre-retry `ollama_attempt_retry` Warn lines surface only mid-loop)
  - `retrieval_complete`      — `seed_count`, `total_ranked`, `effective_depth`, `cap_active` (emitted at Debug — the level filter is the throttle)
  No `entity_id` / `embedding_dim` / `cost_ms` fields are emitted yet
  (TODO §5). Pipe stderr to a JSON-aware log shipper.
- **Graceful shutdown.** Server drains in-flight requests on `SIGINT`/`SIGTERM` with a 10-second timeout, then exits cleanly. Use `kill <pid>` or `systemctl stop hermem`.
- **Backups.** The DB is a single SQLite file. `sqlite3 hermem.db
  ".backup hermem.db.bak"` while the server is running is safe
  (SQLite's online backup API). Plain `cp hermem.db hermem.db.bak`
  while writers are active is **not** safe.

---

## 13. Common pitfalls

- **Stale ini.** Edited `hermem.ini` but didn't restart the server.
  Re-reads happen once at startup; `SIGHUP` reload is not yet
  wired.
- **`store` saying "id required" when you have an id.** Almost
  always a shell-escaping bug in single-quoted JSON. Pipe through a
  file (`./hermem store < req.json`) or use `jq -c … | hermem…`.
- **`search` always returns the same top result.** Either the
  embedder model is misconfigured (check `slog` for
  `embedding_dim`), or the DB has fewer than K similar entities.
- **`/query` returns an empty markdown.** No seed-match → no graph
  walk → empty buckets. Verify with `/search` first that the query
  actually matches stored content.
- **Two concatenations in one body** (`{...}{...}`). Now rejected
  with `trailing_data` (PR7a); pre-PR7a would silently accept the
  first object. If a legacy client still relies on the old behavior,
  simplify its request shape.
- **Embedding dimension drift.** Storing 768-dim and 1536-dim
  entities in the same DB corrupts cosine similarity silently.
  See §11.
- **CLI exit 1 with no log line.** Almost always a stdin read
  failure (Ctrl-D on an empty stdin in a script). Capture stderr.

---

## 14. Where to look in the code

| Concern                         | File                              |
|---------------------------------|-----------------------------------|
| INI parsing, defaults           | `src/config.go`                   |
| Schema, embedding serialisation | `src/db.go`                       |
| VectorIndex interface, search backends (InMemory / SqliteVec) | `src/vector.go` |
| Graph walk, ranking, formatting | `src/retrieval.go`                |
| Background worker, dedup, edges | `src/ingestion.go`                |
| Ollama / OpenAI HTTP            | `src/embedder.go`, `src/extractor.go` |
| HTTP handlers, strict decoder   | `src/server.go`                   |
| CLI entry-point                 | `src/main.go`                     |
| Retention GC loop               | `src/retention.go`                |
| Auth + request-id middleware    | `src/middleware.go`               |
| Metrics                         | `src/metrics.go`                  |
| Accelerate SIMD cosine (darwin) | `src/cosine_darwin.go`            |
| Pure-Go cosine fallback         | `src/cosine.go`                   |
| Per-package tests               | `src/*_test.go`                   |