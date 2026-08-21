## 1. Inventory and contract scaffolding

- [x] 1.1 Inventory `src/internal/core` types, interfaces, DTOs, utilities, and all importers; classify each as `pkg/domain`, `pkg/spi`, `api/v1`, internal-only, or follow-on ADR work.
- [x] 1.2 Add the `pkg/domain` package skeleton with dependency-free package tests and the public entity, edge, task, identifier, draft, and domain-error contracts.
- [x] 1.3 Add the `pkg/spi` package skeleton with Embedder, optional BatchEmbedder, Extractor, Reranker, VectorStore, provider descriptor, typed factory, filter, stats, and typed-error contracts.
- [x] 1.4 Add compile-time contract tests proving public packages do not import `src/internal`, transport, storage, SQL drivers, or concrete providers.

## 2. Domain ownership and legacy facade

- [x] 2.1 Move pure domain model definitions from `src/internal/core` into `pkg/domain` without changing current serialized field behavior at transport boundaries.
- [x] 2.2 Replace migrated `core` domain definitions with deprecated aliases or narrow adapters whose source of truth is `pkg/domain`.
- [x] 2.3 Remove LLM-selected identity from the public extraction draft contract while preserving a compatibility adapter for the current extractor result until ADR-035 ingestion migration lands.
- [x] 2.4 Add migration documentation and deprecation comments that prevent new features from extending the legacy `core` facade.

## 3. Provider SPI and compatibility adapters

- [x] 3.1 Implement adapters for current embedding, extraction, and reranking implementations to satisfy the new `pkg/spi` contracts.
- [x] 3.2 Implement the new-to-legacy embedding/vector adapters needed by existing callers, explicitly preserving IDs-only behavior and returning unsupported errors for unavailable new semantics.
- [x] 3.3 Add conformance tests for current built-in embedding, extraction, and reranking providers plus the legacy vector adapter; native vector backend conformance is covered by tasks 4.1–4.3 after VectorStore migration.
- [x] 3.4 Add an external-like fixture provider that compiles using only `pkg/domain` and `pkg/spi`.

## 4. VectorStore migration

- [x] 4.1 Implement the namespace-aware `InMemoryVectorStore` over the compatibility-safe in-memory engine with `VectorStore` upsert, delete, stats, filtered search, and scored hit results.
- [x] 4.2 Replace silent capacity eviction and dimension fallback with explicit errors or observable configured policies in the in-memory engines.
- [x] 4.3 Add vector conformance tests for score ordering, namespace isolation, equality/range filters, dimension errors, idempotent upsert, partial failure reporting, and capacity behavior.
- [x] 4.4 Migrate vector consumers in ingestion, health, re-embedding, compression, memory, edge, and retrieval to the new contract where their behavior is covered by this change.
- [x] 4.5 Keep the legacy IDs-only adapter until all in-repo callers stop depending on `core.VectorIndex`; do not add methods to the legacy interface.
- [x] 4.6 Leave sqlite-vec, HNSW, remote vector backends, embedding BLOB removal, and migration rollback procedures to their dedicated ADR changes.

## 5. Explicit provider registry and configuration

- [x] 5.1 Add application-owned typed registries for embedders, extractors, rerankers, and vector stores with duplicate-name and missing-provider errors.
- [x] 5.2 Convert built-in provider factories to explicit registration through `internal/app` without mandatory `init()` side effects or global mutable registries.
- [x] 5.3 Add provider descriptors and provider-owned configuration validation, including capability and dimension reporting where applicable.
- [x] 5.4 Add a compatibility adapter from the current flat provider configuration fields to provider-scoped configuration sections.
- [x] 5.5 Add startup and diagnostics tests proving invalid, missing, duplicate, and unsupported provider configurations fail closed before serving.

## 6. Transport and caller migration

- [x] 6.1 Define versioned `api/v1` request/response DTOs for migrated HTTP contracts without importing domain DTOs as wire types.
- [x] 6.2 Add DTO-to-domain and domain-to-DTO mappers with golden JSON tests covering defaults, omitted fields, validation errors, and existing response compatibility.
- [x] 6.3 Update HTTP shells to use the mappers while preserving current routes and wire behavior.
- [x] 6.4 Update MCP and CLI adapters to consume public domain values without importing HTTP DTOs.
- [x] 6.5 Migrate new and changed service wiring to public domain/SPI contracts while leaving retrieval-stage redesign to ADR-037.

## 7. Boundary guardrails and validation

- [x] 7.1 Add CI import-direction checks for public packages, legacy facade usage, transport mapper boundaries, SQL/SQLite leakage, and concrete provider imports.
- [x] 7.2 Add public API compatibility checks that detect required-method additions and accidental exported implementation types.
- [x] 7.3 Add integration coverage proving current HTTP, MCP, CLI, persistence, and provider behavior remains unchanged through the compatibility layer.
- [x] 7.4 Run formatting, unit tests, vet, race-enabled tests, API contract tests, and external-like package compilation; record results for the change.

## 8. Deprecation and release boundary

- [x] 8.1 Mark migrated `core` types and interfaces deprecated and publish the import migration guide for `pkg/domain`, `pkg/spi`, and `api/v1`.
- [x] 8.2 Verify no in-repo production caller adds a new dependency on the legacy facade after the migration is complete.
- [x] 8.3 Define the release gate for removing the facade in the subsequent breaking release; do not remove it in this compatibility release.
