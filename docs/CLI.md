# Hermem — CLI Reference

Full reference for the `hermem` CLI. Every command reads JSON from
stdin and writes JSON to stdout. There is no REPL, no daemon, no IPC —
every invocation is a one-shot read-process-print.

For server mode, see [SERVER.md](SERVER.md). For configuration, see
[USAGE.md](USAGE.md). For production operations, see
[RUNBOOK.md](RUNBOOK.md).

---

## Command tree

The CLI uses a `git`/`kubectl`-style grouped tree. Top-level commands
plus 6 subcommand groups; every group has its own `--help`.

```bash
# Top-level
hermem serve [--port 8420]                    HTTP server
hermem health                                 DB ping (exit 1 on fail)
hermem metrics                                Prometheus exposition
hermem version                                Build metadata (ldflags)

# `hermem memory …` — knowledge CRUD + retrieval
hermem memory store         < req.json        Upsert entity
hermem memory search        < req.json        Top-K cosine neighbours
hermem memory retrieve      < req.json        Graph walk from explicit seed_ids
hermem memory query         < req.json        embed → search → walk → markdown
hermem memory response      < req.json        Full pipeline + LLM response
hermem memory edge          < req.json        Add typed edge (opt auto_create)
hermem memory ingest        < req.json        LLM-extract + dedup-merge
hermem memory explain       < req.json        Retrieval with score breakdown
hermem memory re-embed      [--batch-size N] [--model M]   Batch re-embed all
hermem memory quantize      < req.json        Scalar int8 roundtrip + stats

# `hermem task …` — task lifecycle
hermem task status          < req.json        Update task status
hermem task list            < req.json        Filter by status / goal_id
hermem task show            < req.json        task + blocked_by + recovers_via
hermem task dep             < req.json        add/remove dependency edge
hermem task tree            < req.json        ASCII tree under a goal_id
hermem task create          < req.json        Auto-embed + assign stateful category
hermem task rollback        < req.json        Find recovers_via companion
hermem task next            [{}]              Executable tasks (alias: executable)

# `hermem graph …` — graph analytics
hermem graph plan           < req.json        Topo-sorted plan under goal_id
hermem graph recovery-plan  < req.json        recovers_via chain walk
hermem graph components                      Connected components (size ≥ 2)
hermem graph communities                     Louvain + global modularity
hermem graph verify                          Integrity check (exit 1 on fail)
hermem graph contradictions [entity-id]      Optional positional ID filter
hermem graph provenance [--conversation X] [--message M] [--source S] [--limit N]

# `hermem time …` — temporal queries
hermem time temporal        < req.json        Time-windowed retrieval (RFC3339)
hermem time timeline                         Recent 50 entities (created_at DESC)

# `hermem agent …` — agentic flows
hermem agent loop           < req.json        algo.AgentLoop on a goal_id

# `hermem db …` — migration / schema housekeeping
hermem db migrate                             Migration status (applied / pending)
hermem db rollback                            Roll back most-recent applied migration
hermem db verify                              Checksum integrity check (exit 1)
hermem db schema                              Stored vs current schema fingerprint
```

> **Breaking change (commit `8f0bf71`):** the previously-flat 26-command
> surface is gone — `store`, `task-status`, `migration-rollback`,
> `connected-components`, etc. are no longer callable. All scripts must
> be rewritten to the grouped form above. There are no aliases.

---

## Per-command reference

### `hermem memory store`

Upsert an entity. The embedder is consulted automatically if `embedding`
is omitted from the payload. After storing, edges are automatically
created to up to 3 existing entities with cosine similarity > 0.85
(relation type `related_to`). Unknown category → exit 1 + non-zero
structured error from `httputil.DecodeStrict`.

```bash
echo '{
  "id": "user-likes-coffee",
  "category": "opinion",
  "content": "User drinks espresso every morning"
}' | ./hermem memory store
```

You can supply a pre-computed embedding to skip the embedder call
(useful in tests; must be a `float32` array in §11-correct stride):

