-- Live care view: a guardian opens a short, bounded viewing session on a
-- device the family owns.
--
-- This table stores only the envelope of a viewing session: who watched which
-- device, for how long, and why it ended. Frames themselves are relayed live and
-- are never written here, so a finished session cannot be replayed and a lost
-- frame cannot be recovered from the database. Keeping it that way is the point:
-- there is no archive of a child's room to leak or to subpoena later.
CREATE TABLE IF NOT EXISTS care_view_sessions (
    id UUID PRIMARY KEY,
    family_id UUID NOT NULL REFERENCES parent_accounts (id) ON DELETE CASCADE,
    device_id TEXT NOT NULL REFERENCES device_bindings (device_id) ON DELETE CASCADE,
    child_id UUID REFERENCES child_profiles (id) ON DELETE SET NULL,
    -- The guardian who opened the view. Kept for the access audit required by
    -- the child-safety baseline: every look at a child's room names a person.
    guardian_id TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('requested', 'streaming', 'ended', 'declined', 'failed')),
    started_at TIMESTAMPTZ NOT NULL,
    -- The session stops by itself at this instant even if nobody closes it, so a
    -- forgotten tab cannot keep a camera running.
    expires_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    finish_reason TEXT NOT NULL DEFAULT '',
    frames_delivered BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- A session that ended must say when and why; a live session must not.
    CONSTRAINT care_view_sessions_lifecycle CHECK (
        (state IN ('ended', 'declined', 'failed') AND finished_at IS NOT NULL)
        OR (state IN ('requested', 'streaming') AND finished_at IS NULL)
    ),
    -- Streaming implies frames flowed, so the timestamps must be ordered.
    CONSTRAINT care_view_sessions_time_order CHECK (expires_at > started_at)
);

-- One device can only be streamed to one guardian at a time. Two guardians
-- watching the same room simultaneously is both a privacy and a bandwidth
-- problem, and the device can only serve one capture pipeline.
CREATE UNIQUE INDEX IF NOT EXISTS care_view_sessions_open_device_idx
    ON care_view_sessions (device_id)
    WHERE state IN ('requested', 'streaming');

CREATE INDEX IF NOT EXISTS care_view_sessions_family_started_idx
    ON care_view_sessions (family_id, started_at DESC);

-- The expiry worker scans only open sessions, so keep that scan narrow.
CREATE INDEX IF NOT EXISTS care_view_sessions_open_expiry_idx
    ON care_view_sessions (expires_at)
    WHERE state IN ('requested', 'streaming');

-- Viewing requires the guardian's explicit image consent, stored per family so
-- one decision covers the household rather than being re-granted per view.
CREATE TABLE IF NOT EXISTS care_view_settings (
    family_id UUID PRIMARY KEY REFERENCES parent_accounts (id) ON DELETE CASCADE,
    is_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
