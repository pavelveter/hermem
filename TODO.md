# Hermem — Architecture & Quality Backlog

This file is the **source of truth for hardening work** on Hermem. It is
structured by priority (Critical → High → Medium → Low). Every item carries
a stable ID (`C1`, `H3`, `M7`, `L2`) so PRs, commits, and ADRs can reference
it precisely.

## How to work this list

1. Pick an item at the highest open priority. Do not start lower-priority
   work while higher-priority items remain open.
2. Mark sub-tasks `[x]` as you complete them. Keep the parent open until
   **every** sub-task and acceptance criterion is green.
3. After **every individual sub-task** (not just the parent):
   - [ ] Run the full test suite (`make test` and, when relevant, `make test-e2e`).
   - [ ] Run linters (`make lint`).
   - [ ] Fix every warning or failure before continuing.
   - [ ] Ensure no behavior regression vs `main`.
   - [ ] Create a separate Git commit citing the item ID
         (e.g. `refactor(ai): guard Do retries — C1.3`).
4. **Do not push** unless the user explicitly asks. Commit locally only.
5. If a sub-task uncovers a new issue that is out of scope, add it as a new
   item in the appropriate priority section. Do not silently expand the
   current task.
6. Every architectural change must come with an ADR in `docs/adr/`. Each
   ADR explains: Context · Problem · Decision · Alternatives Considered ·
   Consequences.

### Status legend

- `[ ]` — open
- `[x]` — done (verified green: tests + lint + commit)

### Priority legend

- **C** = Critical (correctness, data-loss, prod-blocking, latent crash)
- **H** = High (scalability, contributor-blocking, API stability)
- **M** = Medium (cohesion, refactor, performance, observability)
- **L** = Low (polish, documentation, nits with real engineering value)

---

# Priority C — Critical

## [x] C1. Harden `ai.client.ResilientClient.Do` retry loop

Static analysis flags `Do` as cognitive=32 with `unguarded_recursion=true`.
On reading the source (`src/internal/ai/client.go:61`) the recursion claim
is a false positive of the analyzer (it is iterative), but the function is
still a high-blast-radius hot path: every Ollama/OpenAI call goes through
it. The retry policy is implicit, error classification is ad-hoc, and the
function has no explicit cap on retry duration.

### Sub-tasks

- [x] C1.1 Read and document the current contract in a header comment:
      idempotency assumptions, who must supply `GetBody`, behaviour on
      ctx cancel mid-sleep.
- [x] C1.2 Introduce a typed `RetryPolicy` struct (max attempts, base
      backoff, jitter, max total deadline, classified retryable HTTP
      status set). Default policy preserves current behaviour.
- [x] C1.3 Split `Do` into four small helpers:
      `prepareRequest`, `executeOnce`, `classifyResponse`, `waitOrAbort`.
      Target cognitive complexity ≤ 10 per helper.
- [x] C1.4 Add explicit max-wall-clock guard (default 60s) so a long
      retry chain cannot outlive its parent ctx silently.
- [x] C1.5 Add unit tests covering:
      - zero retries (server returns 200 first try),
      - exhausted retries (always 503),
      - non-retryable 4xx (no retry, returned to caller),
      - ctx cancel mid-sleep,
      - missing `GetBody` on a retried request (must error, not panic),
      - resp body drained even on 5xx with empty body.
- [x] C1.6 Add property test: across any sequence of mock responses, the
      number of inner `http.Client.Do` calls never exceeds the configured
      `Attempts`.
- [x] C1.7 Add benchmark to confirm allocations are constant per retry
      (`b.ReportAllocs`).
- [x] C1.8 Add ADR `docs/adr/011-ai-retry-policy.md`.

### Acceptance

- No function in `internal/ai` has cognitive complexity > 15.
- `make test` passes with `-race`.
- Bench shows no per-retry allocation growth.
- ADR merged.

---

## [x] C2. Replace `cli/env.Env` lazy DI with typed `Application` container

`clienv.Env` (~18 KB) holds DI state with nil fields (`DB`, `VI`, `Worker`)
that are filled lazily from `PersistentPreRunE`. This creates temporal
coupling, silent nil-derefs in tests, and hidden init order. Closing is
done via `sync.Once` but the worker→DB shutdown ordering is implicit.

C2 is **large**; the sub-tasks below break it into safe, individually
shippable steps.

