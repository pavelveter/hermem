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
echo '{"query":"What is Go?"}' | ./hermem memory query

# Server mode: long-running HTTP service on :8420.
./hermem serve --port 8420 &
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

[ranking]                          # Sprint 5 — tunable ranking weights
vector_weight         = 0.7       # vector similarity weight (0 = disabled)
recency_weight        = 0.3       # recency decay weight
; recency_half_life_hours = 720   # half-life for exp decay (default 720h ≈ 30d)
; depth_penalty         = 0.05    # linear penalty per hop depth
; temporal_weight       = 0.1     # temporal relevance weight
; temporal_half_life_hours = 720  # half-life for temporal decay
; centrality_weight     = 0.05    # graph centrality boost for hub nodes

[reranker]                         # Sprint 5 — optional post-retrieval reranker
; Follows the same provider convention as [embedder] / [extraction].
; When provider is empty or absent, reranking is skipped.
; provider = ollama               # "ollama" (cross-encoder) | "openai" (chat-based)
; url = http://localhost:11434
; model = mxbai-rerank-base
; key =                           # API key (only needed for openai)
; timeout = 30s

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

### 4.1 Command tree (Cobra grouped grammar)

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

### 4.2 Request/response reference (per-command)

Request / response shapes are unchanged from the pre-Cobra release
(`DisallowUnknownFields` strict-decode on the wire), but the invocations
now use the grouped grammar.

#### `hermem memory store`

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

#### `hermem memory search`

Returns the top-K entities by cosine similarity to the embedded query.

```bash
echo '{"query":"coffee preferences","top_k":3}' | ./hermem memory search
```

`top_k` defaults to 5 when omitted or ≤ 0. Output is a JSON array of
`{entity, similarity}` objects sorted descending by `similarity`.

#### `hermem memory query`

Full pipeline: embed → vector search → graph walk → markdown render.

```bash
echo '{"query":"Tell me about France"}' | ./hermem memory query
# → {"context":"## world\n- Paris is the capital of France\n…"}
```

`MaxDepth` for the graph walk uses the value from `[retrieval]`
(`depth_ceiling` is the hard clamp; the CLI always uses 2 by default).

#### `hermem memory edge`

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

#### `hermem memory ingest`

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

#### `hermem task …`

The full task lifecycle group is JSON-stdin driven. Examples:

```bash
# Update status
echo '{"id":"step-1","status":"running"}' | ./hermem task status

# List executable globally (cobra `--help`-equivalent URL query
# param behaviour applies via stdin JSON body for goal-scoped view).
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

#### `hermem graph …`

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

#### `hermem time …`

```bash
echo '{"query":"user beliefs about Go","time_from":"2026-03-01T00:00:00Z","time_to":"2026-04-01T00:00:00Z","top_k":5}' \
  | ./hermem time temporal
./hermem time timeline                                # last 50 entities
```

#### `hermem agent loop`

```bash
echo '{"goal_id":"goal-1"}' | ./hermem agent loop
# Yields one line per task: [<id>] <content>  [<category>]
```

#### `hermem db …`

```bash
./hermem db migrate                  # migration status
./hermem db rollback                 # roll back most-recent applied migration
./hermem db verify                   # checksum integrity (exit 1 on mismatch)
./hermem db schema                   # stored vs current schema fingerprint
```

### `db migrate`

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

### `schema`

Prints the current schema fingerprint (hash of categories, relations,
stateful config, and state machine settings) and the stored fingerprint
from the database's `meta` table. Warns if they differ.

```bash
./hermem schema
# Current schema fingerprint:  a1b2c3d4e5f6g7h8
# Stored schema fingerprint:   a1b2c3d4e5f6g7h8
```

The CLI uses the **same strict JSON contract as the HTTP server**
(`DisallowUnknownFields` etc.), so a payload that works against
`curl` will work against `echo '…' | hermem …` and vice versa.

Validation is **declarative**: categories and relation types are
enforced via the `[schema]` section. Unknown values return
`422 Unprocessable Entity`. When `[schema]` is absent, classic
defaults apply and the state machine is disabled.### `store`

Upsert an entity. The embedder is consulted automatically if
`embedding` is omitted from the payload. After storing, edges are
automatically created to up to 3 existing entities with cosine
similarity > 0.85 (relation type `related_to`). Unknown category
→ `422 Unprocessable Entity`.

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

### `search`

Returns the top-K entities by cosine similarity to the embedded query.

```bash
echo '{"query":"coffee preferences","top_k":3}' | ./hermem memory search
```

`top_k` defaults to 5 when omitted or ≤ 0. Output is a JSON array of
`{entity, similarity}` objects sorted descending by `similarity`.

### `query`

Full pipeline: embed → vector search → graph walk → markdown render.

```bash
echo '{"query":"Tell me about France"}' | ./hermem memory query
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
echo '{"source_id":"user-likes-coffee","target_id":"espresso","relation_type":"prefers"}' \
  | ./hermem memory edge

