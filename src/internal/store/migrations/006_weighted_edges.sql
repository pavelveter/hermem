-- 006_weighted_edges.sql
-- Weighted edges: adds a weight column to edges for semantic path modulation.
-- Weight 1.0 = normal edge; lower weight = stronger/shorter semantic link.
-- Used in graph walk CTE as path_weight accumulator.

ALTER TABLE edges ADD COLUMN weight REAL DEFAULT 1.0;