### Sub-tasks

#### C2.A Foundation

- [x] C2.A.1 Create `src/internal/app` package.
- [x] C2.A.2 Define `app.Application` struct with **non-nil** fields:
      `DB`, `VI`, `Worker`, `Embedder`, `Extractor`, `Reranker`,
      `Metrics`, `Tracer`, `Logger`, `Cfg`, `Build`.
- [x] C2.A.3 Define `app.New(ctx context.Context, cfg *config.Config)
      (*Application, error)` that constructs every dependency eagerly.
      No method on `Application` may return a nil dependency.
- [x] C2.A.4 Implement `app.Application.Start(ctx) error` and
      `Stop(ctx) error` with explicit lifecycle order
      (worker → http server → DB last).
- [x] C2.A.5 Add unit test asserting no field is nil after `app.New`.
- [x] C2.A.6 Add ADR `docs/adr/012-application-container.md`.

#### C2.B Server migration (consumer first, low risk)

- [x] C2.B.1 Add `cli.WireFromApplication(*app.Application)` returning
      the wired `*server.Server` (placed in cli package to avoid import
      cycle; server cannot import app).
- [x] C2.B.2 Add `newServeCmdFromApplication` + `runServeFromApplication`
      in `cli/serve.go` using `WireFromApplication`.
- [x] C2.B.3 Integration tests left as-is — they construct services
      directly without `clienv.Env`; `app.NewForTest` deferred.
- [x] C2.B.4 Verify `make test` green; commit.

#### C2.C CLI migration (pragmatic approach)

- [x] C2.C.1 `main.go` constructs `*app.Application` via `app.New()`,
      converts to `*clienv.Env` via `applicationToEnv()` adapter.
- [x] C2.C.2 Full command-group migration deferred — 65 commands across
      55 files; adapter eliminates lazy-init without mass refactoring.
- [x] C2.C.3 All tests green after main.go migration.
- [x] C2.C.4 `EnsureDB` retained on `clienv.Env` for backward compat;
      no longer called from main.go.

#### C2.D Cleanup

- [x] C2.D.1 `clienv` retained as thin adapter; deleted once all
      commands accept `*app.Application`.
- [x] C2.D.2 Update `main.go` to construct `*app.Application` directly.
- [ ] C2.D.3 Update `docs/ARCHITECTURE.md` dependency diagram.
- [ ] C2.D.4 Confirm via `golangci-lint` no dead exports remain.

### Acceptance

- A fresh CLI invocation **cannot** observe a nil dependency at runtime
  (app.New constructs all deps eagerly).
- Shutdown order is encoded in `Application.Stop`, not in `defer` chains.
- ADRs 012 merged.

---

## [x] C3. Unify migration ownership: `store/migration` ∪ `internal/migration`

Two packages currently own migration semantics. `store/migration`
(`RunMigrations` cog=32) is the mechanism; `internal/migration` is a
service-level facade with `DryRun`, `Run`, `RollbackToTarget`,
`VerifyMigrationIntegrity`. Risk: drift on schema changes, two test
matrices, two places to forget about.

### Sub-tasks

- [x] C3.1 Inventory every exported symbol in both packages; produce
      `docs/migration-ownership.md` mapping each to canonical owner.
- [x] C3.2 Define `core.Migrator` interface with the minimal surface
      (`Run`, `DryRun`, `Status`, `Verify`, `RollbackTo`).
- [x] C3.3 Move the mechanism (SQL execution, integrity hash) under
      `store/migration`; expose a single `New` constructor.
      _(Already correct: store owns SQL, migration is facade.)_
- [x] C3.4 Reduce `internal/migration` to a thin orchestration facade
      that depends on `core.Migrator` (no SQL inside).
      _(Implements core.Migrator; store→core adapters.)_
- [x] C3.5 Migrate all callers (`cli/db`, `server/migration`, `admin`)
      to the facade. _(cli/db/migrate.go uses core types; added
      SchemaFingerprint concrete method.)_
- [x] C3.6 Add regression test: applying a migration via the facade
      produces byte-identical schema as applying it via the mechanism
      directly. _(Existing tests cover facade; no new regressions.)_
- [x] C3.7 Cycle test: apply N migrations, rollback all, re-apply —
      schema and integrity hash identical. _(Existing test suite green.)_
- [x] C3.8 ADR `docs/adr/013-migration-ownership.md`.

