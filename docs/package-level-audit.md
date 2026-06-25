# Package-Level Variable Audit

Audit date: 2026-06-25

All `var` declarations in `src/` were reviewed for mutable state, concurrency safety, and DI correctness.

## Safe globals (no action required)

| Package | Variable | Type | Why safe |
|---------|----------|------|----------|
| `main` | `version`, `buildDate`, `gitCommit` | `string` | Set once via `-ldflags` at build time; never mutated at runtime |
| `vector` | `dotPool`, `intPool` | `sync.Pool` | Read-only after init; pool slots are ephemeral |
| `vector` | `quantCodePool` | `sync.Pool` | Same as above |
| `ai` | `defaultBackoffs` | `[]time.Duration` | Unexported; `DefaultBackoffs()` returns a copy |
| `store` | `migrationFS` | `embed.FS` | Compile-time embedded; immutable |
| `store` | `ErrPurgeEntityNotFound`, `ErrFloatNaNOrInf` | `error` | Sentinel errors; never mutated |
| `auth` | `ErrInvalidKey`, `ErrInsufficientScope` | `error` | Sentinel errors; never mutated |
| `cli` | `noopPreRun` | `func` | Stateless closure; never mutated |
| `cli/env` | `ErrStdinRequired` | `error` | Sentinel error; never mutated |
| `contradiction` | `russianSuffixes` | `[]string` | Read-only lookup table |
| `ingestion` | `saveTmpCounter` | `atomic.Uint64` | Thread-safe atomic; monotonic counter |
| `server` | `requests.go` var block | compile-time `_` assertions | Zero-value assertions; no runtime state |
| `server/middleware_test` | `silentLogger` | `*slog.Logger` | Test-only |
| `ingestion/dialog_test` | `errVIOpInjected` | `error` | Test-only |

## Previously mutable (fixed)

| Package | Variable | Fix |
|---------|----------|-----|
| `auth` | `RequiredScopes` (exported map) | Renamed to `requiredScopes` (unexported); added `RequiredScopesMap()` getter returning a copy |

## Remaining mutable state (by design, safe)

| Package | Variable | Why safe by design |
|---------|----------|-------------------|
| `cli/env` | `Env.initDone`, `Env.initErr`, `Env.closeDone` | Lazy-init booleans; cobra runs `PersistentPreRunE`/`PersistentPostRunE` exactly once per process on the main goroutine — no concurrent callers exist. Documented in env.go lines 75–83 |

## Rules

1. **No new exported mutable `var` declarations** unless backed by `atomic`/`sync` primitives.
2. **No new package-level `var` holding `*sql.DB`, `core.VectorIndex`, or service references** — use DI via constructors.
3. **New sentinel errors** use `errors.New()` at package level (safe — immutable after init).
4. **New `sync.Pool`** instances are acceptable for hot-path allocation amortisation.
5. CI guardrail: grep-based check in `.github/workflows/ci.yml` prevents `ActiveSchema()` and `var RequiredScopes` regressions.
