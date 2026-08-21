# ADR-024: Configuration Sub-Configs

## Status

Accepted

## Context

`src/internal/config/config.go` exposes a single 35-field `Config` struct that is loaded by one `LoadConfig()` and validated by one `Config.Validate()` method. Three problems accumulate as the project grows:

1. **Mis-shaped struct.** The flat struct holds logically-disjoint subsystems (embedder, extractor, reranker, retrieval, retention, ranking, server, database, schema) under one namespace. Every call site reads `cfg.Provider` or `cfg.VectorBackend` directly, so unrelated changes (a new embedder field, a new DB field) collide in code review and force widespread edits when the field is repurposed.
2. **Mis-validation.** `Config.Validate()` checks invariants for all subsystems in one function. New validation rules require editing a function that has no natural seam, and per-subsystem test coverage is hard to scope because there is no per-subsystem unit.
3. **Mis-alignment with hermem.ini.** `hermem.ini` is *already* section-grouped: `[embedder]`, `[database]`, `[server]`, `[vector]`, `[extraction]`, `[ingestion]`, `[retrieval]`, `[retention]`, `[ranking]`, `[reranker]`, `[schema]`. The flat `Config` struct hides this grouping from Go callers, so a reader of `cfg.Ranking.VectorWeight` cannot see from the code path which ini section supplied the value without consulting `ini.go`.

The project has already partially extracted three of the sub-configs into `core.*` types (`core.RetentionPolicy`, `core.RankingWeight`, `core.SchemaConfig`). These compound values sit as named fields on the flat struct today. The remaining fields are still flat primitives.

`TODO.md` A5 captures the removal of this surface area as an open arch goal ("Split into focused sub-configs with dedicated validation; `Config` composes them"). Sonnet code review independently flagged it as the largest architectural debt outside hermem.ini parsing.

## Decision

Replace the flat `Config` with a composition of six sub-configs. Each owns its fields, its ini-section binding, and its validation. `Config` becomes a facade.

### Sub-configs

| Sub-config | File | INI section | Fields (current → future) |
|---|---|---|---|
| `EmbedderConfig` | `config/embedder.go` | `[embedder]` + `[embedding]` | `Provider`, `URL`, `Key`, `Model`, `EmbedderTimeout`, `ModelPath` |
| `ExtractorConfig` | `config/extractor.go` | `[extraction]` | `ExtractProvider`, `ExtractURL`, `ExtractKey`, `ExtractModel`, `ExtractTemperature`, `ExtractTimeout`, `ExtraCategories`, `ExtraRelationTypes` |
| `RerankerConfig` | `config/reranker.go` | `[reranker]` | `RerankerProvider`, `RerankerURL`, `RerankerKey`, `RerankerModel`, `RerankerTimeout` |
| `RetrievalConfig` | `config/retrieval.go` | `[retrieval]` + `[ingestion]` + `[retention]` + `[ranking]` | `DedupThreshold`, `MaxDepthCeiling`, `MaxRetrievedNodes`, `TokenBudget`, `Retention core.RetentionPolicy`, `Ranking core.RankingWeight` |
| `DatabaseConfig` | `config/database.go` | `[database]` + `[vector]` | `DBPath`, `VectorBackend`, `VectorDim`, `AutoMigrate`, `Schema core.SchemaConfig` |
| `ServerConfig` | `config/server.go` | `[server]` | `APIKey`, `APIKeys []auth.Key`, `RateLimitEnabled`, `RateLimitRPS`, `RateLimitBurst`, `RateLimitKeyBy`, nested `AuthConfig` |

`AuthConfig` is nested in `ServerConfig`, not promoted to a top-level sub-config. Auth is one concern today (an API key plus a list of `[scoped]` keys). The ADR commits to a promotion rule: when `AuthConfig` acquires two or more independent top-level concerns — for example `Scopes`, per-key rate-limit overrides, or a TLS material — promote `AuthConfig` to a top-level sub-config alongside `DatabaseConfig`. Until that threshold, nesting keeps the related auth concern co-located with the server middleware that consumes it.

`Config` keeps exactly six fields, one per sub-config:

```go
type Config struct {
    Embedder EmbedderConfig
    Extractor ExtractorConfig
    Reranker RerankerConfig
    Retrieval RetrievalConfig
    Database DatabaseConfig
    Server ServerConfig
}
```

