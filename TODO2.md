# Hermem Convergence Sprint (Do Before New Features)

IMPORTANT

Do not add new features until this list is completed.

After every completed item:

[ ] Run tests
[ ] Run go test -race ./...
[ ] Commit separately
[ ] Push

==================================================
PHASE 1 — REMOVE ARCHITECTURAL DEBT
==================================================

[ ] Find all ActiveSchema() usages
[ ] Remove ActiveSchema() completely
[ ] Inject SchemaConfig through constructors
[ ] Verify no global schema state remains
[ ] Add test covering schema reload after removal

Commit:
refactor(schema): remove ActiveSchema singleton

--------------------------------------------------

[ ] Find all package-level mutable variables
[ ] Remove global mutable state
[ ] Move state into services
[ ] Verify race detector is clean

Commit:
refactor(core): remove global mutable state

--------------------------------------------------

[ ] Find duplicate contradiction functions
[ ] Merge into single implementation
[ ] Remove dead contradiction code
[ ] Add regression tests

Commit:
refactor(contradictions): unify detection pipeline

--------------------------------------------------

[ ] Find duplicate recency scoring logic
[ ] Merge into one implementation
[ ] Remove duplicated functions
[ ] Add tests

Commit:
refactor(retrieval): unify recency scoring

==================================================
PHASE 2 — SERVICE LAYER
==================================================

[ ] Create MemoryService
[ ] Move memory logic out of handlers
[ ] Add service tests

Commit:
refactor(memory): introduce MemoryService

--------------------------------------------------

[ ] Create RetrievalService
[ ] Move retrieval logic out of handlers
[ ] Add service tests

Commit:
refactor(retrieval): introduce RetrievalService

--------------------------------------------------

[ ] Create ContradictionService
[ ] Move contradiction logic out of ingestion
[ ] Add service tests

Commit:
refactor(contradictions): introduce service layer

--------------------------------------------------

[ ] Create TaskService
[ ] Move task logic out of HTTP layer
[ ] Add service tests

Commit:
refactor(tasks): introduce TaskService

==================================================
PHASE 3 — RETRIEVAL STABILIZATION
==================================================

[ ] Create ScoreBreakdown structure
[ ] Add VectorScore
[ ] Add RecencyScore
[ ] Add DepthPenalty
[ ] Add FinalScore
[ ] Return breakdown in retrieval results

Commit:
feat(retrieval): add score breakdown

--------------------------------------------------

[ ] Separate retrieval pipeline stages
[ ] Vector retrieval
[ ] Graph expansion
[ ] Reranking
[ ] Final scoring

Commit:
refactor(retrieval): split retrieval pipeline

--------------------------------------------------

[ ] Add retrieval tracing logs
[ ] Add retrieval debug mode
[ ] Add retrieval explainability output

Commit:
feat(retrieval): add explainability

==================================================
PHASE 4 — MODEL CLEANUP
==================================================

[ ] Audit Entity fields
[ ] Group fields by responsibility
[ ] Identify Fact-specific fields
[ ] Identify Evidence-specific fields
[ ] Identify Task-specific fields
[ ] Document findings

Commit:
docs(model): audit entity responsibilities

--------------------------------------------------

[ ] Introduce Fact model
[ ] Keep compatibility with Entity
[ ] Add conversion tests

Commit:
refactor(model): introduce Fact

--------------------------------------------------

[ ] Introduce Evidence model
[ ] Keep compatibility with Entity
[ ] Add conversion tests

Commit:
refactor(model): introduce Evidence

==================================================
PHASE 5 — EVALUATION FIRST
==================================================

[ ] Create evaluation package
[ ] Create benchmark runner
[ ] Add Recall@K
[ ] Add MRR
[ ] Add NDCG

Commit:
feat(eval): create benchmark framework

--------------------------------------------------

[ ] Create retrieval benchmark dataset
[ ] Add benchmark command
[ ] Store baseline metrics

Commit:
feat(eval): add retrieval benchmark dataset

--------------------------------------------------

[ ] Create contradiction benchmark dataset
[ ] Add benchmark command
[ ] Store baseline metrics

Commit:
feat(eval): add contradiction benchmark dataset

==================================================
PHASE 6 — RELEASE HARDENING
==================================================

[ ] Audit all public APIs
[ ] Audit all CLI commands
[ ] Remove dead code
[ ] Remove unused config fields
[ ] Remove unused migrations
[ ] Remove unused structs

Commit:
refactor(core): remove dead code

--------------------------------------------------

[ ] Run full benchmark suite
[ ] Run race detector
[ ] Run integration tests
[ ] Run migration tests
[ ] Run load tests

Commit:
test(release): final stabilization pass

==================================================
DONE
==================================================

Only after all items above are completed:

[ ] Start Belief Engine
[ ] Start Evidence Graph
[ ] Start Episodic Memory
[ ] Start Semantic Compression
[ ] Start Agent Memory Layer