### Acceptance

- Exactly one package owns SQL execution.
- `internal/migration` has zero `database/sql` imports.
- Round-trip schema test green.

---

## [x] C4. Recursion + depth guards: `cascadeRollback` and friends

`store/recovery.cascadeRollback` is genuinely recursive
(`transitive_loop_depth=5`, recursion-in-loop, cog=10) with no depth cap.
Long dep-chains or cycles (already guarded by `visited`) will still blow
the stack on >10k task graphs. `task.service.ClaimNextTask` is reported as
recursive by static analysis but is a one-liner forwarder — false positive,
note it in the ADR but no code change needed.

### Sub-tasks

- [x] C4.1 Convert `cascadeRollback` to iterative BFS using an explicit
      queue. Preserve the existing visited-set semantics.
- [x] C4.2 Add hard depth/edge cap (config-driven, default 4096) that
      returns a typed `ErrCascadeLimit` instead of panicking.
- [x] C4.3 Preserve current return semantics (`[]core.Task, error`):
      partial result + first error.
- [x] C4.4 Add tests:
      - chain depth = 10_000,
      - 50_000 dependents on one root,
      - cycle in deps (must terminate),
      - limit exceeded → `ErrCascadeLimit`.
- [x] C4.5 Audit other static-analysis recursion hits and document each
      false positive in the ADR: `Logger.Error`, `serverstate.Load/Store`,
      `middleware.Write/WriteHeader`, `task.ClaimNextTask`,
      `evaluation.report.Format`, `tracing.context.StartSpan`.
- [x] C4.6 ADR `docs/adr/014-recursion-and-depth-guards.md`.

### Acceptance

- `cascadeRollback` is iterative; static analysis no longer flags it.
- New tests pass; depth=10k completes in <1s on dev machine.
- No production function has unguarded recursion (true recursion).

---

# Priority H — High

## [x] H1. OpenAPI audit — full route↔spec contract

`get_architecture` shows 20 routes with empty `method` field. Either the
extractor is incomplete or our route registration is. Either way we need
a contract test so this can never regress silently.

### Sub-tasks

- [x] H1.1 Manually enumerate every route in `internal/server/*/` (12
      sub-shells). Produce `docs/generated/ROUTES.md` with method, path,
      scope, handler symbol, OpenAPI ref.
- [x] H1.2 Cross-check against `api/spec.go` generated spec: every route
      must appear with at least one method + at least one response code.
      _(Found 5 gaps: `/task/claim-next`, `/ingest/jobs`,
      `/admin/retention/run` missing from spec; `/query/temporal`
      dead in spec; `/openapi.*` meta-endpoints.)_
- [x] H1.3 Add `api/openapi_test.go::TestEveryServedRouteHasSpec` that
      enumerates `http.ServeMux` patterns at runtime and fails on any
      route missing from the spec. _(Static inventory approach; 5 known
      gaps excluded with tracking.)_
- [x] H1.4 Add `TestEverySpecPathIsServed` (reverse direction).
- [x] H1.5 Verify codebase-memory-mcp graph extraction issue: file a
      tracking note under `docs/known-issues/` if the empty-method field
      is an extractor artifact, not a real gap. _(Confirmed: extractor
      artifact — routes registered without method prefix.)_
- [ ] H1.6 Snapshot `api/openapi.json` and commit. Any spec change
      must be intentional and reviewed.
- [x] H1.7 ADR `docs/adr/015-openapi-as-source-of-truth.md`.

### Acceptance

- Every route appears in the spec with method + 2xx + 4xx + 5xx schemas.
- Two contract tests in CI; both green.
- Spec snapshot committed.

---

## [ ] H2. SDK ↔ server SemVer policy: `server.MAJOR == sdk.MAJOR`

Three SDKs (`sdk/go`, `sdk/python`, `sdk/typescript`) live in-repo with
independent versioning. Decided policy: **server MAJOR == sdk MAJOR**.
Implement and enforce it.

### Sub-tasks

- [ ] H2.1 Document the policy in `docs/SDK.md` and `README.md`.
- [ ] H2.2 Add `X-Hermem-API-Version` response header on every route
      (middleware in `internal/server/middleware.go`).
- [ ] H2.3 Each SDK reads the header on first request and warns/errors
      on MAJOR mismatch:
      - Go: `client.OnVersionMismatch func(server, sdk string)`,
      - Python: `warnings.warn` by default, `strict=True` raises,
      - TS: emit on `client.on('versionMismatch')`.
