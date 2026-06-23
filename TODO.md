HERMEM ROADMAP (STAFF ENGINEER AUDIT)

==========================================================
PHASE 1 — FIX ARCHITECTURAL DEBT
Priority: CRITICAL
ETA: 1-2 days
==========================================================

Goal:
Eliminate global mutable state and prepare codebase for future multi-tenant, server, and library usage.

Tasks:

[ ] Remove global activeSchema singleton

Current:

    ActiveSchema()
    SetActiveSchema()

Problem:

    Process-wide mutable state
    Race potential
    Impossible per-request schemas

Implement:

    type Runtime struct {
        Schema SchemaConfig
    }

Pass schema through:

    StoreEntity()
    ProcessDialog()
    RetrieveContext()
    HTTP handlers

Expected result:

    No global schema state.

----------------------------------------------------------

[ ] Remove global iniRef

Current:

    var iniRef *ini.File

Problem:

    Hidden dependency
    Multiple config loads overwrite state

Implement:

    type Config struct {
        ...
        rawINI *ini.File
    }

or

    type Loader struct {
        ini *ini.File
    }

Expected result:

    Config becomes immutable after load.

==========================================================
PHASE 2 — INGESTION CONSISTENCY
Priority: CRITICAL
ETA: 1 day
==========================================================

Goal:

Prevent half-written graph states.

Tasks:

[ ] Make ingestion transactional

Current flow:

    create entity
    create edges
    update embedding
    merge

Problem:

    Failure in middle leaves broken graph.

Implement:

    tx.Begin()

    store entity
    merge entity
    create edges
    update vectors

    tx.Commit()

Rollback on any failure.

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

[ ] Replace hard merge strategy

Current:

    cosine >= threshold
    => merge

Problem:

    "likes Go"
    and
    "hates Go"

may merge.

Implement:

    similarity gate
    contradiction detector
    confidence comparison

Pseudo:

    high similarity
        +
    same polarity
        =>
    merge

Otherwise:

    create new node

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

[ ] Remove duplicated vector storage

Current:

    flatMatrix
    entries[i].Vector

Store only:

    flatMatrix
    metadata

Expected:

    ~50% RAM reduction

----------------------------------------------------------

[ ] sync.Pool for search buffers

Current:

    make([]float32, n)

per request.

Replace:

    sync.Pool

Expected:

    lower GC pressure

----------------------------------------------------------

[ ] Batch ingestion indexing

Current:

    rebuild index frequently

Implement:

    bulk insert mode

Expected:

    faster large imports

==========================================================
PHASE 6 — OBSERVABILITY
Priority: HIGH
ETA: 1 day
==========================================================

Goal:

Operate Hermem in production.

Tasks:

[ ] Prometheus metrics

Replace:

    expvar

With:

    Prometheus

Metrics:

    ingest_total
    search_total
    retrieve_total
    vector_search_duration
    graph_walk_duration
    llm_extract_duration

----------------------------------------------------------

[ ] OpenTelemetry tracing

Trace:

    query
    search
    graph walk
    extraction
    storage

Compatible with:

    Tempo
    Jaeger
    Grafana

----------------------------------------------------------

[ ] Health levels

Endpoints:

    /health/live
    /health/ready

Ready checks:

    database
    embedder
    extractor

==========================================================
PHASE 7 — SCHEMA ENGINE
Priority: HIGH
ETA: 2-4 days
==========================================================

Goal:

Make schema a first-class feature.

Tasks:

[ ] Schema validation compiler

Startup:

    validate schema

Check:

    duplicate states
    duplicate relations
    invalid FSM

Fail fast.

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
    src/migrations/ with 3 embedded SQL files (//go:embed)
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

[ ] Temporal memory retrieval

Query:

    what did user believe in March?

----------------------------------------------------------

[ ] Contradiction graph

Relations:

    contradicts

Automatic detection.

----------------------------------------------------------

[ ] Episodic memory

Store:

    sessions
    conversations
    timelines

----------------------------------------------------------

[ ] Multi-hop retrieval

Search
    ->
Graph expansion
    ->
Expansion from discovered nodes
    ->
Reranker

----------------------------------------------------------

[ ] Agent state engine

Task graph
FSM
Rollback
Execution planner

Convert Hermem from memory store into agent substrate.

==========================================================
RECOMMENDED ORDER
==========================================================

Sprint 1: ✅
- ✅ FK enforcement
- ⬜ Remove globals
- ⬜ Transactions

Sprint 2: ✅
- ✅ Memory provenance
- ✅ Entity metadata
- ✅ Retrieval explainability

Sprint 3: ✅ (merged into Sprint 5)
- ✅ Reranker
- ✅ Ranking config
- ⬜ Prometheus (metrics exist but not Prometheus format)

Sprint 4: ✅
- ✅ Migration system
- ✅ Schema versioning

Sprint 5: ⬜
- ⬜ Contradiction handling
- ⬜ Temporal memory

Sprint 6: ⬜
- ⬜ Multi-tenant support
- ⬜ RBAC

After Sprint 6 Hermem stops being "graph memory for agents"
and becomes a legitimate memory substrate for agent frameworks.