# ADR-025: `app.Application` as CLI Runtime and `cli/env` Package Split

## Status

Accepted

## Context

`src/internal/cli/env/env.go` carries three distinct concerns under one package name and a single 13-field struct:

1. **I/O helpers.** `ReadStdin`, `DecodeStdin`, `DecodeStdinTyped`, `DecodeString`, `WriteJSON`, `WriteStdout`, `ErrStdinRequired`, `Fatal`. Pure-function stdlib-style helpers; no runtime state required.
2. **Runtime container.** `*clienv.Env` itself plus `EnvManager` with `Reload`, `MutateSet`, `gracefulDrain`, `safeGet`. SIGHUP-aware hot-reload coordinator that carries open handles (DB, VI, AI clients, Workers) across reloads.
3. **Build tooling.** `BuildInfo` (ldflags-injected version metadata) and `Fatal`.

ADR-012 introduced `*app.Application` as the typed DI container — every field non-nil after `New()`, no temporal coupling — but left the migration half-done. `main.go` still converts `*app.Application` to `*clienv.Env` via `applicationToEnv()` for backward compatibility, and 52 reference-sites throughout `src/internal/cli/` and `src/internal/server/` continue to operate on the legacy struct. ADR-009's `cli/wiring.go` `wireAll(env *clienv.Env, refs *serverstate.Ref)` performs the side-effect assignment `env.Retriever = retSvc` — a hazard first named in ADR-012, which establishes `Retriever` as a top-level field on `*core.Application`. ADR-025 closes this hazard by construction because `wireAll` graduates to taking `*core.Application` directly and reads `a.Retriever` rather than mutating a received struct.

Concurrently, the codebase carries TWO non-overlapping reload mechanisms:

- `*serverstate.Ref` (atomic.Pointer[ServerState]) — already extracted in `src/internal/serverstate/`. Reloads configuration state behind SIGHUP; handlers see a consistent snapshot.
- `EnvManager` (atomic.Pointer[handleBundle]) — still inside `clienv`. Reloads open handles (DB, VI, AI clients, Worker) across SIGHUP.

Both happen on the same SIGHUP signal but for different reasons: `serverstate.Ref` swaps the configuration the handlers read; `EnvManager` carries forward the heavyweight runtime resources so a reload doesn't tear down the entire dependency graph. They are orthogonal and both are needed. They live in different packages today (`serverstate` vs `cli/env`) — but `cli/env` is named for the CLI consumer while the runtime is consumed by both CLI and HTTP server, which has muddied the boundary.

Sonnet code-review correctly flagged `applicationToEnv` as a transitional adapter (TODO.md A1) but under-counted the surrounding scope: the legacy struct is also imported by `src/internal/server/middleware.go` and `src/internal/server/middleware_test.go` for `RuntimeMiddleware` SIGHUP handling. That means A1 is not just a CLI rename — it is a cross-cutting refactor across CLI + HTTP-server + tests.

## Decision

### Three-package split of `cli/env`

| New package | Replaces | Houses |
|---|---|---|
| `src/internal/cli/io/` | `clienv.ReadStdin`, `clienv.DecodeStdin`, `clienv.DecodeStdinTyped`, `clienv.DecodeString`, `clienv.WriteJSON`, `clienv.WriteStdout`, `clienv.ErrStdinRequired` | Pure I/O helpers; no struct dependency; no App. |
| `src/internal/runtime/` | `clienv.EnvManager`, `NewEnvManager` (`Get`, `Set`), `Reload`, `MutateSet`, `gracefulDrain`, `safeGet` | SIGHUP-aware hot-reload coordinator for open handles; consumer is both CLI (via Application) and HTTP server (via RuntimeMiddleware). |
| `src/internal/app/` (existing) | `clienv.BuildInfo`, `clienv.Fatal`, plus the existing `app.Application`, `app.New`, `app.Start`, `app.Stop` | The typed DI container + ldflags-injected build metadata + main.go's startup-failure helper. |

The package `cli/env` is deleted entirely at the end of the migration. Until that point, `clienv` re-exports the symbols from the new packages under `// Deprecated:` markers so existing code keeps compiling while consumers migrate.

