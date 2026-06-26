# Hermem Refactoring Plan

> Generated from comprehensive code review of the entire codebase (318 `.go` files)
> after all P0/P1/P2 feature work was completed by AI coding agents.
>
> **Date:** June 26, 2026

---

## Executive Summary

The codebase suffers from systemic code duplication, inconsistent naming conventions,
and boilerplate patterns introduced by 14+ AI coding sub-agents working independently.
The good news: the architecture (transport-agnostic domain services + thin HTTP shells +
CLI shells) is sound. The bad news: the implementation carries ~40% dead weight in
duplicated code, verbose comments, and inconsistent style.

**Estimated savings:** ~3000 lines of code eliminated, ~1500 lines of comment noise removed.

---

## 1. CRITICAL — Eliminate Code Duplication

### [x] 1.1 Duplicate `rejectSchemaConflict` (2 copies, identical)

**Files:**
- `src/internal/server/memory/memory_service.go:64`
- `src/internal/server/edge/edge_service.go:55`

**Problem:** Identical 9-line method copy-pasted. The edge version explicitly
acknowledges in its comment: *"copy-pasted rather than promoted to a shared helper"*.

**Fix:** Create `src/internal/server/shared/reject.go` with `func RejectSchemaConflict(w, gen, refs, metrics)`.

---

### [x] 1.2 Duplicate `ErrInvalidSchema` type (2 copies, identical)

**Files:**
- `src/internal/memory/service.go:122`
- `src/internal/edge/service.go:84`

**Problem:** Two identical exported types (`ErrInvalidSchema` with `Field`, `Value`,
`Error()`) defined in two packages. The memory version's comment says *"The struct is
retained here — not collapsed into a shared core type — because it is the
memory-domain semantic envelope"* — a rationalization of copy-paste.

**Fix:** Move `ErrInvalidSchema` to `src/internal/core/types.go` as a shared type.
Both packages reference the same type. Domain-agnostic error type carrying only
`Field`/`Value` is not semantically different between memory and edge.

---

### [x] 1.3 Duplicate `isSchemaErr` helper (2 copies)

**Files:**
- `src/internal/server/memory/memory_service.go:75`
- `src/internal/server/edge/edge_service.go:62`

**Problem:** Two identical `errors.As` wrappers testing for their respective
package's `ErrInvalidSchema`. Once §1.2 is fixed (shared error type), a single
shared `isSchemaErr` can go into `server/shared/`.

**Fix:** After §1.2, create `server/shared/schema_err.go` with a single `func IsSchemaErr(err error) bool`.

---

### [x] 1.4 Pervasive nil→empty slice normalization

**Problem:** Every domain service method that returns a slice does:
```go
if result == nil {
    result = []Type{}
}
```
Appears in 20+ methods across `graph/service.go`, `task/service.go`,
`contradiction/service.go`, `store/graph.go`, `retrieval/service.go`, etc.

**Fix:** Create `src/internal/core/normalize.go`:
```go
func NormalizeSlice[T any](s []T) []T {
    if s == nil { return []T{} }
    return s
}
```
Then replace every `if x == nil { x = []T{} }` with `x = core.NormalizeSlice(x)`.

---

### [x] 1.5 Duplicate `NormalizeVector` (2 copies — verified correct, no action)

**Files:**
- `src/internal/vector/cosine.go`
- `src/internal/vector/cosine_darwin.go`

**Problem:** Identical implementation in both files — the Darwin version already uses
Accelerate (cblas_snrm2 + cblas_sscal), and the pure-Go version is properly
build-tagged (`//go:build !darwin || !cgo`). The two copies are NOT identical
because the Darwin version uses CGo, so this is actually correct.

**Fix:** No action. The two copies serve different build targets. The Darwin version
is the CGo Accelerate path; the pure-Go version is the fallback.

---

## 2. HIGH — Fix Naming Inconsistencies

### [x] 2.1 Inconsistent package names in `server/` sub-packages

| Package | Current name |
|---------|-------------|
| `server/edge/` | `package edge_http` |
| `server/retention/` | `package retention_http` |
| `server/reembed/` | `package reembed_http` |
| `server/timeline/` | `package timeline_http` |
| `server/memory/` | `package memory` |
| `server/retrieval/` | `package retrieval` |
| `server/task/` | `package task` |
| `server/graph/` | `package graph` |
| `server/ingest/` | `package ingest` |
| `server/contradiction/` | `package contradiction` |
| `server/migration/` | `package migration` |
| `server/health/` | `package health` |