### Constructor migration

The existing `Config.NewEmbedder() / NewExtractor() / NewReranker()` methods move to sub-config types:

```go
func (e EmbedderConfig) NewEmbedder() core.Embedder
func (x ExtractorConfig) NewExtractor() core.LLMExtractor
func (r RerankerConfig) NewReranker() core.Reranker
```

`ai.Factory.NewEmbedder` already accepts an `ai.Config` argument; the relocation is a straight field-selector shift from `cfg.Provider` to `cfg.Embedder.Provider`. No behavior change in `ai.Factory` itself.

### Validation cascade

`Config.Validate()` becomes a fan-out:

```go
func (c Config) Validate() error {
    if err := c.Embedder.Validate(); err != nil {
        return fmt.Errorf("embedder: %w", err)
    }
    if err := c.Extractor.Validate(); err != nil {
        return fmt.Errorf("extractor: %w", err)
    }
    // ... fan-out for each sub ...
    return nil
}
```

Failure mode is fail-fast on the first sub-config error. Cumulative-errors aggregation is rejected (see Alternatives). The error wrapping preserves the sub-config name so the operator can find the offender in hermem.ini.

### Timeout policy: per-subsystem, no global default

Today: `EmbedderTimeout`, `ExtractTimeout`, `RerankerTimeout` are three independent fields with three independent ini keys (`[embedder] timeout`, `[extraction] timeout`, `[reranker] timeout`) and three independent defaults (30s / 300s / 30s).

The decision **does not introduce a global default.** A global `AIRequestTimeout` ancestor field was considered and rejected: it would force operators who set per-subsystem timeouts today to either unset three keys to inherit the global or set four keys. Per-subsystem stays explicit and debuggable; a global field would be a magic shortcut that hides odd per-subsystem values. Defaults from `config/ini.go` stay per-subsystem and migrate as-is to the new sub-config inner-defaults.

### INI format

`hermem.ini` is **byte-compatible** before and after this refactor. The INI file's section groupings already correspond 1:1 to the new sub-config decomposition, so ini-loader maps the same keys to different struct fields. No operator action is required for an upgrade.

## Alternatives Considered

1. **Sub-packages** (`src/internal/config/embedder/` etc.). Rejected: each sub-package rename forces import-path renames across every consumer (`ai`, `clienv`, `serve`, `wiring`, `runtime`). Cost is not validated by the gain; a single package with six well-named types gives equal discoverability through IDE tooling without the rename churn.
2. **Tagged-union style** (`type Config interface { isConfig(); subtype() string }`). Rejected: ini-loading is not polymorphic, and consumers that read *configuration* (not *objects constructed from configuration*) need value semantics rather than interface dispatch. Interfaces here would only be a tax.
3. **Cumulative error aggregation** (`[]error` returned by `Validate` and printed together). Considered: a single bad hermem.ini rarely trips more than one error today, and the operator diagnostic is already precise thanks to the `fmt.Errorf("foo: %w", err)` wrapping. Aggregation adds a slice, a join, and a new failure format (`- slash-separated bullet list of errors`) for negligible gain.
4. **Detached config struct hierarchy with interfaces** (each sub-config `interface { Validate(); Resolve(); }`). Rejected: ini-resolved configs are static data, not behaviour. Interfaces would only exist to satisfy test mocks, which the project handles today with `clienv.Fatal` and explicit `*config.Config` test helpers. No mocking benefit.
5. **Promote `AuthConfig` to top-level** in this ADR. Rejected: it adds a seventh sub-config for a concern that today has one scalar plus a list. The promotion rule above is the right threshold, and it defers the seventh sub-config until the codebase needs it, not until this ADR lands.

## Consequences

- **INI format unchanged.** Operators do not edit `hermem.ini` for this refactor.
- **Internal callers migrate.** Anything reading `cfg.VectorBackend` migrates to `cfg.Database.VectorBackend`, etc. A planned four-PR gradient exhausts every flat-to-nested access. Subsequent PRs are recognised by grep as "any new direct access to `Config.<flat-field>` returns zero results after the fourth migration PR has landed".
- **Validation grows per-subsystem test coverage.** Each `EmbedderConfig.Validate()`, `ExtractorConfig.Validate()`, etc. gets a dedicated `_test.go` file. New validation rules gain a natural seam.
- **Auth concerned parties stay co-located.** `ServerConfig.AuthConfig` keeps the auth.* package call sites in a single struct definition. The promotion rule moves Auth to top-level when it acquires its second top-level concern.
- **Composition remains value semantics.** `Config` is a struct of structs; `*Config` is the consumer's pointer. No interface, no builder, no DI magic. The size of `Config` (sum of sub-configs) is ~700 bytes, still safe to pass by pointer at every call site.
- **Ancillary refactor scope makes this ADR a 4-PR migration.** Listed in the next section.