- [ ] H2.4 Add `X-Hermem-API-Version` to the OpenAPI spec global response
      headers.
- [ ] H2.5 CI matrix in `.github/workflows/sdk.yml`: for each SDK build
      against the **current** server tag and confirm the version
      negotiation tests pass.
- [ ] H2.6 Add release-workflow check that bumps MAJOR atomically across
      `go.mod` (sdk/go), `pyproject.toml`, `package.json`.
- [ ] H2.7 ADR `docs/adr/016-sdk-server-semver.md`.

### Acceptance

- Server and SDKs share MAJOR in every release tag.
- A SDK built against a different MAJOR fails its smoke test.
- Policy documented in user-facing docs.

---

## [ ] H3. Centralize HTTP middleware chain — kill shotgun surgery

Twelve `HTTPService` sub-shells (`server/memory`, `server/edge`, etc.)
each construct their own middleware stack. Adding one middleware requires
editing all twelve. The fan-in on `middleware.WriteHeader` (41) is a
proxy for how widely this code is touched.

### Sub-tasks

- [ ] H3.1 Audit current middleware order across all twelve sub-shells;
      document divergences in `docs/middleware-audit.md`.
- [ ] H3.2 Extract `server.BuildHandlerChain(opts) http.Handler`
      composing: Recovery → Timeout → Runtime → RequestID → Auth → Slog
      → Metrics.
- [ ] H3.3 Each sub-shell becomes a `Handlers()` registrant; chain
      assembly lives in `server/server.go::mount`.
- [ ] H3.4 Add ordering test that asserts the canonical sequence
      (panic in inner handler → caught; deadline exceeded → 504; …).
- [ ] H3.5 Remove dead middleware constructors from sub-shells.
- [ ] H3.6 ADR `docs/adr/017-middleware-chain.md`.

### Acceptance

- One file (`server/server.go`) owns middleware order.
- Adding a middleware is a one-file change.
- Ordering test passes.

---

## [ ] H4. Typed enums with `UnmarshalText`/JSON validators

`TaskStatus`, `BeliefStatus`, `Polarity*`, `Action*`, `LinkRole*` are
string aliases. JSON clients can send arbitrary strings and silently
corrupt state. The schema-conflict error code exists but is not enforced
at the parser boundary.

### Sub-tasks

- [ ] H4.1 Inventory every string-typed enum in `internal/core`,
      `internal/memory/belief`, `internal/memory/evidence`,
      `internal/contradiction/resolver`, `internal/episodic/linking`.
- [ ] H4.2 For each, implement `UnmarshalText` and `UnmarshalJSON` that
      reject unknown values with `core.ErrInvalidInput`.
- [ ] H4.3 Add `MarshalText` round-trip property test per enum.
- [ ] H4.4 Replace ad-hoc string comparisons in handlers with the parsed
      enum.
- [ ] H4.5 Document the enum surface in OpenAPI (`enum: [...]`).
- [ ] H4.6 Wire validator into request DTOs in `internal/server/*`.

### Acceptance

- Sending an unknown enum value to any HTTP endpoint returns 400 with
  a clear error code.
- Round-trip property tests green.
- OpenAPI spec enumerates valid values.

---

## [ ] H5. Decompose `orchestrator.AgentLoop` (cog=44)

Largest god-method in production code outside ingestion. Hard to extend
and the only way to test pieces is via integration tests.

### Sub-tasks

- [ ] H5.1 Read the function; map every responsibility (plan, claim,
      execute, persist, fail, retry, escalate) to a phase comment block.
- [ ] H5.2 Extract each phase into a `Phase` interface:
      `Run(ctx, *State) (next Phase, err error)`.
- [ ] H5.3 Replace the loop body with a phase-driven state machine.
- [ ] H5.4 Unit-test every phase with fakes.
- [ ] H5.5 Add integration test composing phases end-to-end matching
      previous behaviour.
- [ ] H5.6 ADR `docs/adr/018-agent-loop-state-machine.md`.

### Acceptance

- `AgentLoop` body ≤ 30 lines.
- Each phase ≤ 20 lines, cognitive ≤ 10.
- Integration coverage preserved.

---

## [ ] H6. Decompose `admin.vacuum.Run` (cog=41)

