# Hermem — Usage

A runbook for operators. Covers build, configuration, embedding models,
domain models, and the memory evolution subsystem.

For CLI reference, see [CLI.md](CLI.md). For server endpoints and auth,
see [SERVER.md](SERVER.md). For production operations, see
[RUNBOOK.md](RUNBOOK.md). For MCP integration, see [MCP.md](MCP.md).
For SDKs, see [SDK.md](SDK.md). For the OpenAPI spec, see
[OPENAPI.md](OPENAPI.md).

---

## 1. TL;DR

```bash
# Build once (with or without local embedding binary).
make build
# or: go build -o hermem ./src

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
# Recommended: make handles missing bin/ gracefully
make build
# or with -trimpath for reproducible builds
go build -trimpath -ldflags="-s -w" -o hermem ./src
# or without local embedding (faster, no llama-embedding binary needed)
make build-no-local
```

Install into `$PATH`:

```bash
# Recommended: build, sign (macOS), and copy to ~/.local/bin
make install
# Override target dir:
make install INSTALL_DIR=/usr/local/bin

# Manual alternative:
sudo cp hermem /usr/local/bin/    # Linux/macOS, system-wide
# or user-local:
mkdir -p ~/.local/bin && cp hermem ~/.local/bin/
```

> **macOS note:** Go's `linker-signed` signature can be rejected by Gatekeeper
> with `SIGKILL (Code Signature Invalid)` when the binary is launched from a
> non-system path (e.g. `~/.local/bin`). `make install` re-signs the binary
> with a clean ad-hoc signature (`codesign --force --sign -`) to avoid this.
> If you copy the binary manually, run `make sign` first or apply
> `codesign --force --sign - <path-to-hermem>` yourself.

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

[embedding]
; model_path = "./models/nomic-embed-text.gguf"  # local GGUF model (no Ollama/OpenAI needed)

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
token_budget  = 4000              # soft token limit; 0 = unlimited (uses max_nodes only)

[ranking]                          # tunable ranking weights
vector_weight         = 0.7       # vector similarity weight (0 = disabled)
recency_weight        = 0.3       # recency decay weight
; recency_half_life_hours = 720   # half-life for exp decay (default 720h ≈ 30d)
; depth_penalty         = 0.05    # linear penalty per hop depth
; temporal_weight       = 0.1     # temporal relevance weight
; temporal_half_life_hours = 720  # half-life for temporal decay
; centrality_weight     = 0.05    # graph centrality boost for hub nodes

[reranker]                         # optional post-retrieval reranker
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

See §5.

---

## 4. Embedding model & dimensions

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

## 5. Domain Models

Hermem's persistence layer is anchored on `core.Entity`, a 19-field
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
| `Goal`     | 0 new  | Re-views `Task`'s shape with `Category = "goal"` (service-layer intent). |

Total: 4 + 3 + 3 + 4 + 5 = **19 fields**. These are the same 19
fields on `Entity`. Goal adds no new field; it re-views Task's
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

## 6. Evidence API

`src/internal/memory/evidence/` (C2 of P2 MEMORY EVOLUTION on `feat/memory-evolution-c2`,
merged into main via `--no-ff`) provides a first-class Evidence table bound to the Belief
table via FK CASCADE. Each Evidence row represents a typed statement that *supports* or
*refutes* one parent Belief.

### Types

- `evidence.Polarity` (string enum): `support` | `refute` (also constrained in SQL via
  `CHECK(polarity IN ('support','refute'))`).
- `evidence.Evidence` (struct): `ID int64`, `BeliefID int64`, `Polarity`, `Strength
  float64` (absolute magnitude, 0..1), `Content string` (NON NULL), `SourceKind string`,
  `SourceID string`, `CreatedAt`, `UpdatedAt`.

### Service interface

