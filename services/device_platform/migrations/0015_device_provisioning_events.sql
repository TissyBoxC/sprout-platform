-- Device provisioning audit trail and the latest provisioning snapshot.
--
-- Provisioning events describe the network bring-up path (Wi-Fi
-- configuration, binding, time sync, session revocation) without storing the
-- SSID, the Wi-Fi password, device keys, or any child data. The bounded event
-- history mirrors the diagnostic retention model; the status columns let the
-- admin list render the current step without joining the event table.

-- The columns stay nullable because a heartbeat may omit the provisioning
-- extension. A missing value preserves the previously recorded state instead
-- of resetting a provisioned device back to unprovisioned.
ALTER TABLE device_runtime_status
    ADD COLUMN IF NOT EXISTS provisioning_state TEXT,
    ADD COLUMN IF NOT EXISTS wifi_configured BOOLEAN,
    ADD COLUMN IF NOT EXISTS session_state TEXT,
    ADD COLUMN IF NOT EXISTS last_provisioned_at TIMESTAMPTZ;

ALTER TABLE device_runtime_status
    DROP CONSTRAINT IF EXISTS device_runtime_provisioning_state_valid;

ALTER TABLE device_runtime_status
    ADD CONSTRAINT device_runtime_provisioning_state_valid
        CHECK (provisioning_state IN ('unprovisioned', 'provisioning', 'provisioned'));

ALTER TABLE device_runtime_status
    DROP CONSTRAINT IF EXISTS device_runtime_session_state_valid;

ALTER TABLE device_runtime_status
    ADD CONSTRAINT device_runtime_session_state_valid
        CHECK (session_state IN ('ready', 'reauth_required', 'revoked'));

CREATE TABLE IF NOT EXISTS device_provisioning_events (
    id UUID PRIMARY KEY,
    device_id TEXT NOT NULL,
    event_id TEXT NOT NULL,
    sequence BIGINT NOT NULL,
    event_type TEXT NOT NULL,
    detail_code TEXT NOT NULL,
    duration_ms BIGINT NOT NULL DEFAULT 0,
    firmware_version TEXT NOT NULL,
    reported_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT device_provisioning_events_event_unique UNIQUE (device_id, event_id),
    CONSTRAINT device_provisioning_events_sequence_positive CHECK (sequence > 0),
    CONSTRAINT device_provisioning_events_type_valid CHECK (
        event_type IN (
            'provisioning_started',
            'wifi_configured',
            'wifi_failed',
            'binding_completed',
            'binding_removed',
            'network_reconnected',
            'network_lost',
            'time_synced',
            'auth_revoked',
            'auth_restored',
            'binding_confirmed',
            'binding_pending'
        )
    ),
    CONSTRAINT device_provisioning_events_detail_not_blank
        CHECK (length(btrim(detail_code)) > 0),
    CONSTRAINT device_provisioning_events_duration_valid
        CHECK (duration_ms BETWEEN 0 AND 3600000)
);

CREATE INDEX IF NOT EXISTS device_provisioning_events_device_idx
    ON device_provisioning_events(device_id, received_at DESC);

CREATE INDEX IF NOT EXISTS device_provisioning_events_type_idx
    ON device_provisioning_events(device_id, event_type, received_at DESC);
