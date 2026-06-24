# TODO.md — Open Engineering Work

> Distilled from `out.txt` (538-line code review).
> Each entry: priority, where the problem lives, what to do, suggested code shape.
> Mark `[x]` when shipped; keep `[ ]` while open.
>
> Note: this list is regenerated from `out.txt` content only — earlier shipped
> work (round 5 hardening pass, EnvManager, BytesToFloat32Safe, ResilientClient
> wiring, cyclic-task DFS in `BuildNode`, etc.) is NOT in this TODO; it lives
> in `CHANGELOG.md [Unreleased]`. Use this file for what's still actionable.

Priority legend (matches `out.txt` severity buckets):

- 🔴 **P0** — crash / data corruption / no-recover failure mode.
- 🟠 **P1** — race / goroutine / OS-resource leak.
- 🟡 **P2** — performance / UX degradation.
- 🟢 **P3** — code hygiene / docs.

---

## 1. 🔴 State management (SIGHUP / reload)

### [ ] Cross-state transaction consistency on reload
**Where:** `src/internal/serverstate/state.go::StateManager` and `src/internal/server/server.go::Serve`.
**Problem:** `atomic.Pointer[ServerState]` swaps the pointer atomically, but a handler that already captured `state := sm.Load()` and entered `/ingest` runs against the OLD schema while parallel requests see the NEW one. Worse: the in-flight tx commits under a schema that may already have rejected the same payload through a sibling request.
**Fix shape (option A — generation stamp + re-check at commit):**

```go
type ServerState struct {
    Generation            int64                                // bumped on every Reload
    AllowedCategories     map[string]bool
    AllowedRelations      map[string]bool
    DepthCeiling          int
    MaxRetrievedNodes     int
    RankingWeight         core.RankingWeight
    Reranker              core.Reranker
}

type StateManager struct {
    gen  atomic.Int64
    ptr  atomic.Pointer[ServerState]
}

func (sm *StateManager) Reload(s *ServerState) {
    s.Generation = sm.gen.Add(1)
    sm.ptr.Store(s)
}

// handler:
state := sm.Load()
ctx := r.Context()
ctx = context.WithValue(ctx, genKey{}, state.Generation)
... do work, write tx ...
if cur := sm.Load().Generation; cur != state.Generation {
    // New schema arrived mid-request — surface 409 Conflict or auto-retry once.
    return writeJSON(w, 409, "schema changed mid-request; please retry")
}
```

**Fix shape (option B — drain-then-swap):** close the listener, increment a `pendingRequests` counter on entry, decrement on exit, and only `Store()` once counter hits 0. Take a `sync.RWMutex` instead of `atomic.Pointer` so the swap holds the write lock until drain completes.

### [ ] Deep-clone map fields on State construction
**Where:** `src/internal/serverstate/state.go::State.New`.
**Problem:** `New` replaces nil maps with empty maps but does NOT deep-clone provided maps. If a caller mutates `state.AllowedCategories` after `Store()`, handlers reading the same map can race with that write — `fatal error: concurrent map read and map write`, unrecoverable.

**Fix shape (already partially addressed; verify + extend):**

```go
func New(schema core.SchemaConfig, depthCeiling, maxRetrieved int, ranking core.RankingWeight, reranker core.Reranker) *State {
    s := &State{
        Schema:             schema,
        ValidCategories:    cloneBoolMap(schema.AllowedCategories),
        ValidRelationTypes: cloneBoolMap(schema.AllowedRelations),
        DepthCeiling:       depthCeiling,
        MaxRetrievedNodes:  maxRetrieved,
        RankingWeight:      ranking,
        Reranker:           reranker,
    }
    return s
}

func cloneBoolMap(m map[string]bool) map[string]bool {
    if m == nil {
        return map[string]bool{}
    }
    out := make(map[string]bool, len(m))
    for k, v := range m {
        out[k] = v
    }
    return out
}
```

Audit any other `*Manager.Store` / `*Manager.Reload` paths for shallow-copy aliases.

---

## 2. 🔴 Vector index concurrency

### [ ] Run `Search` and `SearchBatch` under `RLock`, not snapshot-and-unlock
**Where:** `src/internal/vector/inmemory.go::Search` and `SearchBatch`.
**Problem:** current implementation snapshots the matrix before compute. For a 50k-entity × 768-dim graph, that's a 200 KB copy per search — a hard cap on throughput. The alternative (release the lock before compute) risks `fatal error: concurrent map iteration and map write` when a parallel `Store / Remove` reallocates `flatMatrix` while `cblas_sgemv` reads it (`&flatMatrix[0]` invalidates after `append`).
**Fix shape:**

