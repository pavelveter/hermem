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
go build -o hermem .

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
go build -o hermem .
# or with -trimpath for reproducible builds
go build -trimpath -ldflags="-s -w" -o hermem .
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
go test -count=1 ./...                # whole suite green? good
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

[extraction]
model       = qwen2.5-coder:7b
temperature = 0.1

[ingestion]
dedup_threshold = 0.88           # cosine floor for merge-during-ingest

[database]
path = hermem.db                  # SQLite file; created on first store

[retrieval]
depth_ceiling = 5                 # hard clamp on requested max_depth
max_nodes     = 100               # soft cap on nodes per RetrieveContext
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
| `edge`  | `{source_id, target_id, relation_type, auto_create?}` | `{"status":"ok"}` |
| `ingest`| `{dialog}`                       | `{"status":"ok"}`                |
| `serve` | (no stdin; takes optional port)  | logs to stderr                   |

The CLI uses the **same strict JSON contract as the HTTP server**
(`DisallowUnknownFields` etc.), so a payload that works against
`curl` will work against `echo '…' | hermem …` and vice versa.

### `store`

Upsert an entity. The embedder is consulted automatically if
`embedding` is omitted from the payload. After storing, edges are
automatically created to up to 3 existing entities with cosine
similarity > 0.85 (relation type `related_to`).

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
echo '{"source_id":"user-likes-coffee","target_id":"espresso","relation_type":"likes"}' | ./hermem edge

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

| Method | Path       | Body                           | Returns                  |
|--------|------------|--------------------------------|--------------------------|
| GET    | `/health`  | —                              | `{"status":"ok"}`        |
| POST   | `/store`   | `StoreRequest`                 | `{"status":"ok"}`        |
| POST   | `/search`  | `SearchRequest`                | `[{entity, similarity}]` |
| POST   | `/retrieve`| `RetrieveRequest`              | `RetrievalResult` (PascalCase keys — see §8 note) |
| POST   | `/edge`    | `EdgeRequest`                  | `{"status":"ok"}`        |
| POST   | `/ingest`  | `IngestRequest`                | `{"status":"ok"}`        |
| POST   | `/query`   | `QueryRequest`                 | `{"context": "..."}`     |

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
authoritative wire shape. **Known inconsistency:** `RetrievalResult`
struct fields do not yet have explicit `json:"..."` tags, so the
HTTP wire shape currently uses Go's default PascalCase keys
(`SeedNodes`, `WorldFacts`, `Opinions`, `Experiences`,
`Observations`). A future PR will convert these to snake_case
(planned: `seed_nodes`, `world_facts` / `opinions` / `experiences`
/ `observations`) for parity with the `/search` wire shape.

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

---

## 6. CLI vs. Server — side-by-side

| Task                | CLI                                                    | HTTP                                                  |
|---------------------|--------------------------------------------------------|-------------------------------------------------------|
| Store a fact        | `… \| ./hermem store`                                  | `curl -X POST …/store -d '{…}'`                       |
| Search by query     | `… \| ./hermem search`                                 | `curl -X POST …/search -d '{…}'`                      |
| Full query → md   | `… \| ./hermem query`                                  | `curl -X POST …/query -d '{…}'`                       |
| Ingest dialog       | `… \| ./hermem ingest`                                 | `curl -X POST …/ingest -d '{…}'`                      |
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
| `category`   | string      | yes      | One of: `world`, `opinion`, `experience`, `observation`. |
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
| `relation_type` | string  | yes      | One of: `prefers`, `uses`, `mentions`, `related_to`, `part_of`, `causes`, `contradicts`. |
| `auto_create`   | bool    | no       | Default `false`. When `true`, missing entities are auto-created as `category=world` placeholders with embeddings. |

### `IngestRequest` (`/ingest`, CLI `ingest`)