```go
type Service interface {
    CreateEvidence(ctx context.Context, e *Evidence) error
    GetEvidence(ctx context.Context, id int64) (*Evidence, error)
    ListForBelief(ctx context.Context, beliefID int64) ([]*Evidence, error)
    UpdateStrength(ctx context.Context, id int64, newStrength float64) error
    DeleteEvidence(ctx context.Context, id int64) error
}
```

Construction: `evidence.NewService(db *sql.DB) Service`. The default fixture is
`store.MemDB()`.

### Semantics

- **Asymmetric defaults across create vs update.** `CreateEvidence` silently maps
  `Strength == 0` to `1.0` (warm, forgiving). `UpdateStrength` accepts 0 strictly
  (0 is meaningful: e.g. retracting evidence to zero). Bounds are tight: < 0 or > 1
  is rejected on either path.
- **Polarity vs strength.** Strength is an absolute magnitude; whether it adds to or
  subtracts from a Belief's confidence is decided by an aggregator (C3 — Confidence
  propagation, not yet shipped).
- **Cascade.** `Evidence.belief_id REFERENCES beliefs(id) ON DELETE CASCADE`. Removing a
  Belief removes the Evidence rows that reference it; this is enforced when SQLite is
  loaded with `PRAGMA foreign_keys = ON`.

### Migration

`src/internal/store/migrations/009_add_evidence_table.sql` (idempotent
`CREATE TABLE IF NOT EXISTS` + two indexes: `belief_id` and `(source_kind, source_id)`).
Verify ordering with `migrate status` and confirm `9` is applied.

### Tests

`src/internal/memory/evidence/evidence_test.go` contains 14 race-safe unit tests including
`TestService_CascadeDelete_WhenBeliefRemoved` (cascade verified via raw `db.Exec`) and
`TestService_ConcurrentCreate_RaceSafe` (N=32 goroutines producing distinct IDs).

---

## 7. Confidence Propagation

Package `src/internal/evolution/propagation.go` (C3 of P2 MEMORY EVOLUTION).

`PropagateConfidence(ctx, bSvc, eSvc, beliefID)` aggregates all evidence
for a belief by polarity and updates the belief's confidence to the ratio
of total support strength over total evidence strength.

Formula: `confidence = clamp(supportSum / (supportSum + refuteSum), 0, 1)`
When total evidence strength is zero, the existing confidence is preserved.

---

## 8. Evidence Aggregation

Package `src/internal/evolution/aggregation.go` (C4 of P2 MEMORY EVOLUTION).

`AggregateEvidence(all, selector)` groups evidence by polarity and returns
aggregated strength values. Three selectors:
- `AggregatorSum` — sum of strengths (default)
- `AggregatorAvg` — average of strengths
- `AggregatorMin` — minimum strength

---

## 9. Trust Scoring

Package `src/internal/evolution/trust.go` (C5 of P2 MEMORY EVOLUTION).

`TrustScore(confidence, sourceKind, updatedAt, weights)` computes a
composite trust score as `confidence × sourceWeight × recencyFactor`.

`TrustDefaults()` returns sensible source weights:
- `user`: 1.0, `observation`: 0.9, `extraction`: 0.7, `inference`: 0.5, `external`: 0.3

Recency factor uses exponential decay: `exp(-hoursSinceUpdate / halfLife)`.
Default half-life: 720h (30 days). Unknown source kinds default to 0.5.

---

## 10. Belief Revision Chains

Package `src/internal/evolution/chains.go` (C6 of P2 MEMORY EVOLUTION).

`TraceRevisions(ctx, db, beliefID)` walks the `parent_chain_id` chain
backward from a belief to its root ancestor using a single recursive CTE
(N+1-safe). Returns an ordered list from oldest to latest. Bounded by
`MaxChainDepth` (32) to prevent infinite loops.

---

## 11. Superseded Beliefs

Package `src/internal/evolution/superseded.go` (C7 of P2 MEMORY EVOLUTION).

`ListActiveBeliefs(ctx, db, includeSuperseded)` returns only beliefs with
`status = 'Active'` by default. When `includeSuperseded=true`, returns all
beliefs regardless of status (for reconciliation/audit).

