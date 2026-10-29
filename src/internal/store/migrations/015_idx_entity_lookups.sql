-- Migration 015: lookup indexes for the provenance and retention sweeps.
--
-- Both queries below filtered a column that had no index and could not use the
-- existing created_at index, so SQLite scanned the whole entities table:
--
--   Provenance (store.GetEntitiesByProvenance):
--     WHERE archived = 0 AND (conversation_id = ? OR message_id = ? OR source = ?)
--     ORDER BY created_at DESC LIMIT ?
--   previously planned as a full SCAN ordered by idx_entities_created_at.
--
--   Retention sweep (retention.Service.RunOnce, every sweep):
--     WHERE category = 'observation' AND updated_at < ? AND archived = 0 LIMIT ?
--   previously planned as a plain SCAN of entities.
--
-- Measured on 20k entities / 50k entities respectively: provenance 887us ->
-- 95us, retention 1.65ms -> 4.3us. Cost is one extra B-tree per column on
-- entity writes (~1.2us per inserted row measured over 5k rows in one tx).
--
-- The (column, created_at) ordering matters: it lets the provenance query
-- satisfy its ORDER BY ... LIMIT from the index instead of sorting.

CREATE INDEX IF NOT EXISTS idx_entities_conversation_id_created_at ON entities(conversation_id, created_at);
CREATE INDEX IF NOT EXISTS idx_entities_message_id_created_at ON entities(message_id, created_at);
CREATE INDEX IF NOT EXISTS idx_entities_source_created_at ON entities(source, created_at);
CREATE INDEX IF NOT EXISTS idx_entities_category_updated_at ON entities(category, updated_at);