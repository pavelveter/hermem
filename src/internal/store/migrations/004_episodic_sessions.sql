-- 004_episodic_sessions.sql
-- Phase 10: Episodic memory — sessions and conversations tables.
-- Sessions group conversations; conversations link to entities via
-- the existing entities.conversation_id column.

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
