## Context

The compatibility release already provides `pkg/domain`, `pkg/spi`, `api/v1`, typed provider registries, vector compatibility adapters, import guardrails, and a migration guide. The remaining facade is used by production services, adapters, CLI/MCP wiring, repositories, and a large test/fixture surface. See `docs/release-core-facade-removal.md` for the release motivation and inventory.

The deletion is cross-cutting and must be sequenced so the repository can compile at every migration checkpoint. Runtime behavior is a compatibility constraint; the change is allowed to alter Go source imports and internal constructor types, not route or persistence semantics.

## Goals / Non-Goals

**Goals:**

- Establish zero production dependencies on `src/internal/core` before deletion.
- Give every remaining core symbol an explicit owner: `pkg/domain`, `pkg/spi`, `api/v1`, or an owning internal package.
- Remove compatibility adapters only after their usage is zero and public conformance tests replace them.
- Make a clean checkout without the facade build and pass the release validation matrix.
- Align server/SDK major versions and preserve the previous compatibility release as rollback target.

**Non-Goals:**

- Redesign vector backends, storage schema, tenancy, durable ingestion, or retrieval algorithms.
- Add a new universal public package to replace core.
- Change HTTP/MCP/CLI wire behavior as part of the deletion.

## Decisions

### 1. Inventory symbols before changing callers

Create a checked-in symbol ownership table from the exported core surface and
classify every symbol as public domain, public SPI, versioned transport DTO,
internal policy, test-only fixture, or delete-with-adapter. This is preferred
over package-by-package deletion because `core` currently mixes concerns in
shared files.

### 2. Migrate leaves toward composition roots

Migrate in dependency order: provider adapters and repositories, domain
services, ingestion/retrieval, application composition, HTTP shells, MCP/CLI,
then tests and examples. This minimizes temporary adapter layering and keeps
application-owned lifecycle wiring as the final integration point.

### 3. Use explicit internal owners for non-public policy

`RetentionPolicy`, ranking/retrieval options, migration values, and other
non-public policy types must move to the package that owns their behavior
rather than being copied into `pkg/domain`. Only stable, implementation-
independent contracts belong in public packages.

### 4. Preserve transport with mapper snapshots

Handlers keep route registration and service behavior but decode/encode
`api/v1` DTOs. Existing JSON golden and integration tests are the baseline;
any intentional wire change requires a separate change and cannot be hidden
in facade removal.

### 5. Remove adapters atomically at the breaking boundary

Compatibility adapters remain during migration and are deleted only in the
final removal PR after a zero-usage check. The final PR also removes the
compatibility exceptions in CI and changes the guard from “no new imports” to
“facade must not exist.”

### 6. Release from a clean compatibility baseline

Tag the last compatibility release before the removal branch. Release
artifacts are built from a clean checkout, with server and SDK MAJOR versions
validated together. A failed candidate is reverted before tagging; a failed
published release receives a new immutable correction and does not restore the
facade on the new major line.

### Alternatives considered

- **Delete core immediately:** rejected because the current production and
  test inventory still contains broad core dependencies and would combine
  unrelated migration work into an unreviewable flag day.
- **Keep a core shim indefinitely:** rejected because it preserves duplicate
  contracts and prevents the public package boundary from becoming enforceable.
- **Create a new universal `pkg/core`:** rejected because it recreates the
  god-package and violates ADR-031's domain/SPI/transport separation.
- **Change runtime behavior during deletion:** rejected because it makes
  compatibility failures impossible to attribute to the facade removal.

## Risks / Trade-offs

- **[Risk] Hidden core dependency remains in a benchmark, generated file, or test helper.** → Mitigate with exported-symbol inventory, `go list` import checks, repository-wide search, and clean-checkout compilation.
- **[Risk] A type alias hides a semantic mismatch with the public replacement.** → Mitigate with explicit mapper/conformance tests and ownership review; do not replace non-equivalent interfaces with aliases.
- **[Risk] HTTP output changes while DTOs are migrated.** → Mitigate with golden JSON, OpenAPI, and end-to-end route snapshots run before and after deletion.
- **[Risk] Dependent ADR work is incomplete.** → Mitigate with a release-blocking ADR gate and keep the prior compatibility branch supported.
- **[Risk] A published major release is hard to roll back.** → Mitigate with immutable tags, retained compatibility artifacts/branch, pre-tag release candidate testing, and a new correction release rather than retagging.

## Migration Plan

1. Create the core symbol ownership inventory and baseline compatibility
   snapshots.
2. Add CI prohibition on new production imports while the facade still exists.
3. Complete ADR-030/032/035/037 prerequisites and migrate core-owned policy
   values to explicit owners.
4. Migrate production packages in dependency order, then all tests, fakes,
   benchmarks, examples, and plugin fixtures.
5. Run zero-reference checks and clean-checkout builds with the facade still
   present; resolve every exception.
6. In the breaking PR, delete `src/internal/core` and compatibility adapters,
   update the guardrail, and run the complete validation matrix.
7. Publish aligned server/SDK MAJOR versions with migration notes and retain
   the compatibility release as rollback target.

Rollback before tagging is a revert of the removal PR. Rollback after tagging
is a new immutable corrected release or a return to the previous compatibility
branch; the facade is not silently restored on the new major line.

## Open Questions

None that change the contract or task breakdown. The exact next MAJOR number
is resolved by the repository's release state at implementation time.