```go
func (idx *InMemoryVectorIndex) Search(_ context.Context, query []float32, topK int) ([]string, error) {
    if len(query) != idx.cols {
        return nil, fmt.Errorf("%w: got %d, want %d", ErrInvalidQueryDim, len(query), idx.cols)
    }
    idx.mu.RLock()
    defer idx.mu.RUnlock()
    n := len(idx.entries)
    if n == 0 {
        return nil, nil
    }
    if len(idx.flatMatrix) != n*idx.cols {
        return nil, fmt.Errorf("%w: matrix has %d, expected %d",
            ErrMatrixCorrupted, len(idx.flatMatrix), n*idx.cols)
    }
    dots := getDots(n)
    BatchDotProducts(query, idx.flatMatrix, n, idx.cols, dots)
    // embeddings are stored as unit vectors; no per-row sqrt needed.
    return topKFromDots(dots, topK, idx.entries), nil
}
```

For `SearchBatch`, do the same — acquire `RLock` once, iterate over the query slice, return. The 2 ms the lock is held for `cblas_sgemv` is far less expensive than the 200 KB alloc it displaces.

### [ ] Strict dimension validation before any CGO call
**Where:** `src/internal/vector/cosine_darwin.go::{VectorNorm, CosineSimilarity, BatchDotProducts}`.
**Problem:** a stale BLOB with `len != dim*4` causes `cblas_sgemv` to read past the slice end → `SIGSEGV` with no `recover()`. Validate before the CGO call.
**Fix shape:** introduce sentinel errors and panic-replace with `return nil, ErrInvalidQueryDim`/`ErrMatrixCorrupted`. The panic-on-bad-input contract still survives as a defensive last-line check inside the function, but the call site must check the returned error first.

```go
// index.go (public surface)
var (
    ErrInvalidQueryDim  = errors.New("vector: query dimension mismatch with index")
    ErrMatrixCorrupted  = errors.New("vector: flat matrix size != N * dim")
)

// cosine_darwin.go
func BatchDotProducts(query []float32, matrix []float32, rows, cols int, dot []float32) {
    if len(query) != cols {
        panic(fmt.Sprintf("%s: query has %d, expected %d", ErrInvalidQueryDim, len(query), cols))
    }
    if len(matrix) != rows*cols {
        panic(fmt.Sprintf("%s: have %d, expected %d", ErrMatrixCorrupted, len(matrix), rows*cols))
    }
    C.Cblas_sgemv(C.CblasRowMajor, C.CblasNoTrans, C.int(cols), C.int(rows),
        ... matrix, query, dot)
}
```

The CGO call site can still surface a Go-level error; the in-function panic is reachable only when callers bypass the public `Search`.

### [ ] `sync.Pool` size discipline for `dotPool` / `intBufPool`
**Where:** `src/internal/vector/inmemory.go::dotPool`, `intBufPool`.
**Problem:** `pool.Get().([]float32)` may return a slice whose `cap` is smaller than the current `n`; the caller appends, then `Put`s back a slice that lost its intended cap. Net effect: the pool degrades into a per-size array churn instead of amortising allocations.
**Fix shape:** keep one hard-cap pool and drop the slice back to that ceiling on `Put`:

```go
const maxSearchN = 50_000

var (
    dotPool = sync.Pool{
        New: func() any {
            s := make([]float32, 0, maxSearchN)
            return &s
        },
    }
    intPool = sync.Pool{
        New: func() any {
            s := make([]int, 0, maxSearchN)
            return &s
        },
    }
)

func getDots(n int) []float32 {
    p := dotPool.Get().(*[]float32)
    if cap(*p) < n {
        return make([]float32, n) // hot path: ceiling still bigger than n
    }
    return (*p)[:n]
}

func putDots(d []float32) {
    if cap(d) != maxSearchN {
        return
    }
    dotPool.Put(&d)
}
```

---

## 3. 🟠 AI client (`ResilientClient`) retry-path safety

### [ ] Never defer `resp.Body.Close()` at the outer `Do()` scope
**Where:** `src/internal/ai/client.go::ResilientClient.Do`.
**Problem:** when `inner.Do(req)` returns `(nil, err)` (e.g. `connect: connection refused` or TLS handshake timeout), any `defer resp.Body.Close()` at the outer scope dereferences nil and crashes the process — `runtime error: invalid memory address or nil pointer dereference`. This will tear down the ingest goroutine.
**Fix shape:** close bodies only inside the `err == nil` branch; never defer a body close at the outer scope.