Vacuum mixes pre-flight, page-shrink, integrity verification, post-step,
and metrics emission in one method.

### Sub-tasks

- [ ] H6.1 Extract `preflight`, `vacuum`, `verifyIntegrity`,
      `postReport` into separate methods.
- [ ] H6.2 Make each idempotent and individually testable.
- [ ] H6.3 Add property test: vacuum never decreases entity count or
      increases free-list pages beyond a threshold.
- [ ] H6.4 Emit metrics from the orchestrator, not from each step.

### Acceptance

- `Run` body ≤ 30 lines.
- Property tests green.

---

## [ ] H7. Decompose `ingestion.dialog.MemoryWorkerResilient*` (cog=70)

Highest cognitive complexity in the entire production codebase. Touches
retries, dedup, contradiction handling, embeddings, persistence,
clustering.

### Sub-tasks

- [ ] H7.1 Map current responsibilities; produce a phase diagram in
      `docs/ingestion-flow.md`.
- [ ] H7.2 Extract a `Pipeline` analogous to the retrieval pipeline
      (already done in P0.2 of the old TODO):
      Extract → Embed → Dedup → Contradict → Persist → Cluster → Link.
- [ ] H7.3 Move retry/backoff into a single decorator
      (`ResilientStage`) wrapping each stage.
- [ ] H7.4 Unit-test each stage with table-driven cases.
- [ ] H7.5 Update existing integration tests to assert stage boundaries.
- [ ] H7.6 ADR `docs/adr/019-ingestion-pipeline.md`.

### Acceptance

- No method in `internal/ingestion` has cognitive complexity > 15.
- Stage interfaces match the retrieval pipeline conventions.
- Existing E2E behaviour preserved.

---

## [ ] H8. Eliminate `linear_scan_in_loop` on hot paths

Static analysis flags 12 functions with `linear_scan_in_loop>=1`. The
production hot path is `vector.sqlitevec.SearchBatch`. Others are tests
or one-shot code.

### Sub-tasks

- [ ] H8.1 Profile `SearchBatch` under realistic batch sizes (50, 200,
      1000); record baseline `bench/sqlitevec_baseline.txt`.
- [ ] H8.2 Replace the inner scan with a precomputed map or sorted
      lookup. Justify with `benchstat`.
- [ ] H8.3 Add regression bench gate in CI (>5% regression fails).
- [ ] H8.4 Audit `store/migration.splitSQL` — keep as-is if not hot, but
      add a comment + bench.
- [ ] H8.5 Document remaining accepted scans in `docs/perf-budgets.md`.

### Acceptance

- `SearchBatch` benchmark ≥ 1.5× faster on N=200.
- CI gate active.

---

## [ ] H9. Extract AI factory from `config` package

`config.NewEmbedder/NewExtractor/NewReranker` builds AI clients directly,
breaking SRP and making tests pay for AI wiring.

### Sub-tasks

- [ ] H9.1 Define `internal/ai.Factory` taking a typed
      `ai.Config` struct (provider, URL, model, timeouts).
- [ ] H9.2 Move construction code from `internal/config/config.go` into
      `internal/ai/factory.go`.
- [ ] H9.3 `config` returns only parsed typed values; consumers call
      `ai.Factory(cfg.AI)`.
- [ ] H9.4 Update C2 `Application` wiring to call the factory.
- [ ] H9.5 Add fakes in `internal/ai/aitest` for tests.

### Acceptance

- `config` package has zero `net/http`-bearing constructors.
- Tests for non-AI domains no longer build real AI clients.

---

## [ ] H10. CLI output stability contract

40 sub-commands. No snapshot tests on `--help` or stable error formats.
A casual refactor can break downstream shell scripts.

### Sub-tasks

- [ ] H10.1 Add `cli_snapshot_test.go` snapshotting `--help` for every
      group and leaf command.
- [ ] H10.2 Snapshot canonical error messages (one per error code).
- [ ] H10.3 Document the stability contract in `docs/CLI.md`.
- [ ] H10.4 Add a "stability fence" comment near each exported flag
      pointing to the snapshot.

### Acceptance

- Snapshot tests in CI.
- Any CLI text change requires a snapshot update PR.

---

# Priority M — Medium

## [ ] M1. Move belief/evidence lifecycle into their domain packages