### `app.BuildInfo` is canonical; `clienv.BuildInfo` deprecated

`app.BuildInfo{Version, BuildDate, GitCommit}` is structurally identical to today's `clienv.BuildInfo`. ADR-025 deprecates `clienv.BuildInfo` once the union is in place and forces all consumers (including `main.go` `noDBEnv`, the tests in `server/middleware_test.go` and `cli/cli_integration_test.go`, and the field reference in `src/main.go applicationToEnv`) onto `app.BuildInfo`. PR3 adds a `// Deprecated:` marker to the shim; any consumer still importing `clienv.BuildInfo` after PR3 is flagged as a missed migration in PR5's grep-zero. PR6 hard-removes the shim together with `clienv.Env`. The `applicationToEnv` adapter falls away inside PR2 already because `*app.Application.Build` is the source of truth.

### `Fatal` helper moves to `app.Fatal`

`clienv.Fatal` is today used only by `src/main.go` for fail-fast startup errors. Two candidate homes: `app.Fatal` (so `main.go` can `app.Fatal("config: %v", err)` after `cfg := config.LoadConfigFromSources(...)`) or inline `main.go` (`fmt.Fprintln(os.Stderr, ...); os.Exit(1)`). ADR-025 chooses `app.Fatal` because (a) it preserves the centralised exit-code-on-startup-failure contract, (b) any future `app.NewForTest(t)` would benefit from the same shape, and (c) the `// Deprecated:` re-export bridge keeps the change reversible per PR.

### `wireAll` graduates onto `*app.Application`

```go
// src/internal/cli/wiring.go
func wireAll(a *core.Application, refs *serverstate.Ref) *server.Server {
    memSvc     := memdomain.New(a.DB, a.VI, a.Embedder)
    edgeSvc    := edgedomain.New(a.DB, a.VI, a.Embedder)
    timelineSvc := timelinedomain.New(a.DB)
    healthSvc := healthdomain.New(
        healthdomain.DBProbe(a.DB),
        healthdomain.VectorIndexProbe(a.VI, a.Cfg.VectorDim),
        healthdomain.EmbedderProbe(a.Embedder),
        healthdomain.ExtractorProbe(a.Extractor),
        healthdomain.RerankerProbe(a.Reranker),
        healthdomain.DiskSpaceProbe(a.Cfg.DBPath),
    ).WithMetrics(a.Metrics)
    reembedSvc := reembeddomain.New(a.DB, a.VI, a.Embedder)
    retSvc     := retdomain.New(a.DB, a.VI, a.Embedder)
    // No env.Retriever = retSvc side-effect: Retriever is already on
    // *core.Application as a top-level field. ADR-012's side-effect-
    // assignment hazard is removed by construction.
    cndSvc     := contradictdomain.New(a.DB)
    taskSvc    := taskdomain.New(a.DB, a.Embedder, a.VI)
    graphSvc   := graphdomain.New(a.DB)
    migrSvc    := migrationdomain.New(a.DB)
    ingestSvc  := ingestdomain.New(a.DB, a.VI, a.Embedder, a.Extractor)
    retentionSvc := retentiondomain.New(a.DB, a.VI)

    return server.NewServerFromDeps(server.ServerDeps{
        Refs:          refs,
        Retrieval:     ret.New(retSvc, a.Metrics, refs),
        Task:          tasksvc.New(taskSvc, a.Metrics, refs),
        Memory:        mem.New(memSvc, a.Metrics, refs, a.Cfg.DedupThreshold),
        Edge:          edge.New(edgeSvc, a.Metrics, refs),
        Timeline:      timeline.New(timelineSvc, a.Metrics),
        Ingest:        ingsrv.New(ingestSvc, a.Metrics, refs, a.Cfg.DedupThreshold),
        Contradiction: cnd.New(cndSvc, a.Metrics),
        Graph:         graphsrv.New(graphSvc, a.Metrics, refs, a.Cfg.VectorDim),
        Migration:     migrsrv.New(migrSvc, a.Metrics, refs),
        Retention:     retention.New(retentionSvc, a.Metrics, refs, a.Cfg.Retention),
        Reembed:       reembed.New(reembedSvc, a.Metrics),
        Health:        healthsrv.New(healthSvc),
        Metrics:       a.Metrics,
    })
}
```

