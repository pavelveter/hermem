# Hermem Architecture

## Module Dependency Diagram

```
┌──────────────────────────────────────────────────────────────────────────────────┐
│                              INTERFACES (core)                                   │
│  Embedder · LLMExtractor · Reranker · VectorIndex · Retriever · RetentionPolicy  │
└──────────────────────────────────┬───────────────────────────────────────────────┘
                                   │ implements
┌──────────────┐  ┌──────────────┐ │ ┌──────────────┐  ┌──────────────────────────┐
│   ai/ollama  │  │   ai/openai  │─┘ │  ai/noop     │  │  logging (slog adapter)  │
│  Embedder    │  │  Embedder    │   │  Reranker    │  └──────────────────────────┘
│  Extractor   │  │  Extractor   │   │  Extractor   │
│  Reranker    │  │  Reranker    │   └──────────────┘
└──────┬───────┘  └──────┬───────┘
       │                 │
       └────────┬────────┘
                │
┌───────────────▼─────────────────────────────────────────────────────────────────────┐
│                              CONFIGURATION (config)                                 │
│  LoadConfig() · LoadConfigFromBinaryDir() · SchemaConfig · Validation               │
└───────────────┬─────────────────────────────────────────────────────────────────────┘
                │
┌───────────────▼─────────────────────────────────────────────────────────────────────┐
│                               PERSISTENCE (store)                                   │
│  SQLite schema · migrations · entity CRUD · edge CRUD · task queries · codec        │
│  StoreEntityWithEmbedding() · SetStatus() · AddEdge() · VerifyGraph()               │
└──────┬────────────────────────┬────────────────────────────────┬────────────────────┘
       │                        │                                │
┌──────▼───────┐  ┌─────────────▼─────────────┐  ┌───────────────▼────────────────────┐
│    vector    │  │     graph/community       │  │          migrations/               │
│  InMemory    │  │  Louvain community detect │  │     SQL migration files            │
│  SQLiteVec   │  │  LoadGraph() · Detect()   │  │     (embedded)                     │
│  CosineSim   │  └───────────────────────────┘  └────────────────────────────────────┘
│  Quantize    │
└──────┬───────┘
       │
┌──────▼────────────────────────────────────────────────────────────────────────────┐
│                            DOMAIN SERVICES                                        │
│                                                                                   │
│  ┌───────────┐ ┌───────────┐ ┌──────────┐ ┌──────────────┐ ┌───────────────────┐  │
│  │  memory   │ │   edge    │ │   task   │ │  retrieval   │ │   contradiction   │  │
│  │  Store()  │ │ AddEdge() │ │ Create() │ │ Search()     │ │ List()            │  │
│  │           │ │ AutoLink  │ │ Status() │ │ Retrieve()   │ │                   │  │
│  │           │ │           │ │ Dep()    │ │ Query()      │ │ ContradictionDet. │  │
│  │           │ │           │ │ Rollback │ │ Response()   │ │  LexicalDet.      │  │
│  │           │ │           │ │ Tree()   │ │ Explain()    │ │  EmbeddingDet.    │  │
│  │           │ │           │ │ Exec()   │ │ Provenance() │ │  CompositeDet.    │  │
│  └────┬──────┘ └────┬──────┘ └────┬─────┘ └──────┬───────┘ └───────┬───────────┘  │
│       │             │             │              │                 │              │
│  ┌────┴───┐  ┌──────┴────┐  ┌─────┴─────┐  ┌─────┴──────┐  ┌───────┴─────────┐    │
│  │ ingest │  │ migration │  │   goal    │  │  timeline  │  │     graph       │    │
│  │Ingest()│  │Status()   │  │ Status()  │  │ Timeline() │  │ Components()    │    │
│  └────┬───┘  │Rollback() │  │ List()    │  └────────────┘  │ Communities()   │    │
│       │      │Schema()   │  │ Get()     │                  │ Verify()        │    │
│       │      └───────────┘  └───────────┘                  │ Provenance()    │    │
│       │                                                    └─────────────────┘    │
│  ┌────▼───────────────────────────────────────────────────────────────────────┐   │
│  │                           ingestion pipeline                               │   │
│  │  IngestionWorker · ProcessDialog() · LLM Extract → Embed → Dedup → Store   │   │
│  └────────────────────────────────────────────────────────────────────────────┘   │
│                                                                                   │
│  ┌──────────────┐ ┌────────────┐ ┌───────────────┐ ┌────────────┐ ┌────────────┐  │
│  │   retention  │ │  reembed   │ │  compression  │ │  health    │ │  episodic  │  │
│  │  RunOnce()   │ │ReEmbedAll()│ │ ClusterEnts() │ │  Probes    │ │  Sessions  │  │
│  │  Run()       │ │            │ │ GenSummaries()│ │  Checks    │ │  Episodes  │  │
│  └──────────────┘ └────────────┘ └───────────────┘ └────────────┘ │  Events    │  │
│                                                                   └────────────┘  │
│  ┌─────────────────────────────────────────────────────────────────────────────┐  │
│  │                           evolution subsystem                               │  │
│  │  TrustScore() · RecordHistory() · PropagateConfidence() · AggregateEvidence │  │
│  │  memory/belief (Belief CRUD) · memory/evidence (Support/Refute artifacts)   │  │
│  └─────────────────────────────────────────────────────────────────────────────┘  │
│                                                                                   │
│  ┌──────────────┐ ┌──────────────┐                                                │
│  │ orchestrator │ │  evaluation  │                                                │
│  │ AgentLoop()  │ │  Metrics     │                                                │
│  └──────────────┘ │  NDCG/MRR    │                                                │
│                   └──────────────┘                                                │
└───────────────────────────────────────────────────────────────────────────────────┘
                                        │
┌───────────────────────────────────────▼────────────────────────────────────────────┐
│                                   TRANSPORT                                        │
│                                                                                    │
│  ┌──────────────────────────────────────────────────────────────────────────────┐  │
│  │  HTTP server/ (12 HTTPService sub-shells)                                    │  │
│  │  memory · edge · retrieval · task · graph · contradiction · timeline         │  │
│  │  health · ingest · migration · retention · reembed · shared                  │  │
│  │                                                                              │  │
│  │  Middleware: Recovery → Timeout → Runtime → RequestID → Auth → Slog          │  │
│  │  Observability: metrics/ (Prometheus) · tracing/ (OpenTelemetry)             │  │
│  └──────────────────────────────────────────────────────────────────────────────┘  │
│                                                                                    │
│  ┌──────────────────────────────────────────────────────────────────────────────┐  │
│  │  CLI cli/ (6 command groups, ~40 subcommands)                                │  │
│  │  memory · task · graph · time · db · agent · serve · health · metrics        │  │
│  │  admin · ops · diagnose · profile · bench · version · re-embed · quantize    │  │
│  │                                                                              │  │
│  │  cli/env (Env) ← transitional adapter (being replaced by app.Application)   │  │
│  └──────────────────────────────────────────────────────────────────────────────┘  │
│                                                                                    │
│  ┌──────────────────────────────────────────────────────────────────────────────┐  │
│  │  app.Application ← typed DI container (C2)                                  │  │
│  │  New() constructs ALL dependencies eagerly; no nil fields, no lazy init.     │  │
│  │  Start()/Stop() with explicit lifecycle ordering.                           │  │
│  │  main.go constructs Application → converts to clienv.Env for CLI commands.   │  │
│  └──────────────────────────────────────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────────────────────────────────────┘
                                         │
┌────────────────────────────────────────▼────────────────────────────────────────────┐
│                                  INFRASTRUCTURE                                     │
│                                                                                     │
│   ┌────────────┐  ┌───────────┐  ┌──────────┐  ┌──────────┐  ┌──────────────────┐   │
│   │   metrics  │  │  tracing  │  │   auth   │  │ logging  │  │  lifecycle/      │   │
│   │ Prometheus │  │ OpenTel.  │  │ API keys │  │ slog     │  │  Graceful start  │   │
│   │            │  │ OTLP      │  │ Scopes   │  │ adapter  │  │  Graceful stop   │   │
│   └────────────┘  └───────────┘  └──────────┘  └──────────┘  └──────────────────┘   │
│                                                                                     │
│   ┌───────────────────────────────────────────────────────────────────────────┐     │
│   │  serverstate (atomic config snapshot — SIGHUP hot-reload)                 │     │
│   │  Ref.Load() · Ref.Store() · Generation stamping · IsStale()               │     │
│   └───────────────────────────────────────────────────────────────────────────┘     │
└─────────────────────────────────────────────────────────────────────────────────────┘
```

