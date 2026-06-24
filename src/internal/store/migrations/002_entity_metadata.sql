-- 002_entity_metadata.sql
-- Sprint 2 entity metadata columns. Adds last_accessed_at (GC tracking),
-- archived flag, confidence scoring, source attribution, temporal validity
-- window, and creation timestamp.
-- Backfills last_accessed_at from updated_at for pre-existing rows.

ALTER TABLE entities ADD COLUMN last_accessed_at DATETIME;
ALTER TABLE entities ADD COLUMN archived INTEGER DEFAULT 0;
ALTER TABLE entities ADD COLUMN confidence REAL DEFAULT 1.0;
ALTER TABLE entities ADD COLUMN source TEXT DEFAULT '';
ALTER TABLE entities ADD COLUMN source_type TEXT DEFAULT '';
ALTER TABLE entities ADD COLUMN created_at DATETIME;
ALTER TABLE entities ADD COLUMN valid_from DATETIME;
ALTER TABLE entities ADD COLUMN valid_to DATETIME;

-- Backfill last_accessed_at for rows created before the column existed.
-- SQLite does not allow non-constant defaults in ALTER TABLE ADD COLUMN,
-- so we add the column nullable and populate it separately.
UPDATE entities SET last_accessed_at = updated_at WHERE last_accessed_at IS NULL;

-- Backfill created_at for rows created before the column existed.
UPDATE entities SET created_at = updated_at WHERE created_at IS NULL;
