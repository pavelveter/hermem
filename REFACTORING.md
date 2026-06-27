# REFACTORING.md

Post-Sprint-4 code review findings. All tasks derived from codebase analysis (3,634 nodes, 13,343 edges, 971 tests).

---

## §R1. Critical: Function Complexity

### §R1.1 Extract `DetectCommunities` (cognitive: 77 → target < 30)
- [x] Create `src/internal/graph/community/louvain.go`
- [x] Extract `loadGraph(db) (ids, adj, totalWeight, nodeIndex)` from lines 12-58
- [x] Extract `louvainPhase1(community, adj, nodeWeight, maxIterations)` from lines 60-120
- [x] Extract `computeModularity(adj, community, nodeWeight, totalWeight)` from lines 145-162
- [x] Extract `buildCommunities(commMembers, adj, nodeIndex, nodeWeight, totalWeight)` from lines 164-192
- [x] Add `context.Context` parameter to `DetectCommunities`
- [x] Update callers: `src/internal/server/graph/` and tests
- [ ] Add unit tests for each extracted function
- [x] Verify 971 tests pass

### §R1.2 Reduce `processOneItemOnce` complexity (cognitive: 19 → target < 15)
- [x] Extract `handleContradiction(existing, incoming) (action, archiveID)`
- [x] Extract `mergeEntity(ctx, existing, incoming, prov) (*core.Entity, error)`
- [x] Extract `createNewItem(ctx, it, prov) error`
- [x] Keep transaction orchestration in `processOneItemOnce`
- [ ] Add tests for contradiction handling paths
- [x] Verify 971 tests pass

### §R1.3 Refactor `LoadConfig` repetitive pattern (cognitive: 43 → target < 20)
- [ ] Define `type configMapping struct { section, key string; apply func(*Config, string) }`
- [ ] Build mapping table for all ~40 fields
- [ ] Replace sequential `if v, ok := getStr(...)` blocks with loop over mapping
- [ ] Extract `applySchemaSection(cfg, section, path)` for schema handling
- [ ] Verify 971 tests pass

---

## §R2. Critical: Parameter Explosion

### §R2.1 Introduce `MemoryWorkerConfig` struct (9 params → 2)
- [x] Define `type MemoryWorkerConfig struct { DB, VI, Extractor, Embedder, DedupThreshold, Schema, CkptPath, PendingPath, WorkerID }`
- [x] Change `MemoryWorkerResilient` signature to `(ctx, cfg, ch)`
- [x] Update all callers (tests + orchestrator)
- [x] Verify 971 tests pass

### §R2.2 Introduce `IngestionWorkerConfig` struct (7 params → 2)
- [x] Define `type IngestionWorkerConfig struct { DB, VI, Extractor, Embedder, DedupThreshold, Schema, Detector }`
- [x] Change `NewIngestionWorker` signature
- [x] Update all 6 callers
- [x] Verify 971 tests pass

### §R2.3 Introduce `ServerDeps` struct (14 params → 1)
- [x] Define `type ServerDeps struct { Refs, Retrieval, Task, Memory, Edge, Timeline, Ingest, Contradiction, Graph, Migration, Retention, Reembed, Health, Metrics }`
- [x] Change `NewServer` signature to `(deps ServerDeps)`
- [x] Update `wireAll()` in `cli/wiring.go`
- [x] Verify 971 tests pass

---

## §R3. High: Separation of Concerns

### §R3.1 Move `DetectCommunities` out of `store` package
- [x] Create `src/internal/graph/community/` package
- [x] Move Louvain algorithm there
- [x] `store` should only provide `LoadGraph(ctx, db) (AdjacencyList, error)`
- [x] Update imports in server handlers
- [x] Verify 971 tests pass

### §R3.2 Split `processOneItemOnce` concerns
- [x] Create `ingestion/contradiction_handler.go` for contradiction logic
- [x] Create `ingestion/merge.go` for merge logic
- [x] Keep `dialog.go` for orchestration only
- [x] Verify 971 tests pass

---

## §R4. Medium: Code Quality

### §R4.1 Externalize hardcoded negation words
- [ ] Create `assets/negations_en.txt` and `assets/negations_ru.txt`
- [ ] Load at init time via `go:embed` or config
- [ ] Remove hardcoded strings from `detectors/lexical.go`
- [ ] Verify 971 tests pass

### §R4.2 Introduce `RouteProvider` registry
- [ ] Refactor `Server` to hold `[]RouteProvider` instead of 13 explicit fields
- [ ] Each service implements `RouteProvider` interface
- [ ] `mount()` iterates registry
- [ ] Reduce `NewServer` params automatically
- [ ] Verify 971 tests pass

### §R4.3 Define named type for anonymous struct in `processOneItemOnce`
- [x] Create `type processInput struct { Entity core.ExtractedEntity; Embedding []float32 }`
- [x] Replace anonymous struct parameter
- [x] Verify 971 tests pass

---

## §R5. Low: Style Consistency

### §R5.1 Standardize error wrapping format
- [ ] Choose convention: `pkg: component: %w` (colon-delimited)
- [ ] Audit all `fmt.Errorf` calls in `store/`, `ingestion/`, `retrieval/`
- [ ] Fix ~50 inconsistencies
- [ ] Verify 971 tests pass

### §R5.2 Add `context.Context` to all store queries
- [ ] Audit `store/community.go` for missing ctx params
- [ ] Add ctx to `DetectCommunities` (covered in §R1.1)
- [ ] Verify 971 tests pass

---

## Execution Order

```
§R1.1 → §R3.1 (move + extract simultaneously)
§R2.1 → §R2.2 → §R2.3 (struct introductions, independent)
§R1.2 → §R3.2 (split concerns)
§R1.3 (standalone)
§R4.1, §R4.2, §R4.3, §R5.1, §R5.2 (independent, low risk)
```

**Total**: 13 sections, ~40 subtasks
