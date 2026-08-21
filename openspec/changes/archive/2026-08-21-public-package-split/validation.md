# Validation record

Executed after tasks 4.5–8.3:

- `go test ./...` — passed.
- `go vet ./pkg/... ./api/... ./src/internal/...` — passed.
- `go test -race ./pkg/... ./api/... ./src/internal/...` — passed.
- `gofmt -s -d` on changed Go packages — clean.
- `bash scripts/check-public-boundaries.sh` — passed.
- `openspec validate "public-package-split" --type change` — valid.

Coverage includes the existing HTTP, MCP, CLI, persistence, provider, and
external-like SPI tests, plus new `api/v1` golden mapper tests, provider
registry descriptor/fail-closed startup tests, and legacy vector compatibility
conformance tests.

The compatibility release intentionally retains `src/internal/core` facade
and the IDs-only `core.VectorIndex` adapter. sqlite-vec/HNSW/remote backends,
embedding BLOB removal, and rollback procedures remain outside this change.
