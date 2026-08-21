# Breaking-release plan: remove the legacy `core` facade

## Status

Planned for the first major release after the public-package compatibility
release. This document is a release plan, not permission to remove the facade
in the current release.

## Goal

Remove `src/internal/core` as a compatibility facade and leave these as the
supported contracts:

```text
pkg/domain  — domain values and domain errors
pkg/spi     — provider capabilities and typed provider contracts
api/v1      — versioned HTTP DTOs and mappers
internal/* — implementation-only services, repositories, and policies
```

The breaking release must not change the existing HTTP/MCP/CLI wire behavior
as part of the facade removal. It changes Go import paths and internal
constructor contracts only; unrelated behavior changes require separate
changes and release notes.

## Scope inventory

### Remove after migration

1. **Domain aliases and projections**
   - `Entity`, `Edge`, `Task`, `GraphNode` and the `Fact`, `Evidence`,
     `Episode`, `Belief`, and `Goal` projection surface.
   - `Compose`, task/entity projection methods, and extraction compatibility
     helpers after their callers use `pkg/domain`.
2. **Legacy capability interfaces**
   - `VectorIndex` → `spi.VectorStore` or an internal repository-owned
     interface where the caller does not need vector SPI semantics.
   - `Embedder` → `spi.Embedder` plus an internal health/legacy adapter only
     where the behavior is genuinely internal.
   - `LLMExtractor` → `spi.Extractor`.
   - `Reranker` → `spi.Reranker`.
   - `Retriever` is not mechanically promoted: complete ADR-037 first and
     migrate callers to its stage contract.
3. **Transport DTOs**
   - HTTP request/response types move to `api/v1` or a command-local CLI/MCP
     type. No HTTP DTO is recreated in `pkg/domain`.
4. **Compatibility-only adapters**
   - `spiadapter.NewLegacyEmbedder`, `NewLegacyVectorIndex`, and extraction
     compatibility adapters are deleted only after their usage gates pass.
5. **Core-owned policy types**
   - Any remaining `RetentionPolicy`, `RankingWeight`, retrieval options,
     migration values, and error values are moved to their owning internal
     package or a reviewed public package before `core` is deleted.

### Explicitly out of scope

- sqlite-vec, HNSW, remote vector backend implementation, embedding BLOB
  removal, and vector rollback procedures; these remain governed by ADR-028
  and the dedicated storage/migration changes.
- Tenancy model changes; ADR-030 owns tenant claims and namespace mapping.
- Durable ingestion orchestration; ADR-032 owns jobs, retries, and batching.
- Retrieval algorithm redesign; ADR-037 must land before removing the legacy
  `Retriever` shape.
- HTTP route or JSON redesign. Existing `api/v1` wire behavior is a release
  compatibility constraint.

## Release gates

The major-release branch may not delete the facade until every gate below is
true and recorded in the release PR.

### Gate A — public contract freeze

- `pkg/domain`, `pkg/spi`, and `api/v1` have documented API ownership and
  compatibility policy.
- A public API snapshot or equivalent compile-time contract test exists for
  every required SPI method.
- No required method has been added to an existing public interface during
  the migration.
- External-like provider fixtures compile using only `pkg/domain` and
  `pkg/spi`.
- `go doc`/API review confirms no `sql.DB`, SQLite error, internal package,
  or concrete provider leaks into public types.

### Gate B — production dependency migration

- `go list -deps ./src/...` contains no production import of
  `src/internal/core` except the temporary compatibility package itself;
  after the deletion it contains none.
- No production constructor accepts a core capability interface or core DTO.
- HTTP handlers use `api/v1` DTOs and mappers.
- MCP and CLI adapters use public domain values or command-local DTOs, never
  HTTP DTOs for convenience.
- Persistence and domain services use `pkg/domain` values or their own
  internal policy types.
- All core-dependent tests are migrated, except a temporary compatibility
  test suite that is removed in the same breaking PR.

### Gate C — dependent ADRs

- ADR-035 identity generation and LLM-selected-ID migration are complete.
- ADR-037 retrieval-stage contracts are complete and no public/internal
  caller needs the old `Retriever` method shape.
- ADR-030 tenancy decisions are reflected in namespace-aware vector callers.
- ADR-032 changes do not require a core extraction or ingestion result.
- Any remaining core-owned policy type has a new owner and migration test.

### Gate D — behavior and operations

- HTTP golden/integration tests confirm route status codes, JSON fields,
  omitted defaults, and error envelopes are unchanged.
- MCP, CLI, persistence, provider, and external-provider tests pass.
- Full unit tests, vet, race tests, API contract tests, fuzz smoke tests, and
  boundary checks pass on the release candidate.
- A clean build succeeds with the facade directory physically absent.
- The release notes include a migration table and a prominent major-version
  warning.

## Migration sequence

### Phase 0 — baseline and inventory

1. Tag the last compatibility release before removal.
2. Generate the exported-symbol inventory for `src/internal/core` and group
   each symbol by new owner.
