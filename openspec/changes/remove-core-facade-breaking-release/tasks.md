## 1. Baseline and ownership inventory

- [x] 1.1 Generate the complete exported-symbol inventory for `src/internal/core`, including aliases, interfaces, DTOs, policy values, errors, helpers, and compatibility adapters.
- [x] 1.2 Classify every symbol with a single owner: `pkg/domain`, `pkg/spi`, `api/v1`, command-local DTO, owning internal package, test-only fixture, or delete.
- [x] 1.3 Record the compatibility-release baseline for HTTP/OpenAPI JSON, MCP tools, CLI output, persistence behavior, provider behavior, and relevant benchmarks.
- [x] 1.4 Add CI enforcement that rejects new production imports of `src/internal/core` while the compatibility facade still exists.

## 2. Public contract readiness

- [x] 2.1 Audit `pkg/domain` for all domain symbols currently required by production callers and add only reviewed domain-owned values or errors.
- [x] 2.2 Audit `pkg/spi` for all required provider capabilities, descriptors, typed errors, and compile-time compatibility tests without adding required methods to frozen interfaces.
- [x] 2.3 Complete `api/v1` DTO and mapper coverage for every HTTP route that currently consumes or emits a core DTO.
- [x] 2.4 Add or update external-like provider fixtures so they compile using only `pkg/domain` and `pkg/spi`.
- [x] 2.5 Add public API snapshots or equivalent contract tests covering exported types, interface method sets, and forbidden internal/storage/provider leaks.

## 3. Dependent ADR gates and internal ownership

- [ ] 3.1 Complete ADR-035 identity generation and remove all production reliance on LLM-selected persistent IDs. (identity home done: all 5 `NewTaskID` callers now use `id.NewTaskID`, `core.NewTaskID` deleted; full ADR-035 strategy deferred — see adr-gates.md)
- [ ] 3.2 Complete ADR-037 retrieval-stage contracts and migrate callers away from the legacy `core.Retriever` construction-dependency shape. (blocked by task 4.x capability migration — see adr-gates.md)
- [x] 3.3 Verify ADR-030 tenancy/namespace decisions and ADR-032 ingestion decisions do not require core-owned contracts.
- [x] 3.4 Move remaining core-owned policy/config values such as ranking, retrieval options, migration values, and internal errors to explicit owning packages. (retention → `retention.Policy`; migration values → `migration`; `RankingWeight`/retrieval options/score types → `retrieval`; misc graph/task/reembed values → owners; legacy wire-error contract → `apperr` verbatim — typed `domain.Error` adoption deferred to the semantic release, see adr-gates.md)
- [x] 3.5 Add migration tests proving each moved policy type preserves current defaults and behavior.

## 4. Production caller migration

> Progress: value-type sweep of the group-4 service packages is DONE —
> `Entity`/`Edge`/`SchemaConfig`/extraction DTOs/`MemoryMessage`/`Polarity`/
> `TimePtr`/`RankingWeight`/`SearchResult`/slim projections are now aliases to
> `pkg/domain`; `NewTaskID`→`id`, `RetentionPolicy`→`retention.Policy`,
> `MigrationStatus/Mismatch/Migrator`→`migration` are deleted from `core`.
> **Embedder + VectorStore are fully migrated**: service constructors,
> `app`/`clienv.Env` fields, and the retrieval pipeline all hold
> `spi.Embedder`; `core.Embedder` is a deprecated alias; both spiadapter
> embedder bridges are gone. Every service and wiring point holds
> `spi.VectorStore` (namespace = `spi.DefaultNamespace`, single-query
> searches replace the legacy batch call); the raw backend keeps its
> legacy IDs-only view behind `vector.NewIndex`, wrapped once at
> composition.
> Remaining in these packages: `LLMExtractor` constructor params
> (blocked on ADR-035 ID semantics) and transport request DTOs —
> tracked by 4.5/4.6 and 6.x.