The `*core.Application` argument is the single source for DI; `wireAll` no longer mutates received state. ADR-012's stated hazard ("side-effect assignment removed — `Retriever` is a field, not assigned in `wireAll`") becomes enforceable.

`serverstate.Ref` keeps its place: `wireAll` still threads `refs` to handlers that need atomic config snapshot reads under SIGHUP. `runtime.EnvManager` is **not** a parameter to `wireAll` — `wireAll` consumes a freshly-constructed `*core.Application` whose dependencies were assembled by `app.New` and (when applicable) carried forward by `runtime.EnvManager.Reload` for hot-reload-overlapping-with-server-startup flows. ADR-025 fixes the threshold: `wireAll` runs at server-startup once; reloads flow through `runtime.EnvManager` separately.

### `NewRootCommand` operates on `*core.Application`

```go
// src/internal/cli/root.go
func NewRootCommand(app *core.Application, refs *serverstate.Ref) *cobra.Command {
    // PersistentPreRunE retrieves *core.Application via app.DB / app.Cfg etc.
    // ensureDB equiv is no longer needed — *core.Application was eagerly
    // constructed before main.go reached NewRootCommand. The DB, VI,
    // embedder etc. are non-nil on a DB path; nil on the noDBLeaves fast
    // path (constructed in main.go via app.NoDBApplication).
    // ...
}
```

The leaf-construction pattern `admin.NewCmd(env) / memory.NewCmd(env) / task.NewCmd(env) / ...` becomes `admin.NewCmd(app) / memory.NewCmd(app) / ...`. Each sub-package is migrated by re-writing its `NewCmd` signature.

`noopPreRun` annotation logic (`prefix` `skip_db_<leaf>`) is preserved: cobra-walk semantics are unchanged. The set of no-DB leaves moves from `main.go`'s `noDBLeaves` map to `app.NoDBLeaves`. The map lives next to `app.NewNoDB` because the constructor and its declared no-DB leaf set must move together when a new no-DB leaf is added — otherwise the two-step update across `main.go` and `app/` would skew easily and the prior `release.yml` Build-matrix breakage would silently return. The fast-path in `main.go` constructs a minimal `*core.Application` via `app.NewNoDB(ctx, cfg, build)` which (a) still validates `cfg`, (b) populates `Metrics` + `Build`, and (c) leaves `DB`/`VI`/`Embedder`/etc. nil for fail-fast detection at command execution time.

### Server middleware shifts to `*runtime.EnvManager`

`src/internal/server/middleware.go` today imports `cli/env` for `*clienv.EnvManager`. ADR-025 ends that import: the middleware switches to `*runtime.EnvManager`. The shape is the same (`Get()` returns an atomic snapshot; the snapshot is the `Env` value the handler reads from closed-over capture); only the package and the simulated `Env` type change.

A README example call like `app := serverstate.NewEnvManager(...)` becomes `mgr := runtime.NewEnvManager(...)` and the middleware tests in `src/internal/server/middleware_test.go` migrate from constructing `&clienv.Env{...}` literals to calling `app.New(ctx, testConfig, testBuild)` followed by `runtime.NewEnvManager(app)`.

### No `applicationToEnv` adapter

ADR-025 deletes `applicationToEnv` in PR2 (when `main.go` graduates onto `*core.Application` directly) and the `*clienv.Env` type itself in PR6. ADR-012's "Migration path: ... `Env` is deleted after all consumers migrate" closes concretely here, with the deletion front-loaded to PR2 so subsequent PRs work against a clean source.



ADR-025 deletes `applicationToEnv` in PR2 (when `main.go` graduates onto `*core.Application` directly) and the `*clienv.Env` type itself in PR6. ADR-012's "Migration path: ... `Env` is deleted after all consumers migrate" closes concretely here, with the deletion front-loaded to PR2 so subsequent PRs work against a clean source.

### Reload failure-mode contract