# Auto-create missing entities on the fly:
echo '{"source_id":"user-likes-coffee","target_id":"new-concept","relation_type":"related_to","auto_create":true}' \
  | ./hermem memory edge
```

### `ingest`

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
ExecStart=/usr/local/bin/hermem serve --port 8420
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
| GET | `/health/live` | — | `{"status":"ok"}` |
| GET | `/health/ready` | — | `{"status":"ok","latency_ms":12,"checks":{...}}` or `{"status":"degraded",...}` (503) |
| GET | `/health/startup` | — | `{"status":"ok"}` |
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
| GET | `/contradictions` | — (opt `?id=X`) | `[{"source_id","source_content","target_id","target_content"}]` |
| POST | `/query/temporal` | `{query, time_from?, time_to?, top_k?}` | `RetrievalResult` (time-filtered) |
| GET | `/timeline` | — (opt `?limit=N`) | `[{"id","category","content","created_at",...}]` |
| GET | `/provenance` | query params: `conversation_id`, `message_id`, `source`, `limit` | `[Entity, ...]` (provenance fields populated) |
| GET | `/recovery-plan` | query param: `?id=X` | `[Entity, ...]` (ordered recovery chain) |
| GET | `/connected-components` | query param: `?min_size=N` | `[{"ids","size","avg_degree"}, ...]` |
| GET | `/communities` | query params: `?min_size=N&max_iterations=N` | `{"communities","global_modularity","total_communities"}` |
| POST | `/admin/re-embed` | `{dim, batch_size?, model?}` | `ReEmbedResult` (total, re_embedded, failed, elapsed, ...) |

Every POST endpoint goes through a strict JSON decoder; fields not in
the request schema are rejected with `400`. See §9 for the error
shape and the full list of codes.

### `/health`

Basic liveness check — no DB hit beyond the open connection. Always returns 200.

```bash
curl -s http://localhost:8420/health
# → {"status":"ok"}
```

### `/health/live`

Kubernetes-style liveness probe. Always returns 200 — used by orchestrators
to decide whether to restart the container.

```bash
curl -s http://localhost:8420/health/live
# → {"status":"ok"}
```

### `/health/ready`

Readiness probe — runs every registered dependency check (DB, vector index,
embedder, LLM extractor, disk space) with individual timeouts and severity
levels. Returns 200 if all **critical** checks pass (warning-level failures
are tolerated), 503 when any critical check fails.

```bash
curl -s http://localhost:8420/health/ready
# → {"status":"ok","latency_ms":12,"checks":{
#     "database":     {"ok":true,"latency_ms":3,"critical":true},
#     "vector_index": {"ok":true,"latency_ms":2,"critical":true},
#     "embedder":     {"ok":true,"latency_ms":6,"critical":true},
#     "extractor":    {"ok":true,"latency_ms":0,"critical":false},
#     "disk_space":   {"ok":true,"latency_ms":1,"critical":true}
#   }}

# Degraded (503) — critical dependency down:
# → {"status":"degraded","latency_ms":5,"checks":{
#     "database":{"ok":false,"latency_ms":3,"error":"unreachable: ...","critical":true},
#     ...
#   }}
```

Use this as your Kubernetes `readinessProbe` target. The `/health/ready`
endpoint replaces the old single-DB-ping shape with full per-dependency
breakdown. Each check has a `critical` flag; only critical failures
produce a 503 response.

### `/health/startup`

Startup probe — returns 200 as soon as the process binds to its port,
before `/health/ready` becomes green. Does not check any dependency;
use this as your Kubernetes `startupProbe` target so the pod is marked
ready only after all critical probes pass.

```bash
curl -s http://localhost:8420/health/startup
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

### `/query/temporal`

Full pipeline (embed → search → graph walk → markdown) filtered by time range.
`time_from` and `time_to` use RFC3339 format. Both are optional — omit to
leave that bound unbounded.

