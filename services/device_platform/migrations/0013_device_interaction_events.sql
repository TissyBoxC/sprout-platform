-- Device interaction events and the guarded remote factory reset command.
--
-- Interaction events describe user-visible device behaviour (wake word,
-- physical buttons, indicator state, factory reset) without carrying audio,
-- credentials, or child content. They share the bounded diagnostic retention
-- model and are written through the same authenticated heartbeat extension.

ALTER TABLE device_runtime_commands
    DROP CONSTRAINT IF EXISTS device_runtime_command_type_valid;

ALTER TABLE device_runtime_commands
    ADD CONSTRAINT device_runtime_command_type_valid
        CHECK (
            command_type IN (
                'refresh_configuration',
                'reconnect_network',
                'resync_time',
                'factory_reset'
            )
        );

CREATE TABLE IF NOT EXISTS device_interaction_events (
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
    CONSTRAINT device_interaction_events_event_unique UNIQUE (device_id, event_id),
    CONSTRAINT device_interaction_events_sequence_positive CHECK (sequence > 0),
    CONSTRAINT device_interaction_events_type_valid CHECK (
        event_type IN (
            'wake_detected',
            'wake_rejected',
            'button_gesture',
            'indicator_state',
            'factory_reset_requested',
            'factory_reset_cancelled',
            'factory_reset_completed',
            'factory_reset_failed'
        )
    ),
    CONSTRAINT device_interaction_events_detail_not_blank
        CHECK (length(btrim(detail_code)) > 0),
    CONSTRAINT device_interaction_events_duration_valid
        CHECK (duration_ms BETWEEN 0 AND 3600000)
);

CREATE INDEX IF NOT EXISTS device_interaction_events_device_idx
    ON device_interaction_events(device_id, received_at DESC);

CREATE INDEX IF NOT EXISTS device_interaction_events_type_idx
    ON device_interaction_events(device_id, event_type, received_at DESC);
