-- Video session module (视频通话会话): the guardian media policy and the
-- bounded per-device session status.
--
-- The guardian owns one media policy per family. Every bound device reads the
-- same revision over its device session and reports back a bounded session
-- status. The default policy keeps the camera and microphone off, so a device
-- never starts capturing before a guardian enables a permission. The platform
-- stores only negotiation metadata (ring window, session ceiling, consent gate,
-- quality band, call state and end reason); no audio or video media is ever
-- persisted here.

CREATE TABLE IF NOT EXISTS video_session_policies (
    family_id UUID PRIMARY KEY REFERENCES parent_accounts(id) ON DELETE CASCADE,
    schema_version INT NOT NULL DEFAULT 1,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    camera_allowed BOOLEAN NOT NULL DEFAULT FALSE,
    microphone_allowed BOOLEAN NOT NULL DEFAULT FALSE,
    require_child_consent BOOLEAN NOT NULL DEFAULT TRUE,
    quality_floor TEXT NOT NULL DEFAULT 'poor',
    ring_timeout_seconds INT NOT NULL DEFAULT 30,
    max_session_seconds INT NOT NULL DEFAULT 1800,
    revision INT NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT video_session_policies_quality_valid
        CHECK (quality_floor IN ('unknown', 'good', 'fair', 'poor')),
    CONSTRAINT video_session_policies_ring_valid
        CHECK (ring_timeout_seconds BETWEEN 5 AND 120),
    CONSTRAINT video_session_policies_session_valid
        CHECK (max_session_seconds BETWEEN 60 AND 7200),
    CONSTRAINT video_session_policies_revision_valid
        CHECK (revision >= 1)
);

-- One row per bound device. A device that never reported is simply absent, so
-- the guardian UI hides the session controls instead of showing dead ones.

CREATE TABLE IF NOT EXISTS device_video_session_status (
    device_id TEXT PRIMARY KEY
        REFERENCES device_credentials(device_id) ON DELETE CASCADE,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    session_supported BOOLEAN NOT NULL DEFAULT FALSE,
    camera_allowed BOOLEAN NOT NULL DEFAULT FALSE,
    microphone_allowed BOOLEAN NOT NULL DEFAULT FALSE,
    call_state TEXT NOT NULL DEFAULT 'idle',
    quality TEXT NOT NULL DEFAULT 'unknown',
    last_end_reason TEXT NOT NULL DEFAULT 'none',
    policy_revision INT NOT NULL DEFAULT 0,
    reported_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT device_video_session_status_state_valid
        CHECK (call_state IN (
            'idle', 'ringing', 'active', 'ended', 'rejected', 'failed'
        )),
    CONSTRAINT device_video_session_status_quality_valid
        CHECK (quality IN ('unknown', 'good', 'fair', 'poor')),
    CONSTRAINT device_video_session_status_reason_valid
        CHECK (last_end_reason IN (
            'none', 'hangup', 'rejected', 'timeout', 'network',
            'permission_revoked'
        )),
    CONSTRAINT device_video_session_status_revision_valid
        CHECK (policy_revision >= 0)
);

CREATE INDEX IF NOT EXISTS device_video_session_status_family_idx
    ON device_video_session_status (family_id, reported_at DESC);
