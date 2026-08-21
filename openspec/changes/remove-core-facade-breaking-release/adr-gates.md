# ADR gates and internal ownership — status (tasks 3.1–3.5)

> **Reading order:** sections below are an append-only log; later sections
> supersede earlier ones. **Final state (verified post-6.x, commits
> 0b1053f..75b9044):** `src/internal/core` is deleted with zero imports
> repo-wide; composition roots (`clienv.EnsureDB`, `app.New`) construct
> stores directly via `vector.NewStore` (the `spiadapter.VectorStore` wrap,
> legacy `vector.Index`, and all embedder/vector/reverse-extractor bridges
> are deleted); `spiadapter` contains exactly one bridge —
> `NewExtractor` over `extraction.LLMExtractor`, pending ADR-035 (§3.1).
> Earlier "Remaining"/"Still core-owned" lists are historical.

## Release gates A–D — sign-off evidence (task 7.6)

Recorded from the validation session of 2026-08-21 (HEAD `75b9044`):

- **Gate A (public contract freeze): PASS.** Public-API snapshot tests
  (`pkg/domain/public_api_test.go`, spi conformance tests, api/v1 DTO
  boundary tests) green in every run; external-like provider fixtures
  compile on `pkg/domain`+`pkg/spi` only (task 2.4); no method added to
  frozen interfaces (`spi.Embedder` single-method, health via optional
  `spi.Pinger`). Boundary guard (`scripts/check-public-boundaries.sh`)
  green after anchoring its patterns to quoted import paths.
- **Gate B (production dependency migration): PASS.** Zero
  `src/internal/core` references repo-wide (package deleted, 6.5);
  strict-mode import guard green; handlers on `api/v1`; MCP/CLI on domain
  values/command-local DTOs; services/persistence on canonical contracts
  (tasks 4.1–4.7, 6.1–6.6).
- **Gate C (dependent ADRs): PASS with recorded deferrals.** ADR-030/032
  verified not to require core-owned contracts (§3.3). ADR-035 identity
  *home* moved (`internal/id`); the UUIDv7/content-addressed *strategy* is
  deliberately deferred (§3.1) — no remaining caller requires a core-owned
  contract, which is the operative condition of the spec's ADR-gate
  scenario ("required by a core caller"). ADR-037 read-side values owned by
  `src/internal/retrieval` with canonical spi capability params (§L2,
  task 3.2); the full pipeline-SPI redesign stays a separate project.
  Policy values have owners + migration tests (3.4/3.5).
- **Gate D (behavior and operations): PASS** (govulncheck executed in CI).
  Clean-checkout build with the facade directory physically absent (7.1,
  worktree at `e36107a`); full race suite 1421 tests / 82 packages green;
  e2e CLI+HTTP+scenarios 121 tests green; `go vet` clean; fuzz smoke 10 s ×
  {FuzzSplitSQL, FuzzCosineSimilarity, FuzzEntityJSONRoundTrip,
  FuzzStoreRequestJSONRoundTrip} green; benchmark smoke green (7.5);
  golangci-lint **0 issues** across ./src/... ./pkg/... ./api/... after
  clearing residual ST1019/S1040/inline findings left by the alias
  collapse; golden/OpenAPI/integration suites show no intentional wire
  deltas (5.3/5.4 baselines still authoritative).
  Local govulncheck could not run (tool built with go1.26 under a go1.27
  toolchain — environment skew, not a code finding); the CI `govulncheck`
  job covers this gate.


This record captures the verification and ownership-move outcomes for the
"dependent ADR gates and internal ownership" phase of the core facade
removal. It is kept in the change directory so the release reviewer can see
exactly what moved, what was verified, and what is deliberately deferred.

## VectorStore capability completion (task 4.3 slice)

Every production service, helper, and wiring point now holds the canonical
`spi.VectorStore` contract instead of `core.VectorIndex`:

