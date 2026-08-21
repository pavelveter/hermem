## Why

Hermem's `src/internal/core` currently combines domain models, provider interfaces, transport DTOs, policy, and utilities. This prevents external Go consumers and inbound plugins from using Hermem in-process, forces provider and vector changes through a high-conflict package, and makes API evolution inseparable from domain evolution. ADR-031 now freezes the public contract, so the package split can proceed without inventing interfaces during implementation.

## What Changes

- Add public `pkg/domain` types for pure domain models, drafts, identifiers, and domain errors.
- Add public `pkg/spi` capability interfaces and typed provider contracts for embedding, optional batch embedding, extraction, reranking, and the scored/filtered/namespaced `VectorStore` contract defined by ADR-028.
- Add an explicit application-owned provider registry with provider descriptors, typed factories, provider-owned configuration specs, and no mandatory `init()` registration.
- Move HTTP transport models into versioned `api/v1` mapping boundaries rather than sharing domain structs as wire DTOs.
- Add compatibility adapters/facade aliases from the current `core` types and interfaces during one migration release; mark the legacy surface deprecated and prevent new features from extending it.
- Add import-direction guardrails preventing public packages from importing `internal`, transport, storage, SQLite, or concrete provider implementations.
- **BREAKING (after the compatibility window):** remove the legacy `core` public facade and require integrations to use `pkg/domain`, `pkg/spi`, and versioned API DTOs.
- Preserve current HTTP, MCP, CLI, persistence, and provider behavior while migration proceeds; this change establishes boundaries and adapters rather than changing product behavior.

## Capabilities

### New Capabilities

- `public-package-api`: Public domain models, stable provider SPI, vector contract, and compatibility rules for in-process Go consumers and plugins.

### Modified Capabilities

None. The repository currently has no OpenSpec capability specs; this change introduces a public implementation/API boundary without changing existing HTTP, MCP, CLI, or persistence requirements.

## Impact

- **Packages:** new `pkg/domain` and `pkg/spi`; `src/internal/core` facade; domain services, vector implementations, AI providers, ingestion, retrieval, and application wiring.
- **Transport:** `api/v1` DTOs and mapper functions become independent from domain models.
- **Configuration:** provider-specific sections and descriptor-driven validation replace global provider-field multiplication behind a compatibility adapter.
- **Plugins:** inbound providers can implement public SPI interfaces without importing `internal/*`; out-of-process plugins will use adapters to the same SPI in a later phase.
- **CI/tooling:** import boundary checks, public API compatibility checks, adapter tests, and conformance tests for vector/provider implementations.
- **Dependencies:** no new runtime dependency is required by the contract itself; implementation may add only provider-specific dependencies behind internal packages.
