-- Video companionship contacts, calls and device call status (P2-08).
--
-- A family owns the contacts a guardian can call and a bounded call history.
-- The platform never stores media: only call metadata, the negotiated quality
-- band and the end reason, so a revoked camera or microphone permission ends
-- the session without any silent capture.

CREATE TABLE IF NOT EXISTS video_contacts (
    id UUID PRIMARY KEY,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    child_id UUID REFERENCES child_profiles(id) ON DELETE SET NULL,
    display_name TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'guardian',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT video_contacts_name_valid
        CHECK (char_length(display_name) BETWEEN 1 AND 40),
    CONSTRAINT video_contacts_role_valid
        CHECK (role IN ('guardian', 'family', 'device'))
);

CREATE INDEX IF NOT EXISTS video_contacts_family_idx
    ON video_contacts (family_id, created_at DESC);

-- One row per call the guardian or a device starts. Only metadata is stored.

CREATE TABLE IF NOT EXISTS video_calls (
    id UUID PRIMARY KEY,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    device_id TEXT REFERENCES device_credentials(device_id) ON DELETE SET NULL,
    contact_id UUID REFERENCES video_contacts(id) ON DELETE SET NULL,
    initiator TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'ringing',
    quality TEXT NOT NULL DEFAULT 'unknown',
    end_reason TEXT,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    answered_at TIMESTAMPTZ,
    ended_at TIMESTAMPTZ,
    CONSTRAINT video_calls_initiator_valid
        CHECK (initiator IN ('guardian', 'device')),
    CONSTRAINT video_calls_state_valid
        CHECK (state IN ('ringing', 'active', 'ended', 'rejected', 'missed', 'failed')),
    CONSTRAINT video_calls_quality_valid
        CHECK (quality IN ('unknown', 'good', 'fair', 'poor')),
    CONSTRAINT video_calls_end_reason_valid
        CHECK (end_reason IS NULL OR end_reason IN (
            'hangup', 'rejected', 'missed', 'timeout', 'network',
            'permission_revoked', 'device_offline'
        ))
);

CREATE INDEX IF NOT EXISTS video_calls_family_idx
    ON video_calls (family_id, started_at DESC);

CREATE INDEX IF NOT EXISTS video_calls_device_idx
    ON video_calls (device_id, started_at DESC);

-- One row per device with the latest call and permission state.

CREATE TABLE IF NOT EXISTS device_video_status (
    device_id TEXT PRIMARY KEY REFERENCES device_credentials(device_id) ON DELETE CASCADE,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    call_state TEXT NOT NULL DEFAULT 'idle',
    quality TEXT NOT NULL DEFAULT 'unknown',
    camera_allowed BOOLEAN NOT NULL DEFAULT FALSE,
    microphone_allowed BOOLEAN NOT NULL DEFAULT FALSE,
    last_call_id UUID,
    reported_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT device_video_status_state_valid
        CHECK (call_state IN ('idle', 'ringing', 'active', 'ended', 'rejected', 'missed', 'failed')),
    CONSTRAINT device_video_status_quality_valid
        CHECK (quality IN ('unknown', 'good', 'fair', 'poor'))
);

CREATE INDEX IF NOT EXISTS device_video_status_family_idx
    ON device_video_status (family_id, device_id);
