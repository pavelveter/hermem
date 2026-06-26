-- Migration 011: P2 EPISODIC MEMORY — episodes, events, and link tables.
--
-- Phase 11 builds a rich episodic layer on top of the existing
-- sessions (migration 004) and conversations (004) tables. An
-- Episode groups a bounded time-window of Events (messages / actions
-- / observations / system signals) with optional links to the
-- entities (memory facts, tasks) that were extracted during it.
--
-- ON DELETE CASCADE on link-table FKs matches the existing
-- pattern in 004_episodic_sessions.sql — deleting an episode
-- should clean up its events and links without leaving orphans.
-- ON DELETE CASCADE on events.episode_id ensures event cleanup
-- follows episode deletion.

-- ------------------------------------------------------------------
-- episodes — first-class episodic memory unit
-- ------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS episodes (
    id              TEXT PRIMARY KEY,
    session_id      TEXT REFERENCES sessions(id) ON DELETE SET NULL,
    conversation_id TEXT REFERENCES conversations(id) ON DELETE SET NULL,
    title           TEXT NOT NULL DEFAULT '',
    summary         TEXT NOT NULL DEFAULT '',
    started_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ended_at        DATETIME,
    metadata        TEXT NOT NULL DEFAULT '{}' -- JSON blob for extensibility
);

CREATE INDEX IF NOT EXISTS idx_episodes_session
    ON episodes(session_id);

CREATE INDEX IF NOT EXISTS idx_episodes_conversation
    ON episodes(conversation_id);

CREATE INDEX IF NOT EXISTS idx_episodes_started_at
    ON episodes(started_at);

-- ------------------------------------------------------------------
-- events — fine-grained episodic signals (one row per occurrence)
-- ------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS events (
    id         TEXT PRIMARY KEY,
    episode_id TEXT NOT NULL REFERENCES episodes(id) ON DELETE CASCADE,
    -- One of message | action | observation | system.
    -- Enforced via CHECK rather than a separate events_type enum
    -- table to keep the schema flat and SQLite-cheap.
    type       TEXT NOT NULL CHECK(type IN ('message', 'action', 'observation', 'system')),
    content    TEXT NOT NULL DEFAULT '',
    timestamp  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    metadata   TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_events_episode
    ON events(episode_id);

CREATE INDEX IF NOT EXISTS idx_events_timestamp
    ON events(timestamp);

CREATE INDEX IF NOT EXISTS idx_events_episode_timestamp
    ON events(episode_id, timestamp);

-- ------------------------------------------------------------------
-- episode_memories — many-to-many: episodes ↔ memory entities
-- ------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS episode_memories (
    episode_id TEXT NOT NULL REFERENCES episodes(id) ON DELETE CASCADE,
    entity_id  TEXT NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    role       TEXT NOT NULL DEFAULT 'extracted', -- extracted | referenced | mentioned
    linked_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (episode_id, entity_id, role)
);

CREATE INDEX IF NOT EXISTS idx_episode_memories_entity
    ON episode_memories(entity_id);

-- ------------------------------------------------------------------
-- episode_tasks — many-to-many: episodes ↔ task entities
-- ------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS episode_tasks (
    episode_id TEXT NOT NULL REFERENCES episodes(id) ON DELETE CASCADE,
    task_id    TEXT NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    linked_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (episode_id, task_id)
);

CREATE INDEX IF NOT EXISTS idx_episode_tasks_task
    ON episode_tasks(task_id);