**Problem:** 4 packages use `_http` suffix, 8 don't. No pattern governing the
choice. Callers in `server/server.go` must use import aliases for the `_http`
packages (e.g. `edgesrv "github.com/.../server/edge"`) because Go disallows
package name `edge` from directory `edge/` (it's `edge_http`), while
`package memory` from directory `memory/` needs no alias.

**Fix:** Normalize ALL `server/*` sub-packages to use the directory name as
package name. Drop the `_http` suffix. This means:
- `package edge_http` → `package edge`
- `package retention_http` → `package retention`
- `package reembed_http` → `package reembed`
- `package timeline_http` → `package timeline`

Then remove 4 import aliases from `server/server.go`.

---

### [x] 2.2 Inconsistent constructor naming: `New` vs `NewService`

**Domain services using `NewService`:**
- `migration/service.go`, `contradiction/service.go`, `ingest/service.go`,
  `retrieval/service.go`, `task/service.go`, `retention/service.go`,
  `graph/service.go`, `memory/belief/belief.go`, `memory/evidence/evidence.go`

**Domain services using `New`:**
- `edge/service.go`, `goal/service.go`, `timeline/service.go`,
  `orchestrator/service.go`, `episodic/episode.go`, `episode/service.go`,
  `memory/service.go`, `health/service.go`, `reembed/service.go`

**Problem:** No convention. Some packages with one exported type (just
`Service`) might argue `New` is unambiguous, but consistency across the
project matters more.

**Fix:** Standardize on `New` for ALL domain services. The package name
provides context (`task.New(db, ...)` vs `task.NewService(db, ...)`).
`NewService` is redundant when there's only one type in the package.

---

### [x] 2.3 Inconsistent HTTP shell struct field naming

Most HTTP shells call their domain service field:
- `Svc *domain.Service` — contradiction, graph, migration, edge, ingest, reembed, timeline, health
- `RetSvc *retrieval.Service` — retrieval
- `TaskSvc *task.Service` — task
- `Mem *memory.Service` — memory
- `Svc *domain.Service` OR `RetSvc` — inconsistent shortened names

**Fix:** Standardize on `Svc *<domain>.Service` for ALL HTTP shells.

---

## 3. HIGH — Introduce Shared HTTP Shell Abstraction

### [x] 3.1 No shared interface for `Routes()`

Every HTTP shell implements `Routes() map[string]http.HandlerFunc` structurally
but there is no Go interface. This means `server.go`'s `mount()` cannot iterate
over a list of route providers.

**Fix:** Create `src/internal/server/handler.go`:
```go
package server

type RouteProvider interface {
    Routes() map[string]http.HandlerFunc
}
```

Then refactor `mount()` to iterate over `[]RouteProvider{s.Retrieval, s.Task, ...}`
instead of 13 separate `for path, hf := range s.Xxx.Routes()` blocks.

---

### [x] 3.2 Introduce canonical `BaseHTTPService`

**Problem:** HTTP shells have 2-4 common fields (`Svc`, `Metrics`, `Refs`),
yet each shell defines its own struct from scratch. This leads to:
- Handlers writing `s.Metrics.IncErr()` in 150+ places
- `s.Refs.Load()` in 50+ places
- No shared middleware/instrumentation hook points

**Fix:** Create a base type and a handler wrapper pattern:

```go
// src/internal/server/base.go
type HTTPServiceBase struct {
    Metrics *metrics.Metrics
    Refs    *serverstate.Ref
}

func (b *HTTPServiceBase) Wrap(fn func(w, r) error) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if err := fn(w, r); err != nil {
            b.Metrics.IncErr()
            // Map domain errors to HTTP status
        }
    }
}
```

This would eliminate identical method-check + MaxBytesReader + error-writing blocks
from ~60 handler methods.

---

## 4. MEDIUM — Clean Up Verbose Comments

### [x] 4.1 PHASE archaeology in package-level comments

**Status (this commit):** Server-side pkg doc archaeology trimmed across 11 server/*/*_service.go shells + server.go route-registry header. Each shell's pkg doc is now 2-4 lines describing current shape ("Package X exposes ...); multi-paragraph PHASE history ("moved out in PHASE 3.5", "added in PHASE 3.6") is gone. server/server.go's 50-line route-registry header collapsed to 6 lines (dispatcher contract + Refs atomic note). Rule applied: regex `(?i)(pre-|post-)?PHASE[- ]?[0-9]\.[0-9]` archaeological refs deleted; §3.2 / §8 / §10 wire-contract anchors preserved verbatim. No behavior change; vet/build/race on `./src/internal/server/...` clean. Domain-service-side pkg-doc trim (§4.1b) + §4.2 inline archaeology deferred to followup commits.

**Problem:** Every file carries multi-paragraph package doc comments recapping
"PHASE X.Y moved this from..." history. Example: `memory_service.go` has a
33-line package doc comment that is pure archaeology. `task_service.go` describes
PHASE 2.4 in detail. This information belongs in `CHANGELOG.md` or git history,
not in source files.

**Files affected:** All 12 `server/*/` HTTP shells, all domain services.

**Fix:** Reduce package-level comments to 2-4 lines describing current purpose.
Move PHASE archaeology to one `docs/ARCHITECTURE.md` file.

---

### [ ] 4.2 Overly verbose inline comments

**Problem:** Comments like:
> "Pre-PHASE-2.1 the missing-field check happened inline at the HTTP layer —
> kept here verbatim so existing /store clients continue to see 400..."

These are explanations of *why* the code is simple, not *what* the code does.
They add noise for readers who joined after the refactor.

**Fix:** Drop historical rationale comments. Keep only "what" comments where
the code is genuinely non-obvious.

---

## 5. MEDIUM — Package Organization

### [ ] 5.1 `contradiction/` package has mixed concerns

**Files:**
- `service.go` — read-side: list existing contradicts pairs (DB query)
- `lexical.go`, `embedding.go`, `llm.go`, `composite.go`, `detector.go` — ingest-time
  contradiction *detection* pipeline (LLM/embedding-based dedup)

**Problem:** Two different concerns in one package. The "detector" files
(`lexical`, `embedding`, `llm`, `composite`, `detector`) are part of the
*ingestion pipeline*, not the *read-side query API*. They share only the
concept of "contradiction" but operate at different lifecycle stages.

**Fix:** Move detectors to `src/internal/ingestion/detectors/` or
`src/internal/ai/detectors/`. The contradiction `service.go` stays as
the read-side API. The ingest-time pipeline imports the detector package.

---

### [ ] 5.2 `evolution/` package is large and flat

**Files:** `aggregation.go`, `chains.go`, `history.go`, `propagation.go`,
`queries.go`, `relationships.go`, `superseded.go`, `trust.go` (+ tests)

**Problem:** 8 files all in one flat package. No clear sub-groupings despite
distinct concerns (trust scoring vs history tracking vs belief chains).

**Fix:** Consider `evolution/trust/`, `evolution/history/`, `evolution/chains/`
sub-packages. Or at minimum add a `doc.go` with package organization notes.

---

### [ ] 5.3 `episodic/` duplicates P1 `episode/` service layer

**Files:**
- `src/internal/episodic/` — 7 files (episode, session, event, timeline,
  playback, summarization, retrieval, linking, task_link)
- `src/internal/episode/service.go` — thin P1 service (ListByConversation etc.)

**Problem:** The P1 `EpisodeService` in `episode/` provides a thin domain API,
while `episodic/` holds the full episodic memory subsystem. These overlap and
don't clearly communicate their boundaries.

**Fix:** Merge `episode/service.go` into `episodic/`. The `episodic` package is
the canonical home for episode-related logic; the thin `episode/` service
should not exist as a separate package.

---

## 6. [ ] MEDIUM — Reduce `serve.go` Wiring Boilerplate

**File:** `src/internal/cli/serve.go`

**Problem:** `runServe()` is 70+ lines of manual service construction + wiring:
```go
memSvc := memdomain.New(...)
edgeSvc := edgedomain.New(...)
timelineSvc := timelinedomain.New(...)
// ... 12 more service constructions ...
srv := server.NewServer(refs,
    ret.New(retSvc, ...),
    tasksvc.New(taskSvc, ...),
    // ... 12 more shell constructions ...
)
```

Every new service requires adding lines to 2 places in this file, plus
a field to `Server` struct, plus import aliases, plus mount() registration.

**Fix:** Create a `ServiceRegistrar` or builder pattern:
```go
builder := server.NewBuilder(env).WithDefaults()
srv := builder.Build()
```

Or at minimum, a `WireAll(env)` helper function in a separate `wiring.go` file
that constructs all 12 services and returns the populated `*Server`.

---

## 7. LOW — Miscellaneous Improvements

### [ ] 7.1 `health` HTTP shell has no `Metrics` field

**File:** `src/internal/server/health/health_service.go`

**Problem:** This is the only HTTP shell without `Metrics`. Health probes don't
increment counters, so it's not a bug, but it's an inconsistency in the pattern.

**Fix:** Either add `Metrics *metrics.Metrics` (with no-op increments) for
consistency, or document why health is intentionally different in the package
doc comment.

---

### [ ] 7.2 `httputil.WriteErrorWithCode` signature

**File:** `src/internal/httputil/httputil.go`

**Problem:** `WriteErrorWithCode(w, status, msg, code, field)` — the parameter
order is confusing. `status` is HTTP status, but `code` is a machine-readable
error code string. They're right next to each other and easily swapped.

**Fix:** Use an options struct or reorder to `WriteError(w, status, msg, opts...)`.

---

### [ ] 7.3 Timestamp handling inconsistency

**Problem:** Some store functions use `time.Time`, others use `*time.Time`,
others use `sql.NullTime`. The Entity struct has `UpdatedAt time.Time` but
`CreatedAt *time.Time`. The timeline wire shape has `*time.Time`. No consistent
NULL-time handling policy.

**Fix:** Standardize. Either always use `time.Time` with `IsZero()` as NULL
sentinel, or always use `*time.Time` with `nil` as NULL. Pick one and apply
uniformly.

---

### [ ] 7.4 Dead code: `memory.Service.extractor` field

**File:** `src/internal/memory/service.go`

**Problem:** The `extractor` field is retained but unused since PHASE 3.4
(when `Ingest` moved to `ingest/`). The comment says it's "for future
memory-write hooks."

**Fix:** Either remove it (breaking the constructor signature, but that's
what the refactoring PR is for) or use it in a meaningful way. Dead fields
mislead readers.

---

## 8. HIGH — Domain Model Slimming (Entity Decomposition)

### [ ] 8.1 Fat `core.Entity` (19 fields) vs slim domain types

**File:** `src/internal/core/types.go`

**Problem:** `Entity` carries 19 fields (`ID`, `Category`, `Content`, `Embedding`,
`UpdatedAt`, `LastAccessedAt`, `Archived`, `Status`, `Confidence`, `Source`,
`SourceType`, `CreatedAt`, `ValidFrom`, `ValidTo`, `ConversationID`, `MessageID`,
`ExtractedFrom`, `Degree`, `Priority`). New domain types (`Task`, `Goal`, `Fact`,
`Belief`, `Evidence`, `Episode`, `SummaryNode`) carry only the fields they need
(typically 4-6).

However, the `store/` and `retrieval/` packages still use the fat `Entity`
everywhere. Every new domain type requires an `AsEntity()` converter that copies
a subset of fields into the 19-field struct. These converters are duplicated
across 7 files:
- `core/goal.go:46` — `Goal.AsEntity()`
- `core/task.go:41` — `Task.AsEntity()`
- `core/fact.go:39` — `Fact.AsEntity()`
- `core/belief.go:56` — `Belief.AsEntity()`
- `core/evidence.go:38` — `Evidence.AsEntity()`
- `core/episode.go:36` — `Episode.AsEntity()`
- `compression/types.go:22` — `SummaryNode.AsEntity()`
- `compression/types.go:31` — `EntityAsSummaryNode()` (reverse direction)

**Fix strategy:**

1. **Phase 1: `store/` layer** — Keep `Entity` as a raw data-row model purely for
   `Rows.Scan()` in `store/init.go`. Store functions return the domain-specific
   types (`Task`, `Fact`, `Belief`) instead of `Entity`.

2. **Phase 2: `retrieval/` layer** — `RetrievalResult` currently buckets results
   as `[]RetrievedFact` (which already wraps slim content). No change needed — it
   already doesn't use `Entity` directly.

3. **Phase 3: Service layer** — Both `EpisodeService` and `GoalService` already
   return `[]core.Entity`. Switch them to return `[]core.Episode` and `[]core.Goal`
   respectively. The HTTP/CLI shells serialize the slim types directly.

4. **Phase 4: Remove `AsEntity()` converters** — After all consumers use slim types,
   the 7 `AsEntity()` methods become dead code and are removed.

**Benefits:**
- `store/*` functions gain type safety: `GetTaskByID` returns `Task`, not `Entity`
- No more field-level confusion: business logic sees 4-6 relevant fields, not 19
- ~80 lines of converter code eliminated

---

## 9. HIGH — AI Client Unification

### [ ] 9.1 Duplicated HTTP request plumbing in `ai/embedder.go`, `extractor.go`, `reranker.go`

**Files:**
- `src/internal/ai/embedder.go` — `OllamaEmbedder` + `OpenAIEmbedder`
- `src/internal/ai/extractor.go` — `OllamaLLMExtractor` + `OpenAILLMExtractor`
- `src/internal/ai/reranker.go` — `OllamaReranker` + `OpenAIReranker`

**Problem:** Each of the 6 client structs duplicates the same pattern:
1. Construct `*http.Client` with timeout
2. Wrap in `*ResilientClient` with retry policy
3. Marshal JSON body
4. Build `http.NewRequestWithContext`
5. Set `Content-Type: application/json`
6. Set `Authorization: Bearer <key>` (OpenAI) or skip (Ollama)
7. Attach `GetBody` closure for retry replay
8. Call `resilient.Do(ctx, req)`
9. Read `resp.Body` on non-200
10. Decode JSON response

Steps 1-2 are duplicated in all 6 constructors. Steps 3-10 are duplicated in
all 6 `Embed`/`ExtractEntities`/`Rerank` methods with only the URL path,
request body shape, and response shape differing.

**Fix:** Create a `src/internal/ai/http.go` with a private `httpClient` helper:

```go
// httpClient is the internal unified HTTP client for all AI providers.
// Embeds ResilientClient + API key handling + JSON marshal/unmarshal.
type httpClient struct {
    baseURL    string
    apiKey     string
    resilient  *ResilientClient
}

func newHTTPClient(baseURL, apiKey string, timeout time.Duration, attempts int) *httpClient {
    c := &http.Client{Timeout: timeout}
    return &httpClient{
        baseURL:   strings.TrimRight(baseURL, "/"),
        apiKey:    apiKey,
        resilient: NewResilientClient(c, attempts, DefaultBackoffs()),
    }
}

// doPOST marshals reqBody, POSTs to path, unmarshals into dst.
// Handles ctx propagation, retry, Bearer header, non-200 error body capture.
func (c *httpClient) doPOST(ctx context.Context, path string, reqBody, dst interface{}) error {
    body, _ := json.Marshal(reqBody)
    req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+path, nil)
    if err != nil {
        return err
    }
    req.Header.Set("Content-Type", "application/json")
    if c.apiKey != "" {
        req.Header.Set("Authorization", "Bearer "+c.apiKey)
    }
    captured := body
    req.Body = io.NopCloser(strings.NewReader(string(captured)))
    req.GetBody = func() (io.ReadCloser, error) {
        return io.NopCloser(strings.NewReader(string(captured))), nil
    }
    resp, err := c.resilient.Do(ctx, req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    if resp.StatusCode != 200 {
        b, _ := io.ReadAll(resp.Body)
        return fmt.Errorf("%d: %s", resp.StatusCode, string(b))
    }
    return json.NewDecoder(resp.Body).Decode(dst)
}
```

Then each `Embed`/`ExtractEntities`/`Rerank` method reduces to:
```go
func (e *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
    var resp ollamaEmbedResp
    if err := e.http.doPOST(ctx, "/api/embeddings",
        ollamaEmbedReq{Model: e.Model, Prompt: text}, &resp); err != nil {
        return nil, fmt.Errorf("ollama embed: %w", err)
    }
    return resp.Embedding, nil
}
```

**Estimated savings:** ~300 lines eliminated. OllamaEmbedder/OpenAIEmbedder →
30-40 lines each. OllamaLLMExtractor/OpenAILLMExtractor → 40-50 lines each.

**Caveat:** `OllamaLLMExtractor` has a double-unmarshal (chat response → JSON
content → ExtractionResult). This specific quirk stays in the extractor method.

---

## 10. HIGH — HTTP Handler Boilerplate Reduction

### [x] 10.1 Identical pattern in every handler: decode → validate → call → respond

**Status (this commit):** 🟢 Substantial conformance — 14 inline "required field missing" gates across `memory` / `task` / `reembed` / `retrieval` / `edge` / `ingest` migrated from `WriteError(400, ...)` to `WriteErrorWithCode(422, ..., "invalid_input", "<field>")` with per-field envelope (composite fields → empty `field=""` matching the `bad_json` / `empty_body` / `trailing_data` convention). 11 paired integration-test assertions updated to expect 422. Each migrated gate preserves `return nil` so the inline path stays bypass-`Wrap` (no double-IncErr, no double-status-map). Verified: `go vet ./src/...` → 0, `go build ./src/...` → 0, `go test -race -count=1 ./src/internal/server/...` → green. Remaining non-conformance surfaces: the documented §3.2 bespoke handler `/provenance` (intentionally not `s.Wrap`-registered) and `TaskExecutable` / `TaskNext` (pure GET, no body-decode step). The §3.2+§10 wire-contract tests + 3 `TestDomainError_*` unit tests already in place pin the §10 envelope shape end-to-end.

**Files affected:** All 12 `server/*/` HTTP shells (~60 handler methods).

**Problem:** Every handler repeats the same 6-line block:
```go
r.Body = http.MaxBytesReader(w, r.Body, httputil.MaxBodyBytes)
var req core.XxxRequest
if code, field, msg, ok := httputil.DecodeStrict(r.Body, &req); !ok {
    httputil.WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
    return
}
if req.Field == "" {
    httputil.WriteError(w, http.StatusBadRequest, "field required")
    return
}
```

This is ~10 lines per handler × 60 handlers = ~600 lines of identical boilerplate.

**Fix:** Introduce two generic helpers in `httputil`:

```go
// DecodeJSON reads r.Body (with MaxBytesReader cap), strict-decodes into *T,
// and returns a descriptive error suitable for WriteErrorWithCode on failure.
func DecodeJSON[T any](w http.ResponseWriter, r *http.Request) (T, error)

// RespondJSON wraps WriteJSON in one call — eliminates the w.Header()+w.WriteHeader()+Encode trio.
func RespondJSON(w http.ResponseWriter, status int, data any)
```

Then handlers become:
```go
func (s *HTTPService) HandleSearch(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
        return
    }
    req, err := httputil.DecodeJSON[core.SearchRequest](w, r)
    if err != nil {
        httputil.WriteError(w, http.StatusBadRequest, err.Error())
        return
    }
    if req.Query == "" {
        httputil.WriteError(w, http.StatusBadRequest, "query required")
        return
    }
    results, err := s.RetSvc.Search(r.Context(), req.Query, req.TopK)
    if err != nil {
        s.Metrics.IncErr()
        httputil.WriteError(w, http.StatusInternalServerError, err.Error())
        return
    }
    s.Metrics.IncSearch()
    httputil.RespondJSON(w, http.StatusOK, results)
}
```

**Estimated savings:** ~35% reduction in `server/` package volume (~350 lines).

**Note:** GET-only handlers (e.g. `HandleContradictions`, `HandleConnectedComponents`,
`HandleProvenance`) don't need `DecodeJSON` — they use `r.URL.Query()`. They stay
as-is but benefit from the `RespondJSON` simplification.

---

## 11. HIGH — Apple AMX Acceleration Fix (CGo Bindings)

### [ ] 11.1 `internal/vector/cosine_darwin.go` CGo entrypoint verification

**Files:**
- `src/internal/vector/cosine_darwin.go` — current
- `src/internal/vector/cosine.go` — pure-Go fallback

**Problem:** When the vector package was moved from `src/internal/algo/` to
`src/internal/vector/`, the CGo-based `cblas_sgemv` wrapper for batch dot
products was preserved — `BatchDotProducts` already calls into `batched_dot`
which wraps `cblas_sgemv`. However, the user notes that the Go linker may
lose context of the Accelerate framework when the CGo preamble is inside
a sub-package. Verifying this is critical.

**Current state (verified correct):** `cosine_darwin.go` has:
```cgo
#cgo LDFLAGS: -framework Accelerate
#include <Accelerate/Accelerate.h>

