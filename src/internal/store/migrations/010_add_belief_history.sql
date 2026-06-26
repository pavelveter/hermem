-- Migration 010: add append-only belief_history table for P2 MEMORY
-- EVOLUTION (C9: Belief history tracking).
--
-- Every mutation that changes a Belief's confidence, status, or
-- superseded_by relationship MUST be recorded here as an append-only
-- INSERT. Rows are NEVER mutated or deleted after creation.
--
-- The reason column documents why the change happened (e.g.
-- "confidence propagation", "manual edit", "superseded by #42").
--
-- FK ON DELETE CASCADE so belief deletion cleans up history.

CREATE TABLE IF NOT EXISTS belief_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    belief_id INTEGER NOT NULL REFERENCES beliefs(id) ON DELETE CASCADE,
    confidence REAL NOT NULL CHECK(confidence >= 0.0 AND confidence <= 1.0),
    status TEXT NOT NULL DEFAULT 'Active',
    reason TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_belief_history_belief_id ON belief_history(belief_id);
CREATE INDEX IF NOT EXISTS idx_belief_history_created_at ON belief_history(created_at);