```bash
curl -s -X POST http://localhost:8420/query/temporal \
  -H 'Content-Type: application/json' \
  -d '{"query":"user beliefs about Go","time_from":"2026-03-01T00:00:00Z","time_to":"2026-04-01T00:00:00Z","top_k":5}'
```

Response shape is identical to `/query` — the markdown context only includes
entities whose `created_at` falls within the time window.

### `/timeline`

Returns entities ordered by `created_at` DESC, grouped by category with
provenance fields. Optional `?limit=N` (default 50).

```bash
curl -s http://localhost:8420/timeline?limit=10
# → [{"id":"...","category":"observation","content":"...","created_at":"2026-06-24T10:00:00Z",
#     "source":"dialog","conversation_id":"conv-1","message_id":"msg-3"}, ...]
```

### `/provenance`

Query entities by memory origin.

```bash
curl -s "http://localhost:8420/provenance?conversation_id=conv-1&limit=10"
# → [{"id":"entity-1","category":"world","content":"...","conversation_id":"conv-1",...}]
```

### `/recovery-plan`

Walk `recovers_via` chain from a failed task.

```bash
curl -s "http://localhost:8420/recovery-plan?id=step-1"
# → [{"id":"step-recover-1","category":"task","content":"Rollback","status":"pending"}, ...]
```

### `/connected-components`

Find connected components in the graph.

```bash
curl -s "http://localhost:8420/connected-components?min_size=3"
# → [{"ids":["a","b","c"],"size":3,"avg_degree":2.5}, ...]
```

### `/communities`

Louvain community detection with modularity scoring.

```bash
curl -s "http://localhost:8420/communities?min_size=3&max_iterations=100"
# → {"communities":[{"id":"community-...","members":[...],"size":5,"modularity":0.123}],"global_modularity":0.452,"total_communities":12}
```

### `/admin/re-embed`

Trigger background re-embedding of all entities.

```bash
curl -s -X POST http://localhost:8420/admin/re-embed \
  -H "Content-Type: application/json" \
  -d '{"dim":768,"batch_size":100}'
# → {"total_entities":1500,"re_embedded":1500,"failed":0,"elapsed":"45.2s",...}
```

### `/contradictions`

Lists all `contradicts` edges in the graph. Optional `?id=X` filters to
contradictions involving a specific entity (checked bidirectionally).

```bash
# all contradictions
curl -s http://localhost:8420/contradictions
# → [{"source_id":"e1","source_content":"User likes Go",
#     "target_id":"e2","target_content":"User hates Go"}]

# filter by entity
curl -s "http://localhost:8420/contradictions?id=e1"
```

---

## 6. CLI vs. Server — side-by-side

CLI invocations use the new Cobra grouped grammar (commit `8f0bf71`).
HTTP endpoints are unchanged. Both share the same `DisallowUnknownFields`
strict-decode contract.

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
| `degree`        | INTEGER     | `0` default; auto-maintained by SQL triggers on edges INSERT/DELETE. Powers `log10(1+degree)` centrality scoring. |
| `priority`      | INTEGER     | `0` default; `task/list` + `task/executable` + `ExecutionPlan` order by `priority DESC`. Added in migration `007_task_priorities.sql`. |
| `degree`        | INTEGER     | `0` default; auto-maintained by SQL triggers on edges INSERT/DELETE. Powers `log10(1+degree)` centrality scoring in the ranker (`[ranking] centrality_weight`). |
| `priority`      | INTEGER     | `0` default; `task/list` + `task/executable` + `ExecutionPlan` order by `priority DESC`. Added in migration `007_task_priorities.sql`. |

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
| `weight`        | REAL        | `1.0` default; added in migration `006_weighted_edges.sql`. Used by CTE `path_weight` accumulator and the ranker's `compositeScore`. Read with `COALESCE(weight, 1.0)`. |
| `weight`        | REAL        | `1.0` default; added in migration `006_weighted_edges.sql`. Used by the CTE `path_weight` accumulator and the ranker's `compositeScore` (which uses `pathWeight` instead of integer `depth` for the penalty term). |