static inline void batched_dot(const float *V, const float *q,
                               int rows, int cols, float *dot) {
    cblas_sgemv(CblasRowMajor, CblasNoTrans, rows, cols,
                1.0f, V, cols, q, 1, 0.0f, dot, 1);
}
```
And the Go wrapper:
```go
func BatchDotProducts(query []float32, matrix []float32, rows, cols int, dot []float32) {
    C.batched_dot(
        (*C.float)(unsafe.Pointer(&matrix[0])),
        (*C.float)(unsafe.Pointer(&query[0])),
        C.int(rows), C.int(cols),
        (*C.float)(unsafe.Pointer(&dot[0])),
    )
}
```

**Verification:** Run `go test -bench=BenchmarkBatchDot ./src/internal/vector/...`
with `CGO_ENABLED=1` on Apple Silicon. If the CGo path is active, the benchmark
will show sub-millisecond times for large matrices. If the linker drops Accelerate,
the pure-Go fallback will be 5-15× slower.

**Potential issue (user report):** If the Go compiler doesn't see the CGo preamble
correctly, it may fall back to compiling the pure-Go `cosine.go` even on darwin
with cgo enabled. This can happen if:
- The build tag `//go:build darwin && cgo` doesn't match
- There's a blank line between the `*/` and `import "C"` directive