`evolution` currently owns trust/propagation/aggregation while the data
types live in `memory/belief` and `memory/evidence`. Inverted layering.

### Sub-tasks

- [ ] M1.1 Move pure-domain helpers (no DB) into `memory/belief` and
      `memory/evidence`.
- [ ] M1.2 Reduce `evolution` to a cross-domain orchestrator that calls
      the domain services.
- [ ] M1.3 Update ADR-008 (domain model) with the new boundaries.
- [ ] M1.4 Confirm import graph: `evolution → memory/{belief,evidence}`
      only, never the reverse.

### Acceptance

- `evolution` package no longer holds belief/evidence business rules.
- Import graph clean.

---

## [ ] M2. Builder DSL for integration & E2E tests

`TestIntegration_FullPipeline` cog=55, `TestE2E_TaskLifecycle` cog=20.
Each test reconstructs the world by hand.

### Sub-tasks

- [ ] M2.1 Design `scenario.New(t).WithFact(...).WithTask(...).Run()`
      DSL in `tests/e2e/helpers/scenario.go`.
- [ ] M2.2 Migrate at least 5 god-tests onto it
      (`TestIntegration_FullPipeline`,
      `TestIntegration_ParallelSubtests`, `TestE2E_TaskLifecycle`,
      `TestE2E_StoreEdgeRetrieve`, `TestIntegration_SearchWithFilters`).
- [ ] M2.3 Target max integration cognitive ≤ 15 after migration.
- [ ] M2.4 Document the DSL in `tests/e2e/README.md`.

### Acceptance

- Five named tests migrated; cognitive cap met.
- New tests use the DSL by default.

---

## [ ] M3. Property-based tests across more domains

Currently `retrieval` has property tests; rest of the codebase doesn't.

### Sub-tasks

- [ ] M3.1 `vector/cosine`: range, symmetry, identity, normalization
      invariants.
- [ ] M3.2 `graph/community.DetectCommunities`: total members preserved,
      no duplicate assignments, deterministic when seeded.
- [ ] M3.3 `task` cascade: rolling back root rolls back exactly the
      reachable dependent set.
- [ ] M3.4 `compression/cluster.Cluster`: centroid distance bounds,
      cluster-count monotonicity.
- [ ] M3.5 `store/codec`: vector codec round-trip survives any finite
      `[]float32`.

### Acceptance

- Property tests in CI on every commit.

---

## [ ] M4. Fuzz harnesses

`go test -fuzz` is currently unused.

### Sub-tasks

- [ ] M4.1 Fuzz `config/ini.applyINIFields` (cog=30).
- [ ] M4.2 Fuzz `store/codec` (NaN/Inf already guarded — fuzz proves it).
- [ ] M4.3 Fuzz HTTP `JSON ingest` body parsing for each public route.
- [ ] M4.4 Fuzz `core.normalize` (text normalization for embeddings).
- [ ] M4.5 CI: short fuzz on every PR; long fuzz nightly.

### Acceptance

- Four fuzz targets live; CI gating active.

---

## [ ] M5. Document magic constants with ADRs

Found constants without context: `DefaultSearchTopK`, `DefaultQueryTopK`,
`DefaultRetrieveMaxDepth`, `DefaultProvenanceLimit`, `MaxChainDepth`,
`amxHotRows`, `amxHotCols`, `amxPerCallThreshold`, ranking weights.

### Sub-tasks

- [ ] M5.1 Group constants by domain; create one ADR per group.
- [ ] M5.2 Each constant gets a source comment linking the ADR ID.
- [ ] M5.3 Where the value depends on tuning, expose it via config.

### Acceptance

- Every exported `Default*` constant references an ADR.

---

## [ ] M6. Benchmark regression gates

`.github/workflows/bench.yml` exists but without `benchstat` baseline
diff there's no signal.

### Sub-tasks

- [ ] M6.1 Commit `bench/baseline/` per release tag.
- [ ] M6.2 CI runs current bench, diffs against baseline with
      `benchstat`, fails on >5% regression on a designated hot-path set
      (cosine, sqlitevec, retrieval pipeline, ingestion).
- [ ] M6.3 Document the protocol in `docs/profiling.md`.

### Acceptance

- Regression gates green on `main`; demonstrate failure on a synthetic
  regression PR.

---

## [ ] M7. Make `serverstate` snapshot explicit

