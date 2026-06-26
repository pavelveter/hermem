-- Migration 009: add the Evidence table for P2 MEMORY EVOLUTION (C2).
--
-- Evidence is a typed artifact that supports or refutes a Belief. Each row
-- is bound to exactly one Belief (FK CASCADE on delete), carries a polarity
-- (support|refute), an absolute strength in [0,1], a content text, and
-- optional source provenance. The table is the canonical persistence shape
-- for pre-aggregation evidence; C3 (Confidence propagation) will combine
-- Evidence.Strength by polarity to update Belief.Confidence.
--
-- FK ON DELETE CASCADE so that deleting a Belief removes the Evidence rows
-- that reference it. Evidence cannot live independently of its parent.

CREATE TABLE IF NOT EXISTS evidence (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    belief_id INTEGER NOT NULL REFERENCES beliefs(id) ON DELETE CASCADE,
    polarity TEXT NOT NULL CHECK(polarity IN ('support','refute')),
    strength REAL NOT NULL CHECK(strength >= 0.0 AND strength <= 1.0),
    content TEXT NOT NULL,
    source_kind TEXT,
    source_id TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_evidence_belief_id ON evidence(belief_id);
CREATE INDEX IF NOT EXISTS idx_evidence_source ON evidence(source_kind, source_id);
