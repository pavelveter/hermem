# ADR-031: Public Domain and SPI Contract Freeze

## Status

Proposed.

This ADR is the **canonical contract-freeze document** for the public
package boundary. ADR-028 and ADR-029 remain focused implementation ADRs:

- ADR-028 owns vector-search semantics and backend behavior.
- ADR-029 owns provider discovery, configuration, and plugin loading.
- This ADR owns the public type shapes, package ownership, dependency
  direction, and compatibility rules that both ADRs must follow.

## Context

`src/internal/core/types.go` currently combines five unrelated concerns:

1. domain models (`Entity`, `Edge`, `Task`, `GraphNode`);
2. service-provider interfaces (`VectorIndex`, `Embedder`, `Retriever`,
   `Reranker`, and related interfaces);
3. HTTP request and response DTOs;
4. business policy and default configuration;
5. utilities such as process-local ID generation.

The package is imported by nearly every subsystem. It also prevents external
Go consumers and plugins from using Hermem in-process because all useful types
are behind `internal/`.

The existing vector and provider ADRs expose the same boundary problem in
three ways:

- ADR-028 needs a scored, filtered, namespaced vector contract, not the
  current IDs-only `core.VectorIndex`.
- ADR-029 needs provider-owned capabilities and configuration without adding
  fields to the global config or editing central switches.
- ADR-031 needs a public package boundary without creating a new god-package
  or freezing transport DTOs into the domain API.

The contract must also remain compatible with later decisions about tenancy
(ADR-030), IDs (ADR-035), asynchronous ingestion (ADR-032), and retrieval
stages (ADR-037).

## Decision

Establish one public API namespace with two distinct responsibilities:

```text
pkg/domain  <- pure domain model
     ^
pkg/spi     <- narrow provider capabilities and typed registries
     ^
internal implementations / in-process providers / plugin adapters
     ^
internal/app <- explicit composition and lifecycle ownership

api/v1 -> mapper -> pkg/domain
```

`pkg/domain` and `pkg/spi` are public Go packages. `api/v1` is the public
transport contract. Implementations, storage, lifecycle, and wiring remain
under `internal/`.

### 1. `pkg/domain` contains only domain concepts

`pkg/domain` owns:

- `Entity`, `Edge`, `Task`, and graph-facing domain models;
- `EntityDraft` and extraction-facing domain values;
- typed entity/task identifiers;
- domain errors and value objects.

`pkg/domain` must not contain:

- HTTP or MCP DTOs;
- JSON tags that exist only for a transport contract;
- `context.Context` in data models;
- `database/sql`, SQLite, provider configuration, or registry types;
- vector backend implementation details;
- process-local ID generation.

An extraction draft does not carry an LLM-selected ID:

```go
type EntityDraft struct {
    Category  string
    Content   string
    Relations []RelationDraft
}
```

The application/domain service assigns identity. This keeps the public
contract compatible with ADR-035 and prevents model output from becoming a
primary-key policy.

Tenant identity is deliberately not added as an ad-hoc field to every domain
model by this ADR. ADR-030 owns the final tenant context and persistence
semantics. Vector namespaces remain opaque at this layer as well.

### 2. `pkg/spi` contains narrow capability interfaces

`pkg/spi` is one public namespace, not one universal interface. Each
capability has a small, independently implementable contract.

#### 2.1 Embedding

The base interface remains minimal:

```go
type Embedder interface {
    Embed(ctx context.Context, text string) ([]float32, error)
}
```

Optional capabilities extend it without breaking existing providers:

```go
type BatchEmbedder interface {
    EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
}

type DescribedProvider interface {
    Descriptor() ProviderDescriptor
}
```

The ingestion runtime uses `BatchEmbedder` when available and otherwise uses
a bounded single-item fallback. Provider identity and dimensions come from
the registered `ProviderDescriptor`; dimensions are also validated against
the target vector namespace.

No new required method is added to `Embedder` after v1. New embedding
features use optional capability interfaces or a versioned interface.

#### 2.2 Vector storage

ADR-028's canonical public contract is `VectorStore`. The old
`core.VectorIndex` remains a compatibility surface only and is adapted to
this contract during migration.

```go
type VectorStore interface {
    Search(ctx context.Context, req SearchRequest) ([]Hit, error)
    Upsert(ctx context.Context, records []VectorRecord) error
    Delete(ctx context.Context, req DeleteRequest) error
    Stats(ctx context.Context, namespace string) (VectorStats, error)
}

type SearchRequest struct {
    Namespace string
    Vector    []float32
    Limit     int
    Filter    Filter
}

type Hit struct {
    ID    string
    Score float32
}

type VectorRecord struct {
    ID       string
    Vector   []float32
    Metadata map[string]string
}
```

The contract has these semantics:

- `Namespace` is an opaque, non-empty application-owned partition key. The
  vector backend does not interpret it as `tenant + model` or any other
  composite value.
- `Score` is cosine similarity with higher-is-better semantics. Adapters
  for other native metrics must normalize to this contract or expose a
  future versioned metric contract.
