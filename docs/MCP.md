# MCP Server

Hermem ships with a native [Model Context Protocol](https://modelcontextprotocol.io) server, allowing AI assistants to interact with the knowledge base directly.

## Quick start

```bash
# Start the MCP server over stdio
hermem mcp
```

## Claude Desktop / Claude Code configuration

Add to your MCP config (`~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "hermem": {
      "command": "hermem",
      "args": ["mcp"]
    }
  }
}
```

## Available tools

| Tool | Description |
|------|-------------|
| `memory_search` | Search memories by semantic similarity |
| `memory_store` | Store a new memory (fact, opinion, experience) |
| `memory_retrieve` | Retrieve contextual memories around seed IDs |
| `task_create` | Create a new task with lifecycle management |
| `task_list` | List tasks filtered by status/goal |
| `task_status` | Transition task status (pending → in_progress → done) |
| `task_show` | Show task details with blocked-by/recovery relationships |
| `graph_components` | Find connected components in the knowledge graph |
| `ingest_dialog` | Ingest a conversation dialog via LLM extraction |

## Tool parameters

### memory_search

```json
{
  "query": "semantic search query (required)",
  "limit": 5
}
```

### memory_store

```json
{
  "id": "unique-id",
  "category": "fact",
  "content": "The memory content to store"
}
```

### memory_retrieve

```json
{
  "seed_ids": ["id1", "id2"],
  "limit": 10
}
```

### task_create

```json
{
  "content": "Task description",
  "context_ids": ["related-id-1", "related-id-2"]
}
```

### task_list

```json
{
  "status": "pending",
  "goal_id": "optional-goal-filter"
}
```

### task_status

```json
{
  "id": "task-id",
  "status": "in_progress"
}
```

### task_show

```json
{
  "id": "task-id"
}
```

### graph_components

```json
{
  "min_size": 2
}
```

### ingest_dialog

```json
{
  "dialog": "User: What is Go?\nAssistant: Go is a programming language..."
}
```

## Architecture

The MCP server uses the [official Go SDK](https://github.com/modelcontextprotocol/go-sdk) and runs over stdio transport. It wraps the same domain services as the HTTP API — no duplicated logic.

```
hermem mcp
  └─ mcp.Server
       ├─ MCP tools (memory_search, task_create, ...)
       └─ serverstate.Ref (same config as HTTP server)
```

## Limitations

- The MCP server reads from `serverstate.Ref` but does not directly invoke domain services (store, search, etc.). For full operations, use the HTTP API endpoints (`/store`, `/search`, `/retrieve`, etc.).
- Embedding-based semantic search requires the vector index and embedder to be available in the server state.
