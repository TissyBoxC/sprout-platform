-- Device boot and removable-module diagnostics.
--
-- Diagnostic payloads are optional extensions of the existing device
-- heartbeat. Keeping them in dedicated tables preserves the runtime status
-- hot path while allowing operators to inspect boot history and failures.

CREATE TABLE IF NOT EXISTS device_boot_events (
    id UUID PRIMARY KEY,
    device_id TEXT NOT NULL,
    event_id TEXT NOT NULL,
    sequence BIGINT NOT NULL,
    uptime_ms BIGINT NOT NULL,
    boot_count BIGINT NOT NULL,
    reset_reason TEXT NOT NULL,
    firmware_version TEXT NOT NULL,
    reported_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT device_boot_events_event_unique UNIQUE (device_id, event_id),
    CONSTRAINT device_boot_events_sequence_non_negative CHECK (sequence >= 0),
    CONSTRAINT device_boot_events_uptime_non_negative CHECK (uptime_ms >= 0),
    CONSTRAINT device_boot_events_boot_count_non_negative CHECK (boot_count >= 0)
);

CREATE INDEX IF NOT EXISTS device_boot_events_device_idx
    ON device_boot_events(device_id, received_at DESC);

CREATE TABLE IF NOT EXISTS device_module_failures (
    id UUID PRIMARY KEY,
    device_id TEXT NOT NULL,
    event_id TEXT NOT NULL,
    sequence BIGINT NOT NULL,
    module_name TEXT NOT NULL,
    error_code TEXT NOT NULL,
    failure_count BIGINT NOT NULL,
    firmware_version TEXT NOT NULL,
    reported_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT device_module_failures_event_unique UNIQUE (device_id, event_id),
    CONSTRAINT device_module_failures_sequence_non_negative CHECK (sequence >= 0),
    CONSTRAINT device_module_failures_count_non_negative CHECK (failure_count >= 0),
    CONSTRAINT device_module_failures_module_not_blank CHECK (length(btrim(module_name)) > 0),
    CONSTRAINT device_module_failures_error_not_blank CHECK (length(btrim(error_code)) > 0)
);

CREATE INDEX IF NOT EXISTS device_module_failures_device_idx
    ON device_module_failures(device_id, received_at DESC);

CREATE TABLE IF NOT EXISTS device_recovery_events (
    id UUID PRIMARY KEY,
    device_id TEXT NOT NULL,
    event_id TEXT NOT NULL,
    sequence BIGINT NOT NULL,
    module_name TEXT NOT NULL,
    firmware_version TEXT NOT NULL,
    reported_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT device_recovery_events_event_unique UNIQUE (device_id, event_id),
    CONSTRAINT device_recovery_events_sequence_non_negative CHECK (sequence >= 0),
    CONSTRAINT device_recovery_events_module_not_blank CHECK (length(btrim(module_name)) > 0)
);

CREATE INDEX IF NOT EXISTS device_recovery_events_device_idx
    ON device_recovery_events(device_id, received_at DESC);
