# Hermem — Server Reference

Full reference for the Hermem HTTP server mode. Covers startup,
endpoints, error model, and authentication.

For CLI reference, see [CLI.md](CLI.md). For configuration, see
[USAGE.md](USAGE.md). For production operations, see
[RUNBOOK.md](RUNBOOK.md).

---

## Starting the server

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

---

## Endpoints

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
the request schema are rejected with `400`. See [Error model](#error-model)
for the error shape and the full list of codes.

---

## Endpoint details

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
Response is the full `RetrievalResult` — see [CLI.md](CLI.md) for the
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
    {"id":"step-recover-1","status":"pending"}
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

## Error model

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

## API Authentication

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