```bash
echo '{
  "id": "f32-explicit",
  "category": "world",
  "content": "Pre-computed embedding test",
  "embedding": [0.1, 0.2, 0.3, 0.4]
}' | ./hermem memory store
```

### `hermem memory search`

Returns the top-K entities by cosine similarity to the embedded query.

```bash
echo '{"query":"coffee preferences","top_k":3}' | ./hermem memory search
```

`top_k` defaults to 5 when omitted or ≤ 0. Output is a JSON array of
`{entity, similarity}` objects sorted descending by `similarity`.

### `hermem memory query`

Full pipeline: embed → vector search → graph walk → markdown render.

```bash
echo '{"query":"Tell me about France"}' | ./hermem memory query
# → {"context":"## world\n- Paris is the capital of France\n…"}
```

### `hermem memory explain`

Explains the retrieval reasoning path: which nodes became seeds, which
edges were traversed, and how scores were computed. Outputs an ASCII
tree by default; use `--json` for raw JSON.

```bash
echo '{"query":"coffee preferences"}' | ./hermem memory explain
# Query: "coffee preferences"
#
# ── Seeds (vector search) ──
#   [entity-42] depth=0 score=0.892 ≈
#
# ── Graph walk (edges traversed) ──
#   d=0 -0.000       coffee is best consumed in the morning [score=0.892 vec=0.912 rec=0.800 cent=0.301 depth=0.000]
#   d=1 -1.000       espresso uses finely ground beans (via 'related_to' from entity-42) [score=0.756 ...]
#
# ── Summary ──
#   Seeds: 1 | World: 3 | Opinion: 1 | Experience: 0 | Observation: 0
```

Use `--json` flag to get raw `RetrievalResult` JSON instead:

```bash
echo '{"query":"coffee"}' | ./hermem memory explain --json
```

`MaxDepth` for the graph walk uses the value from `[retrieval]`
(`depth_ceiling` is the hard clamp; the CLI always uses 2 by default).

### `hermem memory edge`

Create an edge between two existing entities. Optional `auto_create`
(default `false`) will auto-create missing entities as placeholder
nodes (`category=world`, `content=id`) before linking.

```bash
# Both entities must already exist:
echo '{"source_id":"user-likes-coffee","target_id":"espresso","relation_type":"prefers"}' \
  | ./hermem memory edge

# Auto-create missing entities on the fly:
echo '{"source_id":"user-likes-coffee","target_id":"new-concept","relation_type":"related_to","auto_create":true}' \
  | ./hermem memory edge
```

### `hermem memory ingest`

Synchronous one-pass of the ingestion worker — extract entities,
embed, dedup-merge, persist.

```bash
echo '{
  "dialog": "User: What is Go?\nAssistant: Go is a statically typed language.\nUser: Who created it?\nAssistant: Rob Pike, Robert Griesemer and Ken Thompson."
}' | ./hermem memory ingest
```

Use this in cron/automation when you don't need the streaming worker.
For a long-running channel of messages, use the HTTP `/ingest`
endpoint or import `MemoryWorker` directly.

### `hermem task …`

The full task lifecycle group is JSON-stdin driven. Examples:

```bash
# Update status
echo '{"id":"step-1","status":"running"}' | ./hermem task status

# List executable globally
echo '{}' | ./hermem task next
echo '{"goal_id":"goal-1"}' | ./hermem task next        # scoped to a goal
echo '{"status":"pending"}' | ./hermem task list       # filter by status
echo '{"goal_id":"goal-1"}'  | ./hermem task list       # filter by goal
echo '{"id":"step-1"}'       | ./hermem task show       # + blocked_by + recovers_via
echo '{"source_id":"step-1","target_id":"step-0","add":true}'  | ./hermem task dep
echo '{"content":"Run tests"}'                         | ./hermem task create
echo '{"content":"Run tests","context_ids":["step-0"]}' | ./hermem task create
echo '{"goal_id":"goal-1"}' | ./hermem task tree        # ASCII tree
echo '{"id":"step-1"}'      | ./hermem task rollback    # recovers_via neighbour
```

