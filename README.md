<p align="center">
  <img src="banner.jpg" alt="Hermem" width="600">
</p>

# Hermem

A lightweight, zero-dependency graph memory system for LLM agents. Stores facts as a directed graph in SQLite with vector embeddings for semantic retrieval.

**Use case:** Give your agent persistent memory across sessions — it remembers what it learned, who you are, what worked, and what didn't.

## Architecture

```
User Query ──> [Embedder] ──> [Vector Search] ──> Top-K Seeds ──> [CTE Graph Walk] ──> Markdown Context
```

The system stores knowledge as entities (nodes) connected by typed edges. Each entity belongs to one of four memory categories:

| Category | Purpose |
|----------|---------|
| `world` | Facts, definitions, objective knowledge |
| `opinion` | User preferences, beliefs, subjective views |
| `experience` | Past events, interactions, what happened |
| `observation` | Patterns noticed, anomalies, derived insights |

## Features

- **CLI + HTTP server** — single binary, two modes
- **OpenAI-compatible** — works with Ollama or any OpenAI-compatible API
- **Separate embedder/extractor providers** — Ollama for embeddings, OpenAI for extraction (or vice versa)
- **Pluggable vector search** — `InMemoryVectorIndex` (default, zero-dependency) or `SqliteVecIndex` via `sqlite-vec` (indexed KNN)
- **Accelerate SIMD** — `cblas_sdot` via CGo for NEON-optimised cosine on Apple Silicon
- **Automatic retention** — configurable GC loop archives stale observation nodes
- **API key auth** — optional `X-API-Key` middleware
- **Structured logging** — `log/slog` with event fields + `request_id` tracing
- **Request tracing** — every HTTP response gets `X-Request-ID`
- **Metrics** — `/metrics` endpoint via `expvar`
- **Graceful shutdown** — drains in-flight requests on SIGINT/SIGTERM
- **Strict JSON validation** — unknown fields rejected with structured errors
- **Docker** — multi-stage build, non-root user

## Quick Start

```bash
# Clone and build
git clone https://github.com/pavelveter/hermem.git
cd hermem
go build -o hermem ./src

# Run the demo (creates hermem.db)
./hermem
```

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
# Build
go build -o hermem ./src

# Copy to PATH
cp hermem /usr/local/bin/

# Configure: place hermem.ini *next to the binary* so the
# binary-dir resolution picks it up from any working directory.
sudo cp hermem.ini /usr/local/bin/hermem.ini

# Run CLI (works regardless of cwd)
echo '{"query":"What is Go?"}' | hermem query

# Or run as server
hermem serve 8420
```

## Dependencies

- Go 1.21+
- CGO enabled (for `github.com/mattn/go-sqlite3` + `sqlite-vec`)
- One of: Ollama running locally, or an OpenAI API key
- (Optional) `sqlite-vec` — statically linked via `github.com/asg017/sqlite-vec-go-bindings/cgo` when `[database] backend = sqlite-vec`

## Configuration

All settings are read from `hermem.ini` **next to the binary executable**
(`os.Executable()`-resolved), so `~/.hermes/bin/hermem store` behaves
the same regardless of the caller's working directory — a stray
`hermem.db` no longer leaks into a transient CWD. INI format.
If the file doesn't exist, defaults are used.

### hermem.ini — all settings

```ini
[embedder]
provider = ollama               # "ollama" | "openai"
url = http://localhost:11434
model = nomic-embed-text
key =                           # API key for OpenAI (not needed for Ollama)

[extraction]
; provider, url, key — optional, fall back to [embedder] values
provider = ollama               # "ollama" | "openai"
url = http://localhost:11434
key =                           # API key for OpenAI
model = qwen2.5-coder:7b
temperature = 0.1

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
StoreEntityWithEmbedding(db, entity)
```

### 2. Vector search

```go
results, err := SearchByVector(db, queryEmbedding, 10) // top 10
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

```
hermem/
├── src/
│   ├── db.go                # SQLite schema, embedding serialization
│   ├── config.go            # INI config loader
│   ├── embedder.go          # Embedder interface (Ollama / OpenAI)
│   ├── extractor.go         # LLMExtractor interface + allowlist filtering
│   ├── vector.go            # VectorIndex interface + wrappers
│   ├── vector_inmemory.go   # InMemoryVectorIndex — RAM-cached cosine scan
│   ├── vector_sqlitevec.go  # SqliteVecIndex — vec0 KNN (build tag)
│   ├── cosine.go            # CosineSimilarity — pure Go fallback (!darwin)
│   ├── cosine_darwin.go     # CosineSimilarity — Apple Accelerate NEON (darwin)
│   ├── retrieval.go         # Recursive CTE graph walk, ranking, markdown
│   ├── ingestion.go         # Ingestion worker, entity extraction, dedup
│   ├── server.go            # HTTP API server + strict JSON decoder
│   ├── main.go              # Entry point (CLI subcommands + serve)
│   ├── metrics.go           # expvar metrics endpoint
│   ├── middleware.go        # Auth + request-id middleware
│   ├── retention.go         # Garbage collector + retention policy
│   ├── *_test.go            # Per-package tests (90 tests)
├── hermem.ini               # Sample config file
├── plugins/
│   └── memory/
│       └── hermem/          # Hermes Agent memory provider plugin
│           ├── __init__.py
│           └── plugin.yaml
```