`runtime.EnvManager.Reload(cfg)` follows ADR-012's two-phase-commit pattern (as established for `serverstate.Ref`):

1. `cfg.Validate()` runs on the new config first. On failure, `Reload` returns an error and the existing handle bundle stays live — readers (`mgr.Get()`) continue to see the prior bundle.
2. `app.New(ctx, newCfg, prev.Build)` runs against a fresh `context.WithCancel(context.Background())`. On failure (DB-init error, embedding-model unreachable, etc.), the new context is cancelled and discarded; readers continue to see the prior bundle.
3. Only on full success does `m.current.Store(newEnv)` fire. At that moment, the prior bundle enters `gracefulDrain` (the 5-second reprieve on the existing `clienv.EnvManager` is preserved verbatim in `runtime`).

This contract means readers' `mgr.Get()` returns either the prior bundle or the freshly-built one — never a half-built state. The two SIGHUP coordinators (`runtime.EnvManager` for handles, `serverstate.Ref` for config) operate orthogonally: each reloads its concern atomically, and a coordinator's failure does not propagate to the other.

## Alternatives Considered

1. **Single-package rename** (`clienv → cli/clideps`). Rejected: keeps the same god-package shape; the three concerns stay coupled; A1's "transitional adapter" stays alive.
2. **Keep `clienv.*` and add `runtime/` in parallel.** Rejected: dual imports (`clienv.Env` vs `runtime.Env`) would force callers to choose and double the test surface; we want one canonical runtime type per concern, not two.
3. **Move `EnvManager` into `serverstate`.** Considered: it would unify the two atomically-swapped values behind one `serverstate.Ref`. Rejected: `serverstate.Ref` carries **configuration state** (categories, relations, FSM rules); `EnvManager` carries **open handles** (DB, VI, AI clients, Workers). Forcing them into one struct re-creates the `*clienv.Env` god-typed 13-field struct ADR-012 was meant to retire.
4. **Move `EnvManager` into `app`.** Considered: `App.Application` already includes DB / VI / etc.; co-locating the reload coordinator would simplify the import. Rejected: `app.New` is a synchronous constructor. The reload coordinator is a long-running atomically-swapped holder; mixing the two re-imposes temporal coupling on Application.
5. **Wire `*core.Application` into `*cobra.Command` via `cmd.SetContext`.** Rejected: this folds back into the implicit-DI pattern ADR-012 identified as a hazard. ADR-012 establishes the principle "no globals, no nil fields, every dependency injected by constructor"; using `cmd.SetContext` to thread `app.Application` through command closures re-creates a global lookup, just keyed off `*cobra.Command.Context` rather than a package-level variable. Explicit parameter on `NewRootCommand` is the right choice and is consistent with `wireAll`'s explicit-parameter posture in PR2.
6. **Promote `cli.BuildInfo` (or `app.BuildInfo`) into a separate `meta` package.** Rejected: BuildInfo is 3 fields; over-extracting is more friction than it saves.
7. **Compose-package style via small interfaces** (`type EnvIO interface { ReadStdin ... }`). Rejected: the I/O helpers are pure functions; interface satisfaction buys nothing here.

## Consequences