- **Constructors/fields flipped:** memory, edge, reembed, retention (+ its
  confidence lifecycle), task, ingest, ingestion (`IngestionWorker`, both
  memory workers, `applyVIOps`, `IngestionWorkerConfig`/`MemoryWorkerConfig`),
  retrieval (`Service`, `Retriever`, `MultiHopRetrieveContext`,
  `hopVectorSearch`, `GenerateResponse`), health probes, admin
  `RebuildIndex` (its local write-only subset interface became an alias of
  `spi.VectorStore`), store helpers (`StoreEntityWithEmbedding`,
  `PurgeEntity`), vector helpers (`SearchByVector`, `AddEdgeWithAutoCreate`,
  `AutoLinkEdges`).
- **Wiring adapts once at composition:** `clienv.Env.EnsureDB` and
  `app.New` wrap the raw backend with `spiadapter.VectorStore(...)`;
  `Env.VI`, `app.Application.VI`, and `ServeConfig.VI` are public-contract
  handles. The legacy backends keep their IDs-only view behind
  `vector.NewIndex`.
- **Namespace semantics:** all service-side searches/upserts/deletes target
  `spi.DefaultNamespace`, byte-equivalent to the legacy bridge's fixed
  namespace.
- **Batch → per-query:** `spi.VectorStore` has no batch method; the two
  batch fan-outs (ingestion dedup top-1-per-item, multi-hop neighbour
  expansion) became explicit per-query loops with unchanged result shapes.
  Store/Remove call sites map to single-record `Upsert` / `Delete`.
- **Test mocks ported:** `failingVIRecord`, `vecSpy`, `fakeVI`, `stubVI`,
  admin + health mocks now implement `spi.VectorStore`; real-index fixtures
  construct through the same wrap as production.

Remaining group-4 interface work: `core.LLMExtractor` constructor params —
blocked on ADR-035 ID semantics (the legacy shape carries LLM-minted
entity IDs that ingestion persists), not on types.

## Embedder capability completion (task 4.3 slice)

The `Embedder` capability is now genuinely migrated — this closes the gap
where earlier notes claimed a state the tree did not have:

- **`core.Embedder` is a deprecated alias of `spi.Embedder`** (single-method
  contract). Health checking moved to the optional `spi.Pinger` assertion;
  the one production Ping site (`cli/serve.go` startup check) asserts and
  degrades gracefully when absent.
- **All production signatures hold `spi.Embedder`**: memory, edge, reembed,
  ingest, ingestion (worker/config/dialog/resilient), task, episodic
  retrieval, admin rebuild-index, health probes, vector auto-link helpers,
  the retrieval pipeline (`Service`, `Retriever`, walk/response internals),
  `ai.Factory.NewEmbedder`, `config.NewEmbedder`, `app.Application.Embedder`,
  and `clienv.Env.Embedder`.
- **Both spiadapter embedder bridges deleted** after zero-reference
  verification (`NewEmbedder`, `NewLegacyEmbedder`, their adapters, tests,
  and builtin assertions). The extractor/vector bridges remain.
- ai compile-time assertions now pin `spi.Embedder` + `spi.Pinger` per
  provider; `LocalEmbedder` (both build variants) satisfies both.

Remaining group-4 interface work: `core.VectorIndex` constructor params
(method-set mismatch with `spi.VectorStore` — needs per-service call-site
rewrites) and `core.LLMExtractor` (blocked on ADR-035 ID semantics, not
types).

## Resume increment (post-interruption): alias sweep + ownership completion

The working tree had been interrupted mid-migration: new homes existed
(`pkg/domain`, `src/internal/id`, `retention.Policy`, `migration` types,
`vector/provider.go`) but core still carried native duplicates, callers were
unmigrated, and `go build ./...` failed at two points. This increment
completed the interrupted work; every claim below is verified by a green
`go build ./... && go vet && go test -race ./src/... ./pkg/... ./api/...`
(1456 tests).

**Build breaks fixed (exact interruption points):**

