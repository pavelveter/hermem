# REFACTORING.md — Improvement Plan

Based on codebase-memory analysis (3587 nodes, 13914 edges, 37 packages).

---

## Sprint 1 — Quick Wins

### 1.1 Deduplicate Embedder interfaces
- [x] Remove `admin.Embedder` and `episodic.retrieval.Embedder` duplicates
- [x] Keep `core.Embedder` as single source of truth
- [x] Update all consumers to use `core.Embedder`

### 1.2 Consolidate test DB helpers
- [x] Create `internal/testutil/opendb.go` with shared `OpenTestDB(t)`
- [x] Replace 15+ duplicated `openTestDB` across packages
- [x] Ensure uniform cleanup and migration logic

### 1.3 Unify placeholder creation
- [x] Extract placeholder logic into `scripts/ensure-embed-placeholders.sh`
- [x] Update Makefile, pre-push hook, and CI to call the script
- [x] Single source of truth for dylib list

---

## Sprint 2 — Critical Fixes

### 2.1 Replace polling loop in Orchestrator.AgentLoop
- [x] Remove `time.Sleep(500ms)` busy-wait
- [x] Implement backoff-based polling (exponential, capped)
- [ ] Or use SQLite WAL commit hook for notification

### 2.2 Embedder validation on startup
- [x] Add `Ping(ctx) error` method to Embedder interface
- [x] Validate embedder availability in `serve` command before accepting traffic
- [x] Fail-fast with clear error message if provider unreachable

### 2.3 Refactor MultiHopRetrieveContext
- [ ] Extract `hopEmbedFacts()`, `hopVectorSearch()`, `hopMergeSeeds()`
- [ ] Reduce cognitive complexity from 40 to < 20
- [ ] Add unit tests for each extracted step

---

## Sprint 3 — Architecture

### 3.1 Introduce Retriever interface
- [ ] Define `core.Retriever` interface with `RetrieveContext` method
- [ ] Move `walk.RetrieveContext` into `retrieval.Service` method
- [ ] Update 24 callers to use interface instead of bare function
- [ ] Enable mock-based testing of retrieval pipeline

### 3.2 Split store package
- [ ] Create `store/db/` for init, migration, locker
- [ ] Create `store/entity/` for entity CRUD
- [ ] Create `store/edge/` for edge CRUD
- [ ] Create `store/graph/` for graph queries, provenance, community
- [ ] Create `store/task/` for task SQL
- [ ] Update imports across codebase

### 3.3 Harden serverstate.Load()
- [ ] Add nil-safety check or debug-mode panic
- [ ] Consider `sync.Once` for lazy initialization
- [ ] Document thread-safety contract

### 3.4 Fix compression.Metrics race condition
- [ ] Audit `clusterSizes []int64` write paths
- [ ] Add `sync.Mutex` if concurrent writes possible
- [ ] Or document single-writer guarantee

---

## Sprint 4 — Observability & Quality

### 4.1 Structured logging for HTTP handlers
- [ ] Create slog middleware that injects request_id from context
- [ ] Add duration, status_code, method, path to all handler logs
- [ ] Replace scattered `slog.Info/Error` with structured calls

### 4.2 Benchmark CI integration
- [ ] Add benchstat comparison to bench.yml workflow
- [ ] Report regressions as PR comments
- [ ] Track benchmark history over time
