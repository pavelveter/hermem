-- 007_task_priorities.sql
-- P2: Task priorities for execution planning. Higher priority tasks
-- are scheduled first when multiple tasks are executable.
-- Priority 0 = default (lowest), negative = deferred.

ALTER TABLE entities ADD COLUMN priority INTEGER DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_entities_priority
    ON entities(priority);
