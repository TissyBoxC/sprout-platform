-- Growth timeline query support.
--
-- The timeline owns no table: it merges rows other features already wrote. The
-- only thing it can legitimately add is an index that makes the merge cheap.
--
-- Two child-scoped reads are not served by an existing index. The timeline asks
-- for one child's finished assessments (assessment_sessions has only a
-- (family_id, started_at) index and a partial index on active rows) and its
-- growth records (companionship_growth_records has only a (family_id,
-- recorded_at) index). Both are read by exactly this feature and both scan a
-- whole family today before filtering by child.
--
-- Indexes only, and each one IF NOT EXISTS: re-running this migration on a
-- database where a DBA already added the index must be a no-op rather than an
-- error that blocks startup.
CREATE INDEX IF NOT EXISTS assessment_sessions_family_child_finished_idx
    ON assessment_sessions (family_id, child_id, finished_at DESC)
    WHERE state = 'completed';

CREATE INDEX IF NOT EXISTS companionship_growth_records_family_child_recorded_idx
    ON companionship_growth_records (family_id, child_id, recorded_at DESC);