```go
for i := 0; i < attempts; i++ {
    if i > 0 {
        if err := ctx.Err(); err != nil { return nil, err }
        if req.GetBody != nil {
            body, err := req.GetBody()
            if err != nil { return nil, fmt.Errorf("retry: get body: %w", err) }
            req.Body = body
        }
    }
    c := req.Clone(ctx)
    resp, err := inner.Do(c)
    if err != nil {
        lastErr = err
        if ctx.Err() != nil { return nil, ctx.Err() }
        if !backoffSleep(ctx, backoffFor(backoffs, i)) { return nil, ctx.Err() }
        continue
    }
    if resp.StatusCode == 429 || resp.StatusCode >= 500 {
        // Drain so keep-alive can be reused.
        _, _ = io.Copy(io.Discard, resp.Body)
        _ = resp.Body.Close()
        lastErr = fmt.Errorf("HTTP %d (transient)", resp.StatusCode)
        if ctx.Err() != nil { return nil, ctx.Err() }
        if !backoffSleep(ctx, backoffFor(backoffs, i)) { return nil, ctx.Err() }
        continue
    }
    return resp, nil
}
return nil, lastErr
```

### [ ] Drain 5xx response bodies on retry to keep TCP alive
**Where:** same — `Do` retry branch.
**Problem:** if we don't drain before `Body.Close()`, the connection is reset rather than returned to the pool — small GPU, big TCP.
**Fix shape:** same as above; `io.Copy(io.Discard, resp.Body)` before `Close`.

---

## 4. 🟠 Ingestion worker batch resume on context cancellation

### [ ] Checkpoint partial batches on `ctx` cancellation
**Where:** `src/internal/ingestion/dialog.go::MemoryWorker`.
**Problem:** when the parent ctx cancels mid-batch, the in-flight `ProcessDialogWithProvenance` continues until its LLM call resolves. Items already committed stay; items in-flight are lost. On restart, the channel may not redeliver — duplicate entities and/or unprocessed-but-already-committed window.
**Fix shape:** persist a per-worker checkpoint.

```go
type IngestionCheckpoint struct {
    LastCommittedIndex int64     `json:"last_committed_index"`
    LastCommittedAt    time.Time `json:"last_committed_at"`
    WorkerID           string    `json:"worker_id"`
}

func MemoryWorker(ctx context.Context, ckptPath string, ..., ch <-chan core.MemoryMessage) {
    ckpt, _ := loadCheckpoint(ckptPath)
    defer saveCheckpoint(ckptPath, ckpt)
    for msg := range ch {
        ckpt.LastCommittedIndex++
        ... commit logic
        ckpt.LastCommittedAt = time.Now().UTC() // TODO 9.1 normalization
    }
}
```

### [ ] Drain the channel on ctx cancel before returning
**Where:** same — `MemoryWorker` exit path.
**Problem:** items already pushed onto the channel but not yet dispatched to the sem-bounded goroutine pool are dropped silently on `ctx.Done()`. A user who queued 1k messages then SIGINT loses visibility into which ones made it.
**Fix shape:** on ctx cancel, propagate `ctx.Err()` to in-flight goroutines via the same ctx, then snapshot any unprocessed channel items into a side file (`/var/lib/hermem/pending.jsonl`).

```go
go func() {
    <-ctx.Done()
    // Spawn a goroutine to drain ch into a side queue.
    pending := make([]core.MemoryMessage, 0, len(ch))
    for msg := range ch {
        pending = append(pending, msg)
    }
    savePendingQueue(pendingPath, pending)
}()
```

---

## 5. 🟠 HTTP server middleware

### [ ] Always-on `RecoveryMiddleware` wrapping every entry point
**Where:** `src/internal/server/server.go::Serve`.
**Problem:** Go's `net/http` recovers from handler panics but in a way that closes the TCP connection without returning a 500. Production multi-agent swarms will sit waiting for the dead connection instead of falling to their retry path.
**Fix shape:** install `RecoveryMiddleware` as the outermost wrap; verify order is `Recovery → Slog → RequestID → APIKey → MaxBytes → handler`.

```go
var handler http.Handler = s.Mux()
handler = RecoveryMiddleware(handler)                                 // outermost
handler = SlogMiddleware(handler)
handler = RequestIDMiddleware(APIKeyMiddleware(cfg.APIKey)(handler))
handler = MaxBytesMiddleware(httputil.MaxBodyBytes)(handler)
handler = server.TimeoutMiddleware(120 * time.Second)(handler)        // if not already
```

Ensure CLI smoke tests (`src/internal/cli/cli_test.go`) also run server-mode in a test harness to verify the chain compiles.

