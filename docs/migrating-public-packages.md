# Migrating to the Public Domain and SPI Packages

Hermem is introducing public Go contracts in `pkg/domain` and `pkg/spi`.
During the compatibility release, the existing `src/internal/core` names
remain available as deprecated aliases or adapters.

## New imports

Use:

```go
import (
    "github.com/pavelveter/hermem/pkg/domain"
    "github.com/pavelveter/hermem/pkg/spi"
    apiv1 "github.com/pavelveter/hermem/api/v1"
)
```

Use `domain.Entity`, `domain.Edge`, `domain.Fact`, `domain.Task`, and the
other domain projections for new domain code. Implement provider capabilities
against the narrow `spi` interfaces rather than adding methods to the legacy
`core` interfaces.

## Provider contracts

- `spi.Embedder` is the required single-input embedding capability.
- `spi.BatchEmbedder` is optional and can be detected independently.
- `spi.Extractor` returns identity-free `domain.EntityDraft` values.
- `spi.Reranker` works with `spi.Candidate` values.
- `spi.VectorStore` provides scored, filtered, namespace-scoped search and
  idempotent batch upsert/delete operations.

Provider configuration and registration are application-owned. Providers must
not register global mutable state from `init()`.

HTTP integrations should use the versioned `api/v1` DTOs at the transport edge
and map them to domain values before calling internal services. CLI and MCP
adapters may use the public domain values directly; they must not import HTTP
DTOs to share request shapes.

## Legacy rules

Existing `core` imports continue to work during the compatibility window, but:

1. New features must not add new dependencies on `src/internal/core`.
2. The legacy `core.VectorIndex` remains IDs-only; it does not promise scores,
   filters, or namespaces.
3. The legacy extractor result may contain model-selected IDs. The public
   `domain.EntityDraft` deliberately does not expose those IDs; relation
   target references remain opaque until ADR-035 defines resolution.
4. HTTP DTOs remain transport-specific and should not be added to domain or
   SPI packages.

The `core` facade will be removed only in a later breaking release after the
in-repository callers and documented integrations have migrated. The release
gates and migration sequence are documented in
[`release-core-facade-removal.md`](release-core-facade-removal.md).

## Related decisions

- ADR-028: vector behavior and backend obligations
- ADR-029: provider registry, configuration, and plugins
- ADR-030: tenancy and namespace composition
- ADR-031: public package ownership and compatibility rules
- ADR-035: identity generation and legacy ID migration