| Field    | Type   | Required | Notes                                       |
|----------|--------|----------|---------------------------------------------|
| `dialog` | string | yes      | Free-form conversational text.             |

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
  "SeedNodes": [
    {
      "entity":        {"id":"paris","category":"world","content":"Paris is the capital of France","embedding":[...], "updated_at":"..."},
      "relations":     [],
      "depth":         0,
      "parent_id":     "",
      "relation_type": "",
      "ranking_score": 0.9134
    }
  ],
  "WorldFacts":   [{"content":"Paris is the capital of France","parent_id":"france","relation_type":"part_of","depth":1}],
  "Opinions":     [],
  "Experiences":  [],
  "Observations": []
}
```

Each per-category bucket (`WorldFacts` / `Opinions` / `Experiences` /
`Observations`) is `[]RetrievedFact` — i.e. an array of
`{content, parent_id, relation_type, depth}` objects. Empty buckets
remain in the output as `[]` (not absent) so consumers can iterate
without nil-checking. `parent_id` and `relation_type` are `omitempty`:
seed-reached facts (`depth == 0`) emit them as empty strings; graph-walk
facts (`depth > 0`) carry them populated so the calling agent can see
why each fact was pulled in. `SeedNodes` is `[]GraphNode` with full
`entity` + composite score.

**Known wire-shape inconsistency:** `RetrievalResult` has no explicit
`json:"..."` tags, so all five top-level keys above marshal with Go's
default PascalCase. `/search` already uses snake_case via
`SearchResult`'s explicit tags; the `/retrieve` conversion is tracked
as a documented TODO item. PascalCase is stable for now — the next
PR will introduce snake_case (`seed_nodes`, `world_facts`, `opinions`,
`experiences`, `observations`) and call out the breaking change in
`CHANGELOG ## [Unreleased] ### Changed`.

(Use `FormatContextMarkdown` server-side or the wrapper at
`/query` to render to LLM-ready markdown.)

### `QueryResponse`

```json
{"context":"## world\n- Paris is the capital of France\n..."}
```

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

A single SQLite file with two tables. The schema lives in
`db.go → InitDB`; below is the field-by-field reference.

### `entities`

| Column       | SQLite type | Notes                                                 |
|--------------|-------------|-------------------------------------------------------|
| `id`         | TEXT PK     | Stable identifier.                                    |
| `category`   | TEXT        | One of `world`/`opinion`/`experience`/`observation`.  |
| `content`    | TEXT        | Free text.                                            |
| `embedding`  | BLOB        | `len(embedding) * 4` raw little-endian float32 bytes. |
| `updated_at` | DATETIME    | `CURRENT_TIMESTAMP` default; refreshed on each upsert.|

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
| `relation_type` | TEXT        | `prefers`, `uses`, `mentions`, `related_to`, `part_of`, `causes`, `contradicts` (canonical allowlist enforced by `filterRelations` in `extractor.go`). |

Composite PK `(source_id, target_id, relation_type)` means duplicate
edges auto-dedupe on insert. There is no `weight` or timestamp column
on edges — weight is implicit (always 1.0 in the current model) and
edge provenance is recovered via `RetrievedFact.parent_id` /
`relation_type` from the graph walk.

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

- **Working directory matters.** `hermem.ini` is read relative to
  `os.Getwd()`. Run `./hermem serve` from the directory that owns
  the ini file, or pass an explicit `--config <path>` when that
  flag lands.
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
- **Graceful shutdown.** `SIGINT`/`SIGTERM` will halt in-flight
  HTTP handlers immediately. There is no drain phase; clients may
  observe truncated responses. Wrap with `nginx`/`caddy` if you
  need zero-downtime reloads.
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
| INI parsing, defaults           | `config.go`                       |
| Schema, embedding serialisation | `db.go`                           |
| Store, search, cosine           | `vector.go`                       |
| Graph walk, ranking, formatting | `retrieval.go`                    |
| Background worker, dedup, edges | `ingestion.go`                    |
| Ollama / OpenAI HTTP            | `embedder.go`, `extractor.go`     |
| HTTP handlers, strict decoder   | `server.go`                       |
| CLI entry-point                 | `main.go`                         |
| Per-package tests               | `*_test.go` (alongside each file) |