- Scores are returned by the backend and are not recomputed from entity
  BLOBs by callers.
- A filter is evaluated logically before `Limit`:

  ```go
  type Filter struct {
      Equals map[string]string
      Ranges map[string]NumericRange
  }
  ```

  The filter language is intentionally small. Rich domain predicates belong
  in retrieval, not in the vector SPI.
- Dimension mismatch is an explicit typed error. Truncation, padding, and
  silent namespace fallback are forbidden.
- `Upsert` is idempotent by `(namespace, id)`. Partial batch failures must
  identify failed records through a typed error so the ingestion layer can
  retry safely.
- Capacity exhaustion, eviction, and degraded recall must be explicit and
  observable. Silent eviction is not a valid implementation policy.
- Search returns `ID` and `Score`; domain hydration is performed by the
  application/repository layer. Arbitrary `map[string]any` payloads are not
  part of the stable contract.
- `Close` is not required on the main interface. Lifecycle ownership belongs
  to `app.Application`; implementations may additionally satisfy
  `io.Closer`.

`Stats` reports at least vector count, dimensions, and the active model/profile
identity for the namespace. `VectorStore` does not own tenant resolution,
entity persistence, or graph traversal.

#### 2.3 Extraction

Provider extraction is expressed with provider-neutral request and response
values. It is not coupled to HTTP DTOs or to the current `ExtractEntities`
method:

```go
type Extractor interface {
    Extract(ctx context.Context, req ExtractRequest) (ExtractResponse, error)
}

type ExtractRequest struct {
    Dialog        string
    PromptVersion string
    Schema        SchemaDescriptor
}

type ExtractResponse struct {
    Entities []domain.EntityDraft
    Usage    Usage
}

type Usage struct {
    InputTokens  int
    OutputTokens int
}
```

Provider-specific usage fields remain provider telemetry and do not become
part of `pkg/domain`.

#### 2.4 Reranking

Reranking uses a domain-neutral candidate type rather than the existing
`RetrievedFact`, which currently mixes domain, retrieval, and transport
responsibilities:

```go
type Candidate struct {
    ID       string
    Text     string
    Score    float32
    Metadata map[string]string
}

type Reranker interface {
    Rerank(ctx context.Context, query string, candidates []Candidate) ([]Candidate, error)
}
```

The `Retriever` interface and retrieval-stage SPI are intentionally out of
this ADR. ADR-037 defines them after the primitive provider contracts are
stable; the current method that receives `VectorIndex` and `Embedder` as
per-call construction dependencies must not be promoted to the public API.

### 3. Provider descriptors and registry

ADR-029's registry is an explicit application-owned composition object, not a
global mutable singleton:

```text
app.New()
  ├─ register built-in Ollama provider
  ├─ register built-in OpenAI provider
  ├─ register built-in in-memory vector store
  └─ resolve configured typed providers
```

Every provider exposes a descriptor containing at least:

```go
type ProviderDescriptor struct {
    Kind         ProviderKind
    Name         string
    Version      string
    Dimensions   int
    Capabilities CapabilitySet
    ConfigSpec   []byte // provider-owned schema representation
}
```

Registry rules:

1. Built-ins and external providers use the same registration path.
2. Registration is explicit; mandatory `init()` side effects are forbidden.
3. The registry is scoped to an `Application`, which makes tests isolated
   and lifecycle ownership deterministic.
4. Registries are typed by capability. Do not create a universal factory
   returning `any`:
   `EmbedderFactory`, `ExtractorFactory`, `RerankerFactory`, and
   `VectorStoreFactory` each return their own interface.
5. Provider-specific options are held in provider-owned configuration and
   validated by `ConfigSpec`; they are not multiplied into global
   `config.Config` fields.
6. Duplicate names, unsupported capabilities, invalid configuration, and
   missing providers are explicit errors.
7. An out-of-process plugin is an adapter implementing the same SPI. The
   gRPC/protobuf wire contract belongs to the plugin subsystem and must not
   leak into `pkg/spi`.
8. Plugin packages may import `pkg/spi` and required `pkg/domain` values,
   but never `internal/*`, `api/*`, `database/sql`, or a concrete provider.

### 4. Package and dependency rules

The intended dependency graph is:

```text
pkg/domain       -> standard library only
pkg/spi          -> standard library + pkg/domain
api/v1           -> pkg/domain + transport libraries
internal/*       -> pkg/domain and/or pkg/spi
plugins          -> pkg/domain and/or pkg/spi
internal/app     -> implementations + registry + lifecycle
```

Rules enforced by review and CI:

- `pkg/domain` never imports `pkg/spi`.
- `pkg/spi` never imports `internal/*` or `api/*`.
- Public packages never expose `*sql.DB`, `sql.Tx`, SQLite errors, or
  provider-specific configuration.
- Transport DTOs map to/from domain values at the transport edge; domain and
  SPI types do not acquire JSON tags to satisfy a transport.
- `core` becomes a deprecated facade/adapters layer. New functionality must
  not be added to it.