3. Add a CI rule that fails new production imports of `src/internal/core`.
4. Record baseline HTTP/OpenAPI/MCP/CLI snapshots and benchmark smoke results.

Exit criterion: the inventory is complete and new legacy dependencies are
blocked.

### Phase 1 — finish public replacements

1. Complete missing `pkg/domain` value objects and domain errors.
2. Complete `pkg/spi` provider descriptors, errors, and capability tests.
3. Complete `api/v1` DTO coverage for every HTTP route that currently uses a
   core DTO.
4. Publish mapper behavior and JSON golden tests.
5. Resolve any policy/config types currently borrowed from core into their
   owning packages.

Exit criterion: every replacement has a compile-time or runtime conformance
test and no replacement requires importing core.

### Phase 2 — migrate internal production callers

Migrate in dependency order:

1. adapters and provider implementations;
2. repositories and vector consumers;
3. ingestion, memory, edge, re-embedding, health, and retention services;
4. retrieval after ADR-037;
5. application composition and lifecycle;
6. HTTP shells;
7. MCP and CLI wiring;
8. benchmarks, fixtures, integration tests, and examples.

Keep changes behavior-preserving and split by owner. Do not combine facade
removal with a vector backend or database migration.

Exit criterion: Gate B passes and the only remaining core references are the
facade package plus explicitly tracked compatibility tests.

### Phase 3 — deprecation enforcement release

In the final minor release before the major release:

- retain the facade and adapters;
- emit compile-time deprecation comments already present in the facade;
- publish the migration guide and a one-line major-release warning in the
  README, release notes, Go package docs, and SDK documentation;
- keep CI failing on new core imports;
- run a consumer migration dry run from a clean checkout using only public
  packages.

Exit criterion: one complete compatibility release has shipped with the
migration path documented and no new core usage.

### Phase 4 — breaking removal PR

1. Delete the facade aliases, legacy capability interfaces, DTO aliases, and
   compatibility adapters.
2. Remove the temporary compatibility tests and replace them with public
   contract tests where needed.
3. Update all import paths and constructor signatures in one coordinated PR.
4. Remove compatibility-only CI exceptions and invert the guardrail to fail
   if `src/internal/core` exists or is imported.
5. Build and test from a clean checkout with no generated artifacts from the
   prior release.

Exit criterion: the facade directory is absent, the full validation matrix is
green, and the release PR contains the inventory showing zero remaining
production references.

### Phase 5 — major release and post-release monitoring

1. Bump the server and SDK major versions together according to `RELEASE.md`.
2. Update `CHANGELOG.md`, `RELEASE.md`, README migration links, and SDK
   compatibility notes.
3. Publish a migration example for each former core capability:
   domain model, embedder, extractor, reranker, vector store, and HTTP DTO.
4. Tag the release only after CI, release artifact, checksum, and provenance
   jobs pass.
5. Monitor issue reports for one release cycle; do not silently restore the
   facade on the same major line. If a compatibility defect is found, ship a
   follow-up major/minor correction using the public package contract.

## Migration table for release notes

| Legacy import/symbol | Replacement | Notes |
|---|---|---|
| `core.Entity`, `core.Edge`, `core.Task` | `pkg/domain` | Domain values, not HTTP DTOs |
| `core.VectorIndex` | `pkg/spi.VectorStore` | Scores, filters, and namespaces are explicit |
| `core.Embedder` | `pkg/spi.Embedder` | `Ping` is not part of the public SPI; use health adapter |
| `core.LLMExtractor` | `pkg/spi.Extractor` | Returns identity-free drafts |
| `core.Reranker` | `pkg/spi.Reranker` | Uses `spi.Candidate` |
| `core.*Request/*Response` | `api/v1` or command-local DTO | Preserve route wire JSON through mappers |
| `core.Retriever` | ADR-037 stage contract | No direct mechanical alias |
| core policy/config values | Owning internal package or reviewed public value | Avoid recreating a god-package |

## Rollback and support policy

- The previous compatibility release remains the supported rollback target.
- Do not force-push or retag the breaking release.
- If the release candidate fails, revert the removal PR before tagging; do not
  publish a partially removed facade.
- If a tagged release fails after publication, mark it pre-release per
  `RELEASE.md` and publish a corrected major release; keep the old tag
  immutable.
- Maintain the compatibility release branch for the documented support
  window so users can migrate without a forced same-day upgrade.
- A rollback restores the previous binary/version; it does not reintroduce
  facade code into the new major branch.

## Final release checklist

- [ ] All Gates A–D signed off in the release PR.
- [ ] Zero production `src/internal/core` imports.
- [ ] Zero public API leaks of internal/core types.
- [ ] `src/internal/core` removed from a clean checkout build.
- [ ] `api/v1` wire snapshots unchanged.
- [ ] ADR-030/032/035/037 dependency gates recorded.
- [ ] Full test, vet, race, fuzz smoke, API, and boundary checks passed.
- [ ] Migration guide and release notes updated.
- [ ] Server/SDK major versions aligned.
- [ ] Release artifacts, checksums, and provenance verified.
- [ ] Compatibility release support branch identified.
