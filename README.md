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

### Embedder

By default, the embedder connects to a local Ollama instance. To use OpenAI instead:

```go
// Ollama (default)
embedder := NewOllamaEmbedder("http://localhost:11434", "nomic-embed-text")

// OpenAI
embedder := NewOpenAIEmbedder("https://api.openai.com/v1", "sk-...", "text-embedding-3-small")
```

### Database

The database is initialized automatically on first run. Default path: `hermem.db`.

```go
db, err := InitDB("path/to/your.db")
```

WAL mode and `PRAGMA synchronous = NORMAL` are enabled by default for performance.

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
├── embedder.go      # Embedder interface (Ollama / OpenAI)
├── vector.go        # Vector search, entity storage
├── retrieval.go     # Recursive CTE graph walk, markdown formatting
├── ingestion.go     # Ingestion worker, entity extraction, deduplication
├── main.go          # Demo / entry point
└── verify_test.go   # Integration and timing tests
```

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
