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
| `memory_explain` | Return ScoreBreakdown for a single node: similarity, recency, centrality |
| `task_create` | Create a new task with lifecycle management |
| `task_list` | List tasks filtered by status/goal |
| `task_status` | Transition task status (pending → in_progress → done) |
| `task_show` | Show task details with blocked-by/recovery relationships |
| `task_rollback` | Cascade-rollback a failed task and all tasks blocked-by it |
| `task_tree` | Render the task dependency tree as an ASCII diagram |
| `graph_components` | Find connected components in the knowledge graph |
| `graph_communities` | Detect knowledge clusters via Louvain community detection |
| `graph_contradictions` | List all contradiction edges with conflicting node pairs and content |
| `graph_verify` | Run graph integrity checks (orphan edges, dimension mismatches) |
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

### graph_communities

```json
{
  "max_iterations": 10
}
```

Returns community clusters with member lists, sizes, and modularity scores.

### graph_verify

```json
{}
```

Returns `{"pass": true, "issues": [], "count": 0}` when the graph is clean, or a list of issues otherwise.

### task_tree

```json
{
  "goal_id": "optional-goal-filter"
}
```

Returns a human-readable ASCII tree of all tasks (or filtered by goal subtree).

### task_rollback

```json
{
  "id": "task-id",
  "error_context": "Optional reason for failure"
}
```

Marks the root task and all transitively-blocked dependents as rolled back. Appends error context to each task's content. Returns the list of rolled-back tasks with their new statuses.

### memory_explain

```json
{
  "id": "node-id"
}
```

With optional query for vector-similarity context:
```json
{
  "id": "node-id",
  "query": "Is this fact still relevant?"
}
```

Returns a ScoreBreakdown with vector similarity (0 unless query is provided), recency decay, temporal decay, centrality, and the final composite score with weights.

### graph_contradictions

```json
{}
```

With optional entity filter:
```json
{
  "id": "entity-id"
}
```

Returns contradiction pairs with source/target IDs and content. Each pair shows two nodes that contradict each other, with provenance (which node contradicts which).

## Exposed Resources

Hermem exposes read-only runtime data as MCP Resources at `hermem://` URIs:

| URI | Description |
|-----|-------------|
| `hermem://graph/verify` | Current graph integrity report (pass/fail + issues list) |
| `hermem://tasks/active` | Currently-executable tasks (all blockers done) |
| `hermem://timeline/recent` | Most recently created entities (up to 100) |
| `hermem://contradictions/all` | Every contradiction edge with conflicting node content |

Resources are read-only JSON data. AI assistants can subscribe to resources for real-time updates when the underlying data changes.

## Architecture

The MCP server uses the [official Go SDK](https://github.com/modelcontextprotocol/go-sdk) and runs over stdio transport. It wraps the same domain services as the HTTP API — no duplicated logic. Each tool calls domain service methods directly (e.g., `Memory.Store()`, `Retrieve.Search()`, `Task.Create()`).

```
hermem mcp
  └─ mcp.Server
       ├─ MCP tools (memory_search, task_create, ...)
       ├─ memory.Service    ← domain store
       ├─ retrieval.Service ← search, retrieve
       ├─ task.Service      ← task lifecycle
       ├─ graph.Service     ← connected components
       ├─ ingest.Service    ← dialog ingestion
       └─ serverstate.Ref   ← schema config
```
