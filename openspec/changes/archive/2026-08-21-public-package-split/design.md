## Context

See `proposal.md` for the motivation and scope. The current source of truth is
`src/internal/core`; it is imported by domain services, transport shells,
providers, vector implementations, CLI, MCP, and application wiring. Phase 0
already established `app.Application` as the production lifecycle owner, so
this change can focus on type ownership and dependency direction without
combining another lifecycle refactor.

The repository has no existing OpenSpec capability specs. ADR-031 is the
normative contract for this design, while ADR-028 owns vector behavior and
ADR-029 owns provider discovery/configuration.

## Goals / Non-Goals

**Goals:**

- Make `pkg/domain` the source of truth for pure domain models.
- Make `pkg/spi` the source of truth for stable provider capabilities.
- Make `pkg/spi.VectorStore` the scored, filtered, namespaced vector boundary.
- Keep existing HTTP, MCP, CLI, persistence, and provider behavior stable
  throughout the migration.
- Provide a one-release compatibility path from `core` to the public packages.
- Make application composition explicit and keep registry/lifecycle ownership
  in `internal/app`.
- Add compile-time and CI guardrails against public-package dependency leaks.

**Non-Goals:**

- Implementing a second database engine or repository abstraction; ADR-027
  remains a follow-on change.
- Implementing HNSW, sqlite-vec, remote vector backends, or deleting the
  embedding BLOB; ADR-028/033 govern those changes.
- Implementing OIDC, tenancy, asynchronous ingestion, or retrieval pipeline
  stages.
- Loading arbitrary Go `.so` plugins or implementing the phase-2 gRPC plugin
  protocol.
- Removing the `core` facade in the same release in which the public packages
  are introduced.

## Decisions

### 1. Move ownership, then add compatibility aliases

The new packages become the source of truth; `core` does not remain the owner
with public wrappers layered on top.

Migration order:

```text
new pkg/domain values
        │
        ├── internal/core aliases/facade
        ├── API DTO mappers
        └── domain service consumers

new pkg/spi contracts
        │
        ├── typed provider registries
        ├── implementation adapters
        └── internal/core legacy interfaces
```

`pkg/domain` imports only the standard library. `src/internal/core` imports
`pkg/domain` for migrated types and exposes deprecated aliases or adapters for
one compatibility release. This avoids the forbidden dependency direction
where a public package imports `src/internal/core`.

Types that are only temporary retrieval or transport details may remain in
`internal/core` during the migration, but no new feature may add to them.
They are removed or relocated as their owning ADR is implemented.

**Alternative rejected:** Make `pkg/domain` type aliases of `core`.
That would leave public code depending on an internal god-package and make
future extraction impossible.

### 2. Keep the public SPI narrow and capability-based

`pkg/spi` is one public import namespace with separate files and narrow
interfaces. The required base interfaces are:

- `Embedder` for one text at a time;
- optional `BatchEmbedder` for provider batching;
- `Extractor` returning domain drafts and normalized usage;
- `Reranker` over transport-neutral candidates;
- `VectorStore` for scored search, batch upsert, delete, and stats.

Provider descriptors and typed factories live in `pkg/spi`, but registry
construction lives in `internal/app`.

No required method is added to a public interface after its initial release.
New provider abilities use optional capability interfaces or a versioned
interface. This is more compatible with community providers than requiring
all providers to implement every future operation.

**Alternative rejected:** A single generic `Provider` returning `any`.
It would lose compile-time type safety and make incompatible provider
operations fail at runtime.

### 3. Implement `VectorStore` before migrating vector callers

The existing `core.VectorIndex` cannot be losslessly adapted into the new
contract because it discards scores and has no namespace/filter semantics.
Go also cannot overload methods, so one concrete type cannot implement both
the legacy `Search(ctx, []float32, int)` method and public
`Search(ctx, SearchRequest)` method. The public implementation is therefore
`vector.InMemoryVectorStore`, a namespace owner over the compatibility-safe
in-memory entry engine. The existing `InMemoryVectorIndex` remains the legacy
single-namespace facade with shared storage/search logic.

Therefore the migration uses this order:

1. Define `pkg/spi.VectorStore` and its conformance tests.
2. Implement `InMemoryVectorStore` with namespace, metadata filters, scores,
   stats, idempotent upsert, and explicit capacity errors.
3. Add a compatibility adapter from the new store to legacy `core.VectorIndex`
   for callers that still need IDs-only behavior.
4. Migrate ingestion, retrieval, health, re-embedding, and compression callers.
5. Remove the legacy adapter after the compatibility release.

A reverse adapter from old `core.VectorIndex` to full `VectorStore` is not
provided: it would fabricate scores, ignore filters/namespaces, and create a
false guarantee. Any backend that cannot implement the new semantics remains
legacy-only until it is upgraded.

`VectorStore` is backend-neutral. Namespace-to-tenant/model mapping remains
in application/repository composition and is governed by ADR-030.

### 4. Use explicit, typed provider registry composition

The current central provider switches are replaced incrementally by typed
registries. `internal/app` creates a registry instance, registers built-ins,
then registers explicitly supplied external providers before resolving the
configured instances.

Provider packages expose descriptor and factory values; they do not mutate a
global registry from `init()`. Provider configuration is passed as opaque
provider-owned data and validated by its descriptor/config spec. The legacy
flat config fields are translated by a compatibility adapter during the
migration window.