Composite PK `(source_id, target_id, relation_type)` means duplicate
edges auto-dedupe on insert. `weight` defaults to `1.0` for every
write path (auto-link `related_to` >0.85 cosine from `/store`, bulk
merge edges from `ProcessDialogWithProvenance`, manual `memory edge`).
All reads use `COALESCE(weight, 1.0)` so a hand-edited legacy row never
crashes a multiplier path. Edge provenance is recovered via
`RetrievedFact.parent_id` / `relation_type` from the graph walk.

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

Hermem uses a versioned migration system (`schema_migrations` table).
Each embedded SQL migration in `src/migrations/` is tracked with an
applied-at timestamp. `InitDB` applies unapplied files in ordered
transactions at startup; `hermem migrate` shows status for operator
visibility. To change the schema, write a new migration file and
re-build.

For full embedder-model switches, write a new `hermem.db` and
re-ingest (`hermem ingest` against every persisted dialog is
sufficient; the embedded text regenerates).

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
  Send `SIGHUP` to the server process (`kill -HUP <pid>`) to
  reload `hermem.ini` without restart. Schema, categories, and
  relations update atomically across all in-flight handlers.
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
| INI parsing, defaults, Validate | `src/internal/config/ini.go` + `src/internal/config/config.go` (defaults) |
| Schema, embedding serialisation | `src/internal/store/migration.go` (migrations runner) + `src/internal/store/init.go` (DSN + PRAGMAs) |
| VectorIndex interface, search backends (InMemory / SqliteVec) | `src/internal/vector/index.go` (interface) + `src/internal/vector/inmemory.go` + `src/internal/vector/quantize.go` |
| Graph walk, ranking, formatting | `src/internal/retrieval/{walk,scoring,formatting,response,tasks}.go` |
| Background worker, dedup, edges | `src/internal/ingestion/worker.go` (IngestionWorker) + `src/internal/ingestion/dialog.go` (ProcessDialog) |
| Contradiction detection        | `src/internal/contradiction/` (domain Service) + HTTP shell in `src/internal/server/contradiction/` |
| Community detection (Louvain)  | `src/internal/store/community.go` |
| Background re-embedding        | `src/internal/reembed/` (domain Service) + HTTP shell in `src/internal/server/reembed/` |
| Graph verify (integrity check) | `src/internal/graph/service.go::Verify` |
| Agent loop + execution plan    | `src/internal/orchestrator/service.go::AgentLoop` + `::ExecutionPlan` |
| Ollama / OpenAI HTTP (ResilientClient-wrapped) | `src/internal/ai/{client,embedder,extractor,reranker}.go` |
| HTTP handlers, strict decoder   | `src/internal/server/server.go` (mux shell) + `src/internal/server/middleware.go` + `src/internal/httputil/httputil.go::DecodeStrict` |
| Config state, hot reload        | `src/internal/serverstate/state.go` (`atomic.Pointer[State]`) + `src/internal/cli/env/env.go::EnvManager` |
| CLI dispatch (Cobra root)       | `src/internal/cli/root.go`        |
| CLI helpers, runtime Env        | `src/internal/cli/env/env.go`     |
| CLI subcommand groups          | `src/internal/cli/{memory,task,graph,time,agent,db}/<sub>.go` |
| Top-level CLI (`serve`, `health`, `metrics`, `version`) | `src/internal/cli/{serve,health,metrics,version}.go` |
| Binary entry-point              | `src/main.go`                     |
| Retention GC loop               | `src/internal/retention/` (domain Service) + `src/internal/server/retention/` (HTTP) |
| Health probes                   | `src/internal/health/` (domain Service) + `src/internal/server/health/` (HTTP) |
| Accelerate SIMD cosine (darwin) | `src/internal/vector/cosine_darwin.go` (build-tag `darwin && cgo`) |
| Pure-Go cosine fallback         | `src/internal/vector/cosine.go`   (build-tag `!darwin || !cgo`) |
| Coch-Granger cyclic-task safe scheduler   | `src/internal/store/task.go::BuildNode` (iterative work-stack DFS) |
| NaN/Inf-safe embedding read     | `src/internal/store/codec.go::BytesToFloat32Safe` |
| Per-package tests               | `src/**/*_test.go`                |
| Per-domain model projection contracts | `src/internal/core/{fact,evidence,episode,task,goal,belief}_test.go` (4 each) + `compose_test.go` (4) + `pairs_test.go` (35 subtests) — `go test -race ./src/internal/core/...` (64 total) |

---

## 15. Domain Models