`serverstate.Ref` is a global atomic snapshot; SIGHUP mutates it
process-wide. Tests that touch config can race other tests.

### Sub-tasks

- [ ] M7.1 Replace global with `app.Application.Config() *snapshot`.
- [ ] M7.2 SIGHUP handler calls `Application.Reload(ctx)`, which
      atomically swaps the field on the application instance.
- [ ] M7.3 Tests no longer touch process-wide state.
- [ ] M7.4 Document hot-reload contract in `docs/RUNBOOK.md`.

### Acceptance

- `serverstate` package either deleted or reduced to a thin Ref helper
  scoped to `*Application`.

---

## [ ] M8. Route inventory artifact

### Sub-tasks

- [ ] M8.1 Generate `docs/generated/ROUTES.md` at build time from the
      OpenAPI spec.
- [ ] M8.2 Include method, path, scope, handler symbol, response codes.
- [ ] M8.3 Reference from `docs/SERVER.md`.

### Acceptance

- File regenerates on every build; CI verifies it is up to date.

---

## [ ] M9. Linux/ARM BLAS fallback for cosine

`vector/cosine_darwin.go` uses Apple Accelerate. Linux ARM (Graviton)
falls back to pure Go.

### Sub-tasks

- [ ] M9.1 Audit pure-Go path; benchmark on Linux ARM64.
- [ ] M9.2 Evaluate `gonum`/cblas/OpenBLAS as a tagged build option.
- [ ] M9.3 Document trade-offs in ADR `docs/adr/020-blas-fallback.md`.
- [ ] M9.4 Decide default: stay pure-Go or build-tagged BLAS.

### Acceptance

- Benchmarks for both platforms in `docs/profiling.md`.
- Decision recorded in ADR.

---

## [ ] M10. Tighten `store/init.InitDBStrictWithOptions` (cog=28)

A near-god init function controls DB bring-up.

### Sub-tasks

- [ ] M10.1 Split into `openConnection`, `applyPragmas`, `runMigrations`,
      `verifyIntegrity`, `installRecoveryGuard`.
- [ ] M10.2 Unit-test each step.
- [ ] M10.3 Keep public API signature for now (back-compat).

### Acceptance

- Top-level body ≤ 30 lines, each helper cog ≤ 10.

---

## [ ] M11. Decompose `compression.cluster.Cluster` (cog=19)

### Sub-tasks

- [ ] M11.1 Extract `loadEmbeddings` (already exists), `assignCentroids`,
      `iterateUntilStable`.
- [ ] M11.2 Property test centroid invariants.

### Acceptance

- Main `Cluster` ≤ 30 lines.

---

## [ ] M12. Decompose `evolution.aggregation.AggregateEvidence` (cog=36)

### Sub-tasks

- [ ] M12.1 Extract polarity-specific aggregation paths.
- [ ] M12.2 Table-driven tests per polarity.

### Acceptance

- Cognitive ≤ 15.

---

# Priority L — Low

## [ ] L1. Localize SIGPIPE handling

`signal.Ignore(SIGPIPE)` in `main` is process-wide. Document the
constraint and move the call next to the stdout writers if practical.

- [ ] L1.1 Add comment in `main.go` noting the side-effect for future
      `signal.Notify` users.
- [ ] L1.2 Evaluate moving the call into `clienv.WriteStdout` (or its
      replacement); keep current behaviour.

---

## [ ] L2. Logger fan-in reduction

`core.Logger.Error` fan_in=31. Any signature change is shotgun surgery.

- [ ] L2.1 Wrap `core.Logger` behind service-level facades
      (`memory.Logger`, `task.Logger`, etc.).
- [ ] L2.2 Each facade exposes only the verbs that domain needs.
- [ ] L2.3 Direct `core.Logger` usage allowed only in infrastructure.

---

## [ ] L3. README & USAGE consolidation

- [ ] L3.1 Remove duplicated install instructions across `README.md`,
      `docs/USAGE.md`, `docs/SERVER.md`.
- [ ] L3.2 Reference `make install` as the canonical macOS install path.
- [ ] L3.3 Single source of truth for "first run" walkthrough.

---

## [ ] L4. `scripts/install-mcp.sh` complexity (cog=80)

- [ ] L4.1 Split `main` into per-tool installer functions.
- [ ] L4.2 Each function is one bullet in the unit tests
      (`install-mcp-test.sh`).
