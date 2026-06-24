-- 003_provenance.sql
-- Sprint 2 memory provenance columns. Tracks which conversation and
-- message an entity was extracted from, enabling memory explainability
-- and audit trails.

ALTER TABLE entities ADD COLUMN conversation_id TEXT DEFAULT '';
ALTER TABLE entities ADD COLUMN message_id TEXT DEFAULT '';
ALTER TABLE entities ADD COLUMN extracted_from TEXT DEFAULT '';