- `vector.NewInMemoryVectorStore` was referenced by `vector/provider.go` but
  never written. Implemented as a first-class public-contract store:
  namespace-scoped brute-force cosine with metadata Equals/Ranges filters,
  sticky per-namespace dimensions (`spi.DimensionError`), partial-batch
  failures (`spi.PartialBatchError`), explicit capacity reporting instead of
  eviction (`spi.CapacityError`), idempotent upserts, and populated
  `Hit.Score`. The legacy `InMemoryVectorIndex` is untouched and remains the
  compatibility facade's IDs-only view; its `load()` is now nil-DB safe for
  registry composition.
- `app.NewConfiguredReranker` expected `config.NewReranker()` to already be
  `spi.Reranker`; it is not (ai rerankers still implement the legacy
  facts-based contract). Added `publicRerankerFromLegacy` in app/providers.go:
  an inline `RetrievedFact ↔ Candidate` translation at the registry boundary,
  preserving ordering-only semantics.

**core → domain aliases (native duplicates deleted):** `Entity`, `Edge`,
`SchemaConfig` (+ `DefaultSchemaConfig` wrapper), `Relation`,
`ExtractedEntity`, `ExtractionResult`, `Provenance`, `MemoryMessage`,
`Polarity` (+ consts), `RankingWeight` (WithDefaults now comes from domain),
`SearchResult`, `TimePtr` (var), and the six slim projections `Fact`,
`Evidence`, `Episode`, `Task`, `Goal`, `Belief` (+ `Compose` delegating to
`domain.Compose`). All shapes were field-for-field identical before aliasing;
JSON tags unchanged, so wire/storage formats are byte-compatible. The As*/
WithInitialStatus/CanTransitionTo method definitions moved out of core with
the aliases (domain provides them); core's own projection tests now resolve
through the aliased methods.

**Ownership moves completed (callers flipped, core declarations deleted):**

- `NewTaskID`: all five production callers (`cli/task/create`,
  `compression/generate` ×2, `mcp/tools`, `server/task`) now call
  `id.NewTaskID()`; `core.NewTaskID` + its counter deleted. This also removes
  the latent dual-counter ID-collision risk.
- `RetentionPolicy`: `retention/service`, `lifecycle/components/gc`,
  `config/{config,ini}`, `server/server`, `server/retention` shell and tests
  now use `retention.Policy`; `core.RetentionPolicy` deleted.
- Migration values: `migration.Service` methods now return `migration.Status`
  / `migration.Mismatch` via new `toStatus`/`toMismatch` adapters (field-for-
  field pinned by types_test.go); `cli/db/migrate` consumes `migration.Status`;
  `core.MigrationStatus` / `MigrationMismatch` / `Migrator` deleted.
- Extraction compat: `spiadapter` uses `domain.LegacyExtractionDrafts`;
  `core/extraction_compat.go` (+ test) deleted — the converters live only in
  `pkg/domain`.

**Service-package value-type sweep (task 4.3 scope):** ingestion (+detectors),
memory (+evidence,+belief), edge, reembed, health, retention, task, episodic
production files now spell all value types through `pkg/domain`. Remaining in
those packages: constructor-level capability interfaces (`core.VectorIndex`,
`core.Embedder`, `core.LLMExtractor`, `core.Reranker`) and transport request
DTOs — deliberately deferred to the capability-interface migration (4.x) and
HTTP/MCP/CLI DTO tasks (4.5/4.6).

**Test-reality corrections:** `spiadapter/builtins_test.go` asserted ai
rerankers satisfy `spi.Reranker` directly — they do not (single-method
signature clash with the legacy contract). Assertions now pin current reality
(`core.Reranker` satisfaction + adapter bridges) with pointers to tasks 4.x /
6.2 where the flip happens.

## 3.1 — ADR-035 identity generation

**Done:** `NewTaskID()` moved out of `core` into `src/internal/id` (new
owning package). Production callers (`server/task`, `cli/task/create`,
`mcp/tools`, `compression`) now call `id.NewTaskID()`; the `core` facade no
longer owns identity generation.

