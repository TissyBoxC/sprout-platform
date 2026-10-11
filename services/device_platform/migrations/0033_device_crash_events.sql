-- Redacted device crash reports.
--
-- A crash record is a bounded, symbolic signature: cause, fault kind, the
-- crash-reset flag, the faulting task name, a single program counter, the
-- frame count, and the reporting core. Raw memory, registers, backtrace
-- addresses, audio, images, credentials, and child content are never stored.
-- The program counter is a single address used to group crash signatures, not
-- a full trace that could be replayed.

CREATE TABLE IF NOT EXISTS device_crash_events (
    id UUID PRIMARY KEY,
    device_id TEXT NOT NULL,
    event_id TEXT NOT NULL,
    sequence BIGINT NOT NULL,
    cause TEXT NOT NULL,
    fault_kind TEXT NOT NULL,
    task_name TEXT NOT NULL,
    program_counter BIGINT NOT NULL,
    frame_count BIGINT NOT NULL,
    core_id BIGINT NOT NULL,
    firmware_version TEXT NOT NULL,
    reported_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT device_crash_events_event_unique UNIQUE (device_id, event_id),
    CONSTRAINT device_crash_events_sequence_non_negative CHECK (sequence >= 0),
    CONSTRAINT device_crash_events_cause_valid
        CHECK (
            cause IN (
                'panic',
                'watchdog',
                'task_watchdog',
                'interrupt_watchdog',
                'brownout',
                'stack_overflow',
                'heap_corruption',
                'assert',
                'unknown'
            )
        ),
    CONSTRAINT device_crash_events_fault_kind_valid
        CHECK (
            fault_kind IN (
                'exception',
                'watchdog',
                'power_loss',
                'memory',
                'software'
            )
        ),
    CONSTRAINT device_crash_events_program_counter_non_negative
        CHECK (program_counter >= 0),
    CONSTRAINT device_crash_events_frame_count_valid
        CHECK (frame_count BETWEEN 0 AND 64),
    CONSTRAINT device_crash_events_core_id_valid
        CHECK (core_id BETWEEN 0 AND 3)
);

CREATE INDEX IF NOT EXISTS device_crash_events_device_idx
    ON device_crash_events(device_id, received_at DESC);