- [x] 4.1 Migrate provider implementations and `spiadapter` callers to public SPI contracts; retain adapters only for explicitly tracked compatibility tests.
- [x] 4.2 Migrate repositories, vector consumers, and persistence boundaries to `pkg/domain` and `spi.VectorStore` or owning internal interfaces.
- [x] 4.3 Migrate ingestion, memory, edge, re-embedding, health, retention, and task services away from core types. (`spi.Embedder` + `spi.VectorStore` constructors/wiring; legacy extractor contract owned by `extraction` pkg pending ADR-035; all value types canonical)
- [x] 4.4 Migrate application composition, lifecycle ownership, server state, and factory wiring away from core capability interfaces. (extended: `retrieval.Reranker` is now `type Reranker = spi.Reranker`; app/lifecycle/server-state/factory all hold canonical SPI handles; `retrieval.NewLegacyReranker` + `retrieval/legacy.go` removed completely)
- [x] 4.5 Migrate HTTP shells to `api/v1` DTOs and mappers while preserving routes, status codes, JSON fields, omission rules, and error envelopes. (all DecodeJSON sites on apiv1; services own their command inputs (`memory.StoreInput`, `edge.AddEdgeInput`, task/ingest/retrieval take plain args); wire verified by golden + CLI integration suites)
- [x] 4.6 Migrate MCP and CLI adapters to public domain values or command-local DTOs without importing HTTP DTOs for sharing. (cli/task owns its payload structs; cli/memory carriers are inline command locals or service-owned inputs; MCP speaks domain values + spi handles)
- [x] 4.7 Migrate examples, benchmarks, generated fixtures, and plugin/test packages that still depend on the facade. (verified zero facade imports outside src/internal — examples/plugins/sdk never referenced it; the in-tree production alias sweep landed with this task, leaving only legacy-vector compat files + tests)

## 5. Zero-reference and compatibility verification

- [ ] 5.1 Replace core-dependent tests and fakes with public domain/SPI/API contract fixtures; isolate and remove compatibility-only tests.
- [ ] 5.2 Add a repository-wide production import check proving no non-test caller depends on `src/internal/core`.
- [ ] 5.3 Run HTTP golden and OpenAPI contract tests against the migrated implementation and compare with the recorded baseline.
- [ ] 5.4 Run MCP, CLI, persistence, provider, external-like package, and SDK integration suites through the migrated wiring.
- [ ] 5.5 Confirm all compatibility adapters have zero runtime and test references except the final removal task.

## 6. Breaking removal implementation

- [ ] 6.1 Delete deprecated domain aliases and projection wrappers after their ownership inventory entries are migrated.
- [ ] 6.2 Delete legacy `VectorIndex`, `Embedder`, `LLMExtractor`, `Reranker`, and unreplaced `Retriever` facade interfaces after the ADR-037 gate.
- [ ] 6.3 Delete core HTTP/task DTO aliases and extraction compatibility result types after all transport/CLI/MCP callers use replacements.
- [ ] 6.4 Delete `spiadapter` legacy constructors and the IDs-only vector adapter after zero-reference verification.
- [ ] 6.5 Remove the remaining `src/internal/core` package and update imports, package docs, and generated references.
- [ ] 6.6 Invert CI guardrails from “no new imports” to “facade directory and imports must not exist.”

## 7. Release-candidate validation

- [ ] 7.1 Build from a clean checkout with no compatibility-release generated artifacts and verify the facade directory is absent.
- [ ] 7.2 Run full unit/integration tests, `go vet`, race-enabled tests, and API contract tests.
- [ ] 7.3 Run required fuzz smoke tests, external-like compilation, public API checks, boundary checks, and security/lint gates.
- [ ] 7.4 Verify HTTP, MCP, CLI, persistence, and provider behavior against the compatibility baseline with no intentional unrelated changes.
- [ ] 7.5 Run benchmark smoke/regression checks and document any accepted performance delta.
- [ ] 7.6 Review the release PR against every Gate A–D in `docs/release-core-facade-removal.md` and record sign-off evidence.

## 8. Major release and support

- [ ] 8.1 Align server, Go SDK, Python SDK, and TypeScript SDK MAJOR versions and update version-consistency checks.
- [ ] 8.2 Publish the migration table and examples for domain models, embedder, extractor, reranker, vector store, and HTTP DTOs.
- [ ] 8.3 Update `CHANGELOG.md`, `RELEASE.md`, README links, SDK docs, and deprecation/removal notices with a prominent breaking-change warning.
- [ ] 8.4 Identify and maintain the previous compatibility-release branch and rollback artifacts for the documented support window.
- [ ] 8.5 Produce the release candidate, checksums, provenance attestations, and platform artifacts from the clean removal commit.
- [ ] 8.6 Tag the immutable MAJOR release only after all release-candidate gates pass; do not retag or force-push on failure.
- [ ] 8.7 Monitor migration issues for one release cycle and publish corrective releases without silently restoring the facade on the new major line.
