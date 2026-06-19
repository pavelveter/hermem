---
name: hermem
description: Lightweight graph memory for Hermes — store facts, search by vector similarity, retrieve connected context
version: 0.1.0
metadata:
  hermes:
    tags: [memory, graph, vector-search, sqlite]
    category: memory
required_environment_variables:
  - name: HERMEM_URL
    help: "Optional: override Hermem server base URL when using HTTP server mode"
    required_for: HTTP server mode only
---

# Hermem — Graph Memory Skill

Lightweight graph memory via the Hermem CLI binary. Use CLI mode by default. HTTP server mode is optional.

## When to Use

- User asks to remember something for future sessions
- User asks \"what do you know about X?\"
- You need to recall past conversations or facts
- You want to store structured knowledge (facts, opinions, experiences, observations)

## Default Mode: CLI

Use the Hermem CLI binary shipped in `~/.hermes/bin/hermem`. No server is required.

```bash
~/.hermes/bin/hermem ...
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

Use the `store` subcommand with JSON on stdin.

```bash
~/.hermes/bin/hermem store <<'JSON'
{"id":"my-fact","category":"opinion","content":"User prefers dark mode in all editors"}
JSON
```

### 2. Search memory

Use the `search` subcommand.

```bash
~/.hermes/bin/hermem search <<'JSON'
{"query":"editor preference","top_k":5}
JSON
```

### 3. Full context retrieval

Use the `query` subcommand for the complete pipeline (search + graph walk + markdown).

```bash
~/.hermes/bin/hermem query <<'JSON'
{"query":"Tell me about the user's preferences"}
JSON
```

This returns markdown-formatted context grouped by category (WORLD, OPINION, EXPERIENCE, OBSERVATION).

### 4. Ingest conversations

After each conversation turn, the provider's `sync_turn` path ingests the dialog. For manual backfill:

```bash
~/.hermes/bin/hermem ingest <<'JSON'
{"dialog":"User: What is Go?\nAssistant: Go is a statically typed language."}
JSON
```

## Optional: HTTP Server Mode

If you need a shared service, start it separately.

```bash
./hermem serve 8420
export HERMEM_URL=http://localhost:8420
```

When `HERMEM_URL` is set, the provider and direct tooling will switch to HTTP automatically.

## Verification

```bash
# Run from the hermem project directory
~/.hermes/bin/hermem store <<'JSON'
{"id":"smoke","category":"world","content":"Hermem smoke test"}
JSON

~/.hermes/bin/hermem query <<'JSON'
{"query":"smoke test"}
JSON
```
