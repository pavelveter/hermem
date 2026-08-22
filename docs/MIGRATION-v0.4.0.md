# Migrating to v0.4.0 (core facade removal + ADR-035 identity)

> ## ⚠️ BREAKING RELEASE
>
> v0.4.0 removes the deprecated `src/internal/core` compatibility facade.
> Code that imports it will fail to compile with a normal Go missing-package
> error. Runtime behavior (HTTP routes, JSON envelopes, MCP tools, CLI
> output, persistence) is preserved except where this guide explicitly
> documents identity-format changes. The previous release (`v0.3.1`) remains
> available as the rollback target.

## Migration table

| Legacy import / symbol | Replacement | Notes |
|---|---|---|
| `internal/core.Entity/Edge/Task/Fact/Evidence/Episode/Belief/Goal…` | `pkg/domain` | Domain values, not transport DTOs |
| `internal/core.VectorIndex` | `pkg/spi.VectorStore` | Namespaces, filters, scores and typed errors are explicit |
| `internal/core.Embedder` | `pkg/spi.Embedder` | Single frozen `Embed` method; health via optional `spi.Pinger` assertion |
| `internal/core.LLMExtractor` | `pkg/spi.Extractor` | Returns identity-free drafts; the pipeline assigns IDs (ADR-035) |
| `internal/core.Reranker` | `pkg/spi.Reranker` | Operates on `spi.Candidate`; ordering is the only contract |
| `internal/core.*Request/*Response` | `api/v1` DTOs or command-local types | Wire JSON unchanged via mappers |
| `internal/core.Retriever` | `retrieval.Retriever` (internal) / public read APIs | Read-side values owned by the retrieval package |
| Core policy/config values | Owning internal packages (`retention`, `migration`, `apperr`, …) | No god-package replacement |

## 1. Domain models

Before:

```go
import "github.com/pavelveter/hermem/src/internal/core"

e := core.Entity{ID: "paris", Category: "fact", Content: "Paris is the capital of France"}
```

After:

```go
import "github.com/pavelveter/hermem/pkg/domain"

e := domain.Entity{ID: "paris", Category: "fact", Content: "Paris is the capital of France"}
```

All value shapes are field-for-field identical to the facade era; JSON tags
are unchanged, so persisted rows and stored wire formats round-trip as-is.

## 2. Embedders

Providers implement the frozen single-method SPI directly; health checks use
the optional `spi.Pinger` assertion instead of a required `Ping` method:

```go
import "github.com/pavelveter/hermem/pkg/spi"

type myEmbedder struct{}

func (myEmbedder) Embed(ctx context.Context, text string) ([]float32, error) { /* ... */ }

// Optional health check — asserted by callers, never required:
func (myEmbedder) Ping(ctx context.Context) error { return nil }

var _ spi.Embedder = myEmbedder{}
var _ spi.Pinger = myEmbedder{}
```

## 3. Extractors

The SPI returns identity-free drafts. Your provider no longer supplies
entity IDs — the ingestion pipeline assigns deterministic content-addressed
IDs (see §8):

```go
func (p *MyExtractor) Extract(ctx context.Context, req spi.ExtractRequest) (spi.ExtractResponse, error) {
	resp := spi.ExtractResponse{}
	for _, d := range p.runModel(req.Dialog) {
		resp.Entities = append(resp.Entities, domain.EntityDraft{
			Category: d.Category, Content: d.Text,
			Relations: []domain.DraftRelation{{TargetRef: d.Ref, RelationType: d.Rel}},
		})
	}
	return resp, nil
}
var _ spi.Extractor = (*MyExtractor)(nil)
```

## 4. Rerankers

Rerankers operate on candidates; ordering is the only observable effect:

```go
func (r *MyReranker) Rerank(ctx context.Context, query string, cands []spi.Candidate) ([]spi.Candidate, error) {
	sort.Slice(cands, func(i, j int) bool { return score(query, cands[i]) > score(query, cands[j]) })
	return cands, nil
}
var _ spi.Reranker = (*MyReranker)(nil)
```

## 5. Vector stores

Replace the IDs-only view with the full public contract. Namespaces are
explicit (single-tenant runtimes use `spi.DefaultNamespace`), records carry
optional metadata filters, searches return scored hits, and batch failures
are reported per-record:

```go
store, err := hermemVector.NewStore(backend, db, dim) // internal composition
// or implement pkg/spi.VectorStore for a custom backend:
var _ spi.VectorStore = (*myStore)(nil)

hits, err := st.Search(ctx, spi.SearchRequest{
	Namespace: spi.DefaultNamespace,
	Vector:    q,
	Limit:     5,
	Filter:    spi.Filter{Equals: map[string]string{"category": "fact"}},
})
```

Typed errors to handle: `spi.DimensionError`, `spi.CapacityError`,
`spi.PartialBatchError`.

## 6. HTTP DTOs

Decode/encode through `api/v1`; never share transport types with services:

```go
var req apiv1.StoreRequest
if err := httputil.DecodeJSON(w, r, &req); err != nil {
	return // envelope already written
}
entity := req.ToDomain()          // mapper
// ...
apiv1.EntityFromDomain(entity).WriteJSON(w, http.StatusOK)
```

Route paths, status codes, JSON field names, omission rules and error
envelopes are byte-compatible with v0.3.1 (locked by golden/OpenAPI suites).

## 7. External providers (compile isolation)

A provider package must compile against `pkg/domain` + `pkg/spi` only.
Importing anything under `src/internal/...` from outside the module has
never been possible; inside monorepo-style consumers, keep provider code on
the public contracts so it survives future internal refactors.

## 8. Identity changes (ADR-035)

This release ships the ADR-035 generation strategy. **New writes use new ID
formats; existing rows keep their IDs** (the re-key/alias-table backfill
remains governed by ADR-033).

| Kind | Old format | New format | Example |
|---|---|---|---|
| Tasks | `task-<counter>` (process-local, collision-prone) | `task-<ULID>` time-ordered | `task-06G2DYTPD4YMR59TW0TE4PSNJR` |
| Extracted entities | LLM-chosen strings | `ent-<content hash>` deterministic | `ent-K3NJC29ZP0RNJDGM0D70JH9D9J` |

Details:

- Task IDs embed their creation millisecond — sort by ID ≈ sort by creation;
  cross-process collisions are eliminated.
- Entity IDs for extracted content are `"ent-" + base32(sha256("ent-v1" |
  normalized-content | category | tenant | namespace))[:26]`. Same content in
  the same scope always maps to the same ID, so dedup is insert-or-ignore.
  The normalizer (NFC, lowercase, whitespace-collapsed) is version-locked by
  the scheme string.
- Client-side pre-dedup can mirror the formula; treat the scheme version as
  part of the contract.
- Inspect any identifier with the new command:

```bash
hermem id inspect task-06G2DYTPD4YMR59TW0TE4PSNJR ent-K3NJC29ZP0RNJDGM0D70JH9D9J
# task-06G2DYTPD4YMR59TW0TE4PSNJR	kind=task	created=2026-08-22T01:00:01Z
# ent-K3NJC29ZP0RNJDGM0D70JH9D9J	kind=ent
```

## Support window

The `compat/v0.3.x` branch tracks the last pre-break release for the
documented support window. See `RELEASE.md` (rollback policy) — tags are
immutable; corrections ship as new releases.
