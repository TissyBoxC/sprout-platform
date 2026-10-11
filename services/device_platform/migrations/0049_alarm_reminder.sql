-- Alarm reminder module (定时提醒、闹钟和日程).
--
-- A family owns a bounded set of scheduled alarms the device renders and rings
-- locally. Each alarm keeps a local wall-clock time, an ISO weekday set, a
-- label, an optional prompt and the child it belongs to. Rows are retained
-- across "off" toggles so guardian history survives, and the child reference is
-- soft (no cascade) so an alarm outliving a profile deletion stays cleanup-able
-- by the family owner. The platform stores no audio; the device synthesizes the
-- tone from its own prompt-tone catalog.

CREATE TABLE IF NOT EXISTS alarm_reminders (
    id UUID PRIMARY KEY,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    child_id UUID,
    label TEXT NOT NULL,
    prompt TEXT NOT NULL DEFAULT '',
    alarm_time TIME NOT NULL,
    weekdays SMALLINT[] NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    snooze_minutes INT NOT NULL DEFAULT 5,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT alarm_reminders_label_length
        CHECK (char_length(label) BETWEEN 1 AND 48),
    CONSTRAINT alarm_reminders_prompt_length
        CHECK (char_length(prompt) <= 240),
    CONSTRAINT alarm_reminders_snooze_valid
        CHECK (snooze_minutes BETWEEN 1 AND 30)
);

CREATE INDEX IF NOT EXISTS alarm_reminders_family_idx
    ON alarm_reminders(family_id, created_at DESC);

-- One firing acknowledgement per alarm and local alarm date. A device reports a
-- ringing alarm once; a repeated report for the same date is idempotent so a
-- retry after a reconnect never double-counts. Only bounded scalars are stored
-- (state and the local date identifier); no audio or transcript.

CREATE TABLE IF NOT EXISTS alarm_reminder_events (
    id UUID PRIMARY KEY,
    alarm_id UUID NOT NULL REFERENCES alarm_reminders(id) ON DELETE CASCADE,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    device_id TEXT,
    alarm_date TEXT NOT NULL,
    state TEXT NOT NULL,
    reported_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT alarm_reminder_events_date_valid
        CHECK (alarm_date ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$'),
    CONSTRAINT alarm_reminder_events_state_valid
        CHECK (state IN ('ringing', 'snoozed', 'dismissed', 'missed')),
    CONSTRAINT alarm_reminder_events_unique
        UNIQUE (alarm_id, alarm_date)
);

CREATE INDEX IF NOT EXISTS alarm_reminder_events_family_idx
    ON alarm_reminder_events(family_id, reported_at DESC);
