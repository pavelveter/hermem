# Migration Workflow

## Overview

Hermem uses embedded SQL migrations applied automatically at boot.
Each migration is a `.sql` file in `src/internal/store/migrations/`.
Migrations run in lexicographic order and are tracked in the
`schema_migrations` and `migration_checksums` tables.

## Commands

| Command                      | Description                                      |
|------------------------------|--------------------------------------------------|
| `hermem db migrate`          | Show applied/pending status (with SHA-256 checksums) |
| `hermem db dry-run`          | Show pending migrations without applying them     |
| `hermem db rollback`         | Roll back the most-recent applied migration       |
| `hermem db rollback --target=N` | Roll back all migrations after version N       |
| `hermem db verify`           | Verify SHA-256 checksums of all applied migrations |
| `hermem db schema`           | Show stored vs current schema fingerprint         |

## Adding a Migration

1. Add a new SQL file in `src/internal/store/migrations/`.
   Name format: `NNN_description.sql` (e.g. `008_add_index.sql`).
2. Run `hermem db migrate` to confirm it shows as pending.
3. Restart the service (migrations run at boot) or apply manually
   by restarting `InitDB`.

## Integrity

Every migration is checksummed with SHA-256 at apply time. The checksum
is stored in `migration_checksums.checksum_sha256`.

To verify integrity:

```bash
hermem db verify
```

If a migration file is tampered with after application, `verify` reports
a mismatch with both the stored and current checksum.

## Rollback

Rollback deletes the migration row from `schema_migrations` and the
checksum from `migration_checksums`. It does NOT reverse SQL schema
changes — rollback is a metadata operation.

Roll back the last migration:

```bash
hermem db rollback
```

Roll back to a specific version (exclusive):

```bash
hermem db rollback --target=003_provenance.sql
```

## Recovery

If a migration fails mid-apply, the transaction is rolled back entirely
— no partial state is written. The failed migration stays unapplied and
can be retried after fixing the cause.

## Architecture

- `store/migration.go` — low-level DB operations (apply, checksum, verify)
- `migration/service.go` — transport-agnostic domain service
- `cli/db/*.go` — CLI transport shell
- `server/migration/*.go` — HTTP transport shell
