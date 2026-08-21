# ADR-027: Storage Portability and Repository Interfaces

## Status

Proposed (scale trigger: >1 deployment flavor, >1M entities, hosted/SaaS offering)

## Context

Today every domain service (~20 packages: `memory`, `edge`, `task`,
`retrieval`, `graph`, `contradiction`, `ingest`, `ingestion`, `retention`,
`reembed`, `compression`, `timeline`, `episodic`, `evolution`, `migration`,
`admin`, `metrics`, `vector`, `goal`, `orchestrator`) receives a raw
`*sql.DB` and issues hand-written SQLite dialect SQL:

- `INSERT OR REPLACE` / `INSERT OR IGNORE` (SQLite-only upsert syntax)
- `PRAGMA` bootstrap in `store/init.go` (WAL, busy_timeout, cache_size)
- Recursive-CTE graph walk with a `char(31)`-delimited `visited` string
  and `instr()` cycle detection (`retrieval/expand.go`)
- `sqlite3.Error` type-assertion for `SQLITE_BUSY` retries in the domain
  layer (`ingestion/dialog.go` imports `mattn/go-sqlite3` directly)
- `CURRENT_TIMESTAMP` defaults, `datetime` affinity, `IN (?,?,?)`
  placeholder builders (`store.InClauseArgs`)
- `db.SetMaxOpenConns(1)` — a single-connection pool serialized by the
  `store.EntityLocker` striped-mutex pool

ADR-007 explicitly rejected repository structs ("package-level pattern is
idiomatic for SQLite's single-writer model"). That decision is correct for
a single-node embedded engine and wrong for the scale milestone this ADR
addresses.

## Why the current design breaks

1. **Engine swap = full rewrite.** Postgres, MySQL, Cockroach, or a
   managed graph store each require touching ~20 packages, ~14 migrations,
   and every SQL string. There is no seam.
2. **HA is impossible.** SQLite WAL is single-writer, single-node. 5000
   production deployments means 5000 snowflake files to back up, restore,
   and inspect. Fortune 500 RPO/RTO contracts demand PITR, replication,
   and failover that a WAL file cannot provide.
3. **Single-connection pool caps throughput.** `SetMaxOpenConns(1)` +
   `EntityLocker` + `SQLITE_BUSY` retry loops serialize all writes; read
   concurrency is bounded by the same connection. Ingestion already
   retries with exponential backoff on lock contention.
4. **SQL leaks across layers.** `vector.SearchByVector` queries the
   `entities` table; `metrics.AsyncMetricsWorker` updates
   `entities.last_accessed_at`; `server` packages assemble SQL args. A
   schema change ripples into packages that should not know the schema.
5. **CGO driver.** `mattn/go-sqlite3` forces CGO, complicating
   cross-compilation (see `.github/CROSSCOMPILING.md`, AMX guards).

## Decision

Introduce a **repository layer** between domain services and `store`:

1. Define per-domain repository interfaces in the consuming domain
   package (interface segregation; no `GodStore` interface):
   `memory.EntityRepo`, `task.Repo`, `retrieval.GraphReader`,
   `ingestion.Writer`, etc. Methods express domain operations
   (`UpsertEntity`, `WalkGraph`, `ClaimExecutable`) — not SQL.
2. `store/sqlite` implements all repository interfaces over
   `database/sql`, keeping every current query verbatim. This is a
   mechanical move, not a rewrite.
3. Domain services take their narrow repo interface; `app.Application`
   constructs `store/sqlite` once and injects it.
4. Ban `*sql.DB`, `sql.Tx`, and driver imports outside `store/` and
   `migration/`; enforce with the existing grep-based pre-push guardrail.
5. Defer the second engine (Postgres) until a concrete deployment
   requires it — the interfaces are the deliverable, not the engine.

## Alternatives considered

1. **Status quo (raw `*sql.DB` everywhere).** Zero cost today; rewrite
   cost grows linearly with every new query. Rejected: the seam must
   exist *before* the second engine arrives.
2. **Full hexagonal `Store` god-interface.** One interface with ~80
   methods. Rejected: violates interface segregation; mocks become
   unmaintainable; same failure mode as the current `core` god-package.
3. **ORM / sqlc / ent.** Rejected: recursive CTE graph walks, window
   functions, and the `visited`-set cycle detection do not map cleanly;
   codegen adds build complexity; hides the queries that matter most.
4. **Switch engine first, abstract later.** Rejected: migrating 20
   packages of SQLite dialect while simultaneously learning a new engine
   is the riskiest possible order.

## Tradeoffs

- **Cost:** a mechanical but wide refactor (~20 packages, hundreds of
  call sites). Mitigation: do it domain-by-domain behind the interfaces;
  each step compiles and passes the existing suite.
- **Indirection tax:** one more layer to trace when debugging. Accepted:
  the layer is thin (method → query) and typed.
- **Interface drift risk:** narrow repos per domain can duplicate similar
  queries (e.g. entity fetch by ID in 4 domains). Accepted: duplication
  is cheaper than a god-interface; shared read models can be extracted
  later.
- **Performance:** an extra interface call per DB op is noise next to a
  SQLite round-trip. No change to the single-writer model yet.

## Consequences

- A second engine becomes an additive package (`store/pg`) instead of a
  rewrite.
- Domain unit tests mock narrow interfaces instead of opening SQLite.
- `vector`, `metrics`, and `server` stop knowing table names.
- Enables ADR-030 (multi-tenancy) — tenant scoping lands inside repo
  implementations, not in 20 call sites.
- Prerequisite for ADR-033 (migration framework v2) — migrations become
  engine-specific behind the same seam.
