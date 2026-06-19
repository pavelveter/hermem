---
name: hermem
description: Lightweight graph memory for Hermes — store facts, search by vector similarity, retrieve connected context
version: 0.1.0
metadata:
  hermes:
    tags: [memory, graph, vector-search, sqlite]
    category: memory
    config:
      - key: hermem.url
        description: Hermem server URL
        default: "http://localhost:8420"
        prompt: "Hermem server URL (e.g. http://localhost:8420)"
required_environment_variables:
  - name: HERMEM_URL
    help: "Set HERMEM_URL if Hermem runs on a non-default address"
    required_for: connecting to Hermem server
---

# Hermem — Graph Memory Skill

Lightweight graph memory system that stores facts as entities connected by typed edges, with vector embeddings for semantic search.

## When to Use

- User asks to remember something for future sessions
- User asks "what do you know about X?"
- You need to recall past conversations or facts
- You want to store structured knowledge (facts, opinions, experiences, observations)

## Prerequisites

Hermem server must be running:

```bash
# Start the server (from the hermem project directory)
./hermem -server -port 8420
```

## Memory Categories

| Category | What to store |
|----------|---------------|
| `world` | Facts, definitions, objective knowledge |
| `opinion` | User preferences, beliefs, subjective views |
| `experience` | Past events, interactions, what happened |
| `observation` | Patterns noticed, anomalies, derived insights |

## Procedure

### 1. Store a fact

Use `hermem_store` tool:

```
hermem_store(content="User prefers dark mode in all editors", category="opinion")
hermem_store(content="Project uses Go 1.22 with Chi router", category="world")
hermem_store(content="Deployed v2.1 to production on 2026-01-15", category="experience")
```

### 2. Search memory

Use `hermem_search` for vector similarity search:

```
hermem_search(query="What does the user prefer for editors?")
hermem_search(query="deployment history", limit=5)
```

### 3. Full context retrieval

Use `hermem_query` for the complete pipeline (search + graph walk + markdown):

```
hermem_query(query="Tell me about the user's preferences")
```

This returns markdown-formatted context grouped by category (WORLD, OPINION, EXPERIENCE, OBSERVATION) — ready to inject into your response.

### 4. Ingest conversations

After each conversation turn, the `sync_turn` function automatically sends dialog to Hermem for entity extraction. No manual action needed if the memory provider plugin is active.

## API Reference

If you need to call Hermem directly via HTTP:

### Health check
```bash
curl http://localhost:8420/health
```

### Store entity
```bash
curl -X POST http://localhost:8420/store \
  -H "Content-Type: application/json" \
  -d '{"id":"my-fact","category":"world","content":"Paris is the capital of France"}'
```

### Vector search
```bash
curl -X POST http://localhost:8420/search \
  -H "Content-Type: application/json" \
  -d '{"query":"capital of France","top_k":5}'
```

### Full query (search + graph walk)
```bash
curl -X POST http://localhost:8420/query \
  -H "Content-Type: application/json" \
  -d '{"query":"Tell me about France"}'
```

### Ingest dialog
```bash
curl -X POST http://localhost:8420/ingest \
  -H "Content-Type: application/json" \
  -d '{"dialog":"User: What is Go?\nAssistant: Go is a statically typed language."}'
```

## Pitfalls

- Hermem server must be running before using the tools
- Default URL is `http://localhost:8420` — set `HERMEM_URL` env var if different
- Entities with similar content (>88% cosine similarity) are merged automatically
- Graph walk depth defaults to 2 hops — increase via `max_depth` parameter

## Verification

Check Hermem is working:

```bash
# 1. Health check
curl http://localhost:8420/health
# Should return: {"status":"ok"}

# 2. Store and retrieve
curl -X POST http://localhost:8420/store \
  -H "Content-Type: application/json" \
  -d '{"id":"test","category":"world","content":"Hermem is working"}'

curl -X POST http://localhost:8420/query \
  -H "Content-Type: application/json" \
  -d '{"query":"Is Hermem working?"}'
# Should return markdown with the stored fact
```
