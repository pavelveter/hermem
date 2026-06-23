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

[ ] Enable foreign keys

Current:

    FK OFF

Add:

    PRAGMA foreign_keys = ON

Schema:

    FOREIGN KEY (...) REFERENCES entities(id)
    ON DELETE CASCADE

Expected result:

    No orphan edges.

----------------------------------------------------------

[ ] Add graph integrity verifier

CLI:

    hermem verify

Checks:

    orphan nodes
    orphan edges
    broken references
    duplicate relations

Output:

    human readable report

==========================================================
PHASE 3 — MEMORY MODEL V2
Priority: HIGH
ETA: 3-4 days
==========================================================

Goal:

Support contradictory knowledge and memory evolution.

Tasks:

[ ] Extend Entity schema

Add:

    confidence REAL
    source TEXT
    source_type TEXT
    created_at
    updated_at
    valid_from
    valid_to

Example:

    User likes Go

confidence=0.95
source=user

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

[ ] Add memory provenance

Store:

    extracted_from
    conversation_id
    message_id

Goal:

Explain why memory exists.

Future:

    memory explain endpoint

==========================================================
PHASE 4 — RETRIEVAL V2
Priority: HIGH
ETA: 2-3 days
==========================================================

Goal:

Improve retrieval quality.

Tasks:

[ ] Configurable ranking weights

Add:

    [ranking]

    vector_weight
    recency_weight
    depth_penalty

Current constants become config.

----------------------------------------------------------

[ ] Introduce Reranker interface

Interface:

    type Reranker interface {
        Rank(...)
    }

Implementations:

    NoopReranker
    CrossEncoderReranker
    LLMReranker

Pipeline:

    Search
      ->
    Graph expansion
      ->
    Reranker
      ->
    Prompt context

----------------------------------------------------------

[ ] Retrieval explainability

Endpoint:

    /query/explain

Returns:

    why node selected
    vector score
    depth score
    recency score

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

[ ] Schema versioning

Add:

    schema_version

Stored in DB.

Startup:

    compare config schema
    compare DB schema

Detect incompatibilities.

----------------------------------------------------------

[ ] Dynamic schema reload

Signal:

    SIGHUP

Reload:

    hermem.ini

Without restart.

==========================================================
PHASE 8 — DATABASE EVOLUTION
Priority: HIGH
ETA: 2 days
==========================================================

Goal:

Safe upgrades.

Tasks:

[ ] Migration system

Tables:

    schema_migrations

Implement:

    migration runner

Versioned SQL files.

----------------------------------------------------------

[ ] Upgrade command

CLI:

    hermem migrate

Output:

    migration status
    pending migrations

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

Sprint 1:
- Remove globals
- Transactions
- FK enforcement

Sprint 2:
- Memory provenance
- Entity metadata
- Retrieval explainability

Sprint 3:
- Reranker
- Ranking config
- Prometheus

Sprint 4:
- Migration system
- Schema versioning

Sprint 5:
- Contradiction handling
- Temporal memory

Sprint 6:
- Multi-tenant support
- RBAC

After Sprint 6 Hermem stops being "graph memory for agents"
and becomes a legitimate memory substrate for agent frameworks.