### `hermem graph …`

Graph analytics. The first three commands are JSON-stdin driven; the
rest read parameters from cobra flags:

```bash
echo '{"goal_id":"goal-1"}' | ./hermem graph plan
echo '{"id":"step-1"}'      | ./hermem graph recovery-plan
./hermem graph components                             # size ≥ 2
./hermem graph communities                            # Louvain + global Q
./hermem graph verify                                 # integrity check (exit 1 on fail)
./hermem graph contradictions e1                      # optional positional entity-id
./hermem graph provenance --conversation conv-1 --limit 10
./hermem graph provenance --message msg-3 --source dialog --limit 20
```

### `hermem time …`

```bash
echo '{"query":"user beliefs about Go","time_from":"2026-03-01T00:00:00Z","time_to":"2026-04-01T00:00:00Z","top_k":5}' \
  | ./hermem time temporal
./hermem time timeline                                # last 50 entities
```

### `hermem agent loop`

```bash
echo '{"goal_id":"goal-1"}' | ./hermem agent loop
# Yields one line per task: [<id>] <content>  [<category>]
```

### `hermem db …`

```bash
./hermem db migrate                  # migration status
./hermem db rollback                 # roll back most-recent applied migration
./hermem db verify                   # checksum integrity (exit 1 on mismatch)
./hermem db schema                   # stored vs current schema fingerprint
```

#### `db migrate`

Shows versioned migration status. Each embedded SQL migration is listed
with `[OK]` or `[--]` status and applied-at timestamp. Migrations are
applied automatically by `InitDB` at startup; this command is for
operator visibility into the schema_migrations table.

```bash
./hermem db migrate
# [OK] 001_initial_schema.sql      (2026-06-23T10:00:00)
# [OK] 002_entity_metadata.sql     (2026-06-23T10:00:00)
# [OK] 003_provenance.sql          (2026-06-23T10:00:00)
# [OK] 004_episodic_sessions.sql   (2026-06-23T10:00:00)
```

#### `db schema`

Prints the current schema fingerprint (hash of categories, relations,
stateful config, and state machine settings) and the stored fingerprint
from the database's `meta` table. Warns if they differ.

```bash
./hermem schema
# Current schema fingerprint:  a1b2c3d4e5f6g7h8
# Stored schema fingerprint:   a1b2c3d4e5f6g7h8
```

---

## Request payload reference

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

### `ReEmbedRequest` (`/admin/re-embed`)

| Field       | Type   | Required | Notes                                          |
|-------------|--------|----------|------------------------------------------------|
| `dim`       | int    | yes      | Target embedding dimension.                    |
| `batch_size`| int    | no       | Default 50. Entities per DB transaction.       |
| `model`     | string | no       | New model name for meta table.                 |

### `TaskRollbackRequest` (`/task/rollback`, CLI `task-rollback`)

| Field | Type   | Required | Notes                  |
|-------|--------|----------|------------------------|
| `id`  | string | yes      | Failed task entity ID. |

---

## Response payload reference

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

#### `score_breakdown` — explainability payload

When the retrieval is run in **explain mode** (CLI `hermem memory
explain`, HTTP `POST /query/explain`, or any caller setting
`opts.Explain=true` server-side), each `seed_nodes` entry and each
per-bucket fact carries a `score_breakdown` object with the seven
canonical ranking components:

```json
{
  "score_breakdown": {
    "vector_score":     0.9134,
    "recency_score":    0.7821,
    "temporal_score":   0.6512,
    "centrality_score": 0.3010,
    "path_score":       1.0,
    "depth_penalty":    0.05,
    "final_score":      0.9134
  }
}
```

`final_score` always equals the existing scalar `ranking_score` on the
same node/fact (parity between old and new explain fields). On the
non-explain paths (`/retrieve`, `/query`) the `score_breakdown` field
is **omitted** entirely — the JSON envelope stays byte-compatible for
existing clients.

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

