HERMEM ROADMAP (STAFF ENGINEER AUDIT)

==========================================================
PHASE 1 — FIX ARCHITECTURAL DEBT
Priority: CRITICAL
ETA: 1-2 days
==========================================================

Goal:
Eliminate global mutable state and prepare codebase for future multi-tenant, server, and library usage.

Tasks:

[x] Remove global activeSchema singleton  ✅ Sprint 1

    ActiveSchema() / SetActiveSchema() removed.
    Replaced by Runtime struct with explicit DB/VI/Embedder/Extractor/Config fields.
    Server uses atomic.Pointer[ServerState] for lock-free per-request schema access.

----------------------------------------------------------

[x] Remove global iniRef  ✅ Sprint 1

    iniRef variable removed. Config loaded via gopkg.in/ini.v1, parsed into
    Config struct. getStr/getInt/getFloat32/getDuration closures over local
    iniFile, no package-level mutable state.

==========================================================
PHASE 2 — INGESTION CONSISTENCY
Priority: CRITICAL
ETA: 1 day
==========================================================

Goal:

Prevent half-written graph states.

Tasks:

[x] Make ingestion transactional  ✅ Sprint 1

    Per-item transactions: entity INSERT + edges INSERT in same SQL tx.
    On failure: tx.Rollback + vi.Remove rollback. No half-written graphs.
    vi.Store executed pre-tx (network-shaped), vi updates post-commit.

----------------------------------------------------------

[x] Enable foreign keys  ✅ Sprint 1

    PRAGMA foreign_keys = ON (DSN _fk=true + belt-and-suspenders PRAGMA)
    FOREIGN KEY (...) REFERENCES entities(id) ON DELETE CASCADE

----------------------------------------------------------

[x] Add graph integrity verifier  ✅ Sprint 1

    CLI: hermem verify
    Checks: orphan nodes, orphan edges, broken references, duplicate relations

==========================================================
PHASE 3 — MEMORY MODEL V2
Priority: HIGH
ETA: 3-4 days
==========================================================

Goal:

Support contradictory knowledge and memory evolution.

Tasks:

[x] Extend Entity schema  ✅ Sprint 2

    Added: confidence, source, source_type, created_at, valid_from, valid_to
    Migration 002_entity_metadata.sql

----------------------------------------------------------

[x] Replace hard merge strategy  ✅ Phase 10

    src/contradiction.go — isContradiction(existing, incoming) heuristic:
    (1) negation asymmetry with shared words, (2) sentiment-opposite pairs
    via ~45 inflected-form antonym map. No LLM needed. Word overlap >= 25% gate.
    On contradiction: creates contradicts edge, forces separate node instead of merge.

    [x] Confidence comparison  ✅ (this commit)
    highConfidenceThreshold = 0.7. When existing.Confidence >= 0.7:
    keep both with contradicts edge (current behavior). When < 0.7:
    archive existing entity, create incoming as replacement.
    Unset confidence (NULL→0, pre-migration) treated as reliable.
    See src/ingestion.go ProcessDialogWithProvenance.

----------------------------------------------------------

[x] Add memory provenance  ✅ Sprint 2

    Added: conversation_id, message_id, extracted_from
    Migration 003_provenance.sql

==========================================================
PHASE 4 — RETRIEVAL V2
Priority: HIGH
ETA: 2-3 days
==========================================================

Goal:

Improve retrieval quality.

Tasks:

[x] Configurable ranking weights  ✅ Sprint 5

    [ranking] section: vector_weight, recency_weight, depth_penalty, recency_half_life_hours
    RankingWeight struct on Config and RetrieveContextOptions
    defaultCompositeScorer now a factory: func(RankingWeight) CompositeScorer

----------------------------------------------------------

[x] Introduce Reranker interface  ✅ Sprint 5

    Reranker interface: Rank(ctx, query, facts) []Fact
    OllamaReranker (cross-encoder /api/rerank), OpenAIReranker (chat /chat/completions)
    Wired into RetrieveContext pipeline after category buckets
    Optional — nil when provider empty in config

