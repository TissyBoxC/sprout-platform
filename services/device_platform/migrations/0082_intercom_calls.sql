-- Voice intercom between a guardian and a device.
--
-- The guardian can switch intercom off for the whole family. It is separate
-- from the per-child voice policy because calling the device is a household
-- setting, not a learning setting.
CREATE TABLE IF NOT EXISTS intercom_family_settings (
    family_id UUID PRIMARY KEY REFERENCES parent_accounts (id) ON DELETE CASCADE,
    is_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Calls themselves.
--
-- Only lifecycle and timing are persisted. Audio is relayed live and never
-- stored, so this table deliberately has no media, transcript, or token column.
CREATE TABLE IF NOT EXISTS intercom_calls (
    id UUID PRIMARY KEY,
    family_id UUID NOT NULL REFERENCES parent_accounts (id) ON DELETE CASCADE,
    device_id TEXT NOT NULL REFERENCES device_bindings (device_id) ON DELETE CASCADE,
    child_id UUID REFERENCES child_profiles (id) ON DELETE SET NULL,
    state TEXT NOT NULL CHECK (
        state IN ('ringing', 'active', 'ended', 'missed', 'declined', 'cancelled')
    ),
    started_at TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    finish_reason TEXT NOT NULL DEFAULT '',
    -- An accepted call must carry its accept time, and a finished call its end
    -- time; keeping that in the schema stops a half-written row from making a
    -- call look active to the relay after it already ended.
    CONSTRAINT intercom_calls_lifecycle CHECK (
        (state = 'ringing' AND accepted_at IS NULL AND finished_at IS NULL)
        OR (state = 'active' AND accepted_at IS NOT NULL AND finished_at IS NULL)
        OR (state IN ('ended', 'missed', 'declined', 'cancelled')
            AND finished_at IS NOT NULL)
    ),
    CONSTRAINT intercom_calls_accepts_only_when_connected CHECK (
        accepted_at IS NULL
        OR state IN ('active', 'ended')
    ),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One device can only be in one unfinished call, otherwise two guardians could
-- hold the same microphone at once.
CREATE UNIQUE INDEX IF NOT EXISTS intercom_calls_open_device_idx
    ON intercom_calls (device_id)
    WHERE state IN ('ringing', 'active');

-- Rate limiting and guardian history both scan by family and time.
CREATE INDEX IF NOT EXISTS intercom_calls_family_started_idx
    ON intercom_calls (family_id, started_at DESC);

-- The worker closes expired calls; terminal rows never need rescanning.
CREATE INDEX IF NOT EXISTS intercom_calls_open_deadline_idx
    ON intercom_calls (started_at)
    WHERE state IN ('ringing', 'active');
