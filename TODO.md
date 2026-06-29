# TODO: Public Developer Interfaces

OpenAPI 3.1 spec, official SDKs (Go/Python/TypeScript), and native MCP server.

---

## Phase 1 — OpenAPI 3.1 Spec

- [ ] Create `api/openapi.go` — OpenAPI 3.1 spec as Go struct, schemas from `core/types.go`
- [ ] Create `api/spec.go` — JSON/YAML renderers
- [ ] Register `GET /openapi.json` and `GET /openapi.yaml` in `server/server.go`
- [ ] Add `--swagger-ui` flag to `serve` command (embedded Swagger UI + ReDoc)
- [ ] Add `make openapi-check` CI target (diff generated vs committed spec)

---

## Phase 2 — Go SDK

- [ ] Create `sdk/go/hermem.go` — `HermemClient` with sub-clients
- [ ] Create `sdk/go/memory.go` — Store, Search, Query, Retrieve, Ingest, Edge, Explain
- [ ] Create `sdk/go/task.go` — Create, Status, List, Show, Dep, Tree, Rollback, Executable
- [ ] Create `sdk/go/graph.go` — Components, Communities, Verify, Contradictions, Provenance, Timeline
- [ ] Create `sdk/go/errors.go` — typed error mapping
- [ ] Features: configurable base URL, API key, timeout, retries, context support
- [ ] Write integration tests

---

## Phase 3 — Python SDK

- [ ] Generate models from OpenAPI spec via `openapi-python-client`
- [ ] Create `sdk/python/hermem/client.py` — handcrafted ergonomic client
- [ ] Add async client via `httpx`
- [ ] Typed errors mapping HTTP → Python exceptions
- [ ] `pyproject.toml`, docstrings, examples

---

## Phase 4 — TypeScript SDK

- [ ] Generate types from OpenAPI spec via `openapi-typescript`
- [ ] Create `sdk/typescript/src/client.ts` — handcrafted ergonomic client
- [ ] Dual ESM + CJS, browser-safe
- [ ] Zod schemas for runtime validation
- [ ] `package.json`, JSDoc, examples

---

## Phase 5 — MCP Server

- [ ] Create `mcp/server.go` — MCP protocol over stdio + optional SSE
- [ ] Create `mcp/tools.go` — 17 tool definitions with JSON Schema
- [ ] Reuse domain services (no duplicated logic)
- [ ] CLI command: `hermem mcp [--transport stdio|sse] [--port 8421]`

---

## Phase 6 — Examples & CI

- [ ] `examples/openai/` — Function calling with Hermem context
- [ ] `examples/ollama/` — Local offline pipeline
- [ ] `examples/claude-desktop/` — Native MCP integration
- [ ] `examples/cursor/` — MCP tool configuration
- [ ] `examples/vscode/` — Extension integration
- [ ] `examples/mcp-client/` — Generic Python/TypeScript scripts
- [ ] CI: OpenAPI diff check, SDK tests, MCP compliance tests