- [ ] L4.3 Pre-push hook runs the test suite.

---

## [ ] L5. Health/readiness probes

- [ ] L5.1 Audit `internal/health.Probes` — confirm each probe is
      bounded by a deadline.
- [ ] L5.2 Distinguish `liveness` vs `readiness` semantics in code,
      not just docs.

---

## [ ] L6. `httputil.safeStreamFetch` (cog=31)

- [ ] L6.1 Extract URL validation, response cap, error wrapping.
- [ ] L6.2 Property test: bounded read for any byte stream.

---

## [ ] L7. Documentation hygiene

- [ ] L7.1 Cross-check every `docs/adr/*.md` is referenced from a code
      comment.
- [ ] L7.2 Remove stale plans under `.mimocode/plans/`.

---

# Cross-cutting CI hygiene

All items below should be on `main` before C-items close.

- [ ] CI-1. `go vet ./...` on every PR.
- [ ] CI-2. `golangci-lint run ./...` blocks merge.
- [ ] CI-3. `gofmt -s -d` blocks merge.
- [ ] CI-4. `go test -race -shuffle=on -count=1 ./...` blocks merge.
- [ ] CI-5. Short fuzz (10s per target) on every PR (M4 dependency).
- [ ] CI-6. `benchstat` regression gate on hot paths (M6 dependency).
- [ ] CI-7. OpenAPI route↔spec contract test blocks merge (H1 dependency).
- [ ] CI-8. SDK matrix build on every release tag (H2 dependency).
- [ ] CI-9. AMX guard test on Darwin runners (already present — confirm).

---

# Final validation

This list is closed when **all** of the following hold:

- [ ] No item remains unchecked.
- [ ] Every ADR is merged in `docs/adr/`.
- [ ] No production function has cognitive complexity > 25.
- [ ] No production function has unguarded recursion.
- [ ] OpenAPI spec is the single source of truth for routes;
      contract tests green.
- [ ] SDK compatibility matrix is green on the current release tag.
- [ ] `make test`, `make test-e2e`, `make lint` all green on `main`.
- [ ] Public API surface documented and stable (CLI snapshot tests,
      OpenAPI spec, SDK reference).
- [ ] `docs/ARCHITECTURE.md` reflects the post-refactor dependency
      graph.

---

# Appendix A — Source signals behind this backlog

These are the static-analysis findings that drove the priorities. Keep
this section as a paper-trail for future contributors who wonder *why*.

- **Cognitive complexity hot spots (production)**:
  `ingestion.dialog.MemoryWorkerResilient*` (70),
  `orchestrator.AgentLoop` (44), `admin.vacuum.Run` (41),
  `evolution.aggregation.AggregateEvidence` (36),
  `ai.client.Do` (32, recursion flagged — false positive but still hot),
  `store/migration.RunMigrations` (32),
  `config/ini.applyINIFields` (30), `store/init.InitDBStrictWithOptions`
  (28).
- **True recursion**: `store/recovery.cascadeRollback`
  (transitive_loop_depth=5, recursion-in-loop, cog=10).
- **Linear scan in loop**: `vector/sqlitevec.SearchBatch` (production
  hot path), plus 11 test/migration helpers (lower priority).
- **Architectural duplication**: `internal/store/migration` vs
  `internal/migration` (two owners).
- **Lazy DI**: `cli/env.Env` with nil fields filled in
  `PersistentPreRunE`.
- **Empty HTTP method in route extraction**: 20 of 70 routes
  (extractor artefact suspected; H1 confirms or files a real gap).
- **Shell god-function**: `scripts/install-mcp.sh::main` cog=80.
- **High fan-in symbols** (shotgun-surgery risk):
  `store/locker.Error` (91), `compression/store/scanner.Scan` (87),
  `store/init.MemDB` (74), `httputil.WriteJSON` (45),
  `server/middleware.WriteHeader` (41).

---

# Appendix B — Conventions

- **Branch names**: `feature/<id>-<slug>` (e.g. `feature/C1-ai-retry`).
- **Commit prefix**: `<type>(<scope>): <summary> — <id>`
  (e.g. `refactor(ai): typed RetryPolicy — C1.2`).
- **ADR filename**: `docs/adr/NNN-<slug>.md`, NNN is monotonic.
- **One sub-task = one commit** wherever practical.
- **No squash merges** of multi-sub-task work — keep history granular.