**Deferred (not a facade-removal blocker):** the *strategy* change in
ADR-035 (UUIDv7/ULID for server-minted IDs, content-addressed entity IDs,
re-keying migration) is a scale-triggered project of its own. The facade
removal only requires that the identity-generation *home* and the
`ExtractedEntity` type leave `core`. `ExtractedEntity`/`ExtractionResult`
are extraction-compat types that move out in task 6.3. The
LLM-selected-entity-ID behavior is unchanged by the facade removal; its
replacement is tracked by ADR-035, not this change.

## 3.2 — ADR-037 retrieval-stage contracts

**Not started — sequencing-blocked.** `core.Retriever`,
`core.RetrieveContextOptions`, `core.RankingWeight`, `core.CompositeScorer`,
`core.ScoreBreakdown`, `core.GraphNode`, `core.RetrievedFact`, and
`core.RetrievalResult` are retrieval-owned values, but `src/internal/retrieval`
still imports `core` for the legacy capability interfaces (`core.VectorIndex`,
`core.Embedder`, `core.Reranker`). Moving the retrieval values into
`src/internal/retrieval` would create an import cycle (`core` → `retrieval`
→ `core`). They can only move after task 4.x migrates the retrieval package's
capability dependencies to `spi.VectorStore`/`spi.Embedder`/`spi.Reranker`.
The full ADR-037 pipeline-SPI/hybrid-channel work is a separate
scale-triggered project and is not required to delete the facade.

## 3.3 — ADR-030 tenancy and ADR-032 ingestion (verified)

Both ADRs' decisions ride on the **public** SPI, not on `core`:

- **ADR-030 (tenancy):** vector namespaces (`spi.SearchRequest.Namespace`,
  `spi.DefaultNamespace`, `spi.VectorRecord.Namespace`) are defined in
  `pkg/spi`. Row-level `tenant_id` and context-carried tenancy are storage
  concerns (ADR-027 repositories), not `core`-owned contracts.
- **ADR-032 (async ingestion):** batch embedding is `spi.BatchEmbedder`
  (public); the ingestion pipeline already writes through `spi.VectorStore`
  (`Upsert`/`Delete`) and reads via `spi.SearchRequest`. The job/queue model
  is a persistence/worker concern, not a `core` contract.

**Conclusion:** neither ADR requires a `core`-owned contract. No action
needed for facade removal beyond what the public SPI already provides.

## 3.4 — policy/config ownership moves

**Moved (cycle-safe, complete in this phase):**

- `core.RetentionPolicy` → `retention.Policy` (+ `retention.Default()`);
  `core.RetentionPolicy` deleted.
- `core.MigrationStatus` / `core.MigrationMismatch` / `core.Migrator` →
  `migration.Status` / `migration.Mismatch` / `migration.Migrator`
  (+ adapter helpers); deleted from `core`.

**Deferred (cycle-blocked by task 4.x):**

- `core.RankingWeight` + `WithDefaults` — retrieval-owned.
- `core.RetrieveContextOptions`, `core.CompositeScorer`, `core.ScoreBreakdown`,
  `core.GraphNode`, `core.RetrievedFact`, `core.RetrievalResult` — retrieval-owned.
- `core.DomainError` / error codes / sentinel errors — wide call-site
  footprint; migrate after task 4.x to the `pkg/domain` error taxonomy.

## 3.5 — migration tests

Added:

- `src/internal/id/id_test.go` — uniqueness, concurrency, `task-<n>` format.
- `src/internal/retention/policy_test.go` — `Default()` reproduces the
  historical `90d / 1h / 500` values.
- `src/internal/migration/types_test.go` — `toStatus`/`toMismatch` adapters
  preserve every field (including the nullable `ChecksumMatch` pointer).

The existing `migration/service_test.go` and `retention/service_test.go`
suites continue to exercise the moved types' runtime behavior unchanged.

## Group 4 progress (cycle-safe ownership moves)

Group 4 is the full production caller migration (~99 non-test files still
import `core`). The following cycle-safe moves are complete; the cycle-coupled
bulk (capability interfaces, retrieval types, errors) is the remaining work.

**Moved out of `core` (callers migrated to the owning package, `core` keeps a
deprecated alias where a compatibility shim is cheap):**

