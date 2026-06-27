-- Migration 013: P2 EPISODIC MEMORY — timestamp hardening.
--
-- SQLite has no `TIMESTAMP WITH TIME ZONE` type. Migration 011
-- originally used SQLite `DATETIME` (which is just a TEXT-shaped
-- `YYYY-MM-DD HH:MM:SS` — TZ-unaware at the storage layer). The
-- audit at /refactor identified that persisted DATETIME values
-- could carry an implicit non-UTC zone depending on how the writer
-- shaped the time.Time before save, breaking the "what was earlier"
-- ordering invariant under distributed deploys or TZ-shifted
-- migration.
--
-- This migration introduces INTEGER (Unix epoch milliseconds,
-- UTC) columns in their place. The Go-side helper
-- `src/internal/util/time.UnixMillisFromTime` normalises every
-- write through `.UTC()` BEFORE serialising so a non-UTC value
-- physically cannot land in the column — the invariant moves from
-- "convention" to "type-enforced".
--
-- CAVEAT: pre-existing rows are assumed UTC-formatted. The
-- julianday-based backfill (julianday(col) - 2440587.5) parses
-- any DATETIME text as if its digits were UTC; this is correct
-- ONLY for rows inserted via the project's Go code path (which
-- lets mattn/go-sqlite3 serialise time.Time to UTC by default).
-- Rows that carry a non-UTC string are not back-fillable from
-- this migration — re-import from a canonical source if the
-- audit surfaces non-UTC values in production.
--
-- The steps are:
--
--   1. ADD COLUMN *_ms INTEGER with sensible defaults;
--   2. UPDATE *_ms from the existing DATETIME (julianday→ms epoch);
--   3. DROP INDEX IF EXISTS on the DATETIME column (REQUIRED
--      BEFORE step 4 — SQLite's DROP COLUMN refuses to proceed if
--      any active index references the column and emits
--      "error in index <name> after drop column: no such column");
--   4. DROP COLUMN old_DATETIME_col (SQLite >= 3.35.0);
--   5. CREATE INDEX IF NOT EXISTS on the new ms column
--      (idempotent on re-run).
--
-- Foreign-key-side effects: events.episode_id, episode_memories.
-- episode_id, and episode_tasks.episode_id reference episodes(id);
-- DROP COLUMN on episodes never touches episode_id, so FKs are
-- unchanged.
--
-- Per-statement idempotency (b2 runner) absorbs "duplicate column
-- name" on re-run; CREATE INDEX IF NOT EXISTS + DROP INDEX IF
-- EXISTS absorb the index lifecycle steps. DROP COLUMN on a
-- column-that-already-doesn't-exist produces "no such column"
-- (NOT in idempotency list) — but the migration runs inside one
-- transaction (RunMigrations per-statement runner wraps the
-- migration file in a single tx), so a partial failure rolls
-- back cleanly; an idempotent retry restarts from the pre-013
-- schema state.

-- ------------------------------------------------------------------
-- episodes
-- ------------------------------------------------------------------
ALTER TABLE episodes ADD COLUMN started_at_ms INTEGER NOT NULL DEFAULT 0;
UPDATE episodes SET started_at_ms = CAST((julianday(started_at) - 2440587.5) * 86400000 AS INTEGER) WHERE started_at IS NOT NULL;
DROP INDEX IF EXISTS idx_episodes_started_at;
ALTER TABLE episodes DROP COLUMN started_at;

ALTER TABLE episodes ADD COLUMN ended_at_ms INTEGER;
UPDATE episodes SET ended_at_ms = CAST((julianday(ended_at) - 2440587.5) * 86400000 AS INTEGER) WHERE ended_at IS NOT NULL;
ALTER TABLE episodes DROP COLUMN ended_at;

CREATE INDEX IF NOT EXISTS idx_episodes_started_at_ms ON episodes(started_at_ms);

-- ------------------------------------------------------------------
-- events
-- ------------------------------------------------------------------
ALTER TABLE events ADD COLUMN timestamp_ms INTEGER NOT NULL DEFAULT 0;
UPDATE events SET timestamp_ms = CAST((julianday(timestamp) - 2440587.5) * 86400000 AS INTEGER) WHERE timestamp IS NOT NULL;
DROP INDEX IF EXISTS idx_events_timestamp;
DROP INDEX IF EXISTS idx_events_episode_timestamp;
ALTER TABLE events DROP COLUMN timestamp;

CREATE INDEX IF NOT EXISTS idx_events_timestamp_ms ON events(timestamp_ms);
CREATE INDEX IF NOT EXISTS idx_events_episode_timestamp_ms ON events(episode_id, timestamp_ms);

-- ------------------------------------------------------------------
-- episode_memories
-- ------------------------------------------------------------------
ALTER TABLE episode_memories ADD COLUMN linked_at_ms INTEGER NOT NULL DEFAULT 0;
UPDATE episode_memories SET linked_at_ms = CAST((julianday(linked_at) - 2440587.5) * 86400000 AS INTEGER) WHERE linked_at IS NOT NULL;
ALTER TABLE episode_memories DROP COLUMN linked_at;

-- ------------------------------------------------------------------
-- episode_tasks
-- ------------------------------------------------------------------
ALTER TABLE episode_tasks ADD COLUMN linked_at_ms INTEGER NOT NULL DEFAULT 0;
UPDATE episode_tasks SET linked_at_ms = CAST((julianday(linked_at) - 2440587.5) * 86400000 AS INTEGER) WHERE linked_at IS NOT NULL;
ALTER TABLE episode_tasks DROP COLUMN linked_at;