## CLI vs. Server — side-by-side

| Task                | CLI (cobra grouped)                                    | HTTP                                                  |
|---------------------|--------------------------------------------------------|-------------------------------------------------------|
| Store a fact        | `… \| ./hermem memory store`                           | `curl -X POST …/store -d '{…}'`                       |
| Search by query     | `… \| ./hermem memory search`                          | `curl -X POST …/search -d '{…}'`                      |
| Full query → md   | `… \| ./hermem memory query`                           | `curl -X POST …/query -d '{…}'`                       |
| Query with time     | `… \| ./hermem time temporal`                          | `curl -X POST …/query/temporal -d '{…}'`              |
| Ingest dialog       | `… \| ./hermem memory ingest`                          | `curl -X POST …/ingest -d '{…}'`                      |
| Update task status  | `… \| ./hermem task status`                            | `curl -X POST …/task/status -d '{…}'`                 |
| List executable     | `… \| ./hermem task executable` / `task next`          | `curl -X POST …/task/executable`                       |
| Next executable     | `… \| ./hermem task next` (alias of executable)        | `curl -X POST …/task/next`                             |
| List tasks          | `… \| ./hermem task list`                              | `curl -X POST …/task/list`                             |
| Show task           | `… \| ./hermem task show`                              | `curl -X POST …/task/show`                             |
| Task dependency     | `… \| ./hermem task dep`                               | `curl -X POST …/task/dep`                              |
| Create task         | `… \| ./hermem task create`                            | `curl -X POST …/task/create`                           |
| Rollback task       | `… \| ./hermem task rollback`                          | `curl -X POST …/task/rollback`                         |
| Task tree           | `… \| ./hermem task tree`                              | `curl -X POST …/task/tree`                             |
| Verify graph       | `… \| ./hermem graph verify`                           | `curl -X POST …/verify -d '{…}'`                       |
| Timeline            | `./hermem time timeline`                               | `curl …/timeline?limit=N`                             |
| Contradictions      | `./hermem graph contradictions [entity-id]`             | `curl …/contradictions[?id=X]`                        |
| Provenance          | `./hermem graph provenance [--conversation X] [--message M] [--source S] [--limit N]` | `curl …/provenance?conversation_id=X&message_id=Y&source=Z&limit=N` |
| Execution plan      | `./hermem graph plan < req.json`                       | n/a — CLI-only derived view                            |
| Connected components| `./hermem graph components`                            | `curl …/connected-components?min_size=N`              |
| Communities         | `./hermem graph communities`                           | `curl …/communities?min_size=N&max_iterations=N`      |
| Re-embed            | `./hermem memory re-embed [--batch-size N] [--model M]`| `curl -X POST …/admin/re-embed -d '{…}'`               |
| Health              | `./hermem health` (exit 1 on DB unreachable)             | `curl …/health/ready`                                 |
| Metrics             | `./hermem metrics`                                     | `curl …/metrics`                                      |
| Long-running        | No — one-shot per process                              | Yes — single process, multiple requests               |
| Errors              | Exit non-zero + cobra error renderer to stderr         | `HTTP 400` + structured `ErrorResponse` body          |
| Embedding model     | Read from `[embedder] model`                           | Same                                                  |
| DB file             | `[database] path` from `hermem.ini` next to binary      | Same                                                  |
| Strict JSON         | Yes (`httputil.DecodeStrict` / `DisallowUnknownFields`)| Same                                                  |

The CLI uses the **same strict JSON contract as the HTTP server**
(`DisallowUnknownFields` etc.), so a payload that works against
`curl` will work against `echo '…' | hermem …` and vice versa.

Validation is **declarative**: categories and relation types are
enforced via the `[schema]` section. Unknown values return
`422 Unprocessable Entity`. When `[schema]` is absent, classic
defaults apply and the state machine is disabled.
