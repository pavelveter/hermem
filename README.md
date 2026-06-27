<p align="center">
  <img src="banner.jpg" alt="Hermem" width="600">
</p>

# Hermem

A lightweight, single-binary graph memory system for LLM agents. Stores facts as a directed graph in SQLite with vector embeddings for semantic retrieval.

**Use case:** Give your agent persistent memory across sessions — it remembers what it learned, who you are, what worked, and what didn't.

## Architecture

```
User Query ──> [Embedder] ──> [Vector Search] ──> Top-K Seeds ──> [CTE Graph Walk] ──> Markdown Context
```

The system stores knowledge as entities (nodes) connected by typed edges. Each `Entity` is a 19‑field umbrella persistence view that decomposes into 5 per-domain models on demand (`Fact` / `Evidence` / `Episode` / `Task` / `Belief`) plus a `Goal` view that re‑views `Task`’s shape with intent. New code prefers the per‑domain types; existing code continues to use `Entity` directly. See **[docs/USAGE.md §15](docs/USAGE.md#15-domain-models)** for the model map, Compose/Decompose helpers, and the Goal‑reduces‑through‑Task contract.

All 12 domain services use constructor injection — no singletons, no global mutable state, no service locators. Schema is passed per-call so SIGHUP hot-reload swaps config atomically without reconstructing services. See **[docs/service-dependencies.md](docs/service-dependencies.md)** for the full dependency graph, HTTP shell wiring matrix, and data flow diagram.

Each entity belongs to a category defined in `[schema]`:

| Category (default) | Purpose |
|--------------------|---------|
| `world` | Facts, definitions, objective knowledge |
| `opinion` | User preferences, beliefs, subjective views |
| `experience` | Past events, interactions, what happened |
| `observation` | Patterns noticed, anomalies, derived insights |
| `task` (example) | Actionable work items, steps, to-dos with status tracking |

## Features

- **CLI + HTTP server** — single binary, two modes
- **OpenAI-compatible** — works with Ollama or any OpenAI-compatible API
- **Separate embedder/extractor providers** — Ollama for embeddings, OpenAI for extraction (or vice versa)
- **Pluggable vector search** — `InMemoryVectorIndex` (default, pure-Go brute-force) or `SqliteVecIndex` via `sqlite-vec` (indexed KNN)
- **Accelerate SIMD** — `cblas_sgemv` via CGo for AMX-optimised batch dot products on Apple Silicon
- **Automatic retention** — configurable GC loop archives stale observation nodes
- **API key auth** — optional `X-API-Key` middleware
- **Structured logging** — `log/slog` with event fields + `request_id` tracing
- **Request tracing** — every HTTP response gets `X-Request-ID`
- **Metrics** — `/metrics` endpoint via `expvar`
- **Graceful shutdown** — drains in-flight requests on SIGINT/SIGTERM
- **Strict JSON validation** — unknown fields rejected with structured errors
- **State-on-Graph (Batch 9)** — stateful entities with `status`, configurable dependency relations, CTE-based executable-node walk, rollback lookup, `/task/status` + `/task/executable` HTTP endpoints
- **Declarative schema** — categories, relation types, FSM rules defined in `hermem.ini` `[schema]`; no recompilation needed
- **Foreign-key enforcement** — FK constraints on edges prevent orphan references at the SQL engine layer; ingestion wraps entity+edges in atomic per-item transactions
- **Graph integrity verify** — `hermem graph verify` checks entities, edges, embeddings, corrupt blobs, orphan edges, invalid status/relation types (exit 1 on problems)
- **Retrieval explainability** — `/query/explain` endpoint returns a `score_breakdown` object per retrieved fact and seed node carrying the seven canonical ranking components (`vector_score`, `recency_score`, `temporal_score`, `centrality_score`, `path_score`, `depth_penalty`, `final_score`); non-explain paths omit the breakdown and stay byte-compatible
- **Per-domain Entity decomposition** — 5 typed models (Fact, Evidence, Episode, Task, Belief) projected from the 19‑field umbrella `Entity`; `core.Compose(…)` reassembles; 64 contract tests lock orthogonal‑band semantics. Goal re‑views Task’s shape with no new field.
- **Contradiction detection** — heuristic `isContradiction` detects conflicting statements at ingest; prevents merging, creates `contradicts` edges instead
- **Temporal retrieval** — `/query/temporal` endpoint filters graph walk by time range (`time_from`/`time_to` RFC3339)
- **Episodic memory** — `/timeline` endpoint returns entities ordered by `created_at` DESC with provenance
- **Memory provenance** — tracks `conversation_id`, `message_id`, `extracted_from` per entity; entity metadata (`confidence`, `source`, `source_type`, temporal validity)
- **Graph centrality** — `degree` column on entities (auto-maintained via SQL triggers on edges); `log10(1+degree)` scoring boosts hub nodes
- **Weighted edges** — `weight` column on edges (default 1.0); `path_weight` accumulation in CTE graph walk replaces integer depth for penalty
- **Provenance APIs** — `GET /provenance?conversation_id=X&message_id=Y&source=Z` returns entities by memory origin
- **Task priorities** — `priority` column on stateful entities; `ExecutionPlan` and `GetExecutableTasks` order by priority DESC
- **Critical path analysis** — `CriticalPath(db, schema, goalID)` walks the longest weighted path from leaf to goal
- **Recovery plans** — `GenerateRecoveryPlan` follows `recovers_via` chains; `GET /recovery-plan?id=X` HTTP endpoint
- **Graph clustering** — `FindConnectedComponents` BFS-based connected components; `GET /connected-components?min_size=N`
- **Community detection** — Louvain one-pass modularity optimisation; `hermem graph communities` CLI + `GET /communities` HTTP
- **Background re-embedding** — `ReEmbedAll` batch re-embeds all entities after model/dim change; `hermem memory re-embed [--batch-size N] [--model M]` CLI + `POST /admin/re-embed` HTTP
- **Vector quantization** — `QuantizeVector` / `DequantizeVector` scalar int8 compression (4× storage reduction); `hermem memory quantize` (stdin) CLI
- **Docker** — multi-stage build, non-root user
- **Zero global mutable state** — all services use constructor injection; `ActiveSchema()` singleton removed; package-level variables audited and documented
- **Local embedding** — `llama-embedding` binary + dylibs embedded via `go:embed`; no external dependencies for embedding (extracts to temp dir at runtime)

## CLI Commands

Cobra-grouped grammar (`git` / `kubectl` style). Every command reads its
payload from stdin; `hermem <group> --help` shows only that group's
commands. Top-level commands: `serve`, `health`, `metrics`, `version`.

> **Breaking change (commit `8f0bf71`):** the previously-flat 26-command
> surface is gone. There are no back-compat aliases. Any script that
> ran `hermem store`, `hermem task-status`, `hermem migration-rollback`,
> etc. must be rewritten to the grouped form below.

```bash
# Top-level
hermem serve [--port 8420]              HTTP server (SIGHUP reloads hermem.ini)
hermem health                           DB ping (mirrors /health/ready, exit 1 on fail)
hermem metrics                          Prometheus exposition
hermem version                          ldflags build metadata
hermem diagnose [--json]                Self-diagnostics (schema, vectors, beliefs)
hermem bench [--iterations N] [--json]  Latency percentiles for each subsystem

# API-key management (auth v2 — multi-key, scoped)
hermem admin keys list                   List API keys (masked) + scopes/labels
hermem admin keys add [--scope S]        Generate a new key (32-byte CSPRNG → 64 hex)
hermem admin keys rotate <label>         Issue a new value, retain label/scope
hermem admin keys revoke <label>         Remove a key from hermem.ini

# Offline ops (admin maintenance)
hermem ops stats                         Node/edge counts, embedding coverage, last GC
hermem ops integrity [--fail-on-warning] Exit 1 on integrity issues
hermem ops vacuum                        VACUUM with progress + bytes-reclaimed report
hermem ops rebuild-index                 [--category C] [--since D] [--only-archived] [--dry-run]

# Opt-in runtime profiling (off by default)
hermem profile cpu      [N]              CPU profile (default 10s) → protobuf via stdout
hermem profile heap                      Heap snapshot → /tmp/hermem-heap.pprof
hermem profile goroutine                 Goroutine dump (text) → stdout
hermem profile trace    [N]              Execution trace (default 10s) → /tmp/hermem-trace.out
# Server-side: set HERMEM_PPROF_ENABLED=1 to expose /debug/pprof/*

# Memory CRUD + retrieval
hermem memory store        < req.json   Upsert entity (id/category/content + opt embedding)
hermem memory search       < req.json   Top-K cosine neighbours (default top_k=5)
hermem memory retrieve     < req.json   Graph walk from explicit seed_ids
hermem memory query        < req.json   embed → search → walk → markdown context
hermem memory response     < req.json   Full pipeline + LLM-generated response
hermem memory edge         < req.json   Add typed edge (body.auto_create=true creates endpoints)
hermem memory ingest       < req.json   LLM-extract + dedup-merge entities from dialog
hermem memory explain      < req.json   Retrieval with score breakdown per fact
hermem memory re-embed     [--batch-size N] [--model M]   Batch re-embed all entities
hermem memory quantize     < req.json   Scalar int8 roundtrip + compression stats

# Task lifecycle
hermem task status         < req.json   Update task status (pending/running/completed/failed)
hermem task list           < req.json   Filter by status / goal_id
hermem task show           < req.json   task + blocked_by + recovers_via relations
hermem task dep            < req.json   add/remove a dependency edge
hermem task tree           < req.json   ASCII tree under a goal_id
hermem task create         < req.json   Auto-embed + assign first stateful category
hermem task rollback       < req.json   Find recovers_via companion task
hermem task next           [{}]         Executable tasks (alias: task executable)
hermem task executable     [{}]         Same as `task next`

# Graph analytics
hermem graph plan          < req.json   Topologically-sorted plan under goal_id
hermem graph recovery-plan < req.json   recovers_via chain walk from failed task
hermem graph components                  Connected components (size ≥ 2)
hermem graph communities                 Louvain community detection + global modularity
hermem graph verify                      Integrity check (exit 1 on problems)
hermem graph contradictions [entity-id]  Optional positional ID filter
hermem graph provenance [--conversation X] [--message M] [--source S] [--limit N]

# Temporal
hermem time temporal        < req.json  Time-windowed retrieval (time_from/time_to RFC3339)
hermem time timeline                    Recent 50 entities by created_at DESC

# Agent flows
hermem agent loop           < req.json  algo.AgentLoop on a goal_id (yields each task)

# Database ops
hermem db migrate                       Migration status (applied / pending, per-row SHA-256)
hermem db dry-run                       List pending migrations without applying
hermem db rollback [--target N]          Roll back recent (or up to a target version)
hermem db verify                        Checksum integrity check (per-mismatch breakdown)
hermem db schema                        Stored vs current schema fingerprint
```

All `memory`/`task`/`graph`/`time`/`agent` commands that read structured
input require JSON on **stdin** (`echo '{...}' | hermem <group> <cmd>` or
`hermem <group> <cmd> < req.json`). Cobra flags (`--port`, `--batch-size`,
`--conversation`, `--limit`, etc.) use Go-style `--name value` syntax
and are documented under each command via `hermem <group> <cmd> --help`.

## Quick Start

```bash
# Clone and build
git clone https://github.com/pavelveter/hermem.git
cd hermem
make build        # works with or without local embedding binary
# or: go build -o hermem ./src   # same as make build

# Inspect the command tree (top-level + 6 groups)
./hermem --help
```

The pre-cobra default `./hermem` was a `store → query` smoke demo; it no
longer creates a DB on its own. New smoke sequence after build:

```bash
./hermem serve --port 8420 &      # boot HTTP server (background)
curl -s http://localhost:8420/health/ready   # → {"status":"ok"}
echo '{"id":"hello","category":"world","content":"hello world"}' \
  | ./hermem memory store           # creates hermem.db on first store
```

For one-shot CLI use without a server, see [USAGE.md §4.2](docs/USAGE.md#4-cli-mode-runbook).

## Installation

### For Hermes Agent (recommended)

One command installs everything — binary, plugin, and config:

```bash
curl -fsSL https://raw.githubusercontent.com/pavelveter/hermem/main/install.sh | bash
```

Or install manually:

```bash
# 1. Build the binary
go build -o hermem ./src

# 2. Copy binary to ~/.hermes/bin/
mkdir -p ~/.hermes/bin
cp hermem ~/.hermes/bin/

# 3. Copy plugin to ~/.hermes/hermes-agent/plugins/memory/
cp -r plugins/memory/hermem ~/.hermes/hermes-agent/plugins/memory/

# 4. Copy config
cp hermem.ini ~/.hermes/hermem.ini

# 5. Set provider in ~/.hermes/config.yaml
# memory.provider: hermem

# 6. Restart Hermes
hermes gateway restart
```

### Standalone (without Hermes)

```bash
# Build (with or without local embedding — make handles missing bin/)
make build

# Or build without local embedding (no llama-embedding binary needed)
make build-no-local

# Copy to PATH
cp hermem /usr/local/bin/

# Configure: place hermem.ini *next to the binary* so the
# binary-dir resolution picks it up from any working directory.
sudo cp hermem.ini /usr/local/bin/hermem.ini

# Run CLI (works regardless of cwd)
echo '{"query":"What is Go?"}' | hermem memory query

# Or run as server (port is a real cobra flag, not a positional arg)
hermem serve --port 8420
```

## Dependencies

- Go 1.21+
- CGO enabled (required by `github.com/mattn/go-sqlite3` + Accelerate on darwin; pure-Go `modernc.org/sqlite` pending)
- One of: Ollama running locally, or an OpenAI API key, or a local GGUF model for embedding
- (Optional) `sqlite-vec` — statically linked via `github.com/asg017/sqlite-vec-go-bindings/cgo` when `[database] backend = sqlite-vec`

## Configuration

All settings are read from `hermem.ini` **next to the binary executable**
(`os.Executable()`-resolved), so `~/.hermes/bin/hermem memory store`
behaves the same regardless of the caller's working directory — a
stray `hermem.db` no longer leaks into a transient CWD. INI format.
If the file doesn't exist, defaults are used. The CLI is cobra-grouped;
see [USAGE.md §4.1](docs/USAGE.md#4-command-tree-cobra-grouped-grammar) for the
full subcommand tree.

### hermem.ini — all settings

```ini
[embedder]
provider = ollama               # "ollama" | "openai"
url = http://localhost:11434
model = nomic-embed-text
key =                           # API key for OpenAI (not needed for Ollama)
timeout = 30s                   # HTTP request timeout (Go duration)

[embedding]
; model_path = "./models/nomic-embed-text.gguf"  # local GGUF model (no Ollama/OpenAI needed)

[extraction]
; provider, url, key — optional, fall back to [embedder] values
provider = ollama               # "ollama" | "openai"
url = http://localhost:11434
key =                           # API key for OpenAI
model = qwen2.5-coder:7b
temperature = 0.1
timeout = 300s                  # HTTP request timeout (Go duration)

[ingestion]
dedup_threshold = 0.88          # cosine floor for merge-during-ingest (0.0–1.0)

[retrieval]
depth_ceiling = 5               # hard clamp on requested max_depth
max_nodes = 100                 # soft cap on RetrieveContext output size

[retention]
observation_ttl = 2160h         # observations older than this → archived (Go duration)
run_interval = 1h               # how often the GC loop fires
batch_size = 500                # max nodes archived per cycle (0 = no limit)

[database]
path = hermem.db                # SQLite file; created on first store
backend = in-memory             # "in-memory" | "sqlite-vec"

[vector]
dim = 768                       # embedding dimension for vec0 table (must match model)

[server]
api_key =                       # X-API-Key auth (empty = disabled)

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

### Provider examples

**Ollama (default):**
```ini
[embedder]
provider = ollama
url = http://localhost:11434
model = nomic-embed-text

[extraction]
; inherit provider/url/key from embedder, override only model
model = qwen2.5-coder:7b
temperature = 0.1

[database]
path = hermem.db
```

**OpenAI (same backend for both):**
```ini
[embedder]
provider = openai
url = https://api.openai.com/v1
model = text-embedding-3-small
key = sk-you...here

[extraction]
; inherit provider/url/key from embedder
model = gpt-4o-mini
temperature = 0.1

[database]
path = hermem.db
```

**Mixed backends (Ollama embedder + OpenAI extractor):**
```ini
[embedder]
provider = ollama
url = http://localhost:11434
model = nomic-embed-text

[extraction]
provider = openai
url = https://api.openai.com/v1
key = sk-you...here
model = gpt-4o-mini
temperature = 0.1

[database]
path = hermem.db
```

**Custom OpenAI-compatible endpoint (vLLM, LiteLLM, etc.):**
```ini
[embedder]
provider = openai
url = http://localhost:8000/v1
model = your-model-name
key = your-key

[extraction]
model = your-chat-model
temperature = 0.1

[database]
path = hermem.db
```

### Defaults

Every key is optional; missing keys fall back to the defaults below.

| Section.key | Default | Description |
|-------------|---------|-------------|
| `embedder.provider` | `ollama` | Embedder backend (`ollama` \| `openai`). |
| `embedder.url` | `http://localhost:11434` | API endpoint. |
| `embedder.model` | `nomic-embed-text` | Embedding model name. |
| `embedder.key` | *(empty)* | API key (OpenAI only). |
| `embedding.model_path` | *(empty)* | Path to local GGUF model file. When set, uses embedded llama-embedding binary instead of Ollama/OpenAI. |
| `extraction.provider` | `"ollama"` *(inherits embedder)* | LLM provider for extraction (`ollama` \| `openai`). |
| `extraction.url` | *(inherits embedder)* | API endpoint for extraction. |
| `extraction.key` | *(inherits embedder)* | API key for extraction (OpenAI). |
| `extraction.model` | `qwen2.5-coder:7b` | LLM model used by extractor. |
| `extraction.temperature` | `0.1` | Sampler temperature for extraction. |
| `ingestion.dedup_threshold` | `0.88` | Cosine floor for merge-during-ingest (see Deduplication, below). |
| `retrieval.depth_ceiling` | `5` | Hard clamp on requested `max_depth`. |
| `retrieval.max_nodes` | `100` | Soft cap on `RetrieveContext` output size. |
| `database.backend` | `in-memory` | Vector index backend: `in-memory` (Go brute-force) or `sqlite-vec` (indexed KNN). |
| `vector.dim` | `768` | Embedding dimension for `vec0` virtual table. Must match your model's output dim. |
| `database.path` | `hermem.db` | SQLite database file. |
| `retention.observation_ttl` | `2160h` | Observation nodes older than this TTL are archived. |
| `retention.run_interval` | `1h` | How often the GC loop fires. |
| `retention.batch_size` | `500` | Max nodes archived per cycle. |
| `server.api_key` | *(empty)* | API key for `X-API-Key` auth (empty = disabled). |
| `embedder.timeout` | `30s` | HTTP request timeout per embedder call (Go duration). |
| `extraction.timeout` | `5m` | HTTP request timeout per LLM extractor call (Go duration). |

Invalid integer / float parse values are logged at warning level and
the corresponding default is kept; the server still boots.

## Usage

### 1. Store entities with embeddings

```go
entity := Entity{
    ID:        "paris-fact",
    Category:  "world",
    Content:   "Paris is the capital of France",
    Embedding: []float32{0.1, 0.2, 0.3}, // from your embedder
}
StoreEntityWithEmbedding(db, vi, schema, entity)
```

### 2. Vector search

```go
results, err := SearchByVector(db, vi, queryEmbedding, 10) // top 10
for _, r := range results {
    fmt.Printf("%s (similarity: %.3f)\n", r.Entity.Content, r.Similarity)
}
```

### 3. Graph traversal (retrieval)

```go
// Find seed nodes by vector search, then walk the graph 2 hops deep
result, err := RetrieveContext(db, seedIDs, RetrieveContextOptions{MaxDepth: 2})

// Format as markdown for injection into LLM prompt
markdown := FormatContextMarkdown(result)
```

The retrieval pipeline is split into five named stages —
`expand_graph` → `score_and_rank` → `rank_sort` → `bucketize` →
`rerank` — each file-isolated under `src/internal/retrieval/`, each
tracing-spanned (`retrieval.expand_graph`, `retrieval.score_and_rank`,
`retrieval.rank_sort`, `retrieval.bucketize`, `retrieval.rerank`),
and each benchmark-able via `go test -bench=. -benchmem
./src/internal/retrieval/`. Per-stage contracts, span names, and
failure modes live in
[`src/internal/retrieval/PIPELINE.md`](src/internal/retrieval/PIPELINE.md).

### 4. Ingest dialog (background worker)

```go
ch := make(chan MemoryMessage, 16)
go MemoryWorker(db, extractor, embedder, ch)

// After each agent turn
ch <- MemoryMessage{Dialog: conversationHistory}
```

The ingestion worker:
- Extracts entities from dialog text
- Deduplicates by vector similarity (threshold: 0.88)
- Merges content of similar entities instead of creating duplicates
- Creates edges from extracted relations

## File Structure

Post-Cobra-migration layout (commit `8f0bf71`). The old `src/<file>.go`
flat tree and the `src/cmd/<name>.go` registry are gone. Source lives
under 13 packages in `src/internal/`.

```
hermem/
├── README.md                ← This file (high-level overview + quick start)
├── docs/
│   ├── USAGE.md             ← Operator runbook (CLI + HTTP side-by-side,
│   │                          payload reference, error model, DB schema,
│   │                          embedding-model notes, operational runbook)
│   ├── CHANGELOG.md         ← Keep-a-Changelog format, semver
│   ├── ROADMAP.md           ← (project planning docs)
│   ├── VISION.md
│   └── andrey-karpathy-skills.md
├── skills/hermem/
│   └── SKILL.md             ← Hermes Agent skill manifest
├── Dockerfile               ← Multi-stage build, non-root user
├── docker-compose.yml       ← Local stack orchestration
├── hermem.ini               ← Sample config (full key reference in §Configuration)
├── install.sh               ← One-command installer for Hermes Agent
├── go.mod / go.sum          ← Go module + lockfile (uses gopkg.in/ini.v1, spf13/cobra)
├── plugins/memory/hermem/   ← Hermes Agent provider plugin (Python)
│   ├── __init__.py
│   └── plugin.yaml          # 10 tools: search, store, query, edge, retrieve,
│                            # timeline, contradictions, task create/status/list
└── src/
    ├── main.go              ← Binary entry: loads config + DB + VI + Embedder +
    │                         Extractor + Reranker + BuildInfo, constructs
    │                         cli.Env, calls cli.NewRootCommand(env).Execute().
    │                         Builds SIGHUP reload loop on prod. server (hermem serve).
    └── internal/            ← Flat domain packages + CLI + server shells
        ├── ai/              ← LLM client helpers (Ollama + OpenAI + local),
        │                      extractor base, retrieval prompts
        ├── cli/             ← Cobra command tree (~44 files; see CLI Commands
        │                      section above for the user-visible layout)
        │   ├── root.go      ← NewRootCommand wires top-level + 6 groups
        │   ├── env/env.go   ← Runtime Env, BuildInfo, ReadStdin, DecodeStdin,
        │   │                  DecodeString, WriteJSON, ErrStdinRequired
        │   ├── serve.go | health.go | metrics.go | version.go   ← top-level
        │   ├── memory/      ← 10 subs: store/search/retrieve/query/response/
        │   │                  edge/ingest/explain/re-embed/quantize
        │   ├── task/        ← 8 subs: status/list/show/dep/tree/create/
        │   │                  rollback/executable (alias `next`)
        │   ├── graph/       ← 7 subs: plan/recovery-plan/components/
        │   │                  communities/verify/contradictions/provenance
        │   ├── time/        ← 2 subs: temporal/timeline
        │   ├── agent/       ← 1 sub: loop
        │   └── db/          ← 4 subs: migrate/rollback/verify/schema
        ├── config/          ← INI loader (gopkg.in/ini.v1), Config.Validate,
        │                      schema fingerprinting, Reranker factory
        ├── contradiction/   ← Contradiction detection domain Service
        ├── core/            ← Public types: Entity, Edge, request/response
        │                      structs, SchemaConfig, RetrieveContextOptions
        ├── edge/            ← Relation-edge domain Service
        ├── graph/           ← Graph analytics domain Service
        ├── health/          ← Health-probe domain Service (DB ping)
        ├── httputil/        ← HTTP strict-decode helpers (DecodeStrict,
        │                      WriteJSON, WriteError, MaxBodyBytes)
        ├── ingest/          ← Synchronous dialog ingestion Service
        ├── memory/          ← Memory CRUD domain Service (Store, edge, ingest)
        ├── metrics/         ← AsyncMetricsWorker + Prometheus exposition
        ├── migration/       ← Schema migration domain Service
        ├── orchestrator/    ← AgentLoop + ExecutionPlan (CLI-only)
        ├── reembed/         ← Background re-embedding Service
        ├── retention/       ← GC archival domain Service
        ├── retrieval/       ← Recursive CTE graph walk, ranking pipeline,
        │                      FormatContextMarkdown, contradiction helpers
        ├── server/          ← HTTP API server, split into per-domain sub-services
        │   ├── server.go    ← Slim shell: route registration, ReloadState
        │   │                  (atomic.Pointer swap), graceful shutdown
        │   ├── middleware.go ← X-API-Key, request-id, slog, recover, max-body
        │   ├── contradiction/← GET /contradictions
        │   ├── edge/        ← POST /edge
        │   ├── graph/       ← /connected-components, /communities, /graph/verify
        │   ├── health/      ← /health, /health/live, /health/ready
        │   ├── ingest/      ← POST /ingest, GET /ingest/jobs
        │   ├── memory/      ← POST /store
        │   ├── migration/   ← /db/migrate, /db/rollback, /db/verify, /db/schema
        │   ├── reembed/     ← POST /admin/re-embed
        │   ├── retention/   ← POST /admin/retention/run
        │   ├── retrieval/   ← POST /search, /query, /retrieve, /explain,
        │   │                  /response, /query/temporal (read)
        │   ├── task/        ← POST /task/*
        │   └── timeline/    ← GET /timeline
        ├── serverstate/     ← atomic.Pointer[State] for SIGHUP-safe config
        │                      reload across all handlers
        ├── store/           ← SQL access + migration runner (init, codec,
        │   │                  entity, edge, task, graph, community, recovery,
        │   │                  schema)
        │   ├── migration.go ← InitDB, runMigrations, StoreSchemaFingerprint
        │   ├── init.go      ← DSN + PRAGMAs (WAL, FK, synchronous=NORMAL,
        │   │                  auto_vacuum=INCREMENTAL)
        │   ├── *_test.go    ← 4 test files in this pkg
        │   └── migrations/  ← Embedded SQL apply-set
        │       ├── 001_initial_schema.sql
        │       ├── 002_entity_metadata.sql
        │       ├── 003_provenance.sql
        │       ├── 004_episodic_sessions.sql
        │       ├── 005_centrality.sql
        │       ├── 006_weighted_edges.sql
        │       └── 007_task_priorities.sql
        ├── task/            ← Task lifecycle domain Service
        ├── timeline/        ← Time-ordered entity projection
        └── vector/          ← VectorIndex + backends + cosine + quant
            ├── index.go     ← VectorIndex interface, embedding serialisation,
            │                  NormalizeVector at ingest
            ├── inmemory.go  ← InMemoryVectorIndex: flatMatrix + cblas_sgemv
            │                  (or pure-Go loop), BatchDotProducts
            ├── cosine.go    ← Pure-Go fallback (build tag: !darwin || !cgo)
            ├── cosine_darwin.go ← Apple Accelerate (cblas) AMX fast path
            │                  (build tag: darwin && cgo)
            ├── quantize.go  ← Scalar int8 roundtrip + compression stats
            │                  (QuantizeVector / DequantizeVector)
            └── *_test.go    ← Cosine parity tests + quantize roundtrip
```

All `*_test.go` files together amount to ~12 test files across the
`internal/` tree. Each file typically holds 5-15 tests.

## HTTP API Server

Run Hermem as an HTTP service for integration with Hermes Agent or other systems:

```bash
./hermem serve --port 8420
```

### Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Liveness check (always 200) |
| `/health/live` | GET | Kubernetes liveness probe |
| `/health/ready` | GET | Readiness probe (DB ping, 503 if degraded) |
| `/store` | POST | Store an entity |
| `/search` | POST | Vector similarity search |
| `/retrieve` | POST | Graph walk from seed IDs |
| `/ingest` | POST | Ingest dialog text |
| `/query` | POST | Full pipeline: search + graph walk + markdown |
| `/query/explain` | POST | Query with full ScoreBreakdown (vector / recency / temporal / centrality / path / depth_penalty / final) per node and fact |
| `/query/temporal` | POST | Query filtered by time range (time_from/time_to) |
| `/task/status` | POST | Update task execution status |
| `/task/executable` | POST | List executable tasks (CTE dependency walk) |
| `/task/next` | POST | Alias for executable |
| `/task/list` | POST | Filter tasks by status/goal |
| `/task/show` | POST | Show task + blocked_by / recovers_via relations |
| `/task/dep` | POST | Manage task dependencies |
| `/task/create` | POST | Create task with auto-linked context edges |
| `/task/tree` | POST | Print task tree (blocked_by parents) |
| `/task/rollback` | POST | Find rollback task for a failed task |
| `/verify` | POST | Graph integrity check (entities, edges, corrupt blobs, orphan edges) |
| `/metrics` | GET | expvar counters (stores / searches / retrieves / queries / errors / task ops) |
| `/edge` | POST | Add a typed edge between two entities (or auto-create missing ones) |
| `/contradictions` | GET | List contradict edges (optional `?id=X` filter) |
| `/timeline` | GET | Recent entities by created_at DESC (optional `?limit=N`) |
| `/provenance` | GET | Entities by memory origin (`?conversation_id=&message_id=&source=&limit=`) |
| `/recovery-plan` | GET | Recovery task chain for failed task (`?id=X`) |
| `/connected-components` | GET | Graph connected components (`?min_size=N`) |
| `/communities` | GET | Louvain community detection (`?min_size=N&max_iterations=N`) |
| `/admin/re-embed` | POST | Trigger background re-embedding (`{"dim": 768, "batch_size": 50}`) |
| `/graph/verify` | GET | Graph integrity check (entities, edges, embedding dims) |

### Examples

**Store an entity:**
```bash
curl -X POST http://localhost:8420/store \
  -H "Content-Type: application/json" \
  -d '{"id":"paris","category":"world","content":"Paris is the capital of France"}'
```

**Search:**
```bash
curl -X POST http://localhost:8420/search \
  -H "Content-Type: application/json" \
  -d '{"query":"capital of France","top_k":5}'
```

**Full query (search + graph walk + markdown):**
```bash
curl -X POST http://localhost:8420/query \
  -H "Content-Type: application/json" \
  -d '{"query":"Tell me about France"}'
```

**Ingest dialog:**
```bash
curl -X POST http://localhost:8420/ingest \
  -H "Content-Type: application/json" \
  -d '{"dialog":"User: What is Go?\nAssistant: Go is a statically typed language."}'
```

**Task management:**
```bash
# create a task manually
curl -X POST http://localhost:8420/store \
  -H "Content-Type: application/json" \
  -d '{"id":"step-1","category":"task","content":"Run tests"}'

# create a task with auto-linked context
curl -X POST http://localhost:8420/task/create \
  -H "Content-Type: application/json" \
  -d '{"content":"Run tests","context_ids":["step-0"]}'

# update status
curl -X POST http://localhost:8420/task/status \
  -H "Content-Type: application/json" \
  -d '{"id":"step-1","status":"running"}'

# list executable tasks
curl -X POST http://localhost:8420/task/executable \
  -H "Content-Type: application/json" \
  -d '{}'

# show task with dependencies
curl -X POST http://localhost:8420/task/show \
  -H "Content-Type: application/json" \
  -d '{"id":"step-1"}'
```

## Hermes Agent Integration

Hermem ships with a memory provider plugin for [Hermes Agent](https://github.com/NousResearch/hermes-agent).

### Install with script (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/pavelveter/hermem/main/install.sh | bash
```

### Install manually

```bash
# 1. Build the binary
go build -o hermem ./src

# 2. Copy binary
mkdir -p ~/.hermes/bin
cp hermem ~/.hermes/bin/

# 3. Copy plugin
mkdir -p ~/.hermes/hermes-agent/plugins/memory
cp -r plugins/memory/hermem ~/.hermes/hermes-agent/plugins/memory/

# 4. Copy config
cp hermem.ini ~/.hermes/hermem.ini

# 5. Set provider
sed -i '' 's/^  provider:.*/  provider: hermem/' ~/.hermes/config.yaml

# 6. Restart Hermes
hermes gateway restart
```

### Verify installation

```bash
hermes memory
# Should show:
#   Provider:  hermem
#   Plugin:    installed ✓
#   Status:    available ✓
```

### Start Hermem server (optional)

The plugin works in CLI mode by default (no server needed). For server mode:

```bash
~/.hermes/bin/hermem serve --port 8420
export HERMEM_URL=http://localhost:8420
```

### Plugin tools

The plugin exposes ten tools to the Hermes agent:

| Tool | Description |
|------|-------------|
| `hermem_search` | Search graph memory by vector similarity |
| `hermem_store` | Store a fact in graph memory |
| `hermem_query` | Full pipeline: search + graph walk + markdown context |
| `hermem_edge` | Link two entities with a typed relation |
| `hermem_retrieve` | Graph walk from explicit seed IDs (multi-hop) |
| `hermem_timeline` | Most recent entities (ordered by created_at) |
| `hermem_contradictions` | List conflicting facts (global or ID-scoped) |
| `hermem_task_create` | Create a task node, optionally linked to context |
| `hermem_task_status` | Update task lifecycle status |
| `hermem_task_list` | List operational tasks filtered by status / goal |

### How it works with Hermes

1. **prefetch**: Before each turn, Hermes calls `hermem_query` to retrieve relevant context from the graph
2. **sync_turn**: After each turn, the conversation is sent to `/ingest` for entity extraction
3. **Tools**: The agent can explicitly search or store memories via tool calls

## How it works

### Storage

Entities are stored in a flat SQLite table with a BLOB column for embeddings (raw `float32` bytes, no JSON overhead). Edges use a composite primary key `(source_id, target_id, relation_type)` for automatic deduplication.

### Retrieval

1. Query embedding is generated for the user's input
2. Vector search finds the top-K most similar seed entities
3. A recursive CTE walks the graph from seed nodes up to `maxDepth` hops
4. Results are grouped by memory category and formatted as markdown

### Deduplication

When ingesting new facts, the ingestion worker reads the top-1
candidate by cosine similarity; if the score is at or above the
`[ingestion] dedup_threshold` (default `0.88`, configurable; cosine
similarity ∈ [0, 1] for unit-length embeddings), the system checks
for contradiction before merging. If `isContradiction` detects
conflicting statements (negation asymmetry, sentiment opposites),
a `contradicts` edge is created and the new entity is stored as a
separate node. Otherwise, the new content is merged into the existing
entity (concatenated with `"; "` if not already substring-contained),
re-embedded, and persisted. Relations from the extraction are appended
as `INSERT OR IGNORE` edges (composite-PK dedup on
`(source_id, target_id, relation_type)`).

### Extraction validation

`OllamaLLMExtractor` enforces a hardcoded allowlist of categories
(`world` / `opinion` / `experience` / `observation` / `task`) and
relation types (`prefers` / `uses` / `mentions` / `related_to` /
`part_of` / `causes` / `contradicts` / `blocked_by` /
`recovers_via`) at parse time via `filterEntities` and
`filterRelations`. Out-of-allowlist values are silently dropped
rather than aborting the ingest, so a partially-correct LLM output
still yields graph-safe entities. The 5xx-retry / 4xx-no-retry path
is retry-budgeted (3 attempts, exponential backoff 200ms→2s, capped
total latency).

## Performance

Vector search benchmark: `go test -bench=BenchmarkInMemorySearch -benchmem -count=3 ./src`.
Graph topology as described above. Numbers are machine-dependent; re-run to refresh.

### Topology

Each entity has **~8 edges on average**:
- **5 forward chain edges** to `(i+1..i+5)` when target < n,
  relation_type `next` — gives locality along the chain
- **3 hash-based long-range edges**, target
  `((i+1) * mult) % n` for `mult ∈ {7, 11, 13}`, relation_type
  `long-range` — breaks locality so fan-out grows with depth

The SQLite recursive CTE walks edges bidirectionally
(`source_id = gw.id OR target_id = gw.id`), so a forward-only
edge is enough for the walk to find the reverse connection.

### Numbers

Benchmarked on Apple M1 Pro (768D embeddings, `topK=10`, 3 runs, medians):

| N | In-Memory (flatMatrix + Accelerate) | sqlite-vec (KNN index) | B/op (mem / vec) |
|--:|-------------------------------------:|-----------------------:|------------------:|
| 100 | **60 µs** | 291 µs | 108 KB / 114 KB |
| 1,000 | **170 µs** | 949 µs | 119 KB / 114 KB |
| 5,000 | **2.1 ms** | 4.4 ms | 168 KB / 114 KB |
| 10,000 | **1.9 ms** | 9.0 ms | 230 KB / 114 KB |

### Scaling

- **In-Memory** (`InMemoryVectorIndex`, default) — pre-built
  `flatMatrix` row-major in RAM, single `cblas_sgemv` call via Apple
  Accelerate (AMX co-processor). Constant 318 allocs/op regardless
  of N — no per-search matrix rebuild. At 10K entities ~1.9 ms.
  Good for datasets up to ~50K entities on consumer hardware.
- **sqlite-vec** (`SqliteVecIndex`, `[database] backend = sqlite-vec`)
  — indexed KNN via `vec0` virtual table. Constant 363 allocs/op,
  ~114 KB/op flat allocation. SQLite query overhead (plan, MATCH,
  distance sort). At N < 100K in-memory is faster; sqlite-vec
  pulls ahead at larger scales where O(N) scan becomes prohibitive.
- **Graph walk** — dominated by SQLite recursive-CTE JOIN
  cost over edges, scales roughly linearly with edge count.

## Testing

A project-wide **pre-push hook** lives at `.githooks/pre-push` (read it
with `cat .githooks/pre-push` to inspect the exact gates — it's only ~110
lines) and mirrors CI gates locally — architecture guardrails, `gofmt`,
`go vet`, `go build`, `go test -race -count=1 -timeout 10m`, optional
`golangci-lint`, and the AMX Apple-Accelerate guard on Darwin. Activate
on a fresh clone with:

```bash
git config core.hooksPath .githooks
```

Verify it took effect (matters if a system / global / worktree `git
config` layer is stomping on it):

```bash
git config --get core.hooksPath   # → .githooks
```

Bypass with `git push --no-verify` if needed.

```bash
go test -count=1 ./src                     # full suite (~3s)
go test -v -count=1 -run TestServer ./...  # strict-decode table only
go test -bench=BenchmarkInMemorySearch -benchmem -count=3 ./src    # in-memory vector perf
go test -tags sqlite_vec -bench=BenchmarkSqliteVecSearch -benchmem -count=3 ./...  # sqlite-vec perf
go test -v -run TestIntegration
go test -v -run TestTiming
```

Per-package tests live under each internal package as `*_test.go`.
The store package's `helpers_test.go → memDB(t)` fixture sets up an
in-memory SQLite + stub embedder/extractor mocks used by the
integration tests; `tasks_test.go` and `recovery_test.go` (also in
`store/`) exercise scheduler-flavoured SQL roundtrips. The file-backed
test in `store/` (using `verify-test.db`) is the only one that runs
against the real `PRAGMA journal_mode = WAL` path. The CLI smoke test
lives at `src/internal/cli/cli_test.go` and asserts the cobra tree
shape + `--help` rendering.

## Documentation

For the full operator runbook — CLI mode and server mode side-by-side,
request/response reference, the strict-decode error model, DB schema
notes, embedding-model/dimension gotchas, and operational pitfalls —
see **[docs/USAGE.md](docs/USAGE.md)**.

## License

MIT