- `internal/app.Application` remains the owner of provider instances,
  vector stores, workers, and shutdown. A registry does not own lifecycle.

### 5. Compatibility and versioning

The first public release freezes the following rules:

1. Existing public interfaces do not gain required methods.
2. New capabilities are optional interfaces or new versioned interfaces.
3. `core.VectorIndex` and current `core.Embedder` receive adapters during
   migration; they are not extended in place.
4. Existing HTTP contracts remain under `api/v1`; DTO evolution does not
   change `pkg/domain`.
5. Public errors support `errors.Is`/`errors.As`; callers must not match
   provider error strings.
6. Namespace, filter, score, and dimension semantics are versioned as part
   of `VectorStore`; backend-specific behavior cannot silently redefine them.
7. A breaking public SPI change requires a new package/version, not a
   central interface edit.

## Relationship to adjacent ADRs

| ADR | Canonical responsibility |
|---|---|
| ADR-028 | `VectorStore` behavior: scored search, filters, namespaces, dimensions, capacity, backend adapters |
| ADR-029 | Provider descriptors, typed registries, config specs, and in/out-of-process loading |
| ADR-030 | Tenant claims, tenant-scoped repositories, and mapping tenant identity to vector namespaces |
| ADR-031 | Public package boundaries, type ownership, SPI shapes, dependency direction, and compatibility |
| ADR-032 | Durable ingestion jobs, batching orchestration, retries, idempotency, and usage accounting |
| ADR-035 | Server/content identity, aliasing, supersession, and removal of LLM-minted IDs |
| ADR-037 | Retrieval stages, channels, fusion, ranking profiles, caching, and cursors |

When these ADRs overlap, ADR-031 is authoritative for public type ownership
and compatibility; the specialized ADR is authoritative for runtime behavior
inside that contract.

## Alternatives considered

### Keep `core` and document discipline

Rejected. The package already combines transport, domain, policy, and SPI;
documentation cannot enforce dependency direction or prevent all contributors
from editing the same boundary.

### One universal `Provider` interface

Rejected. A vector store, embedder, extractor, and renderer have different
lifecycles, configuration, errors, and capability sets. A universal `any`
factory would move type errors from compile time to runtime.

### Put all vector metadata in `map[string]any`

Rejected. It makes serialization, filtering, backend portability, and plugin
compatibility provider-specific. The stable contract uses typed filters and
string metadata; richer predicates remain in retrieval.

### Mandatory batch methods on every provider

Rejected for v1 compatibility. `BatchEmbedder` is an optional capability;
the runtime supplies a bounded fallback. Providers can adopt batching without
forcing a flag-day migration for every community implementation.

### Global `init()` registration

Rejected. It hides composition, complicates tests, and conflicts with the
explicit lifecycle ownership established by Phase 0. Explicit registration
also makes a future signed or out-of-process plugin loader easier to add.

### Freeze `Retriever` in this ADR

Rejected. Retrieval is still being redesigned in ADR-037. Freezing the
current interface would preserve construction-dependency leakage and bind
public SPI to SQL-backed graph traversal.

## Consequences

### Positive

- Domain models, public provider contracts, and wire DTOs can evolve
  independently.
- Vector backends can return scores, apply filters, and support model/tenant
  namespaces without exposing database schema.
- Provider additions become registration/configuration work rather than edits
  to central switches and global config fields.
- External Go consumers and plugins can implement stable interfaces without
  importing `internal/` packages.
- ADR-030 can define tenant semantics later without changing the vector
  interface; the application maps tenant scope to an opaque namespace.
- ADR-032 can add batching and usage accounting around stable provider
  contracts, and ADR-037 can build retrieval stages above them.

### Costs and risks

- One-time import and mapper churn across the repository.
- Public `pkg/domain` and `pkg/spi` become long-lived compatibility promises.
- DTO-to-domain mapping adds boilerplate at transport boundaries.
- Capability interfaces require runtime capability checks and fallback paths.
- The temporary `core` facade creates two names for some types during the
  migration window.
- Score normalization and filter support must be tested consistently across
  every vector backend.

## Migration sequence

1. Add `pkg/domain` types and `pkg/spi` contracts without changing wire
   behavior.
2. Add adapters from the current `core` interfaces and models to the new
   contracts.
3. Move transport DTOs into versioned `api/v1` mapping code.
4. Migrate the in-memory vector implementation to `VectorStore`; preserve
   the legacy adapter for existing callers.
5. Replace central provider switches with explicit typed registries while
   retaining the flat-config compatibility adapter.
6. Migrate ingestion to `EntityDraft`, application-assigned IDs, and the
   optional `BatchEmbedder` capability.
7. Move retrieval to ADR-037's stage contracts; remove the legacy public
   `Retriever` construction-dependency shape.
8. Deprecate and eventually delete the `core` facade after one compatibility
   release.

No database migration is implied by this ADR. Identity, tenancy, vector BLOB
removal, and durable ingestion are separate changes governed by ADR-035,
ADR-030, ADR-028/033, and ADR-032 respectively.