----------------------------------------------------------

[x] Retrieval explainability  ✅ Sprint 2

    Endpoint: /query/explain
    Returns: vector score, depth score, recency score per fact

==========================================================
PHASE 5 — VECTOR INDEX IMPROVEMENTS
Priority: MEDIUM
ETA: 2 days
==========================================================

Goal:

Reduce allocations and memory footprint.

Tasks:

[x] Remove duplicated vector storage  ✅ (multi-sprint)

    Removed vec []float32 from vectorEntry — only flatMatrix stores vectors.
    entries keep id + norm (metadata). ~50% RAM reduction on entries slice.

----------------------------------------------------------

[x] sync.Pool for search buffers  ✅ (multi-sprint)

    dotPool + intBufPool reuse dot/idx buffers across Search/SearchBatch.
    getDotBuf/putDotBuf/getIntBuf/putIntBuf helpers. Lower GC pressure on hot paths.

----------------------------------------------------------

[x] Batch ingestion indexing  ✅ (this commit)

    BulkStore added to VectorIndex interface + InMemoryVectorIndex
    + SqliteVecIndex. Single lock acquisition for all pairs.
    Ingestion normalizes and bulk-stores all embeddings before
    per-item loop. Per-item rollback still uses individual vi.Remove.

==========================================================
PHASE 6 — OBSERVABILITY
Priority: HIGH
ETA: 1 day
==========================================================

Goal:

Operate Hermem in production.

Tasks:

[x] Prometheus metrics  ✅ (this commit)

    Replaced expvar with prometheus/client_golang. 16 counters registered
    via MustRegister in init(). metricsHandler serves promhttp.Handler().
    Go runtime metrics included free. Backward-compatible inc*() helpers.

----------------------------------------------------------

[x] OpenTelemetry tracing  ✅ (this commit)

    src/tracing.go — InitTracing with stdout exporter, Tracer() helper.
    Spans on: ProcessDialogWithProvenance, SearchByVector, RetrieveContext.
    Wired in serve mode with defer shutdown. OTLP exporter via env var.
    Compatible with Tempo, Jaeger, Grafana via OTEL_EXPORTER_OTLP_ENDPOINT.

----------------------------------------------------------

[x] Health levels  ✅ (multi-sprint)

    /health/live — always 200 (liveness probe)
    /health/ready — DB ping check, returns 503 with per-dependency status if degraded
    TODO: add embedder/extractor health checks to /health/ready

==========================================================
PHASE 7 — SCHEMA ENGINE
Priority: HIGH
ETA: 2-4 days
==========================================================

Goal:

Make schema a first-class feature.

Tasks:

[x] Schema validation compiler  ✅ (multi-sprint)

    ValidateSchema() checks: duplicate states, stateful_categories requires states,
    state_unblocking ∈ valid_states, blocking/recovery ∈ allowed_relations.
    Integrated into Config.Validate() — runs at startup and on SIGHUP.

----------------------------------------------------------

[x] Schema versioning  ✅ Sprint 4

    HashSchema() deterministic SHA-256 fingerprint (sorted map keys)
    CheckSchemaFingerprint compares stored vs current on startup
    StoreSchemaFingerprint writes after SIGHUP reload
    hermem schema CLI shows current vs stored

----------------------------------------------------------

[x] Dynamic schema reload  ✅ Sprint 4

    SIGHUP reloads hermem.ini without restart
    Server uses atomic.Pointer[ServerState] for lock-free swaps
    Server.ReloadState atomically swaps schema, categories, relations, ranking, reranker

==========================================================
PHASE 8 — DATABASE EVOLUTION
Priority: HIGH
ETA: 2 days
==========================================================

Goal:

Safe upgrades.

Tasks:

