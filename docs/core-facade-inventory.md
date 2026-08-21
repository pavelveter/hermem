# `src/internal/core` facade inventory

This is the checked-in inventory backing the `remove-core-facade-breaking-release`
change. It lists the complete exported surface of `src/internal/core` and assigns
each symbol a single owner for the breaking removal.

Generated from `go doc -all github.com/pavelveter/hermem/src/internal/core` on
2026-08-20.

Scope: the `core` package's exported Go surface. `src/internal/core/fsutil` is a
separate subpackage and is relocated as part of the facade removal, not listed
as a `core` symbol.

## Ownership legend

- `pkg/domain` — public domain values and domain errors.
- `pkg/spi` — public provider capability contracts.
- `api/v1` — versioned HTTP transport DTOs.
- `internal/*` — implementation-owned type, usually in the package that owns the behavior.
- `delete` — compatibility-only surface removed after its callers migrate.
- `ADR-037` — retrieval stage contract, finalized before the `Retriever` surface is removed.

## Domain aliases and projections

| Symbol | Owner |
|---|---|
| `Entity` | `pkg/domain` |
| `Edge` | `pkg/domain` |
| `Task` | `pkg/domain` |
| `Fact` | `pkg/domain` |
| `Evidence` | `pkg/domain` |
| `Episode` | `pkg/domain` |
| `Belief` | `pkg/domain` |
| `Goal` | `pkg/domain` |
| `SchemaConfig` | `pkg/domain` |
| `DefaultSchemaConfig` | `pkg/domain` |
| `Compose` | `pkg/domain` |
| `ComposeFromTask` | `pkg/domain` |

## Provider capability interfaces

| Symbol | Owner |
|---|---|
| `Embedder` | `pkg/spi` (health/Ping stays internal via adapter) |
| `VectorIndex` | `pkg/spi.VectorStore` or owning internal interface |
| `LLMExtractor` | `pkg/spi.Extractor` |
| `Reranker` | `pkg/spi.Reranker` |

## HTTP transport DTOs

| Symbol | Owner |
|---|---|
| `ErrorResponse` | `api/v1` |
| `StoreRequest` | `api/v1` |
| `SearchRequest` | `api/v1` |
| `TemporalQueryRequest` | `api/v1` |
| `RetrieveRequest` | `api/v1` |
| `IngestRequest` | `api/v1` |
| `EdgeRequest` | `api/v1` |
| `SearchResult` | `api/v1` (HTTP search response) |
| `TaskStatusRequest` | `api/v1` |
| `TaskExecutableResponse` | `api/v1` |
| `TaskListRequest` | `api/v1` |
| `TaskShowRequest` | `api/v1` |
| `TaskShowResponse` | `api/v1` |
| `TaskDepRequest` | `api/v1` |
| `TaskRollbackRequest` | `api/v1` |
| `TaskRollbackResponse` | `api/v1` |
| `TaskTreeRequest` | `api/v1` |
| `TaskTreeResponse` | `api/v1` |
| `TaskCreateRequest` | `api/v1` |
| `TaskCreateResponse` | `api/v1` |
| `TaskClaimRequest` | `api/v1` |
| `TaskClaimResponse` | `api/v1` |

## Internal policy, domain, and retrieval types

| Symbol | Owner |
|---|---|
| `RetentionPolicy` | `internal/retention` |
| `RankingWeight` | `internal/retrieval` |
| `GraphNode` | `ADR-037` / `internal/retrieval` |
| `RetrievalResult` | `ADR-037` / `internal/retrieval` |
| `RetrieveContextOptions` | `ADR-037` / `internal/retrieval` |
| `RetrievedFact` | `ADR-037` / `internal/retrieval` |
| `ScoreBreakdown` | `ADR-037` / `internal/retrieval` |
| `CompositeScorer` | `ADR-037` / `internal/retrieval` |
| `Retriever` | `ADR-037` (not mechanically promoted) |
| `Provenance` | `internal/ingestion` |
| `MemoryMessage` | `internal/ingestion` |
| `Relation` | `internal/ingestion` (extraction compatibility) |
| `ExtractedEntity` | `delete` after ADR-035 extraction migration |
| `ExtractionResult` | `delete` after ADR-035 extraction migration |
| `LegacyEntityDraft` | `delete` after ADR-035 extraction migration |
| `LegacyExtractionDrafts` | `delete` after ADR-035 extraction migration |
| `ReEmbedResult` | `internal/reembed` |
| `VerifyReport` | `internal/graph` |
| `Community` | `internal/graph/community` |
| `ContradictionPair` | `internal/contradiction` |
| `ConnectedComponent` | `internal/graph` |
| `Polarity` | `internal/memory/evidence` or owning domain value |
| `TreeNode` | `internal/task` |
| `MigrationStatus` | `internal/migration` |
| `MigrationMismatch` | `internal/migration` |
| `Migrator` | `internal/migration` |
| `Component` | `internal/lifecycle` |
| `Logger` | `internal/logging` |
| `NoopLogger` | `internal/logging` |

## Error taxonomy

| Symbol | Owner |
|---|---|
| `DomainError` | `internal/apperror` (or reviewed domain error) |
| `CodeNotFound`, `CodeConflict`, `CodeInvalidInput`, `CodeSchemaConflict`, `CodeUnauthorized`, `CodeInvalidSchema`, `CodeInvalidGraph`, `CodeCorruptedIndex`, `CodeInternalError` | `internal/apperror` |
| `ErrNotFound`, `ErrConflict`, `ErrInvalidInput`, `ErrSchemaConflict`, `ErrUnauthorized`, `ErrInvalidGraph`, `ErrCorruptedIndex` | `internal/apperror` |
| `NewNotFoundError`, `NewConflictError`, `NewInvalidInputError`, `NewSchemaConflictError`, `NewInvalidSchemaError`, `NewInvalidGraphError`, `NewCorruptedIndexError` | `internal/apperror` |

Note: `CodeInternalError` has no matching sentinel or constructor today; it is
used as a bare classification string. The owning error taxonomy should
reconcile that asymmetry during migration.

## Helpers and utilities

| Symbol | Owner |
|---|---|
| `NewTaskID` | `delete` / ADR-035 identity service |
| `TimePtr` | `internal/util` |
| `NormalizeSlice` | `internal/util` |

## Compatibility baseline

The compatibility-release baseline is the set of existing, checked-in
contracts, snapshots, and benchmark baselines rather than a new duplicated
copy:

- HTTP/OpenAPI JSON: `api/openapi_test.go` byte-snapshot suite and
  `api/v1` golden mapper tests.
- Wire-byte stability history: `docs/CHANGELOG.md` and `docs/adr/026-typed-entity-models.md`.
- Benchmark baseline: `bench/baseline/baseline.txt` and
  `docs/profiling.md`.
- MCP behavior: `src/internal/mcp` tool contracts exercised by integration tests.
- CLI output: `tests/e2e/cli` and `src/internal/cli` command tests.
- Persistence and provider behavior: `src/internal/store`, `src/internal/ai`,
  `src/internal/vector`, `src/internal/spiadapter`, and domain service tests.

These become the comparison point for tasks 5.3, 5.4, and 7.4. Any intentional
behavior change outside facade removal must be a separate change with its own
baseline update.
