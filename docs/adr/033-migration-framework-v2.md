# ADR-033: Migration Framework v2

## Status

Proposed (scale trigger: 5000 deployments on mixed versions, >1M-row tables, zero-downtime upgrades)

## Context

Migrations today (`store/migration.go`, `migration/service.go`):

- 14 sequentially-numbered embedded SQL files, applied in lexicographic
  order inside a per-file transaction.
- Idempotency by **string-matching driver errors** ("duplicate column
  name", "already exists") and skipping the statement.
- Integrity: two checksum columns (legacy FNV `checksum` +
  `checksum_sha256`) recorded per file; boot refuses on drift
  (`ErrSchemaIntegrityBroken`).
- **`RollbackMigration` does not roll back.** It `DELETE`s the
  bookkeeping rows from `schema_migrations`/`migration_checksums` and
  leaves all DDL in place — "rollback" means "unmark", then the next
  boot re-applies the same file.
- **No down migrations, no data-migration mechanism** (the one data
  migration, `013_episodic_timestamps_unix_ms`, is raw SQL with no
  progress tracking or resume).
- Schema changes happen **outside** the framework too:
  `metrics.InitMetricsDB` runs `CREATE TABLE IF NOT EXISTS` at boot with
  errors ignored; `init.go` creates the migration tables imperatively
  and `ALTER`s with duplicate-column string matching.
- Version identity is the **filename** — renaming a file looks like a
  new migration.

## Why the current design breaks

1. **False rollback semantics.** An operator who runs
   `db migrate rollback` before a downgrade believes the schema
   reverted; it didn't. With 5000 deployments on mixed versions this
   produces silent schema/binary skew — the worst failure mode in fleet
   operations.
2. **No zero-downtime path.** SQLite `ALTER TABLE` rewrites the table
   under a write lock; on a 10M-row `entities` table that's minutes of
   full write outage per migration. There is no expand-contract
   (add-column → backfill → dual-read → drop) discipline.
3. **Data migrations are ad-hoc.** Backfills (tenant_id from ADR-030,
   embedding recompute from ADR-028, timestamp conversions) need chunked
   resumable jobs with progress, not one-shot SQL.
4. **String-matched idempotency is fragile.** Driver error text is not
   an API; it varies across `mattn/go-sqlite3` versions and is
   meaningless for any future second engine (ADR-027).
5. **Two sources of schema truth.** Tables created outside the migration
   ledger (metrics) can't be drift-checked, can't be rolled back, and
   surprise operators diffing the ledger against reality.
6. **Checksum scheme is doubled** (FNV + SHA-256) — legacy cruft that
   new contributors must rediscover each time.

## Decision

Build migration framework v2:

1. **Migration = Go struct**, not bare SQL file:
   ```go
   type Migration struct {
       ID       string // "20260718-tenant-id", stable, never renamed
       Up       func(ctx, Exec) error
       Down     func(ctx, Exec) error   // required, or explicitly marked irreversible
       Data     *DataMigration          // optional chunked backfill spec
       Engines  []string                // sqlite, pg — per-engine SQL allowed
   }
   ```
   Registered in code; SQL files remain as assets for pure-DDL cases.
2. **Real `Down`** executed on rollback; irreversible migrations are
   flagged and require `--force` with a printed blast radius.
3. **Data migrations** run as chunked, resumable jobs (keyset
   pagination, progress persisted in the ledger, throttle knob), separate
   from DDL, executed by `hermem db migrate data --resume`.
4. **Expand-contract enforced by convention + CI check:** destructive
   changes (DROP, type narrowing) must ship one release after the
   corresponding expand migration; a linter flags same-release
   expand+drop pairs.
5. **Single checksum** (SHA-256 over canonical content + ID); the FNV
   column is dropped by a v2 ledger migration.
6. **All DDL goes through the ledger.** `metrics_entity_access` and the
   migration tables are converted to migrations; `CREATE TABLE IF NOT
   EXISTS` outside the ledger is added to the pre-push guardrail.
7. Boot keeps refusal-mode (no auto-migrate in prod); adds
   `db migrate plan` showing pending Up/Down/Data per deployment.

## Alternatives considered

1. **Adopt golang-migrate / goose / atlas.** Viable; rejected as
   *wholesale* replacement: they don't provide resumable chunked data
   migrations or expand-contract linting, and goose's Go migrations are
   close to the design above — adopting goose with a thin hermem layer
   is an acceptable implementation shortcut (left to the implementer).
2. **Keep forward-only, document rollback as "restore from backup".**
   Rejected as the only story: restores at 5000 fleets are hours of
   downtime; down-migrations cover the common "deploy failed health
   checks" case in seconds.
3. **ORM-managed schema (ent/Atlas declarative).** Rejected: conflicts
   with ADR-027's engine-specific SQL and the recursive-CTE walk.

## Tradeoffs

- **`Down` functions are real work** and sometimes genuinely impossible
  (data-destroying transforms); the irreversible flag makes the danger
  explicit instead of pretending (today's fake rollback pretends).
- **Go-struct migrations couple schema to binary version** — which is
  already true; the ledger now records binary version per migration for
  fleet observability.
- **Chunked backfills are slower than one-shot SQL** but bounded,
  observable, resumable — the only acceptable shape at 10M+ rows.
- **Migration count grows faster** (expand + contract as two entries);
  accepted — history is append-only anyway.

## Consequences

- Rollback means rollback; fleet downgrade playbooks become safe.
- Zero-downtime deploys (expand → roll → contract) become the default
  pattern, documented in `docs/migration-workflow.md`.
- Schema truth has exactly one ledger; drift detection covers every
  table.
- Unblocks ADR-028 (drop embedding BLOB), ADR-030 (tenant backfill) —
  both are data migrations this framework makes routine.
