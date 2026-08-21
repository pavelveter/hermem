# public-package-api Specification

## Purpose

Provides a stable public Go contract for domain values and provider capabilities so external consumers and plugins can integrate without importing Hermem internals, transport DTOs, or storage implementations.

## Requirements

### Requirement: Public domain values are transport- and storage-independent

The public domain package SHALL expose domain values that can be consumed by an external Go package without importing internal packages, HTTP/MCP transport packages, SQL drivers, or concrete provider implementations.

#### Scenario: External consumer uses a domain value

- **WHEN** an external Go package imports the public domain package and constructs or reads an entity, edge, task, or extraction draft
- **THEN** the package SHALL compile without importing any `internal` package, transport package, database package, or provider implementation

#### Scenario: Transport representation evolves

- **WHEN** a versioned HTTP or MCP representation changes its field names or optional fields
- **THEN** the public domain value SHALL remain independently versionable and the transport change SHALL be handled at a mapping boundary

### Requirement: Provider capabilities have stable narrow contracts

The public SPI SHALL expose independent capability contracts for embedding, extraction, reranking, and vector storage. A provider SHALL be able to implement a required base capability without implementing unrelated capabilities.

#### Scenario: Provider implements one capability

- **WHEN** a provider implements only the embedding capability
- **THEN** it SHALL be registrable and usable as an embedder without implementing extraction, reranking, vector storage, or transport interfaces

#### Scenario: Provider offers optional batching

- **WHEN** an embedder supports batch processing
- **THEN** it SHALL be able to advertise and provide that optional capability without changing the required single-input embedder contract

#### Scenario: Provider capability is unavailable

- **WHEN** a caller requests an optional capability that a provider does not advertise
- **THEN** the runtime SHALL return an explicit unsupported-capability result or use the documented bounded fallback, and SHALL NOT silently produce incompatible output

### Requirement: Vector storage preserves scored, filtered, namespaced search semantics

The public vector contract SHALL support namespace-scoped search with a limit, a small equality/range filter, and returned similarity scores. Results SHALL use higher-is-better cosine similarity semantics, and vector implementations SHALL report dimension or capacity violations explicitly.

#### Scenario: Search returns filtered scored hits

- **WHEN** a caller submits a valid vector, namespace, filter, and limit
- **THEN** every returned hit SHALL belong to the requested namespace, satisfy the filter, include its identifier and similarity score, and the number of hits SHALL NOT exceed the limit

#### Scenario: Vector dimensions do not match

- **WHEN** a caller submits a vector whose dimensions do not match the target namespace
- **THEN** the vector operation SHALL fail with an explicit dimension error and SHALL NOT truncate, pad, or silently switch namespaces

#### Scenario: Vector capacity cannot satisfy an upsert

- **WHEN** a vector implementation cannot accept an upsert without eviction or degraded recall
- **THEN** it SHALL return an explicit observable capacity/degradation error or apply an explicitly configured policy, and SHALL NOT silently remove searchable records

#### Scenario: Vector upsert is retried

- **WHEN** the same vector record is upserted repeatedly for the same namespace and identifier
- **THEN** the operation SHALL be idempotent and SHALL NOT create duplicate logical records

### Requirement: Provider selection and configuration fail closed

The runtime SHALL discover providers through explicit capability-typed registration and SHALL validate provider-owned configuration and descriptors before serving requests.

#### Scenario: Built-in and external providers are selected uniformly

- **WHEN** a built-in provider and an externally supplied provider expose the same capability
- **THEN** both SHALL be selectable through the same provider resolution contract without a privileged central switch

#### Scenario: Provider configuration is invalid

- **WHEN** configured provider data fails the provider's declared configuration validation
- **THEN** application startup SHALL fail with a provider-scoped validation error before serving requests

#### Scenario: Provider name is missing or duplicated

- **WHEN** a configured provider name is unknown or two registrations claim the same capability/name pair
- **THEN** provider resolution SHALL fail explicitly and SHALL NOT select an arbitrary implementation

### Requirement: Legacy compatibility remains bounded and observable

During the compatibility window, existing legacy consumers SHALL continue to work through adapters, while new capabilities SHALL target the public domain and SPI contracts rather than extending the legacy core surface.

#### Scenario: Legacy consumer uses an adapted provider

- **WHEN** an existing consumer uses the legacy vector or embedding interface during the compatibility window
- **THEN** the adapter SHALL preserve the existing caller-visible behavior and SHALL identify unsupported new semantics instead of fabricating them

#### Scenario: New code is introduced during migration

- **WHEN** a new provider, domain service, or transport integration is added after the public contracts exist
- **THEN** it SHALL depend on the public domain/SPI contract and SHALL NOT add new dependencies on the legacy core facade

#### Scenario: Compatibility window ends

- **WHEN** the documented breaking release removes the legacy facade
- **THEN** integrations SHALL be required to use the public domain package, public SPI, or versioned transport contract

### Requirement: Public package dependency direction is enforced

The public domain and SPI packages SHALL remain independent of internal implementations, transport DTOs, storage engines, and concrete providers.

#### Scenario: Boundary regression is introduced

- **WHEN** a change adds an import from a public package to an internal implementation, transport package, SQL driver, or concrete provider
- **THEN** the repository validation SHALL reject the change before merge

#### Scenario: External-like plugin is compiled

- **WHEN** a plugin fixture imports only the public domain and SPI packages
- **THEN** it SHALL compile and implement its declared capability without importing Hermem internals