## Dependency Layers (bottom-up)

```
Layer 0 — Leaf packages (zero internal deps):
  core, auth, metrics, tracing, util/safego, util/time, evaluation,
  memory/belief, memory/evidence, cli/diagnose/checks

Layer 1 — Infrastructure:
  config (→ ai, auth, core)
  store (→ core, graph/community)
  vector (→ core, store)
  ai (→ core, httputil)
  httputil (→ core)
  serverstate (→ core)
  admin (→ core, store)

Layer 2 — Domain services:
  memory (→ core, store, vector)
  edge (→ core, store, vector)
  graph (→ core, graph/community, store)
  contradiction (→ core, store)
  retrieval (→ core, store, vector, tracing)
  task (→ core, config, store, vector)
  goal (→ core, store)
  migration (→ core, store)
  timeline (→ core)
  retention (→ core)
  reembed (→ core, store, vector)
  compression (→ core, store, vector)
  health (→ metrics)
  ingest (→ core, ingestion)
  ingestion (→ core, store, contradiction, vector, ingestion/detectors)
  ingestion/detectors (→ core, contradiction)
  episodic (→ core, store, util/time)
  evolution (→ memory/belief, memory/evidence)
  orchestrator (→ core, store, task)
  admin (→ core, store)

Layer 3 — Transport:
  server/ (→ all Layer 2 + serverstate, metrics, auth, httputil, lifecycle)
  cli/ (→ all Layer 2 + cli/env, config, store, server, serverstate)

Layer 4 — Application container:
  app (→ config, core, metrics, retrieval, store, tracing, vector)

Layer 5 — Top-level:
  main.go (→ app, cli, cli/env, config)
```
