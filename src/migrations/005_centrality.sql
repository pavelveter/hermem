-- 005_centrality.sql
-- Graph centrality scoring: degree column on entities with auto-maintenance
-- triggers on edges. Degree counts total edges (in + out) for each entity.

ALTER TABLE entities ADD COLUMN degree INTEGER DEFAULT 0;

-- Initialize degree for existing edges.
UPDATE entities SET degree = (
    SELECT COUNT(*) FROM edges
    WHERE source_id = entities.id OR target_id = entities.id
);

-- Auto-increment degree on edge insertion.
CREATE TRIGGER IF NOT EXISTS trg_edges_insert_degree
AFTER INSERT ON edges
BEGIN
    UPDATE entities SET degree = degree + 1 WHERE id = NEW.source_id;
    UPDATE entities SET degree = degree + 1 WHERE id = NEW.target_id;
END;

-- Auto-decrement degree on edge deletion.
CREATE TRIGGER IF NOT EXISTS trg_edges_delete_degree
AFTER DELETE ON edges
BEGIN
    UPDATE entities SET degree = degree - 1 WHERE id = OLD.source_id;
    UPDATE entities SET degree = degree - 1 WHERE id = OLD.target_id;
END;