Hermem’s persistence layer is anchored on `core.Entity`, a 19-field
struct that maps onto the underlying SQLite `entities` row. The 19
fields decompose into 5 per-domain model types — each carrying one
orthogonal concern — plus a derived view (Goal) that re-views one
of those types with intent.

For new code, prefer the per-domain types (`Fact`, `Evidence`,
`Episode`, `Task`, `Belief`) and use the `Compose`/`Decompose`
helpers when `Entity` is the only available interface. Existing
code continues to operate on `Entity` directly — **no breaking
change**. Internal packages migrate to per-domain types at their
own pace.

### The 5 per-domain models

| Model      | Fields | Purpose                                              |
|------------|--------|------------------------------------------------------|
| `Fact`     | 4      | The semantic claim — content + embedding.            |
| `Evidence` | 3      | Quality meta — confidence + source + source type.    |
| `Episode`  | 3      | Provenance — conversation / message / extraction origin. |
| `Task`     | 4      | Lifecycle — status + validity window + priority.    |
| `Belief`   | 5      | Persistence / retention / centrality — timestamps + archived + degree. |
| `Goal`     | 0 new  | Re-views `Task`’s shape with `Category = "goal"` (service-layer intent). |

Total: 4 + 3 + 3 + 4 + 5 = **19 fields**. These are the same 19
fields on `Entity`. Goal adds no new field; it re-views Task’s
shape with intent.

### Entity is the umbrella persistence view

`Entity` is what the SQLite `entities` table stores and what
`store.go` reads into. All 19 fields live on this single struct.
Whenever a 19-field row is the only available representation, work
with `Entity` and decompose on demand.

### Decompose / Compose

Each band has a typed projection method on `Entity`:

```go
fact := entity.AsFact()       // Fact{ID, Category, Content, Embedding}
ev   := entity.AsEvidence()   // Evidence{Confidence, Source, SourceType}
ep   := entity.AsEpisode()    // Episode{ConversationID, MessageID, ExtractedFrom}
tk   := entity.AsTask()       // Task{Status, ValidFrom, ValidTo, Priority}
b    := entity.AsBelief()     // Belief{CreatedAt, UpdatedAt, LastAccessedAt, Archived, Degree}
g    := entity.AsGoal()       // Goal{Status, ValidFrom, ValidTo, Priority}
                              // — Task shape with Category="goal" intent
```

Each per-domain model has the inverse `AsEntity()`. To reassemble
a complete `Entity` from the 5 per-domain projections:

```go
entity := core.Compose(fact, evidence, episode, task, belief)
```

`Compose` is a free function (not a method on `Entity`) — the
receiver would be unused. Field-by-field, no shared mutable state.

### Goal reduces through Task

Goal exposes no `Goal.AsTask()` method by design. Callers that
want to bridge Goal into a Task-positioned slot (e.g. when calling
`Compose`) inline-copy the 4 lifecycle fields:

```go
task := core.Task{
    Status:    goal.Status,
    ValidFrom: goal.ValidFrom,
    ValidTo:   goal.ValidTo,
    Priority:  goal.Priority,
}
entity := core.Compose(fact, ev, ep, task, b)
```

The pattern is documented once in `compose.go` and locked by
`TestGoal_ReducesToTask` in `goal_test.go`.

### When to use which type

| Use case                          | Type                            |
|-----------------------------------|---------------------------------|
| New persistence code              | per-domain model                |
| Graph traversal edge code         | compose-decompose at boundary   |
| SQLite scan / store               | `Entity` (umbrella view)        |
| HTTP request/response bodies      | `Entity` (umbrella view)        |
| Internal service (band work)      | per-domain type                 |

The cross-pair projection matrix in `pairs_test.go` locks the
orthogonal-band invariant: every ordered pair (X, Y) yields equal
band values regardless of which X was round-tripped through.
Pointer identity is preserved on `*time.Time` fields for self-pairs
(`X == Y`) and lost (zeroed) for cross-pairs (`X != Y`).

---

## 16. API Authentication

Hermem supports **scoped multi-key authentication** for the HTTP server.
Each key carries one of three scopes that control which endpoints it can
access.

### Scopes (most → least permissive)

| Scope    | Access                                                          |
|----------|-----------------------------------------------------------------|
| `admin`  | All endpoints, including `/admin/*`                             |
| `write`  | Read + write endpoints (`/ingest`, `/store`, etc.)              |
| `read`   | Read-only endpoints (`/search`, `/retrieve`, `/query`)          |

