-- 012_create_summary_nodes.sql
-- P2 SEMANTIC COMPRESSION: summary_nodes table for persisting
-- SummaryNode domain objects. Each row corresponds to one
-- compression operation over a set of source entities.

CREATE TABLE IF NOT EXISTS summary_nodes (
    id             TEXT PRIMARY KEY,
    content        TEXT NOT NULL DEFAULT '',
    compressed_from TEXT NOT NULL DEFAULT '[]',    -- JSON array of source entity IDs
    compressed_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    confidence     REAL NOT NULL DEFAULT 0.0,
    provenance     TEXT NOT NULL DEFAULT '',
    generation     INTEGER NOT NULL DEFAULT 1,
    extractor_model TEXT NOT NULL DEFAULT '',
    superseded_by  TEXT REFERENCES summary_nodes(id) ON DELETE SET NULL,
    regenerated_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_summary_nodes_generation
    ON summary_nodes(generation);

CREATE INDEX IF NOT EXISTS idx_summary_nodes_superseded_by
    ON summary_nodes(superseded_by);