### [ ] `httputil.DecodeStrict` contract: handler MUST close body on error
**Where:** `src/internal/httputil/httputil.go::DecodeStrict` (docstring) + every call site.
**Problem:** when `r.Body = http.MaxBytesReader(...)` triggers, `json.NewDecoder.Decode` returns a sentinel error. If the handler logs and returns without `io.Copy(io.Discard, r.Body)` and `r.Body.Close()`, the connection leaks into `CLOSE_WAIT` → `ulimit -n` saturation under load.
**Fix shape:** document the contract on `DecodeStrict`, then add a `defer` wrapper at the call sites:

```go
// In every /store / /ingest / /edge handler that calls DecodeStrict:
func safeBodyClose(r *http.Request) {
    _, _ = io.Copy(io.Discard, r.Body)
    _ = r.Body.Close()
}

// at every handler entry:
defer safeBodyClose(r)
```

### [ ] Per-request timeout middleware
**Where:** `src/internal/server/middleware.go` (new function).
**Problem:** a slow LLM-side `/response` request can pin a worker goroutine indefinitely without server-side cancellation. Currently the `WriteTimeout: 120 * time.Second` on `http.Server` covers reads + write completion but DOES NOT abort a hung streaming handler.
**Fix shape:**

```go
func TimeoutMiddleware(d time.Duration) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            ctx, cancel := context.WithTimeout(r.Context(), d)
            defer cancel()
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}
```

---

## 6. 🟡 SQLite under load

### [ ] Busy-timeout PRAGMA + the right tx-isolation for TOCTOU writes
**Where:** `src/internal/store/init.go::InitDB`, `src/internal/store/edge.go::AddEdge`.
**Problem:** handler-side retries can re-enter the same write path under MESI invalidation storms; without `BEGIN IMMEDIATE` or an explicit busy-timeout, SQLite may upgrade mid-tx and fail with `SQLITE_BUSY`, which the existing `isSQLiteBusyError` retry loop in `dialog.go::processOneItem` already handles — but only on the ingest path. Plugin-driven `/edge` writes do not.
**Fix shape:** add `PRAGMA busy_timeout = 5000` to init (or DSN `_busy_timeout=5000`), and switch write tx to `LevelSerializable` so SQLite acquires the write lock up front.

```sql
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA busy_timeout = 5000;
PRAGMA foreign_keys = ON;
```

```go
// edge.go:
tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
if err != nil { return fmt.Errorf("begin edge tx: %w", err) }
```

### [ ] Per-test random DSN for in-memory DB
**Where:** `src/internal/store/helpers_test.go::memDB`.
**Problem:** `file::memory:?cache=shared` makes the DB process-global. `go test -race ./...` runs goroutines against the same instance — corrupts fixtures.
**Fix shape (use a unique DSN per test goroutine):**

```go
func memDB(t *testing.T) *sql.DB {
    t.Helper()
    var b [8]byte
    _, _ = rand.Read(b[:])
    dsn := fmt.Sprintf("file:memdb-%s?mode=memory&cache=shared&_busy_timeout=5000",
        hex.EncodeToString(b[:]))
    db, err := sql.Open("sqlite3", dsn)
    if err != nil {
        t.Fatalf("memDB open: %v", err)
    }
    t.Cleanup(func() { _ = db.Close() })
    return db
}
```

---

## 7. 🟡 Contradiction heuristic — linguistic limitation

### [ ] Stem / lemmatise before the antonym scan
**Where:** `src/internal/ingestion/dialog.go::IsIngestionContradiction`.
**Problem:** Russian inflection (`любит` / `разлюбил`, `не очень любит`) defeats the 45-pair antonym table → false merge → corrupted embedding.
**Fix shape (option A — pure-Go stemmer + tokenize):**

```go
import "github.com/clipperhouse/stemmer/russian" // snowball-stemmer alt

func IsIngestionContradiction(a, b string) bool {
    aLem := strings.Join(russian.StemWords(tokenize(a)), " ")
    bLem := strings.Join(russian.StemWords(tokenize(b)), " ")
    negWords := []string{"not", "don't", "doesn't", "isn't", "aren't", "won't", "can't", "never", "no", "hate", "dislike",
        "не", "нет", "разлюбил", "ненавидит", "никогда"}
    for _, n := range negWords {
        if strings.Contains(aLem, n) != strings.Contains(bLem, n) {
            return true
        }
    }
    return false
}
```

**Fix shape (option B — fall back to LLM quick-check):**

```go
type LLMExtractor interface {
    ExtractEntities(ctx context.Context, dialog string) (*core.ExtractionResult, error)
    QuickContradictionCheck(ctx context.Context, existing, incoming string) (bool, error)
}
```

