# Official SDKs

Hermem ships official SDKs for Go, Python, and TypeScript. All SDKs share the same design: a `Client` entry point with sub-clients for memory, task, graph, and admin operations.

## Go SDK

### Install

```bash
go get github.com/pavelveter/hermem/sdk/go
```

### Usage

```go
import hermem "github.com/pavelveter/hermem/sdk/go"

client := hermem.New("http://localhost:8420",
    hermem.WithAPIKey("your-key"),
    hermem.WithTimeout(10*time.Second),
)

// Store
client.Memory.Store(ctx, &hermem.StoreRequest{
    ID:       "paris",
    Category: "fact",
    Content:  "Paris is the capital of France",
})

// Search
results, _ := client.Memory.Search(ctx, &hermem.SearchRequest{
    Query: "capital of France",
    TopK:  5,
})

// Tasks
task, _ := client.Task.Create(ctx, &hermem.TaskCreateRequest{
    Content: "Implement feature X",
})

// Graph
components, _ := client.Graph.ConnectedComponents(ctx, 2)

// Health
health, _ := client.Admin.Health(ctx)
```

### Options

| Option | Description |
|--------|-------------|
| `WithAPIKey(key)` | Set API key for authentication |
| `WithTimeout(d)` | Set per-request timeout |
| `WithHTTPClient(hc)` | Use custom `*http.Client` |

### Module

Separate Go module: `github.com/pavelveter/hermem/sdk/go`

## Python SDK

### Install

```bash
pip install hermem
```

### Usage

```python
from hermem import Client, StoreRequest, SearchRequest

client = Client("http://localhost:8420", api_key="your-key")

# Store
client.memory.store(StoreRequest(
    id="paris",
    category="fact",
    content="Paris is the capital of France",
))

# Search
results = client.memory.search(SearchRequest(query="capital of France", limit=5))

# Tasks
task = client.task.create(TaskCreateRequest(content="Implement feature X"))

# Graph
components = client.graph.connected_components(min_size=2)

# Health
health = client.admin.health()
```

### Dependencies

Zero external dependencies — uses only Python stdlib (`urllib.request`).

## TypeScript SDK

### Install

```bash
npm install hermem
```

### Usage

```typescript
import { Client } from "hermem";

const client = new Client("http://localhost:8420", {
  apiKey: "your-key",
  timeout: 30_000,
});

// Store
await client.memory.store({
  id: "paris",
  category: "fact",
  content: "Paris is the capital of France",
});

// Search
const results = await client.memory.search({ query: "capital of France", limit: 5 });

// Tasks
const task = await client.task.create({ content: "Implement feature X" });

// Graph
const components = await client.graph.connectedComponents({ min_size: 2 });

// Health
const health = await client.admin.health();
```

### Options

| Option | Description |
|--------|-------------|
| `apiKey` | API key for authentication |
| `timeout` | Request timeout in milliseconds |

## API reference

All SDKs expose the same sub-clients:

### MemoryClient

| Method | HTTP endpoint |
|--------|---------------|
| `store(req)` | `POST /store` |
| `search(req)` | `POST /search` |
| `retrieve(req)` | `POST /retrieve` |
| `query(req)` | `POST /query` |
| `explain(req)` | `POST /query/explain` |
| `ingest(req)` | `POST /ingest` |
| `edge(req)` | `POST /edge` |
| `reEmbed(req)` | `POST /admin/re-embed` |

### TaskClient

| Method | HTTP endpoint |
|--------|---------------|
| `create(req)` | `POST /task/create` |
| `status(req)` | `POST /task/status` |
| `list(req)` | `POST /task/list` |
| `show(req)` | `POST /task/show` |
| `dep(req)` | `POST /task/dep` |
| `tree(req)` | `POST /task/tree` |
| `rollback(req)` | `POST /task/rollback` |
| `executable(req)` | `POST /task/executable` |
| `next(req)` | `POST /task/next` |

### GraphClient

| Method | HTTP endpoint |
|--------|---------------|
| `verify(req)` | `POST /verify` |
| `contradictions()` | `GET /contradictions` |
| `connectedComponents(minSize)` | `GET /connected-components` |
| `communities(minSize, maxIter)` | `GET /communities` |
| `timeline(limit)` | `GET /timeline` |
| `provenance(req)` | `GET /provenance` |
| `recoveryPlan(id)` | `GET /recovery-plan` |

### AdminClient

| Method | HTTP endpoint |
|--------|---------------|
| `health()` | `GET /health` |
| `ready()` | `GET /health/ready` |
| `migrateStatus()` | `GET /admin/migrate-status` |
| `schema()` | `GET /admin/schema` |
| `verifyDB(dim)` | `POST /verify` |

## Error handling

All SDKs raise `APIError` on HTTP 4xx/5xx responses:

```go
// Go
var apiErr *hermem.APIError
if errors.As(err, &apiErr) {
    fmt.Println(apiErr.StatusCode, apiErr.Message, apiErr.Code)
}
```

```python
# Python
try:
    client.memory.store(req)
except APIError as e:
    print(e.status_code, e.message, e.code)
```

```typescript
// TypeScript
try {
    await client.memory.store(req);
} catch (e) {
    if (e instanceof APIError) {
        console.log(e.statusCode, e.message, e.code);
    }
}
```

## Examples

See `sdk/go/examples/`, `sdk/python/examples/`, `sdk/typescript/examples/`.

## CI

SDK tests run automatically via `.github/workflows/sdk.yml`:
- Go SDK: `go test -race ./...`
- Python SDK: `pytest tests/`
- TypeScript SDK: `vitest run`