## Migration

Four PRs isolate the blast radius so the refactor is reviewable and revertable per step. Each PR's acceptance criteria are concrete; failing a PR criterion rolls back exactly that PR without affecting the next.

### PR1 — Sub-struct types only (zero behavior change)

- Introduce `EmbedderConfig`, `ExtractorConfig`, `RerankerConfig`, `RetrievalConfig`, `DatabaseConfig`, `ServerConfig` types in new files under `src/internal/config/`.
- Each gets a `Validate()` method stub that delegates to the existing flat `Config.Validate()` for the fields it owns.
- `Config` keeps its current flat fields.
- Tests: one round-trip test per sub-config (`EmbedderConfig{...}.Validate() == nil` for valid input, errors on bad shapes).
- Acceptance: `go test ./src/internal/config/...` is green; behavior is unchanged.

### PR2 — Transfer fields + relocate constructors

- Move each flat field off `Config` into its sub-config. `Config` now exposes only the six sub-config fields.
- `NewEmbedder()` / `NewExtractor()` / `NewReranker()` move from `*Config` to sub-config types. `ai.Factory` is updated to source from sub-config fields.
- `Config.Validate()` becomes the fan-out from Decision above.
- `defaultConfig()` is split into per-sub-config defaults.
- Tests: existing tests adjusted to access through sub-config; equivalent coverage is maintained.
- Acceptance: `go test ./src/...` green; `make pre-push` green; `git grep 'cfg\.(Provider|VectorBackend|DBPath|APIKey|AutoMigrate|RateLimitEnabled|ExtractProvider|RerankerProvider)\b' src/` returns **zero non-test references**.

### PR3 — Consumer-site rename

- A scripted global rename of every direct sub-config field access (e.g. `cfg.VectorBackend` → `cfg.Database.VectorBackend`).
- `git grep` per PR review checklist confirms zero remaining flat-field access.
- Acceptance: `make pre-push` green; CI green; benchmarks show no regression; OpenAPI byte-snapshot test (`api/openapi_test.go`) unchanged.

### PR4 — Cleanup

- Remove the deprecated-direct-field-access search; CHANGELOG `[Unreleased]` entry; `TODO.md` A5 checked off.
- Optional: deprecation aliases. The decision above does not introduce them, so PR4 is the final cleanup rather than a deprecation phase.

## Out of scope

- HTTP wire contract (`OpenAPI`, server JSON shapes). Unchanged. Future HTTP-bound typed-JSON belongs in ADR-026.
- Hermes Agent plugin (`plugins/memory/hermem/plugin.yaml`) and SDK packages. Unaffected; they read ini through the loader, not the Go struct.
- INI section groupings. Already aligned with sub-config decomposition. No file format change.
- CLI command surface (`cobra.Command` consumers of `*config.Config`). Unaffected — they read `cfg.<field>` paths, which change shape, but the entries themselves do not change.
- `Config`-as-interface or `Config`-as-builder. Rejected above; not deferred, not followed up.

## References

- `docs/adr/009-dependency-injection.md` — pre-cursor DI discussion; ADR-024 builds on its preference for explicit constructor wiring.
- `docs/adr/012-application-container.md` — defines `app.Application` which receives a `*config.Config`; the sub-config shape moves through unchanged.
- `TODO.md` A5 — "Reduce config.Config surface area." Closed by this ADR.
- `src/internal/core/types.go` (`SchemaConfig`, `RankingWeight`, `RetentionPolicy`) — the three already-extracted compound sub-types referenced by `DatabaseConfig.Schema`, `RetrievalConfig.Ranking`, `RetrievalConfig.Retention`.
- `src/internal/config/ini.go` — the loader this ADR aligns; its `defaultConfig()` is split per sub-config in PR2.