The registry returns capability-specific interfaces rather than `any`. A
provider name collision or missing capability is a construction error before
serving begins.

**Alternative rejected:** Global `init()` registration. It hides dependency
composition, makes tests order-sensitive, and conflicts with Phase 0's
application-owned lifecycle.

### 5. Separate transport DTOs with mappers

Existing HTTP and MCP behavior remains unchanged at the wire level. DTOs are
moved to versioned transport packages, beginning with `api/v1`, and mappers
convert between DTOs and `pkg/domain` values at the edge.

Mapping rules:

- domain types do not acquire JSON tags solely for HTTP compatibility;
- API versioning is owned by DTO packages;
- omitted/default fields preserve current response behavior;
- mapper tests use golden JSON and validation cases;
- MCP/CLI adapters may share domain mappers but do not import HTTP DTOs.

This intentionally introduces mapper boilerplate to prevent wire evolution
from recompiling or mutating the domain contract.

### 6. Enforce boundaries through import and compatibility checks

Add CI checks based on `go list`/package imports, supplemented by focused
source checks where the repository already uses them. The checks must fail
when:

- `pkg/domain` imports `pkg/spi`, `internal/*`, `api/*`, SQL, or providers;
- `pkg/spi` imports `internal/*`, transport packages, storage, or providers;
- public packages expose `*sql.DB`, `sql.Tx`, SQLite errors, or concrete
  provider configuration;
- a new implementation imports `src/internal/core` instead of the public
  contract;
- a transport package introduces a new domain/DTO alias instead of a mapper.

Add compile-time conformance tests for each built-in provider and vector
backend. Add an external-like fixture package that imports only `pkg/domain`
and `pkg/spi`, proving that plugin consumers do not need `internal/*`.

### 7. Keep lifecycle and ownership below the public contract

`pkg/spi` describes operations and capabilities but does not own resources.
`internal/app.Application` owns provider instances, vector stores, workers,
and shutdown. `Close` is not required on the base SPI interfaces; implementations
may additionally satisfy `io.Closer`, and Application closes them through an
explicit owned-resource list.

This prevents a registry from becoming a second lifecycle container and keeps
the public contract usable by both embedded and remote implementations.

## Risks / Trade-offs

- **[Risk] Import cycles during the type move** → Move domain values first,
  keep `pkg/domain` dependency-free, and make `core` depend on the new package
  rather than the reverse.
- **[Risk] Temporary duplicate names confuse contributors** → Add deprecation
  comments, migration documentation, compile-time aliases only where safe,
  and CI preventing new imports of the legacy facade.
- **[Risk] Existing JSON behavior changes during DTO extraction** → Preserve
  `api/v1` field names/defaults and add golden contract tests before changing
  handlers.
- **[Risk] Legacy vector callers assume IDs-only results** → Keep the new-to-
  legacy adapter; never pretend the old interface supports scores, filters,
  or namespaces.
- **[Risk] Provider implementations lack optional capabilities** → Use
  capability detection with bounded fallbacks and expose capabilities in
  provider diagnostics; do not widen required interfaces.
- **[Risk] Public SPI freezes an unsuitable abstraction** → Require conformance
  tests for in-memory, sqlite-vec, and a fake remote adapter before removing
  the facade; treat ADR-031 as the compatibility gate for ADR-028/029.
- **[Risk] Provider config migration breaks existing installations** → Keep a
  flat-config compatibility adapter for one release/deprecation window and
  report provider-scoped validation errors before Application startup.
- **[Risk] Public packages accidentally gain implementation dependencies** →
  enforce import checks in CI and require external-like compile fixtures in
  review.

## Migration Plan

1. Add public package skeletons and contract tests with no production wiring
   changes.
2. Move pure domain types into `pkg/domain`; make `core` aliases/facades point
   to the new source of truth.
3. Add `pkg/spi` capability contracts, provider descriptors, typed factories,
   and typed error categories.
4. Migrate the in-memory vector implementation to `VectorStore`; add the
   legacy IDs-only adapter and vector conformance suite.
5. Move transport DTOs to `api/v1` and introduce mapper tests while keeping
   endpoint behavior unchanged.
6. Replace AI/vector central switches with explicit registries in
   `internal/app`; retain the flat configuration adapter.
7. Migrate domain callers by capability: ingestion and embedding first, then
   vector consumers, then provider-dependent services.
8. Add import guardrails, external-like plugin fixtures, and public API
   compatibility checks as required CI gates.
9. Mark `core` interfaces/types deprecated and document the one-release
   migration path.
10. In a later breaking release, remove the facade only after all in-repo
    callers and documented integrations use `pkg/domain`/`pkg/spi`.

Rollback is source-level: revert the package/adaptor changes as one release
unit. This design introduces no database migration and no required wire
contract change. If a provider or vector implementation cannot meet the new
contract, leave it behind the legacy adapter until a dedicated upgrade is
ready rather than weakening the public SPI.

## Open Questions

None. Namespace composition, tenancy claims, identity migration, durable
ingestion, vector backend selection, and retrieval-stage contracts remain
explicitly owned by ADR-030, ADR-035, ADR-032, ADR-028/033, and ADR-037.