Unmatched URL paths default to `ScopeWrite`.

### Configuration

In `hermem.ini`, under the `[server]` section:

```ini
[server]
api_keys = hermes-key-abc123:admin:ci-bot, hermes-key-def456:write, hermes-key-789ghi:read:readonly-app
```

Format: comma-separated entries of `key:scope:label` (label is optional).

#### Legacy single-key (backward-compatible)

```ini
[server]
api_key = hermes-key-abc123
```

A lone `api_key` gets `ScopeAdmin`. If both `api_key` and `api_keys`
are present, `api_key` wins with a startup warning.

### Usage

All authenticated requests must include the `X-API-Key` header:

```bash
curl -H "X-API-Key: hermes-key-abc123" http://localhost:8420/search
```

### Response codes

| Code | Meaning                                  |
|------|------------------------------------------|
| 401  | Missing, invalid, or revoked key         |
| 403  | Key is valid but scope is insufficient   |
| 200  | Allowed                                  |

### Health endpoints bypass auth

`/health`, `/health/live`, `/health/ready`, `/health/startup` — no
`X-API-Key` required.

### Admin CLI

```bash
hermem admin keys list                  # show all keys (masked)
hermem admin keys add --scope write     # generate + add new key
hermem admin keys rotate <label>        # replace key value, keep scope+label
hermem admin keys revoke <label>        # remove key by label
```

`add` creates a 32-byte cryptographically random key, hex-encoded
(64 characters), and writes it to `hermem.ini` on the `api_keys` line.

### Key architecture

- `src/internal/auth/auth.go` — Scope type, Key struct, Authenticator interface
- `src/internal/auth/scope.go` — CanAccess hierarchy, ScopeForPath, RequiredScopes
- `src/internal/auth/static.go` — StaticAuthenticator (constant-time comparison)
- `src/internal/server/middleware.go` — AuthMiddleware (parameterless, health bypass)
- `src/internal/config/ini.go` — api_keys parsing (key:scope:label)
- `src/internal/config/update.go` — AddKeyToFile, RemoveKeyFromFile, RotateKeyInFile
- `src/internal/cli/admin/keys.go` — CLI subcommands, GenerateKey, MaskKey

---

## 18. Admin Operations

The `hermem ops` group provides offline database diagnostics and
maintenance — no HTTP server required.

### Commands

| Command             | Description                                         |
|---------------------|-----------------------------------------------------|
| `hermem ops stats`  | Print entity/edge/contradiction counts + embedding coverage |
| `hermem ops integrity` | Run integrity checks: missing embeddings, dangling edges, archive consistency |
| `hermem ops vacuum` | Reclaim disk space via SQLite VACUUM               |
| `hermem ops rebuild-index` | Re-generate embeddings and re-index entities |

### Examples

```bash
# Statistics
hermem ops stats                    # tabular output
hermem ops stats --json             # machine-readable JSON

# Integrity checks
hermem ops integrity                # exit 0 (OK), 1 (critical issue)
hermem ops integrity --json         # machine-readable JSON
hermem ops integrity --fail-on-warning  # exit 2 on warnings

# Vacuum (SQLite VACUUM)
hermem ops vacuum                   # with progress bar
hermem ops vacuum --no-progress     # silent mode

# Rebuild vector index
hermem ops rebuild-index --dry-run                          # preview only
hermem ops rebuild-index --category fact                    # by entity category
hermem ops rebuild-index --since 2026-01-01                 # recent only
hermem ops rebuild-index --only-archived                    # archived entities only
```

### Integrity issue codes

| Code                  | Level    | Meaning                                         |
|-----------------------|----------|-------------------------------------------------|
| `MISSING_EMBEDDING`   | warning  | Few entities without embeddings (<10)            |
| `MISSING_EMBEDDING`   | critical | ≥10 entities without embeddings                 |
| `DANGLING_EDGE`       | critical | Edge references a non-existent entity           |
| `ARCHIVE_CONSISTENCY` | warning  | Archived entity still has embedding in vector index |

### Exit codes

| Code | Meaning                          |
|------|----------------------------------|
| 0    | OK (no critical issues)          |
| 1    | Critical integrity issue found   |
| 2    | Warning-level issue (--fail-on-warning) |

### Cron recommendation

Run `hermem ops integrity` weekly to catch drift early:

```bash
0 3 * * 1 cd /opt/hermem && hermem ops integrity --fail-on-warning
```