---

## 12. Support/Refute Relationships

Package `src/internal/evolution/relationships.go` (C8 of P2 MEMORY EVOLUTION).

`GetSupportRefute(ctx, db, beliefID)` counts evidence rows by polarity in
a single SQL query. Returns `RelationshipCounts` with support/refute
counts, total, and percentage breakdown.

---

## 13. Belief History

Package `src/internal/evolution/history.go` (C9 of P2 MEMORY EVOLUTION).

- `RecordHistory(ctx, db, beliefID, confidence, status, reason)` — append-only INSERT into `belief_history` table.
- `ListHistory(ctx, db, beliefID)` — returns all history entries ordered by `created_at ASC`.

Migration `010_add_belief_history.sql` creates the table with FK CASCADE
to `beliefs` and indexes on `belief_id` and `created_at`.

---

## 14. Evolution Queries

Package `src/internal/evolution/queries.go` (C10 of P2 MEMORY EVOLUTION).

- `GetSupersededBy(ctx, db, beliefID)` — returns the successor Belief ID or 0 if active/not found.
- `StateAt(ctx, db, beliefID, timestamp)` — returns the belief's state as it existed at a point in time (most recent history entry before or at the timestamp).

Both use JOIN-style single-SQL queries (N+1-safe). `StateAt` has a benchmark.

---

## 15. Episodic Memory

The `src/internal/episodic/` package provides a layered episodic
memory subsystem on top of the existing `entities` and `sessions`
tables. One episode groups a bounded time-window of fine-grained
events plus optional links to extracted memory entities and
task entities. The subsystem is read-write through Go services;
no HTTP shell ships in this release (transport is a follow-up).

### Service surface

```go
import (
    "github.com/pavelveter/hermem/src/internal/core"
    "github.com/pavelveter/hermem/src/internal/episodic"
)

// EpisodeService — CRUD on the episodes table.
epSvc  := episodic.New(db)
epSvc.CreateEpisode(ctx, episodic.Episode{
    ID:        "ep-1",
    SessionID: "sess-1",
    Title:     "first conversation",
})
ep, _ := epSvc.GetEpisode(ctx, "ep-1")
epSvc.UpdateSummary(ctx, "ep-1", "user asked about Go modules")
epSvc.EndEpisode(ctx, "ep-1", time.Time{})  // zero = now

// SessionService — group containers around episodes.
sessSvc := episodic.NewSessionService(db)
sessSvc.CreateSession(ctx, episodic.Session{ID: "sess-1"})
sessSvc.EndSession(ctx, "sess-1", time.Time{})

// EventService — typed events on an episode (message | action |
// observation | system). SQL CHECK constraint is the authoritative
// guard; the Go type catches bad inputs with a friendly error.
evSvc := episodic.NewEventService(db)
evSvc.CreateEvent(ctx, episodic.Event{
    ID: "ev-1", EpisodeID: "ep-1",
    Type:    episodic.EventMessage,
    Content: "hello world",
})

// LinkService + TaskLinkService — many-to-many links.
linkSvc := episodic.NewLinkService(db)
linkSvc.LinkMemory(ctx, "ep-1", "ent-1", "extracted")
linkSvc.LinkMemory(ctx, "ep-1", "ent-2", "referenced")
taskSvc := episodic.NewTaskLinkService(db)
taskSvc.LinkTask(ctx, "ep-1", "task-1")

// TimelineService — chronological feed merging events + linked
// memories + linked tasks.
ts := episodic.NewTimelineService(db)
entries, _ := ts.ReconstructTimeline(ctx, "ep-1")

// RetrievalService — filter + optional semantic rerank.
retSvc := episodic.NewRetrievalService(db, nil)  // nil embedder = pure SQL
results, _ := retSvc.SearchEpisodes(ctx, "", episodic.EpisodeFilter{
    SessionID:         "sess-1",
    HasLinkedMemories: true,
    Limit:             10,
})

// Summarizer — hand dialog to an LLM extractor; persist summary.
ext := /* core.LLMExtractor — Ollama / OpenAI / stub */
sum := episodic.NewSummarizer(db, ext)
summary, _ := sum.SummarizeEpisode(ctx, "ep-1")

// PlaybackService — render the chronological feed as frames
// (JSON / Markdown / plain text export).
pb := episodic.NewPlaybackService(db)
frames, _ := pb.Playback(ctx, "ep-1")
md := pb.ExportMarkdown(frames)
txt := pb.ExportText(frames)
jsonBytes, _ := pb.ExportJSON(frames)
```

