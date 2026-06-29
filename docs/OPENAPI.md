# OpenAPI 3.1 Specification

Hermem generates an OpenAPI 3.1 spec from Go structs, served at two endpoints.

## Endpoints

| Path | Format | Description |
|------|--------|-------------|
| `GET /openapi.json` | JSON | OpenAPI 3.1 spec |
| `GET /openapi.yaml` | YAML | OpenAPI 3.1 spec |

```bash
# Fetch the spec
curl -s http://localhost:8420/openapi.json | jq .
curl -s http://localhost:8420/openapi.yaml
```

## Spec structure

The spec is defined in `api/openapi.go` as Go structs that marshal to JSON/YAML:

- **35+ paths** covering memory, task, graph, temporal, admin, and health endpoints
- **30+ component schemas** (Entity, Edge, SearchResult, RetrievalResult, Task, etc.)
- **Auth**: `X-API-Key` header
- **Tags**: memory, ingest, task, graph, temporal, admin, health

## Using the spec

### Code generation

```bash
# TypeScript client
npx openapi-typescript http://localhost:8420/openapi.json -o types.ts

# Python client
pip install openapi-python-client
openapi-python-client generate --url http://localhost:8420/openapi.json

# Go client
go install github.com/deepmap/oapi-codegen/cmd/oapi-codegen@latest
oapi-codegen -generate types -o types.go http://localhost:8420/openapi.json
```

### Swagger UI / ReDoc

```bash
# Serve Swagger UI locally
npx swagger-ui-dist http://localhost:8420/openapi.json

# Or open in browser
open https://editor.swagger.io/?url=http://localhost:8420/openapi.json
```

## Spec generation

The spec is generated at startup via `api.GenerateSpec()`. To verify the spec matches the code:

```bash
# The spec is committed to the repo — any drift is caught by code review
go test ./api/ -run TestSpec
```

### Tests

`api/openapi_test.go` verifies:
- Spec generates without error
- All paths have operationId
- All paths have at least one tag
- All schemas have descriptions
- Path count and schema count meet minimum thresholds