## HTTP API Server

Run Hermem as an HTTP service for integration with Hermes Agent or other systems:

```bash
./hermem serve 8420
```

### Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Health check |
| `/store` | POST | Store an entity |
| `/search` | POST | Vector similarity search |
| `/retrieve` | POST | Graph walk from seed IDs |
| `/ingest` | POST | Ingest dialog text |
| `/query` | POST | Full pipeline: search + graph walk + markdown |

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
~/.hermes/bin/hermem serve 8420
export HERMEM_URL=http://localhost:8420
```

### Plugin tools

The plugin exposes three tools to the Hermes agent:

| Tool | Description |
|------|-------------|
| `hermem_search` | Search graph memory by vector similarity |
| `hermem_store` | Store a fact in graph memory |
| `hermem_query` | Full pipeline: search + graph walk + markdown context |

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
similarity ∈ [0, 1] for unit-length embeddings), the new content is
merged into the existing entity (concatenated with `"; "` if not
already substring-contained), re-embedded, and persisted. Otherwise a
new row is created and the relations from the extraction are appended
as `INSERT OR IGNORE` edges (composite-PK dedup on
`(source_id, target_id, relation_type)`).

### Extraction validation

`OllamaLLMExtractor` enforces a hardcoded allowlist of categories
(`world` / `opinion` / `experience` / `observation`) and relation
types (`prefers` / `uses` / `mentions` / `related_to` / `part_of` /
`causes` / `contradicts`) at parse time via `filterEntities` and
`filterRelations`. Out-of-allowlist values are silently dropped
rather than aborting the ingest, so a partially-correct LLM output
still yields graph-safe entities. The 5xx-retry / 4xx-no-retry path
is retry-budgeted (3 attempts, exponential backoff 200ms→2s, capped
total latency).

## Performance

Measured by `go test -v -run TestTiming -count=1 ./...`. The
test seeds a single SQLite file-backed DB incrementally for
entities and rebuilds the edges table from scratch per cohort
**against the cohort's `n`**, so the seeded graph is
characteristic of N at every measurement slice. Numbers are
machine-dependent; re-run the test to refresh.

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

Benchmarked on Apple M1 (768D embeddings, `topK=10`):

| N | In-Memory (cache + Accelerate) | sqlite-vec (KNN index) | Allocs (mem / vec) |
|--:|-------------------------------:|-----------------------:|-------------------:|
| 100 | 77 µs | 410 µs | 245 / 290 |
| 1,000 | 473 µs | 1.0 ms | 245 / 290 |
| 5,000 | 3.1 ms | 6.8 ms | 245 / 290 |
| 10,000 | 5.9 ms | 10.2 ms | 245 / 290 |

### Scaling

- **In-Memory** (`InMemoryVectorIndex`, default) — RAM-cached
  O(N) cosine scan via Apple Accelerate framework (`cblas_sdot`,
  NEON SIMD). 245 allocs per search (vs 70,268 in SQLite scan).
  At 10K entities ~5.9 ms. Good for datasets up to ~20K entities.
- **sqlite-vec** (`SqliteVecIndex`, `[database] backend = sqlite-vec`)
  — indexed KNN via `vec0` virtual table. 290 allocs per search.
  SQLite query overhead (plan, MATCH, distance sort). At N < 100K
  in-memory is faster on M1; sqlite-vec pulls ahead at larger
  scales where O(N) scan becomes prohibitive.
- **Graph walk** — dominated by SQLite recursive-CTE JOIN
  cost over edges, scales roughly linearly with edge count.

## Testing

```bash
go test -count=1 ./src                     # full suite (90 tests, ~3s)
go test -v -count=1 -run TestServer ./...  # strict-decode table only
go test -bench=BenchmarkInMemorySearch -benchmem -count=3 ./...    # in-memory vector perf
go test -tags sqlite_vec -bench=BenchmarkSqliteVecSearch -benchmem -count=3 ./...  # sqlite-vec perf
go test -v -run TestIntegration
go test -v -run TestTiming
```

The per-package tests (`config_test.go`, `vector_test.go`,
`retrieval_test.go`, `ingestion_test.go`, `extractor_test.go`,
`server_test.go`) use `helpers_test.go → memDB(t)` (`:memory:`
SQLite + `stubEmbedder`/`stubExtractor` mocks); `verify_test.go` is
file-backed so its `TestTiming` exercises the real `PRAGMA journal_mode
= WAL` path.

## Documentation

For the full operator runbook — CLI mode and server mode side-by-side,
request/response reference, the strict-decode error model, DB schema
notes, embedding-model/dimension gotchas, and operational pitfalls —
see **[USAGE.md](USAGE.md)**.

## License

MIT
