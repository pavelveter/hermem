# Hermem — Hindsight Lite

A lightweight, zero-dependency graph memory system for LLM agents. Stores facts as a directed graph in SQLite with vector embeddings for semantic retrieval.

**Use case:** Give your agent persistent memory across sessions — it remembers what it learned, who you are, what worked, and what didn't.

## Architecture

```
User Query ──> [Embedder] ──> [Vector Search] ──> Top-K Seeds ──> [CTE Graph Walk] ──> Markdown Context
```

The system stores knowledge as entities (nodes) connected by typed edges. Each entity belongs to one of four Hindsight categories:

| Category | Purpose |
|----------|---------|
| `world` | Facts, definitions, objective knowledge |
| `opinion` | User preferences, beliefs, subjective views |
| `experience` | Past events, interactions, what happened |
| `observation` | Patterns noticed, anomalies, derived insights |

## Quick Start

```bash
# Clone and build
git clone <repo-url>
cd hermem
go build -o hermem .

# Run the demo (creates hermem.db)
./hermem
```

## Dependencies

- Go 1.21+
- CGO enabled (for `github.com/mattn/go-sqlite3`)
- One of: Ollama running locally, or an OpenAI API key

## Configuration

All settings are read from `hermem.ini` (INI format). If the file doesn't exist, defaults are used.

### hermem.ini

```ini
[embedder]
provider = ollama          # "ollama" or "openai"
url = http://localhost:11434
model = nomic-embed-text
key =                      # API key for OpenAI (not needed for Ollama)

[database]
path = hermem.db
```

### Provider examples

**Ollama (default):**
```ini
[embedder]
provider = ollama
url = http://localhost:11434
model = nomic-embed-text
```

**OpenAI:**
```ini
[embedder]
provider = openai
url = https://api.openai.com/v1
model = text-embedding-3-small
key = sk-your-api-key-here
```

**Custom OpenAI-compatible endpoint (vLLM, LiteLLM, etc.):**
```ini
[embedder]
provider = openai
url = http://localhost:8000/v1
model = your-model-name
key = your-key
```

### Defaults

| Key | Default | Description |
|-----|---------|-------------|
| `provider` | `ollama` | Embedder backend |
| `url` | `http://localhost:11434` | API endpoint |
| `model` | `nomic-embed-text` | Embedding model name |
| `key` | *(empty)* | API key (OpenAI only) |
| `path` | `hermem.db` | SQLite database file |

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
result, err := RetrieveContext(db, seedIDs, 2)

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
├── db.go            # SQLite schema, embedding serialization, cosine similarity
├── config.go        # INI config loader
├── embedder.go      # Embedder interface (Ollama / OpenAI)
├── vector.go        # Vector search, entity storage
├── retrieval.go     # Recursive CTE graph walk, markdown formatting
├── ingestion.go     # Ingestion worker, entity extraction, deduplication
├── server.go        # HTTP API server
├── main.go          # Entry point (demo or server mode)
├── hermem.ini       # Sample config file
├── verify_test.go   # Integration and timing tests
└── plugins/
    └── memory/
        └── hermem/  # Hermes Agent memory provider plugin
            ├── __init__.py
            └── plugin.yaml
```

## HTTP API Server

Run Hermem as an HTTP service for integration with Hermes Agent or other systems:

```bash
./hermem -server -port 8420
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

### Install the plugin

```bash
# Copy plugin to Hermes plugins directory
cp -r plugins/memory/hermem ~/.hermes/plugins/memory/

# Or symlink for development
ln -s $(pwd)/plugins/memory/hermem ~/.hermes/plugins/memory/hermem
```

### Start Hermem server

```bash
./hermem -server -port 8420
# Or with custom config
./hermem -server -port 8420 -config /path/to/hermem.ini
```

### Configure Hermes

Add to `~/.hermes/config.yaml`:

```yaml
memory:
  provider: hermem
```

Set the server URL (optional, defaults to `http://localhost:8420`):

```bash
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
4. Results are grouped by Hindsight category and formatted as markdown

### Deduplication

When ingesting new facts, the system checks if a similar entity already exists (cosine similarity > 0.88). If found, the new content is appended to the existing entity instead of creating a duplicate.

## Performance

Tested on a local SQLite database:

| Operation | Time |
|-----------|------|
| Vector search (1000 entities) | ~2.3ms |
| Graph walk (depth 2) | ~0.2ms |

The system handles up to ~20k entities with in-memory cosine similarity. Beyond that, consider switching to `sqlite-vec` for indexed vector search.

## Testing

```bash
go test -v -run TestIntegration
go test -v -run TestTiming
```

## License

MIT
