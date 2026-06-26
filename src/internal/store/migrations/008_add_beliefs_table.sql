-- Migration 008: add the first-class `beliefs` table for the
-- P2 MEMORY EVOLUTION subsystem (commit C1: Add Belief abstraction).
--
-- This table is the canonical persistence shape for first-class Beliefs.
-- The thin `core.Belief` projection off of `core.Entity` is preserved
-- untouched for backward compatibility with retrieval/contradiction paths.
--
-- Status semantics:
--   Active     \u2014 default; live belief, retrievable.
--   Superseded \u2014 replaced by a successor belief (SupersededBy FK).
--   Archived   \u2014 retired; excluded from retrieval unless explicitly opted in.
--
-- Self-referential FKs (superseded_by, parent_chain_id) are filled in
-- later commits (C6 revision chains).

CREATE TABLE beliefs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    content TEXT NOT NULL,
    confidence REAL NOT NULL DEFAULT 1.0
        CHECK(confidence >= 0.0 AND confidence <= 1.0),
    source_kind TEXT,
    source_id TEXT,
    status TEXT NOT NULL DEFAULT 'Active'
        CHECK(status IN ('Active','Superseded','Archived')),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    superseded_by INTEGER NULL REFERENCES beliefs(id),
    parent_chain_id INTEGER NULL REFERENCES beliefs(id),
    archived_at TIMESTAMP NULL,
    last_accessed_at TIMESTAMP NULL
);

-- Index status first: most queries (Active-only retrievals) filter on it.
-- Composite source index enables provenance lookups.
CREATE INDEX idx_beliefs_status ON beliefs(status);
CREATE INDEX idx_beliefs_source_kind_source_id
    ON beliefs(source_kind, source_id);
CREATE INDEX idx_beliefs_superseded_by ON beliefs(superseded_by)
    WHERE superseded_by IS NOT NULL;
