-- 004_episodic_sessions.sql
-- Phase 10: Episodic memory — sessions and conversations tables.
-- Sessions group conversations; conversations link to entities via
-- the existing entities.conversation_id column.
--
-- SELF-HEALING: 002 also adds `entities.created_at`. If a hermem.db
-- was created before 002 was amended (or 002 was partially applied
-- under the pre-b2 single-tx runner that silently marked the whole
-- migration as skipped on the first duplicate column), this ALTER
-- re-creates the column if it's missing and backfills NULL rows so
-- the timeline index has data to point at.
--
-- The ALTER omits DEFAULT on purpose: SQLite <3.31 rejects non-
-- constant DEFAULT clauses in ALTER TABLE ADD COLUMN, and OLD runner
-- behaviour produced the user's exact symptom (CGo-linked
-- mattn/go-sqlite3 on darwin uses the system libsqlite3 which is
-- often below 3.31). 002 follows the same nullable + backfill
-- pattern for the same reason — be consistent.

ALTER TABLE entities ADD COLUMN created_at DATETIME;

-- Backfill NULL created_at rows from updated_at (the existing column
-- every entity has since 001). Rows with NULL updated_at stay NULL
-- created_at; the index on a nullable column still works.
UPDATE entities SET created_at = updated_at WHERE created_at IS NULL;

CREATE TABLE IF NOT EXISTS sessions (
    id         TEXT PRIMARY KEY,
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ended_at   DATETIME,
    metadata   TEXT DEFAULT '{}' -- JSON blob for extensibility
);

CREATE TABLE IF NOT EXISTS conversations (
    id         TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    summary    TEXT DEFAULT '',
    metadata   TEXT DEFAULT '{}' -- JSON blob for extensibility
);

CREATE INDEX IF NOT EXISTS idx_conversations_session
    ON conversations(session_id);

-- Index for timeline queries (ORDER BY created_at DESC).
CREATE INDEX IF NOT EXISTS idx_entities_created_at
    ON entities(created_at);
