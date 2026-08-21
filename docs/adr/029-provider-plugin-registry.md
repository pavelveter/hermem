# ADR-029: Provider and Plugin Registry

## Status

Proposed (scale trigger: community contributions, third-party providers)

## Context

There is **no inbound plugin system** today. Every extension point is a
compiled-in `switch`:

- `ai.Factory.NewEmbedder/NewExtractor/NewReranker` switch on provider
  name: `ollama`, `openai`, `local` (3 hardcoded arms, duplicated in 3
  methods).
- `vector.NewIndex` switches on `"in-memory"` / `"sqlite-vec"`.
- Contradiction detectors (`lexical`, `composite`), ranking strategies
  (ADR-003), renderers, retention policy — all compiled in.
- `plugins/memory/hermem/` is a **Python outbound adapter** (hermem as a
  memory backend for an external agent framework) — not an extension
  mechanism for hermem itself.

Provider config multiplies by field: `Config` carries
`ExtractProvider/URL/Key/Model/Temperature/Timeout` × reranker ×
embedder + shared fallbacks — a flat ~40-field struct where every new
provider adds ~6 fields and edit conflicts in `config/ini.go`.

## Why the current design breaks

1. **Contribution wall at 100k stars.** Every "add Anthropic / Voyage /
   Jina / Bedrock / Gemini / Azure / Cohere" PR touches the same
   `factory.go` switches, `ai.Config`, `config.Config`, and INI parsing —
   guaranteed merge-conflict magnet and maintainer bottleneck.
2. **No third-party backends.** A company that needs its internal
   embedding service or a compliance-approved LLM must fork the repo.
3. **Per-provider config sprawl.** Provider-specific options
   (Azure deployment IDs, Bedrock regions, Ollama keep-alive) have no
   home in the flat struct; they get squeezed into shared fields with
   fallback chains nobody can validate.
4. **No runtime discovery.** Available providers can't be listed,
   health-checked, or capability-queried (batch? dimensions? max tokens?)
   — the health probes hardcode the compiled set.

## Decision

The public provider capability shapes are frozen by **ADR-031** in
`pkg/spi`. This ADR owns discovery, configuration, and loading; it must not
redefine `Embedder`, `VectorStore`, `Extractor`, or `Reranker` interfaces.

Introduce an explicit, application-owned registry for each extension point:

```text
app.New()
  ├─ register built-in providers
  ├─ register explicitly supplied external providers
  └─ resolve configured typed providers
```

1. Extension points include `Embedder`, `Extractor`, `Reranker`,
   `VectorStore` (ADR-028), `ContradictionDetector`, `Renderer`,
   `RetentionPolicy`, and `AuthProvider` (ADR-036). Each uses the narrow
   capability contract defined by ADR-031 or its specialized ADR.
2. Providers self-describe with a `ProviderDescriptor` containing kind,
   name, version, dimensions where applicable, capabilities, and a
   provider-owned `ConfigSpec` representation.
3. Provider configuration is stored in per-provider sections such as
   `[embedder.openai]`. The config layer passes opaque provider config to
   the typed factory; the provider validates it. The old flat fields remain
   behind a compatibility adapter for one deprecation window.
4. Built-ins and external providers register through the same explicit path;
   mandatory `init()` registration and global mutable registries are
   forbidden.
5. Factories are typed by capability. A universal `Factory(...)(any, error)`
   is not part of the public design.
6. **Out-of-process plugins (phase 2):** a gRPC plugin protocol (HashiCorp
   go-plugin style) adapts to the same `pkg/spi` interfaces. The protocol is
   versioned separately, executed with resource limits, and is not exposed
   from `pkg/spi`.

## Alternatives considered

1. **Keep switches, accept PR churn.** Rejected: scales linearly with
   maintainer review time; blocks proprietary providers entirely.
2. **Go `plugin` package (.so).** Rejected: linux/mac only, exact
   toolchain match required, no sandboxing — a support nightmare at
   5000 deployments.
3. **WASM plugins (wazero).** Attractive sandbox; deferred: embedding
   payloads are large (6 KB floats × batch) and cross-boundary copying
   costs real latency; revisit for untrusted third-party logic.
4. **Scripting (Lua/Starlark) for ranking/detectors.** Deferred with
   WASM; most demand is for providers/backends, where gRPC suffices.

## Tradeoffs

- **Registry wiring is explicit but still indirect.** Provider discovery is
  one level less greppable than direct constructors; mitigate with
  `hermem providers list`, a registry dump in `hermem diagnose`, and typed
  registration calls in `internal/app`.
- **Config compatibility.** Old flat fields must keep working for one
  major version (adapter maps them into provider sections); two-version
  deprecation window.
- **gRPC plugin operational cost** (phase 2): process lifecycle, crash
  isolation, versioning. Accepted only after in-process registry proves
  the seams.
- **Security:** plugin binaries are arbitrary code. Phase 2 requires
  signed plugins + allowlist; documented as an operator responsibility.

## Consequences

- New providers become single-package PRs with no core edits.
- Proprietary/compliance providers possible without forking.
- Config validation errors name the provider and its spec.
- Unblocks community ecosystem at 100k stars; sets up ADR-036 auth
  providers and ADR-028 remote vector backends as first-class plugins.
