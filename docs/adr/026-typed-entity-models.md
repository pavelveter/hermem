# ADR-026: Typed Entity Models and Reassembly

## Status

Accepted

## Context

`core.Entity` carries nineteen fields covering identity, content, embedding, retention metadata, lifecycle state, provenance, and graph mechanics. As the project grew, the single struct became a friction point: callers that needed only workflow state were passing the full row, type assertions at internal boundaries grew (`e.Category == "task"` checks against duck-typed strings), and refactors risked spilling unrelated fields together.

The P0 ENTITY MODEL REFACTOR — items #3–#8 documented inline in `src/internal/core/compose.go` and the four sibling-band source files (`fact.go`–`belief.go`) — was already executed. Five typed sibling models landed:

- `core.Fact` — identity + content + embedding (4 fields)
- `core.Evidence` — embeds `Fact`, + confidence / source / source-type (3 fields)
- `core.Episode` — embeds `Fact`, + conversation-id / message-id / extracted-from (3 fields)
- `core.Task` — embeds `Fact`, + status / valid-from / valid-to / priority (4 fields)
- `core.Belief` — embeds `Fact`, + created-at / updated-at / last-accessed-at / archived / degree (5 fields)

`core.Compose(Fact, Evidence, Episode, Task, Belief) Entity` is the canonical reassembly path — a free function that lays out all nineteen fields explicitly. `core.ComposeFromTask` exists for callers that hold a `Task` (with embedded `Fact`) and need the full `Entity` shape (e.g. orchestrator's execFunc).

In §8.4, the inverse bridges `Evidence.AsEntity`, `Episode.AsEntity`, `Task.AsEntity`, `Belief.AsEntity` were **deliberately removed** because they silently zeroed 14–16 Entity fields per band, AND for the embedded-Fact bands (`Task` / `Evidence` / `Episode` / `Belief`) the four identity fields were not propagated through the bridge — a writeback of any of `Task.AsEntity()` / `Evidence.AsEntity()` / `Episode.AsEntity()` / `Belief.AsEntity()` would have created an `Entity` with band-specific fields set but no row identity. The pre-§8.4 bridges were unsafe on two axes at once. They felt safe (the embedded `Fact` carries identity), but the four downstream fields (`Confidence`/`Source`/`SourceType`, the Episode provenance triple, the Task state triple, the Belief retention/centrality quintuple) all reset to zero — which on a writeback would silently wipe provenance, retention TTL, and graph centrality for that row. Only `Fact.AsEntity` (which carries full identity itself) and `compression.SummaryNode.AsEntity` (an ephemeral compression node, structurally distinct from domain rows) remain.

The work is partially landed: `core/` carries the typed models, tests, and `Compose`/`ComposeFromTask`. The remaining surface is adoption — pushing typed shapes into `internal/ingestion/`, `internal/server/{contradiction,timeline,memory}/`, and bumping SDK awareness of the typed shapes. The HTTP wire contract today remains the 19-field `Entity`-shaped JSON envelope; SDKs (Go/Python/TS) parse it duck-typed. Sonnet code-review misidentified the typed-model split as aspirational, citing names ("Task/Episode/Fact/Contradiction/Source") that did not match the actually-implemented set (Fact/Evidence/Episode/Task/Belief). ADR-026 ratifies the implementation as ground truth.

## Decision

### Band ownership — the 19 fields split into five typed bands

| Band | Owns | Why this band |
|---|---|---|
| `Fact` | `ID`, `Category`, `Content`, `Embedding` | The unadorned semantic identity. Embedded in `Evidence` / `Episode` / `Task` / `Belief` so any of those typed shapes can serialise with identity without going through fat `Entity`. |
| `Evidence` | `Confidence`, `Source`, `SourceType` | Epistemological origin. `Confidence` evaluates extraction quality or source trustworthiness, not a universal truth metric. Excludes `Degree` (structural centrality, not trust). |
| `Episode` | `ConversationID`, `MessageID`, `ExtractedFrom` | Provenance timeline from the dialog ingestion. Strictly "where in the dialog did this originate?" — separate from source-trust (`Evidence`) and retention (`Belief`). |
| `Task` | `Status`, `ValidFrom`, `ValidTo`, `Priority` | Workflow state-machine concerns. `UpdatedAt` deliberately excluded: state transitions also write `UpdatedAt`, but so do TTL ticks and evidence reinforcement — it is a database-layer clock, not task-business state. `Degree` excluded: connectivity metrics don't reflect task completion. |
| `Belief` | `CreatedAt`, `UpdatedAt`, `LastAccessedAt`, `Archived`, `Degree` | Persistence and graph mechanics. `LastAccessedAt` is GC/retention TTL — a memory-management concern that does not belong on `Evidence` (which would conflate ep(istemological) trust with retention ticks). `Degree` is pure graph centrality maintained by SQL triggers on edge insert/delete; the ranker reads `log10(1+degree)`. |

Sum: 4 (Fact) + 3 (Evidence) + 3 (Episode) + 4 (Task) + 5 (Belief) = 19. The split is exhaustive; every `Entity` field is owned by exactly one typed band.

### Reassembly is `Compose`-only

`core.Compose(Fact, Evidence, Episode, Task, Belief) Entity` is the canonical reassembly path. It assigns all nineteen fields explicitly by band. Free function (not method) because all five inputs come from outside — there is no useful receiver.

**Compose identity contract.** The explicit `Fact` parameter (position 1) is the canonical identity source for the resulting `Entity.ID`, `.Category`, `.Content`, `.Embedding` — even though `Task`/`Evidence`/`Episode`/`Belief` each physically embed a `Fact`. In normal use, the embedded facts inside the typed arguments carry the same identity; if a caller constructs `Compose(f, ev, ep, t, b)` with `t.Fact.ID != f.ID`, the explicit `f` parameter wins.

**Contract (declarative, not enforced):** `Compose`'s explicit `Fact` argument (position 1) is the canonical identity source for the resulting `Entity.ID` / `.Category` / `.Content` / `.Embedding`. Disjoint-template assembly is permitted — `Compose` does **not** panic or assert on identity mismatch between `f` and the embedded Facts of `ev` / `ep` / `t` / `b`. The common path uses `ComposeFromTask` to make coordination explicit. The contract is descriptive, not runtime-enforced; review-time discipline matches the §8.4 bridge-removal posture.

**Bridge-removal policy (§8.4).** `Evidence.AsEntity`, `Episode.AsEntity`, `Task.AsEntity`, `Belief.AsEntity` are removed and will not return. The `Entity → typed` down-projections (`Entity.AsFact`, `Entity.AsEvidence`, `Entity.AsEpisode`, `Entity.AsTask`, `Entity.AsBelief`) are the canonical projections from a row read; the reverse typed → Entity path goes through `Compose`. Only `Fact.AsEntity` (whose full identity is preserved because `Fact` itself carries `ID`/`Category`/`Content`/`Embedding`) and `compression.SummaryNode.AsEntity` (which manufactures new rows where the omitted fields are legitimately meant to be zero on first write) are preserved.

### Embedding pattern

`Evidence`, `Episode`, `Task`, `Belief` each embed `Fact`:

```go
type Task struct {
    Fact
    Status    string
    ValidFrom *time.Time
    ValidTo   *time.Time
    Priority  int
}
```

Go's anonymous-embed promotion means `Task` instances expose `.ID`, `.Category`, `.Content`, `.Embedding` directly. This lets `/task/*`, `/evidence/*`, `/episode/*`, `/belief/*` HTTP endpoints serialise typed shapes directly without going through fat `Entity`. The embedding is structural-identity-share, not field-duplication.

`compression.SummaryNode` does **not** embed `Fact` — it is structurally distinct (it carries compression metadata) and bridges to `Entity` only for the synthesised write, where zeroed non-Fact fields are correct on first write.

### Ingestion and orchestrator assist

`core.ComposeFromTask(Task) Entity` packages the common Compose-caller pattern when only a `Task` is held (and other bands are zero). This is the one path where the `Evidence` / `Episode` / `Belief` arguments are *explicitly* zero — appropriate at orchestrator's `execFunc` boundary where only task state has been computed. The zero-other-bands semantic is documented.

### HTTP wire — unchanged in this ADR

The HTTP wire contract continues to use the 19-field `Entity` JSON envelope. SDKs (Go/Python/TS) parse it duck-typed. ADR-026 does not change JSON shapes, OpenAPI field names, or envelope semantics. Future ADR for HTTP-bound typed JSON (with SDK versioning strategy) will consider an **overlay `kind` discriminator pattern**: add `kind` as a new JSON field alongside the existing `category`, allowing SDKs to opt into sum-type deserialisers without breaking legacy 19-field generic parsers. Replacing `category` with `kind` outright would be a SDK-MAJOR breaking change and is not chosen.

## Alternatives Considered

1. **`Entity`-only**, no typed bands. Rejected: identifies a real friction (`e.Category == "task"`, `:="ducktyped"`-shaped code at boundaries) and a real row-corruption risk (the §8.4 bridge subtlety). The split is the only path that resolves both.
2. **Keep `Task.AsEntity()` etc. as easy-up bridges.** Rejected in §8.4 because they silently zeroed 14–16 fields per band and dropped the embedded-Fact identity (see Context for band-by-band counts). The "easy" reverse path masks data loss on two axes; the explicit `Compose` path forces reassembly decisions to be deliberate.
3. **Typed JSON on the wire now.** Rejected: replaces `category` with `kind` is SDK-MAJOR breaking, and adding `kind` overlay without a SDK plan risks ambiguous JSON shapes. Deferred to a future ADR.
4. **No `core.Compose` — each HTTP shell reassembles ad-hoc.** Rejected: divergent assembly paths would re-create the bridge-subtlety problem in a different form. One canonical reassembler is the only stable answer.
5. **Promote `compression.SummaryNode.AsEntity` to removal.** Rejected: `SummaryNode` is structurally distinct from a domain row; its `AsEntity` correctly manufactures a new row from scratch, and the omitted-field-zeroing is the desired behaviour on first write.
6. **Embed `Fact` in `SummaryNode`.** Rejected: `SummaryNode` carries compression-specific fields; identity in compression is a synthesis product, not a stored entity. Keeping `SummaryNode` distinct preserves the bridge-removal rationale.

## Consequences

- **Storage unchanged.** SQLite continues to hold a single wide table; `core.Entity` is the row projection. Migrations are unaffected.
- **HTTP wire unchanged.** SDKs do not change parsers. Existing OpenAPI snapshots remain byte-stable.
- **Internal callers gain type-safety.** A handler that needs only `Task` reads `core.Task` (with `WithInitialStatus`, `CanTransitionTo`); a writer that needs workflow state writes through `Compose`. There is no longer a reason to type-assert `Category == "task"` in Go code at internal boundaries.
- **Reassembly is explicit.** `Compose` lays out all 19 fields — callers cannot accidentally zero Epi(sode) provenance or Bel(ief) timing. The §8.4 bridge-removal policy makes this enforceable.
- **SDK-typed-JSON remains deferred.** Until a follow-on ADR frames the SDK-versioning policy, HTTP responses continue to be `Entity`-shaped. Internally, Go callers see typed shapes; externally, JSON stays unified.
- **`SummaryNode` special case persists.** Compression produces synthetic rows whose initial omission is correct on first write. Replacing it with `Compose` would over-write zero fields into existing rows and risk retention/centrality wipeout.

## Migration

Four PRs push typed shapes through internal layers in priority order, each independently revertable. PR1–PR4 hold the HTTP wire **byte-stable** (no JSON envelope change, `api/openapi_test.go` snapshot unchanged, SDKs continue to see 19-field `Entity`-shaped responses).

**Ordering rationale.** `internal/ingestion/` is the only write-path that produces new `Entity` rows from extracted facts; adopting typed shapes there establishes the canonical `Compose` scaffold that the read-path shells (PR2–PR4) project from. PR2 / PR3 / PR4 can technically be parallelised once PR1 lands (their read-side projections touch independent code paths via `AsBelief` / `AsEpisode` / `AsTask` respectively), but the sequential numbering surfaces the data-flow dependency before concurrency and aids review.

### PR1 — `internal/ingestion/`

- LLM extraction emits `Fact`, `Evidence`, `Episode` per extracted element (instead of raw `Entity` literals).
- Persist path converts via `Compose` for the SQLite write.
- Tests: round-trip `Fact → Compose → Entity → AsFact → identity-equal`; ensure no field is silently zeroed.
- Acceptance: `go test ./src/internal/ingestion/...` green; existing ingest e2e unchanged; SQLite row schema unchanged.

### PR2 — `internal/server/contradiction/`

- Internal contradiction logic operates on `Belief` (graph mechanics) and `Fact` (identity).
- Endpoint reads `e.AsBelief()` from DB fetch, computes, and reassembles via `Compose`.
- Tests: ACL around `LastAccessedAt` (which is GC-touched, not contradiction-relevant) is preserved on `Compose` (no field zeroing).
- Acceptance: `go test ./src/internal/server/contradiction/...` green; HTTP wire unchanged.

### PR3 — `internal/server/timeline/`

- Internal trajectory mapping consumes `Episode` arrays rather than 19-field rows.
- Endpoint projection surfaces `Episode` only; serialisation is `Episode`-shaped on the typed-side, `Entity`-shaped on the wire (no JSON change).
- Acceptance: `go test ./src/internal/server/timeline/...` green; openapi snapshot unchanged.

### PR4 — `internal/server/memory/`

- `/memory/store` and `/memory/search` adopt `Fact` internally for the read/write path. The 19-field JSON envelope remains.
- Tests: search-result projection path is `[]core.Entity → []core.SearchResult{Fact, Similarity}`.
- Acceptance: `go test ./src/internal/server/memory/...` green; `/memory/*` HTTP wire **byte-stable** (no JSON envelope change, OpenAPI snapshot unchanged, SDKs continue to see 19-field `Entity`-shaped responses).

After PR4, all four internal layers that touch the domain rows use typed shapes; HTTP wire is byte-stable. SDKs continue to parse `Entity`-shaped responses. A separate ADR (deferred) defines the `kind` discriminator overlay when / if typed JSON joins the wire.

## Out of scope

- **HTTP wire JSON changes.** Future ADR for `kind`-overlay is unblocked but not started.
- **SDK type additions.** Go / Python / TS SDKs continue to expose `Entity` as the surface type.
- **`compression.SummaryNode.AsEntity` removal.** This bridge is preserved by design.
- **Goal refactor.** The Goal-typed view (compose.go mentions inline 4-field copy from `Goal → Task`) is a TODO item; it stays as a code-level convention rather than a struct, per the documented contract in compose.go.
- **Refactoring of `/v1/*` non-shell HTTP paths.** The migration gradient covers the main domains (`ingestion`, `contradiction`, `timeline`, `memory`). Other shells migrate opportunistically as natural follow-ups.
- **Bridge policy extension to other domains.** §8.4 is **reaffirmed by this ADR** as fixed policy for the five listed bands. New domains that compose into `Entity` adopt the same pattern (no `X.AsEntity` bridges for new bands, only `Compose`). ADR-026 does not re-litigate §8.4 — the policy continues in force.

## References

- `src/internal/core/compose.go` — the canonical `Compose` function and §8.4 bridge-removal policy.
- `src/internal/core/fact.go`, `evidence.go`, `episode.go`, `task.go`, `belief.go` — the five typed band definitions and their `Entity → typed` projections.
- `src/internal/core/types.go` — the 19-field `Entity` row projection; defines `SchemaConfig`, `RankingWeight`, `RetentionPolicy`, `Migrator` and other cross-band types.
- `src/internal/orchestrator/service.go` — the only orchestrator-side consumer of `ComposeFromTask` (line `core.ComposeFromTask(task)` in `executeTask`).
- `src/internal/store/`, `src/internal/ingestion/`, `src/internal/server/{contradiction,timeline,memory}/` — migration PR1-PR4 targets.
- `docs/adr/008-domain-model.md` — pre-cursor discussion of typed domain models.
- `docs/adr/024-configuration-subconfigs.md` — sibling ADR for the configuration surface.
- `README.md` “Features” section, “Per-domain Entity decomposition” entry — public-facing interface statement of the typed-model split: *“5 typed models (Fact, Evidence, Episode, Task, Belief) projected from the 19-field umbrella `Entity`; `core.Compose(…)` reassembles.”*
- `src/internal/core/{compose,fact,evidence,episode,task,belief}.go` block-comment markers (`P0 ENTITY MODEL REFACTOR (item #N)`) — the execution trail this ADR ratifies. Note: TODO.md does not carry these items as a numbered list.