- `Polarity` / `PolaritySupport` / `PolarityRefute` → `domain`
  (callers: `evolution/aggregation.go`, `memory/evidence/evidence.go`).
- `TimePtr` → `domain.TimePtr` (callers: `compression`, `ingestion`).
- `Component` → `lifecycle.Component` (caller: `lifecycle/manager.go`).
- `Logger` / `NoopLogger` → `logging.Logger` / `logging.NoopLogger`
  (callers: `logging/slog.go`, `logging/test.go`, `logging/slog_test.go`).

**Remaining (cycle-coupled / semantic — see 3.2 and the group 4 task list):**

- Capability interfaces: `VectorIndex` (→ `spi.VectorStore` via 4.2) and
  `Retriever` (→ retrieval). `Embedder`, `LLMExtractor` and `Reranker` are
  now fully pluggable through `spi` — see the Group 4.1 sections.
  `LLMExtractor` (→ `spi.Extractor`), `Reranker` (→ `spi.Reranker`), and
  `Retriever` (→ retrieval). `Embedder` is now fully migrated to
  `spi.Embedder`/`spi.Pinger` — see the Group 4.1 section. **4.4 done:**
  `retrieval.Reranker` is now `type Reranker = spi.Reranker`; the
  `app.Env`, `cli.Env`, and `serverstate.State` boundaries store canonical
  SPI handles with no `retrieval.NewLegacyReranker` indirection.
- Retrieval types: `RankingWeight`, `RetrieveContextOptions`, `CompositeScorer`,
  `ScoreBreakdown`, `GraphNode`, `RetrievedFact`, `RetrievalResult`,
  `SearchResult` (→ `src/internal/retrieval`). **4.4 done:** the walk
  pipeline performs the `[]RetrievedFact` ↔ `[]spi.Candidate` round-trip
  inline at the `applyReranker` boundary so internal shapes never leak
  into the public contract.
- `DomainError` + codes + sentinels + constructors (→ `pkg/domain` error
  taxonomy; shape change from `Code string` to `ErrorCode`).
- Extraction/ingestion types: `ExtractedEntity`, `ExtractionResult`, `Relation`,
  `Provenance`, `MemoryMessage`.
- Misc value types: `ReEmbedResult`, `VerifyReport`, `Community`,
  `ContradictionPair`, `ConnectedComponent`, `TreeNode`.
- Domain-alias mechanical sweep (`core.Entity`/`Edge`/`SchemaConfig`/`Task`/
  `Fact`/`Evidence`/`Episode`/`Belief`/`Goal` → `domain.*`) across the remaining
  ~99 files.

## Group 4.4 progress (lifecycle wiring → canonical `spi` handles)

The application composition, lifecycle ownership, server state, and factory
wiring are now fully migrated to canonical SPI handles:

- **`retrieval.Reranker` is `type Reranker = spi.Reranker`** — no separate
  legacy contract. The walk pipeline translates
  `[]RetrievedFact` ↔ `[]spi.Candidate` inline at `applyReranker` time, so
  internal data structures never leak into the public contract.
- **`retrieval.NewLegacyReranker` and `retrieval/legacy.go` removed
  entirely** — there is no longer a spi → retrieval bridge, only a
  positional+identity-keyed inline translation in `walk.go`.
- **`app.Env.Reranker` and `cli.Env.Reranker`** are `spi.Reranker` (alias to
  `retrieval.Reranker`), populated directly from
  `app.NewConfiguredReranker` without bridging.
- **`serverstate.New(…, reranker retrieval.Reranker)`** takes the canonical
  `spi.Reranker`; honoured contract by the `ai.NoopReranker`,
  `ai.OllamaReranker`, `ai.OpenAIReranker` implementations, which already
  satisfy `spi.Reranker` per task 4.1.
- **`health.RerankerProbe` operates on `spi.Reranker`** and the probe fakes
  (`mockReranker`, `failingReranker`) were ported to `spi.Candidate` shapes.