- **`*clienv.Env` is fully retired.** No re-export shim after the final PR. `git grep '*clienv\.Env'` returns zero non-test results.
- **`*app.Application` is the single DI consumer type.** Every CLI command factory signature, every server side-effect wiring, every `wireAll` consumer reads from `a.DB`, `a.VI`, `a.Embedder`, etc.
- **Three orthogonal concerns live in three packages.** I/O helpers (`cli/io`), hot-reload coordinator (`runtime`), DI container + build metadata (`app`). Each package is small and importable in isolation.
- **SIGHUP coordinator boundary is explicit.** `runtime.EnvManager` is the type both `cli.NewRootCommand` (via `app.Application`'s construction flow) and `server.RuntimeMiddleware` consume. They never need to share a struct; they share a contract.
- **wireAll has no side-effect assignment.** `env.Retriever = retSvc` is gone; `a.Retriever` is set by `app.New` and never mutated.
- **`applicationToEnv()` is deleted.** `main.go` becomes a straight-line construction-and-execute with no transitional adapter.

## Migration

Six PRs isolate blast radius. Each is independently revertable.

### PR1 — `cli/env` package split (no behavior change)

- Create `src/internal/cli/io/io.go` (package `io`) containing `ReadStdin`, `DecodeStdin`, `DecodeStdinTyped`, `DecodeString`, `WriteJSON`, `WriteStdout`, `ErrStdinRequired`.
- Create `src/internal/runtime/manager.go` (package `runtime`) containing `EnvManager`, `NewEnvManager`, `Get`, `Set`, `Reload`, `MutateSet`, `gracefulDrain`, `safeGet`.
- Move `BuildInfo` from `clienv` to `app`. Move `Fatal` to `app.Fatal`.
- In `src/internal/cli/env/env.go`, re-export everything from the new packages under `// Deprecated:` comments. `*clienv.Env` keeps shape.
- Tests: existing tests pass unchanged. New unit tests for `cli/io` (I/O round-trips) and `runtime` (atomic reload snapshot consistency).
- Acceptance: `go test ./src/internal/cli/io/... ./src/internal/runtime/... ./src/internal/app/...` green; existing `clienv` re-exports compile and pass.

### PR2 — `cli/wiring.go` + `cli/serve.go` + `cli/mcp.go` (top-level CLI handlers)

- `wireAll` signature moves from `(*clienv.Env, *serverstate.Ref)` to `(*core.Application, *serverstate.Ref)`. Side-effect assignment removed.
- `cli/serve.go` constructors take `*core.Application`.
- `cli/mcp.go` constructor takes `*core.Application`.
- `main.go applicationToEnv` is removed; the fast-path line becomes `app := app.NewNoDB(ctx, cfg, build)` and the loaded-DB path continues to call `app.New(ctx, cfg, build)` directly.
- Test/adapter: `cli/cli_integration_test.go` rebuilds via `app.New(...)` and tests against `admin.NewCmd(app)`, etc.
- `applicationToEnv` adapter is removed from `src/main.go` — main.go now builds `app := app.NewNoDB(ctx, cfg, build)` for the fast path and `app := app.New(ctx, cfg, build)` for the loaded-DB path, with no transitional struct conversion.
- Acceptance: `go test ./src/internal/cli/...` green; `go test ./src/internal/server/...` green (server middleware still imports `clienv` until PR5 — this PR keeps `clienv` re-exports alive).

### PR3 — `admin/`, `config/` sub-packages

- `cli/admin/keys.go` (5 reference-sites) and `cli/admin/admin.go` migrate to `(app *core.Application)`.
- `cli/config.go` (3 reference-sites) migrates.
- All `clienv.BuildInfo`/`clienv.Fatal` consumers in these packages switch to `app.BuildInfo` / `app.Fatal`.
- Acceptance: `go test ./src/internal/cli/admin/... ./src/internal/cli/...` green; `git grep` zero remaining `*clienv\.Env` in those packages.

### PR4 — Read-side sub-packages: `memory/`, `task/`, `graph/`, `time/`, `agent/`, `db/`

- Each `NewCmd(app *core.Application) *cobra.Command` and the internal command-execution closures read from `app.DB`, `app.VI`, `app.Cfg`, etc.
- Each branch lands as a sub-package-internal PR (`cli/memory/...` is one PR; `cli/task/...` is a separate PR; etc.). Independent revert. Can be merged as six PRs or batched into one umbrella PR.
- Acceptance: per-package `go test ...` green; the umbrella acceptance is `go test ./src/internal/cli/... ./src/internal/server/...`.

### PR5 — `cli/diagnose/`, `cli/profile/`, `cli/adminops/`, `*_test.go`, server `middleware.go`

- `cli/diagnose/diagnose.go` migrates.
- `cli/profile/profile.go` migrates.
- `cli/adminops/adminops.go` migrates (register-time).
- `cli/cli_integration_test.go` and `server/middleware_test.go` updated to construct via `app.New(...)`.
- `server/middleware.go` switches `*clienv.EnvManager` → `*runtime.EnvManager`. `src/internal/server/` no longer imports `cli/env`.
- Acceptance: `go test ./...` green; `go vet ./src/internal/server/...` no longer complains about `cli/env` import; `git grep -E 'cli/env|clienv\.' src/internal/server/` returns zero (direct AND transitive; consistency with PR6 acceptance).

### PR6 — Cleanup

- Delete `src/internal/cli/env/env.go` entirely. `*clienv.Env` type disappears (its companion `applicationToEnv` adapter was already removed at PR2 when `main.go` graduated to constructing `*core.Application` directly).
- Remove `applicationToEnv` from `src/main.go` is implicit for PR6 — the shim is already gone by this point, but a literal `git grep "applicationToEnv" src/` should return zero as a final sanity check.
- Update CHANGELOG `[Unreleased]` with a single feat entry covering the consolidated migration.
- `TODO.md` A1 marked closed.
- Acceptance: `git grep -E 'clienv\.|applicationToEnv' src/` returns zero (direct AND transitive); `go test ./...` green; `golangci-lint run ./...` clean; `make pre-push` green; OpenAPI snapshot unchanged; benchmark numbers within ±5% of baseline.

## Out of scope

- **HTTP `server.Serve` loop internals** beyond `RuntimeMiddleware`'s switch to `*runtime.EnvManager`. Other middleware (auth, rate-limit) does not touch `clienv` and stays unchanged.
- **MCP `serve.go` extension beyond the `*core.Application` signature.** MCP's internal request handlers don't touch `clienv`.
- **`serverstate.Ref` restructuring.** `serverstate` already follows the atomic.Pointer pattern ADR-012 endorsed; ADR-025 does not propose changes to it.
- **CLI command shape reorganisation.** Sub-package migration touches command signatures, not command surface; today `./hermem memory --help`, `./hermem task --help`, etc. behave identically post-PR.
- **Adding a `cmd.SetContext(application)` accessor.** Discussed in ADR-012 and rejected as folding back into the global pattern.
- **BuildInfo being split across `app.BuildInfo` (binary version) and CLI-specific build flags.** The 3-field tuple is too small for sub-structuring; ADR-025 keeps it whole.

## References

- `src/internal/app/app.go` — the existing `Application` struct, `BuildInfo`, `New`, `Start`, `Stop`, `stopOnce` lifecycle.
- `src/internal/cli/env/env.go` — the legacy code being split; `*clienv.Env`, `EnvManager`, `Reload`, `MutateSet`, `gracefulDrain`, `safeGet`, `BuildInfo`, `Fatal`, I/O helpers.
- `src/internal/cli/root.go` — `NewRootCommand`, `noopPreRun`, `PersistentPreRunE` / `PersistentPostRunE` lifecycle, 19 subcommand registrations.
- `src/internal/cli/wiring.go` — the `wireAll` function with the side-effect assignment that ADR-012 was meant to retire.
- `src/main.go` — `applicationToEnv`, `noDBLeaves`, `noDBEnv`, `firstLeafArg`.
- `src/internal/serverstate/` — the existing `*serverstate.Ref` config-snapshot holder (pre-cursor, orthogonally retained).
- `src/internal/server/middleware.go`, `middleware_test.go` — the HTTP server `RuntimeMiddleware` consumer.
- `docs/adr/009-dependency-injection.md` — pre-cursor `wireAll`-centric DI, now superseded by `app.New`.
- `docs/adr/012-application-container.md` — establishes `app.Application` AND the side-effect-assignment hazard (`env.Retriever = retSvc` in `wireAll`) that ADR-025 closes. ADR-012 commits to stepwise migration "C2.B/C2.C/D migrate consumers from `Env` to `Application` incrementally; `Env` is deleted after all consumers migrate" — this ADR is the closing milestone for that commitment. It also establishes `Start`/`Stop` lifecycle and ordered shutdown, which `runtime.EnvManager.Reload` mirrors.
- `docs/adr/024-configuration-subconfigs.md` — sibling ADR for the configuration surface.
- `docs/adr/026-typed-entity-models.md` — sibling ADR for typed Entity bands.
- `TODO.md` A1 — "Eliminate `applicationToEnv` adapter." Closed by this ADR.