**Fix checklist:**
1. Verify `//go:build darwin && cgo` is exactly correct (no trailing spaces)
2. Verify no blank lines between `*/` and `import "C"`
3. Verify `CGO_ENABLED=1` in build scripts (`install.sh`, `Dockerfile`, CI)
4. Add `go test -bench=. -benchtime=100ms ./internal/vector/...` to CI
5. Benchmark should assert a minimum throughput (e.g. >100K dot-products/sec)
   to catch silent Accelerate degradation

**Important:** ARM NEON (128-bit SIMD) is NOT a substitute for Apple AMX.
AMX operates on 64-byte registers and is accessed exclusively through the
Accelerate.framework's BLAS bindings. Any attempt to replace CGo with Go
assembly using NEON intrinsics would be strictly slower for matrix operations.

---

## Priority Matrix

| Priority | Category | Effort | Impact |
|----------|----------|--------|--------|
| **✅ DONE** | §1.1-1.3: Shared error types + rejectSchemaConflict | — | ~100 LOC eliminated |
| **✅ DONE** | §1.4: NormalizeSlice[T] | Low | ✅ DONE — core.NormalizeSlice[T] generic adopted across 30+ service methods |
| **✅ DONE** | §2.1: Drop `_http` package suffix | Medium | ✅ DONE — all 4 `_http` suffixes dropped; 4 import aliases removed from server.go |
| **HIGH** | §2.2-2.3: Naming conventions | Low | ✅ DONE — BOTH complete |
| **HIGH** | §3.1-3.2: RouteProvider + BaseHTTPService | High | ✅ §3.1+§3.2 DONE — 12 for-loops collapsed + ~250 LOC via Wrap + mapStatus |
| **HIGH** | §8: Entity decomposition | High | 🟡 PARTIAL DONE — §8.1+§8.2 Type-Prep landed (5 slim types embed `core.Fact`; `core/slim_types_test.go` pins new wire shape; vet/build/race clean). ✅ §8.3 read-path switchover DONE — audit confirmed zero non-test production callers of the `X.AsEntity().ID|Category|Content|Embedding` roundtrip pattern (4-grep sweep across `src/`); the §8 NOTE/TODO godocs on Task/Goal/Episode/Evidence/Belief were dead-code warnings, now resolved. §8.4 `AsEntity()` removal still pending. Caller **note** (until §8.4): producers needing a slim→Entity reassembly should use `core.Compose(f.AsFact(), ev.AsEvidence(), ep.AsEpisode(), t.AsTask(), b.AsBelief())` rather than calling the unsafe `X.AsEntity()` bridges directly (which silently drops the 4-fact-band identity fields). |
| **✅ DONE** | §9: AI client unification | Medium | ✅ DONE — 6 clients collapsed to httpClient.doPOST; ~23 net LOC after helper + 215 LOC of test coverage |
| **✅ DONE** | §10: HTTP handler boilerplate | Medium | ✅ DONE — httputil.DecodeJSON[T] + RespondJSON + §3.2 Wrap routes *core.DomainError through WriteErrorWithCode; 15 POST handlers across 6 shells collapsed; 1 new end-to-end 422 wire-contract test (TestStore_MalformedJSONReturns422WithCodeField) pins {error, code:"invalid_input"} envelope; 2 stale-test fixes (TestTaskDep missing-field test data + TestStore_RejectsLargeBody status assertion widened 400→422 per §3.2+§10 wire evolution) |
| **HIGH** | §11: AMX CGo verification | Low | No code change, CI guard only |
| **MEDIUM** | §4.1-4.2: Comment cleanup | Low | 🟡 PARTIAL DONE — §4.1 server-side pkg-doc trim landed (11 server/*/* shells + server.go route registry: archaeology removed; §3.2+§8+§10 anchors preserved). §4.1 domain-side + §4.2 inline archaeology still pending. |
| **MEDIUM** | §5.1-5.3: Package organization | Medium-High | Structure clarity |
| **MEDIUM** | §6: serve.go wiring | Medium | ~50 LOC eliminated |
| **LOW** | §7.1-7.4: Misc | Low | Minor improvements |

---

## Execution Order

1. **§1.1, §1.2, §1.3** — Shared `ErrInvalidSchema` + `rejectSchemaConflict` + `isSchemaErr`
   (most egregious copy-paste, low risk, enables later steps)

2. **§2.1** — Normalize server package names (drop `_http` suffix)
   (safe Go rename, trivial with `gorename` or sed)

3. **§2.2, §2.3** — Standardize constructor naming (`New`) and field naming (`Svc`)
   (sed-friendly, low risk)

4. ~~**§3.1** — Add `RouteProvider` interface~~ ✅ DONE
   (12 compile-time assertions on every shell; mount() now iterates a single list)

5. **§1.4** — Add `NormalizeSlice[T]` and use everywhere
   (Go 1.18+ generics, safe transformation)

6. ~~**§9** — AI client unification (`httpClient` helper)~~ ✅ DONE
   (6 clients collapsed to httpClient.doPOST; ~23 net LOC + 215 LOC test coverage)

7. ~~**§10** — HTTP handler boilerplate (`DecodeJSON[T]` + `RespondJSON`)~~ ✅ DONE
   (was high payoff, depended on §2.1 + §3.1 for clean base) — 15 POST handlers across 6 shells
   (task, retrieval, memory, edge, ingest, reembed) collapsed to `httputil.DecodeJSON[T] + return err`.
   Net LOC delta is small because the boilerplate was already compact, but the
   wire contract is now unified: type-mismatch / unknown-field / MalformedJSON /
   TrailingData / MaxBytes-overflow all drop to 422 with `{error, code:"invalid_input", field}`
   via the §3.2 Wrap's `errors.As(*core.DomainError)` → `WriteErrorWithCode` path.
   New regression test `TestStore_MalformedJSONReturns422WithCodeField` exercises
   the cumulative DecodeJSON→Wrap→WriteErrorWithCode path on a real POST and
   pins the `code="invalid_input"` envelope exactly.

8. ~~**§3.2** — `BaseHTTPService` with `Wrap` pattern~~ ✅ DONE
   (depends on §10 for the handler simplification) — 11 shells + ~250 LOC eliminated, 9 shared tests pin regression coverage including the silent-bug CodeInvalidInput 400→422 fix.

9. **§8** — Entity decomposition (switch `store/` to slim types) ✅ §8.1+§8.2 (Type-Prep) DONE — anon-embed `core.Fact` in 5 slim types + new wire-shape regression test in `core/slim_types_test.go`. ✅ §8.3 (read-path switchover) DONE — audit confirmed zero non-test callers of the `X.AsEntity()` roundtrip pattern (4-grep sweep across `src/`); the §8 NOTE/TODO godocs were dead-code warnings, now resolved. 🟡 PENDING: §8.4 `AsEntity()` removal — the 5 unsafe bridges (Task / Goal / Episode / Evidence / Belief) can now be deleted in confidence (Fact.AsEntity() stays lossless; compression/SummaryNode bridge stays). Caller **note** (pre-§8.4): slim→Entity reassembly should use `core.Compose(f.AsFact(), ev.AsEvidence(), ep.AsEpisode(), t.AsTask(), b.AsBelief())` rather than calling `t.AsEntity()` directly.
   (high effort but high payoff, structural change)

10. **§4.1** ✅ — Comment cleanup (server-side pkg-doc trim landed; see spec-body + matrix row) | **§4.2** 🟡 PENDING — Comment cleanup (inline archaeology; defer to followup)
    (safe, improves readability)

11. **§5.1** — Split contradiction detectors
    (medium risk, structural)

12. **§5.2, §5.3** — Package re-organization
    (higher risk, import path updates)

13. **§6** — `serve.go` wiring simplification
    (depends on §3.1)

14. **§11** — AMX CGo verification
    (benchmark + CI guard, no code change)

15. **§7.1-7.4** — Miscellaneous fixes
    (independent, low risk)

---

## 12. HIGH — Architectural Foundations

These six items raise the project from "working code" to "production-grade
engineering." They add almost no functional behaviour but drastically improve
maintainability, testability, and onboarding velocity.

---

### [x] 12.1 Unified DI through constructors

**Status: ✅ DONE (verified).**

Every domain service receives dependencies via constructor (`New(db, vi, embedder)`).
No service reaches into a global registry or uses `init()` for wiring. The
`serverstate.Ref` uses `atomic.Pointer` for SIGHUP-driven config swaps — handlers
capture a snapshot at request entry, never read a global.

**Remaining gap:** `cli/serve.go` has 70+ lines of manual wiring.

**Fix (already covered by §6):** Replace manual wiring with a builder pattern.

```go
srv := server.NewBuilder(env).WireAll().Build()
```

---

### [x] 12.2 Unified error model (`errors.Is`/`errors.As` + typed errors)

**Status: ⚠️ PARTIAL.**

Good:
- All domain services use `fmt.Errorf("prefix: %w", err)` for error wrapping.
- `errors.Is` and `errors.As` are used in 14 places for sentinel checks.
- `store.Edge` has a sentinel `ErrPurgeEntityNotFound` following the Go convention.
- `auth` package has `ErrInvalidKey` and `ErrInsufficientScope` sentinels.

Bad:
- No central error taxonomy. `ErrInvalidSchema` is duplicated across `memory/` and `edge/`.
- Domain services return raw `fmt.Errorf` strings that HTTP shells must
  string-match: `strings.Contains(err.Error(), "not found")` in `task_service.go`.
- No error code enum — HTTP shells map errors to status codes via ad-hoc
  string inspection.

**Fix:**

1. **Central error taxonomy** — `src/internal/core/errors.go`:
```go
package core

// Sentinel errors for domain-level failure modes.
var (
    ErrNotFound       = errors.New("not found")
    ErrInvalidInput   = errors.New("invalid input")
    ErrSchemaConflict = errors.New("schema conflict")
    ErrUnauthorized   = errors.New("unauthorized")
)

// DomainError carries a machine-readable code for HTTP mapping.
type DomainError struct {
    Code    string  // "not_found", "invalid_input", "schema_conflict"
    Field   string  // optional: which field caused the error
    Message string  // human-readable
    Err     error   // wrapped cause
}

func (e *DomainError) Error() string { ... }
func (e *DomainError) Unwrap() error { return e.Err }
```

2. **HTTP error mapper** — `src/internal/server/errors.go`:
```go
func MapError(err error) (int, string) {
    var de *core.DomainError
    if errors.As(err, &de) {
        switch de.Code {
        case "not_found":        return 400, de.Message
        case "invalid_input":    return 400, de.Message
        case "schema_conflict":  return 409, de.Message
        case "unauthorized":     return 401, de.Message
        default:                 return 500, de.Message
        }
    }
    return 500, "internal error"
}
```

3. **Eliminate string-matching in handlers** — Replace:
```go
if strings.Contains(err.Error(), "not found") {
    httputil.WriteError(w, http.StatusBadRequest, err.Error())
}
```
with:
```go
status, msg := MapError(err)
httputil.WriteError(w, status, msg)
```

**Benefit:** Handlers become oblivious to error string content. New domain errors
are mapped in one place. Testing error paths becomes `errors.Is(err, core.ErrNotFound)`.

---

### [ ] 12.3 Clear domain / application / infrastructure separation

**Status: ⚠️ IMPLICIT — needs explicit layer boundaries.**

The three-layer split exists de facto but not de jure:

| Layer | Current location | Status |
|-------|-----------------|--------|
| **Domain** | `core/types.go` (interfaces), `*domain*/service.go` (logic) | ✅ Good |
| **Application** | `server/*/` (HTTP), `cli/*/` (CLI) | ✅ Good |
| **Infrastructure** | `store/` (SQLite), `ai/` (Ollama/OpenAI), `vector/` (index) | ✅ Good |

**Problem:** The boundary is violated in several places:

1. **Domain services contain raw SQL** — `graph/service.go` has 30+ lines of
   `db.Query(...)` inside `Verify()`. `task/service.go` has 50+ lines of
   recursive CTE SQL inside `getExecutableForGoal()` and `getExecutableGlobal()`.
   Domain services should call store functions, not write SQL.

2. **No `package doc` layer markers** — There's no file-level convention marking
   which layer a package belongs to. A new developer can't tell at a glance
   whether `retrieval/` is domain, application, or infrastructure.

**Fix:**

1. **Extract SQL from domain services** — Move `graph/service.go:Verify()` SQL to
   `store/graph_verify.go`. Move `task/service.go:getExecutable*()` SQL to
   `store/task_executable.go`. Domain services become pure orchestration.

2. **Add layer markers** — Each package's `doc.go` starts with:
   ```go
   // Package graph — domain layer: graph analytics service.
   // Depends on: store (infrastructure), core (domain interfaces).
   // Used by: server/graph (application), cli/graph (application).
   ```

3. **Enforce import direction** — Domain never imports application. Infrastructure
   never imports domain (except `core` interfaces). Add a `golangci-lint` rule
   (`depguard`) to prevent future violations.

---

### [x] 12.4 Minimise global state

**Status: ✅ DONE (verified).**

P0 work eliminated the `ActiveSchema()` singleton. Current state:
- `serverstate.Ref` — `atomic.Pointer[State]` for config, safe concurrent swaps.
- `env.EnvManager` — `atomic.Pointer[Env]` for runtime, no package-level `var`.
- `metrics.Metrics` — constructed in `main.go`, passed by pointer; no global registry.
- `cli/root.go` — `noopPreRun` is a package-level `var` but it's a constant function
  literal, not mutable state.
- `ai/defaultBackoffs` — package-level `var` but read-only after init (immutable slice).

**No action needed.** Global mutable state is eliminated. The `sync.Once` in
`metrics/worker.go` is instance-scoped (worker owns its `once`), not package-global.

---

### [x] 12.5 Unified logging interface

**Status: ✅ DONE.**

`slog` is used everywhere (74 call sites) as a concrete import:
```go
slog.Info("server ready", "port", cfg.Port)
slog.Error("panic", "err", rec)
slog.Warn("retention archive", "id", id, "err", uerr)
```

**Problems:**
1. **Not testable** — You can't inject a test logger. Every `slog.Info` writes to
   stderr during tests.
2. **No interface** — Packages can't swap the logger implementation. If you want
   JSON output, you must configure the global `slog.SetDefault()` before any
   goroutine starts.
3. **Inconsistent call sites** — Some places use `slog`, others use `log.Printf`
   (`server/server.go:271` uses `log.Fatalf`). No unified policy.

**Fix:**

1. **Logger interface** — `src/internal/core/logger.go`:
```go
package core

type Logger interface {
    Debug(msg string, args ...any)
    Info(msg string, args ...any)
    Warn(msg string, args ...any)
    Error(msg string, args ...any)
}
```

2. **Slog adapter** — `src/internal/logging/slog.go`:
```go
package logging

type SlogLogger struct{ handler slog.Handler }

func NewSlogLogger(level slog.Level, w io.Writer) *SlogLogger { ... }
func (l *SlogLogger) Info(msg string, args ...any) { ... }
// ... Debug, Warn, Error
```

3. **Inject logger via DI** — Add `Logger core.Logger` to every service struct.
   Default to `logging.NewSlogLogger(slog.LevelInfo, os.Stderr)` in constructors.

4. **Test logger** — `logging/test.go` provides a `TestLogger` that captures
   messages in a buffer for assertions:
```go
func TestLogger(t *testing.T) *TestLogger
func (tl *TestLogger) Messages() []string
```

**Migration path:** Add `Logger` to constructors as the LAST parameter with a
`slog`-based default. Existing `slog.Info(...)` call sites are replaced
incrementally — the global `slog` still works during the transition.

---

### [ ] 12.6 Unified component lifecycle (`Start(ctx)`, `Stop(ctx)`)

**Status: ❌ NOT DONE.**

Components start and stop in ad-hoc ways:
- HTTP server: `httpSrv.ListenAndServe()` in a goroutine, `httpSrv.Shutdown()` on signal.
- GC loop: `gcCtx, gcCancel` + goroutine in `server.Serve()`.
- Metrics worker: created in `cli/env/env.go:EnsureDB()` via `metrics.NewAsyncMetricsWorker`,
  stopped in `env.Close()`.
- SIGHUP loop: `safego.Go(env.Ctx, "sighup-reload", ...)` — tied to `env.Ctx`.
- CLI agent loop: `orchestrator/service.go` spawns goroutines per task.

No component implements a common interface. If you add a new background goroutine,
there's no standard place to wire its start or stop.

**Fix:**

1. **Lifecycle interface** — `src/internal/core/lifecycle.go`:
```go
package core

type Component interface {
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
}
```

2. **Lifecycle manager** — `src/internal/lifecycle/manager.go`:
```go
package lifecycle

type Manager struct {
    components []core.Component
}

func (m *Manager) Register(c core.Component) { ... }

// Start calls Start(ctx) on every registered component in order.
// If any Start fails, stops already-started components in reverse order.
func (m *Manager) Start(ctx context.Context) error { ... }

// Stop calls Stop(ctx) on every component in reverse order.
// Errors are collected, not short-circuited — every component gets a chance to stop.
func (m *Manager) Stop(ctx context.Context) error { ... }
```

3. **Wire components** — Each long-lived goroutine becomes a `Component`:
```go
// GCComponent implements lifecycle.Component
type GCComponent struct {
    svc *retention.Service
    cfg core.RetentionPolicy
}
func (c *GCComponent) Start(ctx context.Context) error {
    go c.svc.Run(ctx, c.cfg)
    return nil
}
func (c *GCComponent) Stop(ctx context.Context) error {
    // ctx is already cancelled by the manager before Stop is called
    return nil
}

// HTTPComponent wraps *http.Server
type HTTPComponent struct{ srv *http.Server }
func (c *HTTPComponent) Start(ctx context.Context) error {
    go func() {
        if err := c.srv.ListenAndServe(); err != http.ErrServerClosed {
            slog.Error("http", "err", err)
        }
    }()
    return nil
}
func (c *HTTPComponent) Stop(ctx context.Context) error {
    return c.srv.Shutdown(ctx)
}
```

4. **Use in `server.Serve()`** — Replace the 20-line signal handling block with:
```go
mgr := lifecycle.NewManager()
mgr.Register(NewHTTPComponent(httpSrv))
mgr.Register(NewGCComponent(retentionSvc, cfg.Retention))
mgr.Register(NewSIGHUPComponent(sighupHandler))

ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer cancel()

if err := mgr.Start(ctx); err != nil {
    return err
}
<-ctx.Done()
return mgr.Stop(context.Background()) // fresh ctx for graceful shutdown
```

**Benefit:** Adding a background worker becomes a 3-line registration instead of
copy-pasting signal handling + goroutine + defer logic. All components get
cancellation propagation for free. Shutdown order is explicit and auditable.

---

## Priority Matrix

| Priority | Category | Effort | Impact |
|----------|----------|--------|--------|
| **✅ DONE** | §1.1-1.3: Shared error types + rejectSchemaConflict | — | ~100 LOC eliminated |
| **✅ DONE** | §1.4: NormalizeSlice[T] | Low | ✅ DONE — core.NormalizeSlice[T] generic adopted across 30+ service methods |
| **✅ DONE** | §2.1: Drop `_http` package suffix | Medium | ✅ DONE — all 4 `_http` suffixes dropped; 4 import aliases removed from server.go |
| **HIGH** | §2.2-2.3: Naming conventions | Low | ✅ DONE — BOTH complete |
| **HIGH** | §3.1-3.2: RouteProvider + BaseHTTPService | High | ✅ §3.1+§3.2 DONE — 12 for-loops collapsed + ~250 LOC via Wrap + mapStatus |
| **HIGH** | §8: Entity decomposition | High | 🟡 PARTIAL DONE — §8.1+§8.2 Type-Prep landed (5 slim types embed `core.Fact`; `core/slim_types_test.go` pins new wire shape; vet/build/race clean). ✅ §8.3 read-path switchover DONE — audit confirmed zero non-test production callers of the `X.AsEntity().ID|Category|Content|Embedding` roundtrip pattern (4-grep sweep across `src/`); the §8 NOTE/TODO godocs on Task/Goal/Episode/Evidence/Belief were dead-code warnings, now resolved. §8.4 `AsEntity()` removal still pending. Caller **note** (until §8.4): producers needing a slim→Entity reassembly should use `core.Compose(f.AsFact(), ev.AsEvidence(), ep.AsEpisode(), t.AsTask(), b.AsBelief())` rather than calling the unsafe `X.AsEntity()` bridges directly (which silently drops the 4-fact-band identity fields). |
| **✅ DONE** | §9: AI client unification | Medium | ✅ DONE — 6 clients collapsed to httpClient.doPOST; ~23 net LOC after helper + 215 LOC of test coverage |
| **✅ DONE** | §10: HTTP handler boilerplate | Medium | ✅ DONE — httputil.DecodeJSON[T] + RespondJSON + §3.2 Wrap routes *core.DomainError through WriteErrorWithCode; 15 POST handlers across 6 shells collapsed; 1 new end-to-end 422 wire-contract test (TestStore_MalformedJSONReturns422WithCodeField) pins {error, code:"invalid_input"} envelope; 2 stale-test fixes (TestTaskDep missing-field test data + TestStore_RejectsLargeBody status assertion widened 400→422 per §3.2+§10 wire evolution) |
| **HIGH** | §11: AMX CGo verification | Low | No code change, CI guard only |
| **✅ DONE** | §12.2: Central error taxonomy | — | Eliminates string-matching in handlers |
| **HIGH** | §12.5: Unified logging interface | Medium | ✅ DONE — core.Logger + SlogLogger + TestLogger |
| **HIGH** | §12.6: Component lifecycle | High | Predictable start/stop, less signal-handling boilerplate |
| **MEDIUM** | §4.1-4.2: Comment cleanup | Low | 🟡 PARTIAL DONE — §4.1 server-side pkg-doc trim landed (11 server/*/* shells + server.go route registry: archaeology removed; §3.2+§8+§10 anchors preserved). §4.1 domain-side + §4.2 inline archaeology still pending. |
| **MEDIUM** | §5.1-5.3: Package organization | Medium-High | Structure clarity |
| **MEDIUM** | §6: serve.go wiring | Medium | ~50 LOC eliminated |
| **MEDIUM** | §12.3: Explicit layer boundaries | Medium | Enforced import direction |
| **LOW** | §7.1-7.4: Misc | Low | Minor improvements |
| **✅ DONE** | §12.1: DI through constructors | — | Already verified |
| **✅ DONE** | §12.4: Minimise global state | — | Already verified |

---

## Execution Order

1. ~~**§12.2** — Central error taxonomy + `DomainError` type~~ ✅ DONE
   ~~(foundational — every subsequent step benefits from typed errors)~~

2. ~~**§1.1, §1.2, §1.3** — Shared `ErrInvalidSchema` + `rejectSchemaConflict` + `isSchemaErr`~~ ✅ DONE
   ~~(now trivial with the central error taxonomy in place)~~

3. ~~**§12.5** — Unified logging interface~~ ✅ DONE
   ~~(injectable logger, test logger — enables better test coverage in later steps)~~

4. ~~**§2.1** — Normalize server package names (drop `_http` suffix)~~ ✅ DONE

5. ~~**§2.2, §2.3** — Standardize constructor naming (`New`) and field naming (`Svc`)~~ ✅ DONE

6. **§3.1** — Add `RouteProvider` interface

7. ~~**§1.4** — Add `NormalizeSlice[T]` and use everywhere~~ ✅ DONE

8. **§12.6** — Component lifecycle (`Start`/`Stop`)
   (replaces ad-hoc goroutine management in `server.Serve()`)

9. ~~**§9** — AI client unification (`httpClient` helper)~~ ✅ DONE — 6 clients collapsed; ~23 net LOC; +7 contract tests

10. ~~**§10** — HTTP handler boilerplate (`DecodeJSON[T]` + `RespondJSON`)~~ ✅ DONE — httputil.DecodeJSON[T] + RespondJSON collapsed 15 POST handlers; Wrap now routes `*core.DomainError` through WriteErrorWithCode; end-to-end 422 wire contract pinned by `TestStore_MalformedJSONReturns422WithCodeField`.

11. ~~**§3.2** — `BaseHTTPService` with `Wrap` pattern~~ ✅ DONE — 11 shells + ~250 LOC eliminated; silent-bug fixed; 9 regression tests in `shared/base_test.go`.

12. **§12.3** — Explicit layer markers + extract SQL from domain services

13. **§8** — Entity decomposition (switch `store/` to slim types) ✅ §8.1+§8.2 (Type-Prep) DONE — anon-embed `core.Fact` in 5 slim types + new wire-shape regression test in `core/slim_types_test.go`. ✅ §8.3 (read-path switchover) DONE — audit confirmed zero non-test callers of the `X.AsEntity()` roundtrip pattern (4-grep sweep across `src/`); the §8 NOTE/TODO godocs were dead-code warnings, now resolved. 🟡 PENDING: §8.4 `AsEntity()` removal — the 5 unsafe bridges (Task / Goal / Episode / Evidence / Belief) can now be deleted in confidence (Fact.AsEntity() stays lossless; compression/SummaryNode bridge stays). Caller **note** (pre-§8.4): slim→Entity reassembly should use `core.Compose(f.AsFact(), ev.AsEvidence(), ep.AsEpisode(), t.AsTask(), b.AsBelief())` rather than calling `t.AsEntity()` directly.

14. **§6** — `serve.go` wiring simplification (builder pattern)

15. **§4.1** ✅ — Comment cleanup (server-side pkg-doc trim landed; see spec-body + matrix row) | **§4.2** 🟡 PENDING — Comment cleanup (inline archaeology; defer to followup)

16. **§5.1** — Split contradiction detectors

17. **§5.2, §5.3** — Package re-organization

18. **§11** — AMX CGo verification

19. **§7.1-7.4** — Miscellaneous fixes

---

## Verification Checklist

After each step:
- [ ] `go vet ./src/...` — zero warnings
- [ ] `go test -race ./src/...` — all tests pass
- [ ] `go build ./src/...` — compiles clean
- [ ] Import alias diff in `server/server.go` — reduced, not grown
- [ ] No net-new exported symbols without matching test coverage
- [ ] (After §11) `CGO_ENABLED=1 go test -bench=BenchmarkBatchDot ./internal/vector/...` passes