### Types

```go
type Episode struct {
    ID, SessionID, ConversationID, Title, Summary string
    StartedAt                            time.Time
    EndedAt                              *time.Time
    Metadata                             map[string]any
}

type Session struct {
    ID        string
    StartedAt time.Time
    EndedAt   *time.Time
    Metadata  map[string]any
}

type Event struct {
    ID, EpisodeID string
    Type          EventType  // EventMessage | EventAction | EventObservation | EventSystem
    Content       string
    Timestamp     time.Time
    Metadata      map[string]any
}

type TimelineEntry struct {
    Kind                              TimelineEntryKind // TimelineEvent | TimelineMemory | TimelineTask
    SourceID, EpisodeID, Type, Content string
    Timestamp                         time.Time
}

type PlaybackFrame struct {
    Timestamp                time.Time
    Type                     string  // "event" | "memory" | "task"
    Source, Actor, Content  string
}

type EpisodeFilter struct {
    SessionID         string
    TimeFrom, TimeTo  time.Time  // zero = no constraint
    HasSummary        bool
    HasLinkedMemories bool
    Limit             int        // <= 0 = no cap
}
```

### Conventions

- **Flat package, stateless Services**: each Service is a one- or
  two-field struct (just `db`, or `db + embedder` for retrieval)
  constructed once and held long-lived.
- **Idempotent linking**: `LinkMemory` / `LinkTask` use
  `ON CONFLICT DO NOTHING` so duplicate inserts are no-ops.
- **ON DELETE CASCADE** on event + link-table FKs; `SET NULL` on
  episode.session_id / episode.conversation_id so the session
  layer remains independent of episode lifecycle.
- **JSON metadata** is the extensibility seam — embeddings (for
  semantic retrieval) and per-episode annotations land in
  `metadata.embedding` and free-form keys.
- **Nil embedder** in RetrievalService disables semantic rerank
  cleanly; the Service falls back to pure SQL filtering.

---

## 16. Compression API

The `src/internal/compression/` package implements P2 — SEMANTIC
COMPRESSION: entity clustering, summary generation, recursive
recompression, and provenance tracking.

### Types

```go
type SummaryNode struct {
    ID             string     // unique summary node ID
    Content        string     // the summary text (bulleted extraction output)
    CompressedFrom []string   // source entity IDs (carried forward on recompress)
    CompressedAt   time.Time  // when compression ran
    Confidence     float32    // average confidence of source entities
    Provenance     string     // human-readable provenance chain
    Generation     int        // 1 = first compression, 2+ = recursive
    ExtractorModel string     // model identifier (default "llm")
    SupersededBy   string     // set when a newer recompression replaces this node
    RegeneratedAt  *time.Time // set on Regenerate() calls
}
```

### Clusterer

```go
cfg := compression.DefaultClustererConfig()
cfg.SimilarityThreshold = 0.8  // default 0.75
clusterer := compression.NewClusterer(db, cfg)
clusters, err := clusterer.Cluster(ctx, entityIDs)
```

Greedy clustering: seed an entity, group all within cosine similarity
threshold, remove from pool, repeat.

### Compressor

```go
ext := /* core.LLMExtractor */
cp := compression.NewCompressor(db, ext)

// One-shot compression
node, err := cp.Compress(ctx, entityIDs)

// Batch over pre-computed clusters
nodes, err := cp.CompressCluster(ctx, clusters)

// Recursive — carries old summary ID + entities forward, Gen++
recompressed, err := cp.Recompress(ctx, node.ID)

// Regenerate — same source, same generation, refreshed content
regenerated, err := cp.Regenerate(ctx, node.ID)
```