- **`spiadapter.NewReranker` and `TestNewReranker_*` are gone** — the
  `spiadapter` package no longer bridges rerankers (only the still-needed
  `NewEmbedder` / `NewLegacyEmbedder` / `NewExtractor` / `NewLegacyExtractor`
  bridges remain).
- **`retrieval.Service`** no longer carries the legacy
  `vi core.VectorIndex` compatibility view (it was test-only); the public
  constructor signature is `retrieval.New(db *sql.DB, vectors spi.VectorStore,
  embedder spi.Embedder)`.

This breaks the spiadapter ↔ retrieval import cycle that previously lived
behind `retrieval/legacy.go`: now the test packages may import `retrieval`
without selecting the `spiadapter`-side branch.

## Group 4.2 progress (vector consumers → `spi.VectorStore`)

## Group 4.1 progress (embedder capability → `spi`)

The embedder capability is now fully migrated to the public SPI. `spi.Pinger`
was added as an optional health-check capability, so `spi.Embedder` stays
frozen at the single `Embed` method while embedders that can reach a remote
endpoint expose `Ping` separately.

**Provider implementations** (`src/internal/ai`) now implement
`spi.Embedder` (+ `spi.Pinger` where applicable):

- `OllamaEmbedder`, `OpenAIEmbedder` — compile-time assertions updated to
  `spi.Embedder` + `spi.Pinger`.
- `LocalEmbedder` (both build variants) and `NoopEmbedder` already satisfied
  both method sets structurally; doc comments updated.
- `ai.Factory.NewEmbedder()` now returns `spi.Embedder`.

**Wiring:**

- `config.Config.NewEmbedder()` → `spi.Embedder`.
- `app.NewConfiguredEmbedder()` → `spi.Embedder`, returned directly from the
  typed factory registry (no `spiadapter.NewLegacyEmbedder` wrap).
- `app.Application.Embedder`, `app.Runtime.Embedder`, `env.Env.Embedder` →
  `spi.Embedder`.
- The single `.Ping()` call site (`cli/serve.go` startup health check) now
  type-asserts `spi.Pinger` and skips gracefully when absent.

**Service consumers** migrated from `core.Embedder` to `spi.Embedder`:
`memory`, `edge`, `reembed`, `task`, `retrieval` (regular read path),
`ingestion` (worker + config), `ingest`, `episodic`, `admin`, `health`
(`EmbedderProbe`), and `vector.AddEdgeWithAutoCreateStore`/`AutoLinkEdgesStore`.

**Adapters retained for compatibility tests only:** `spiadapter.NewEmbedder`
and `spiadapter.NewLegacyEmbedder` now have zero production references; they
are exercised solely by `spiadapter/builtins_test.go` and will be deleted in
task 6.4 once their last compat-test use is removed.

**Still core-owned (blocked by DTO migration, task 4.3):** `LLMExtractor`
(`ExtractEntities` → `*core.ExtractionResult`) and `Reranker`
(`Rerank` → `[]core.RetrievedFact`) remain legacy capability interfaces
because their input/output types (`ExtractionResult`, `ExtractedEntity`,
`RetrievedFact`) still live in `core`. The provider implementations and
`spiadapter.NewExtractor`/`NewReranker` bridges stay in place until those
DTOs move to owning packages.

## Group 4.1 extraction & reranker complement

The extractor side now spans the public SPI cleanly:

- `pkg/domain/extraction.go` defines `ExtractedEntity`, `Relation`,
  `ExtractionResult`, `Provenance`, plus `LegacyEntityDraft` /
  `LegacyExtractionDrafts` (moved from `src/internal/core/extraction_compat.go`).
  `core` keeps deprecated `type X = domain.X` aliases so the dozens of
  call sites that still reference `core.ExtractedEntity` /
  `core.ExtractionResult` / `core.Provenance` round-trip untouched.
