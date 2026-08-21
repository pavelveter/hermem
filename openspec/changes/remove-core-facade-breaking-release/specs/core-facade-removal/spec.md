## Purpose

Define the major-release contract for removing the deprecated internal core facade while preserving runtime behavior and providing explicit public package migration guarantees.

## ADDED Requirements

### Requirement: Major release has no legacy core facade

The breaking release MUST NOT ship the `src/internal/core` compatibility facade, its legacy capability interfaces, or compatibility-only adapters. Supported Go integrations MUST use `pkg/domain`, `pkg/spi`, `api/v1`, or documented command-local types.

#### Scenario: Legacy facade is absent from the release

- **WHEN** a consumer builds the breaking-release source tree
- **THEN** `src/internal/core` is absent and no production package imports it

#### Scenario: Legacy import is rejected as an unsupported migration

- **WHEN** an integration still references a removed `core` alias or capability interface
- **THEN** the integration fails at compile time with the normal Go missing-symbol or missing-package error and the migration documentation identifies the supported replacement

### Requirement: Public replacements remain stable and separated

The release MUST expose domain values through `pkg/domain`, provider capabilities through `pkg/spi`, and HTTP transport contracts through `api/v1`. Public packages MUST NOT expose internal storage, SQLite, lifecycle, or concrete provider implementation types.

#### Scenario: External provider compiles against public contracts

- **WHEN** an external-like provider imports only `pkg/domain` and `pkg/spi`
- **THEN** it compiles and satisfies the required capability interface without importing `src/internal` or `api`

#### Scenario: HTTP DTO boundary remains independent

- **WHEN** an HTTP request or response is encoded through the supported API boundary
- **THEN** it uses `api/v1` DTOs and mappers rather than domain or legacy core DTO aliases

### Requirement: Runtime behavior remains compatible

Removing the facade MUST preserve current HTTP routes, JSON field names and omission rules, MCP tool behavior, CLI output contracts, persistence semantics, and provider behavior unless a separate approved breaking change explicitly changes them.

#### Scenario: HTTP compatibility survives facade removal

- **WHEN** the existing HTTP integration and golden contract suite runs against the breaking release
- **THEN** routes, status codes, error envelopes, response fields, and omitted defaults match the compatibility-release baseline

#### Scenario: Non-HTTP adapters remain compatible

- **WHEN** the MCP, CLI, persistence, and provider integration suites run against the breaking release
- **THEN** they pass without depending on the removed facade and preserve their compatibility-release behavior

### Requirement: Release gates prevent premature deletion

The release MUST NOT be tagged until public-contract, production-migration, dependent-ADR, behavior, and operational validation gates are recorded as passing.

#### Scenario: Dependent ADR gate is incomplete

- **WHEN** ADR-030, ADR-032, ADR-035, or ADR-037 has an unresolved contract required by a core caller
- **THEN** the facade-removal release remains blocked and the prior compatibility release remains the supported version

#### Scenario: Clean release candidate passes all gates

- **WHEN** a clean checkout has no facade and passes unit, vet, race, API, integration, boundary, and required fuzz-smoke checks
- **THEN** the release candidate is eligible for major-version tagging

### Requirement: Versioning and rollback are explicit

The breaking release MUST align server and SDK major versions, publish migration guidance, retain the previous compatibility release as the rollback target, and keep published tags immutable.

#### Scenario: Major versions are aligned

- **WHEN** the release workflow validates the breaking tag
- **THEN** server and supported SDK MAJOR versions match and release notes identify the removed facade as a breaking change

#### Scenario: Post-tag release issue occurs

- **WHEN** a published breaking release has a release-blocking defect
- **THEN** the previous compatibility release remains available, its tag is not changed, and the correction is published as a new immutable release rather than silently restoring the facade on the same major line