### Provenance

- `CompressedFrom` holds the original entity IDs at generation 1.
- On `Recompress`, the old summary ID is appended to `CompressedFrom`
  and the original node's `SupersededBy` field points to the new node.
- `Generation` increments on each recursive recompression (max depth = 3).

### Metrics

```go
metrics := compression.NewMetrics()
cp.WithMetrics(metrics)

metrics.CompressCount()        // total Compress calls
metrics.RecompressCount()      // total Recompress calls
metrics.RegenerateCount()      // total Regenerate calls
metrics.CompressedEntities()   // total entities passed to Compress/Recompress
metrics.ClusterCount()         // clusters processed via CompressCluster
metrics.TotalDuration()        // cumulative Compress duration
metrics.RecompressDuration()   // cumulative Recompress duration
```

---

## 17. E2E Testing

### Running E2E Tests

```bash
# Run all E2E tests
make test-e2e

# Or directly with go test
go test ./tests/e2e/... -v -timeout 5m
```

### Test Structure

```
tests/e2e/
├── cli/                    # CLI command tests
│   ├── memory_test.go      # memory store, search, query, edge, ingest
│   ├── task_test.go        # task create, status, list, show, dep, tree
│   ├── graph_test.go       # graph verify, components, communities
│   ├── db_test.go          # db migrate, schema, verify, dry-run
│   └── top_test.go         # version, health, metrics, diagnose
├── http/                   # HTTP endpoint tests
│   ├── health_test.go      # /health, /health/live, /health/ready
│   ├── memory_test.go      # /store, /search, /query, /retrieve, /edge, /ingest
│   ├── persistence_test.go # Data persistence across restarts
│   └── auth_test.go        # API key authentication
├── helpers/                # Test helpers
│   ├── workspace.go        # Temporary workspace creation
│   ├── server.go           # Server startup/shutdown
│   ├── http.go             # HTTP client wrapper
│   ├── cli.go              # CLI command wrapper
│   ├── json.go             # JSON comparison utilities
│   └── scenario.go         # YAML scenario runner
└── fixtures/               # Test data fixtures
```

### YAML Scenario Runner

Scenarios in `testdata/scenarios/` define cross-interface tests:

```bash
# Run a specific scenario
go test ./tests/e2e/scenarios/ -run TestBasicMemoryScenario -v

# Run all scenarios
go test ./tests/e2e/scenarios/ -run TestAllScenarios -v
```

Available scenarios:
- `basic_memory.yaml` — Store, search, query, edge creation
- `task_planner.yaml` — Task lifecycle: create → status → dependencies → executable
- `contradictions.yaml` — Ingest contradicting facts, verify contradicts edges
- `provenance.yaml` — Store with provenance, query by conversation_id/message_id
- `retrieval.yaml` — Graph traversal, depth limits, explain mode
- `timeline.yaml` — Timeline ordering, temporal filtering
- `communities.yaml` — Connected components, community detection

### Writing New Tests

Every test starts from a clean temporary directory:

```go
func TestMyFeature(t *testing.T) {
    dir, _ := helpers.TempWorkspace(t)
    helpers.WriteConfig(t, dir, helpers.DefaultConfig(helpers.DBPath(dir)))
    
    // CLI test
    cli := helpers.NewCLI(helpers.BinaryPath(t), dir)
    result := cli.Run(t, "memory", "store", `{"id":"e1","category":"world","content":"test"}`)
    result.MustSucceed(t)
    
    // HTTP test
    srv := helpers.StartServer(t, dir)
    client := helpers.NewHTTPClient(srv.URL)
    resp := client.Post(t, "/store", map[string]interface{}{
        "id": "e2", "category": "world", "content": "test2",
    })
    helpers.MustStatus(t, resp, 200)
}
```