- `ai.OllamaLLMExtractor` and `ai.OpenAILLMExtractor` declare both the
  legacy `core.LLMExtractor` (kept — internal compression/ingestion
  pipelines still consume the LLM-ID-bearing `*ExtractionResult`) and a
  new `Extract(ctx, spi.ExtractRequest) (spi.ExtractResponse, error)`
  method so the same provider type plugs into the public SPI registry
  without losing the internal compatibility method. `aitest.FakeExtractor`
  and `FakeReranker` carry the matching assertions.
- `spieadapter.NewLegacyExtactor` was intentionally NOT added; instead
  the bidirectional story lives in branches: `NewEmbedder` /
  `NewLegacyEmbedder` (both kept in spiadapter; zero production callers).
  The asymmetry between embedder (full bi-di) and extractor (no `Legacy*`)
  is documented below.

### Why extractor does NOT have a `NewLegacyExtractor` bridge yet

The public `spi.ExtractResponse.Entities` is `[]domain.EntityDraft` —
identity-free. The legacy `core.LLMExtractor.ExtractEntities` returns
`*core.ExtractionResult` whose `ExtractedEntity`s carry the
LLM-suggested entity IDs that the ingestion/compression pipelines
persist into SQLite as primary keys. Synthesizing IDs at the bridge
boundary would either (a) collide on retry or (b) silently change the
persisted IDs of every legacy caller. Neither is safe in the
compatibility release. The right place to break ID dependence is ADR-035
(task 6.3). For 4.1 we therefore accept a partial completion: provider
implementations satisfy `spi.Extractor`, the registry resolves to
`spi.Extractor`, but the application boundary (`env.Extractor` /
`app.Application.Extractor`) still stores `core.LLMExtractor`. The
remaining consumers (compression, ingestion worker, health probes)
keep using the legacy shape and stay on the cycle-coupled DTO move
track.

## Loop session: contract ownership completion (L1–L4)

Four verified increments, each committed green (`go build` + `go vet` +
`go test -race`, 1454 tests):

- **L1 — misc value types to owners (6baf138):** `ReEmbedResult`→reembed,
  `TreeNode`→store, graph result quartet
  (`VerifyReport`/`Community`/`ContradictionPair`/`ConnectedComponent`) →
  `pkg/domain/graph_results.go` (wire envelopes; store produces them, so a
  below-store owner is impossible without cycles — same precedent as
  `domain.SearchResult`).
- **L2 — retrieval owns the read-side contract (3c8a910):** the interrupted
  work's `retrieval/types.go` locals are now THE contracts;
  `ScoreBreakdown`/`RetrievedFact`/`GraphNode`/`RetrievalResult`/
  `CompositeScorer`/`RetrieveContextOptions`/`Retriever` deleted from core.
  All three ai rerankers + fakes rewritten to the public `spi.Candidate`
  API; `applyReranker` performs the RetrievedFact↔Candidate translation
  inline (ordering is the only observable effect); the app-side legacy
  reranker adapter was deleted — `config.NewReranker` returns `spi.Reranker`
  directly. Closes the substance of task 3.2's in-scope slice and 4.1's
  reranker complement.
- **L3 — extraction owns the legacy extractor (16e8c56):** new
  `internal/extraction` package holds the ID-bearing `LLMExtractor`
  contract pending ADR-035; ingestion/compression/health/spiadapter/config
  reference the owner, not core.
- **L4 — apperr owns the wire-error contract:** `core.DomainError`,
  codes, sentinels, constructors moved VERBATIM to `internal/apperr`.
  Zero behavior change: §10 pinned rendering ("msg (field)") and HTTP/CLI
  bytes preserved. Deliberate deferral with rationale: adopting the typed
  `pkg/domain.Error` taxonomy changes error strings — that is a
  behavior-visible change belonging to the semantic-foundation release,
  forbidden by this change's own compatibility requirement. This is the
  one intentional remainder in core after 6.x.

Core facade residual after L1–L4: deprecated aliases (value types), the
legacy `VectorIndex` interface (compat backends + spiadapter bridge),
`Component`/`Logger` natives (callers flip during 5.x/6.x), `NormalizeSlice`,
and the transport DTO family (consumed by shells until 4.5/4.6).
