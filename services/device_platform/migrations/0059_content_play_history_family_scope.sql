-- Playback history must not become visible to a new guardian after a device
-- is unbound and rebound. Existing rows lack a trustworthy family owner, so
-- erase them rather than guessing from the current binding.

DELETE FROM content_play_history;

ALTER TABLE content_play_history
    ADD COLUMN IF NOT EXISTS family_id UUID NOT NULL
        REFERENCES parent_accounts(id) ON DELETE CASCADE;

DROP INDEX IF EXISTS content_play_history_device_idx;

CREATE INDEX IF NOT EXISTS content_play_history_family_device_idx
    ON content_play_history(family_id, device_id, played_at DESC);