The `QuickContradictionCheck` returns a single bool + ~50 tokens consumed — cheap to run in the ingest path.

---

## 8. 🟡 Performance

### [ ] Record baseline benchmarks
**Where:** `src/internal/vector/inmemory.go` (current bench suite lives in `vector_bench_test.go`).
**Problem:** followups need a regression baseline. Re-run benchmarks on the current commit and pin the numbers in `CHANGELOG.md [Unreleased]`.
**Fix shape:**

```bash
go test -count=1 -bench=BenchmarkInMemorySearch -benchmem -count=3 -benchtime=20x ./src
go test -count=1 -tags=sqlite_vec -bench=BenchmarkSqliteVecSearch -benchmem -count=3 -benchtime=20x ./src
```

Goal: per search N=10000 ≤ 2.5 ms on Apple M1 Pro (768-dim, in-memory backend).

### [ ] SQLite PRAGMA `synchronous=NORMAL` only after WAL — verify and document
**Where:** `src/internal/store/init.go::InitDB` PRAGMA order.
**Problem:** `synchronous=NORMAL` without WAL can corrupt on power loss. The PRAGMA order must be `journal_mode=WAL → synchronous=NORMAL`.
**Fix shape:** assertion in `InitDB`'s test suite (or run-time INFO-level slog):

```go
var mode string
db.QueryRow("PRAGMA journal_mode").Scan(&mode)
if mode != "wal" {
    slog.Warn("InitDB: WAL not active; synchronous=NORMAL may be unsafe")
}
```

---

## 9. 🟢 Plugin / docs

### [ ] Bump `skills/hermem/SKILL.md` front-matter `version` to current release
**Where:** `skills/hermem/SKILL.md` (front matter).
**Problem:** the `version: 0.1.0` slug is stale post the round-5 hardening pass (`ResilientClient`, `EnvManager`, `BytesToFloat32Safe`).
**Fix shape:** bump to match the next release tag (currently `[Unreleased]`; once cut, `v0.2.0` is the candidate).

### [ ] Drop residual flat-name aliases that survived the Cobra migration
**Where:** `src/internal/cli/...`.
**Problem:** any old flat-name verb that's still registered is a regression against the §4.1 breaking-change note.
**Fix shape:** grep for `Register(\"store\")` etc. — they should all be gone. If a leftover exists, remove it.

```bash
rg "Init\(\) { RootCmd\.AddCommand\(newXxxCmd\) }" src/internal/cli/
rg "Use:\\s*\"store\"|Use:\\s*\"ingest\"|Use:\\s*\"query\"" src/internal/cli/
```

### [ ] Reconcile `USAGE.md §10` schema table against actual migrations
**Where:** `USAGE.md` (§10).
**Problem:** add `weight REAL` row on edges (`006_weighted_edges.sql`); add `degree` and `priority` rows on entities (`005_centrality.sql`, `007_task_priorities.sql`).

```go
// entities table adds:
| `degree`   | INTEGER | `0` default; auto-maintained by SQL triggers on edges. Powers `log10(1+degree)` centrality. |
| `priority` | INTEGER | `0` default; ordered DESC by `task/list`, `task/executable`, `ExecutionPlan`. |

// edges table adds:
| `weight`   | REAL    | `1.0` default; added in `006_weighted_edges.sql`. Read with `COALESCE(weight, 1.0)`. |
```

---

## 10. 🟢 Final verification checklist

- [ ] `gofmt -l ./src` returns empty
- [ ] `go vet ./src/...` clean
- [ ] `go build -o /tmp/hermem ./src` succeeds
- [ ] `go test -race -count=1 -timeout 180s ./src/...` green
- [ ] `go test -bench=BenchmarkInMemorySearch -benchmem -count=3 ./src` — numbers recorded
- [ ] `wrk -t4 -c32 -d30s http://localhost:8420/health` — no 5xx at ~1k QPS
- [ ] 1-hour soak under `runtime/pprof` — no OOM, no panic
- [ ] SIGINT during `task tree` / `task rollback` exits 130 cleanly (no stack trace)
- [ ] `hermem.ini` with `vector.dim = 0` or `extraction.timeout = -1s` fails `Config.Validate()` with a concrete 400/422, not a panic
- [ ] `hermem memory query --text "x" | head -1` exits 0 with no `EPIPE` in stderr

---

## Source

Regenerated from `out.txt`. Items extracted verbatim from the per-issue "Problem" /
"Решение" sections; code shapes quoted from the "Исправление N" snippets.
Cross-references to current code locations verified by listing `src/internal/`
subdirectories. Each task inherits the priority emoji the original review assigned;
no priority has been raised or lowered in this regeneration.
