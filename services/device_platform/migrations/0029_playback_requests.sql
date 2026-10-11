-- Guardian-initiated on-demand playback requests (P1-05 点播).
--
-- A guardian asks one bound device to start a published content package. The
-- request is family-scoped on the platform; the device lists its pending
-- requests and reports a terminal outcome. Requests never carry content bytes,
-- conversation text, audio, or upstream errors: only a package id plus the
-- small delivery state machine below.

CREATE TABLE IF NOT EXISTS playback_requests (
    id UUID PRIMARY KEY,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    device_id TEXT NOT NULL REFERENCES device_credentials(device_id) ON DELETE CASCADE,
    package_id TEXT NOT NULL,
    source TEXT NOT NULL DEFAULT 'guardian',
    status TEXT NOT NULL DEFAULT 'pending',
    note TEXT NOT NULL DEFAULT '',
    requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    acknowledged_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    outcome TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT playback_requests_package_id_valid
        CHECK (package_id ~ '^[a-z][a-z0-9]*(?:_[a-z0-9]+){1,7}$'),
    CONSTRAINT playback_requests_source_valid
        CHECK (source IN ('guardian', 'recommendation')),
    CONSTRAINT playback_requests_status_valid
        CHECK (status IN ('pending', 'acknowledged', 'completed', 'failed', 'cancelled')),
    CONSTRAINT playback_requests_outcome_valid
        CHECK (outcome IS NULL OR outcome IN ('played', 'skipped', 'error')),
    CONSTRAINT playback_requests_note_valid
        CHECK (length(note) <= 200)
);

CREATE INDEX IF NOT EXISTS playback_requests_family_idx
    ON playback_requests (family_id, requested_at DESC);

CREATE INDEX IF NOT EXISTS playback_requests_device_pending_idx
    ON playback_requests (device_id, status, requested_at)
    WHERE status IN ('pending', 'acknowledged');