[x] Migration system  ✅ Sprint 4

    schema_migrations table tracks applied versions
    src/migrations/ with 4 embedded SQL files (//go:embed)
    runMigrations reads embedded SQL, applies in tx, records version
    Replaces old ad-hoc migrateSchema (ALTER TABLE with swallowed errors)

----------------------------------------------------------

[x] Upgrade command  ✅ Sprint 4

    CLI: hermem migrate — shows status of all migrations
    MigrationStatus / PendingMigrations exported for CLI use
    --db flag for targeting specific database

==========================================================
PHASE 9 — ENTERPRISE FEATURES
Priority: MEDIUM
ETA: 1 week
==========================================================

Tasks:

[ ] Multi-tenant namespaces

Tables:

    tenant_id

Isolation:

    search
    retrieval
    ingestion

----------------------------------------------------------

[ ] RBAC

Roles:

    admin
    writer
    reader

----------------------------------------------------------

[ ] API rate limiting

Per:

    API key
    IP

==========================================================
PHASE 10 — LONG TERM DIFFERENTIATORS
Priority: OPTIONAL
ETA: 2-4 weeks
==========================================================

Tasks:

[x] Temporal memory retrieval  ✅ Phase 10

    RetrieveContextOptions.TimeFrom/TimeTo — CTE filters both anchor and recursive arms.
    /query/temporal endpoint + temporal CLI — query with time window.
    Time parse warnings on malformed RFC3339 input.

----------------------------------------------------------

[x] Contradiction graph  ✅ Phase 10

    ContradictionPair type with json tags. GetContradictions(db, entityID) —
    bidirectional filtering (source_id OR target_id). /contradictions[?id=X] endpoint.
    contradictions CLI [entity_id]. contradict edges included in graph walks.

----------------------------------------------------------

[x] Episodic memory  ✅ Phase 10

    Migration 004_episodic_sessions.sql: sessions + conversations tables.
    /timeline endpoint + timeline CLI — entities ordered by created_at DESC.
    idx_entities_created_at index for timeline queries.

----------------------------------------------------------

[x] Multi-hop retrieval  ✅ (this commit)

    src/multi_hop.go — MultiHopRetrieveContext: iterative search→expand→
    re-expand from top-3 discovered facts. Embed content as queries,
    SearchBatch for single lock, merge results dedup by content.
    MultiHopCount field on RetrieveContextOptions (0/1 = single hop).
    Wired into GenerateResponse in main.go.

----------------------------------------------------------

[x] Agent state engine  ✅ (this commit)

    src/agent_loop.go — ExecutionPlan (CTE topological sort, leaf-first),
    ExecuteNext (auto-transition to running), ExecuteComplete (advance state
    machine), ExecuteFail (mark failed + return rollback task),
    AgentLoop (loop: ExecuteNext → callback → ExecuteComplete),
    nextValidState helper. agent-loop CLI command wired.

==========================================================
RECOMMENDED ORDER
==========================================================

Sprint 1: ✅
- ✅ FK enforcement
- ✅ Remove globals
- ✅ Transactions

Sprint 2: ✅
- ✅ Memory provenance
- ✅ Entity metadata
- ✅ Retrieval explainability

Sprint 3: ✅ (merged into Sprint 5)
- ✅ Reranker
- ✅ Ranking config
- ✅ Health levels (/health/live, /health/ready)
- ✅ Prometheus (prometheus/client_golang, 16 counters, promhttp)

Sprint 4: ✅
- ✅ Migration system
- ✅ Schema versioning

Sprint 5: ✅
- ✅ Vector index dedup (flatMatrix-only storage, ~50% RAM)
- ✅ sync.Pool for search buffers (dotPool + intBufPool)
- ✅ Schema validation compiler (ValidateSchema)
- ✅ Contradiction graph (/contradictions endpoint, contradicts edges)
- ✅ Temporal retrieval (/query/temporal, time filter in CTE)
- ✅ Episodic memory (sessions + conversations, /timeline endpoint)

Sprint 6: ⬜
- ⬜ Multi-tenant support
- ⬜ RBAC

After Sprint 6 Hermem stops being "graph memory for agents"
and becomes a legitimate memory substrate for agent frameworks.