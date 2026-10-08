-- Add the device-reported daily usage ledger used by the guardian usage report.
--
-- A device may retry or backfill a day after being offline. The unique
-- (device_id, report_date) key makes the upsert idempotent, while the family
-- owner is resolved server-side and stored for scoped guardian/admin reads.
CREATE TABLE IF NOT EXISTS device_usage_daily (
    report_date DATE NOT NULL,
    parent_account_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    device_id TEXT NOT NULL,
    timezone_offset_minutes INTEGER NOT NULL,
    active_seconds INTEGER NOT NULL DEFAULT 0,
    conversation_count INTEGER NOT NULL DEFAULT 0,
    conversation_seconds INTEGER NOT NULL DEFAULT 0,
    content_play_count INTEGER NOT NULL DEFAULT 0,
    content_seconds INTEGER NOT NULL DEFAULT 0,
    category_usage JSONB NOT NULL DEFAULT '[]'::JSONB,
    blocked_counts JSONB NOT NULL DEFAULT '{}'::JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (device_id, report_date),
    CONSTRAINT device_usage_daily_timezone_valid
        CHECK (timezone_offset_minutes BETWEEN -840 AND 840),
    CONSTRAINT device_usage_daily_active_non_negative CHECK (active_seconds >= 0),
    CONSTRAINT device_usage_daily_conversation_count_non_negative
        CHECK (conversation_count >= 0),
    CONSTRAINT device_usage_daily_conversation_seconds_non_negative
        CHECK (conversation_seconds >= 0),
    CONSTRAINT device_usage_daily_content_count_non_negative
        CHECK (content_play_count >= 0),
    CONSTRAINT device_usage_daily_content_seconds_non_negative
        CHECK (content_seconds >= 0),
    CONSTRAINT device_usage_daily_category_usage_array
        CHECK (jsonb_typeof(category_usage) = 'array'),
    CONSTRAINT device_usage_daily_blocked_object
        CHECK (jsonb_typeof(blocked_counts) = 'object')
);

CREATE INDEX IF NOT EXISTS device_usage_daily_family_day_idx
    ON device_usage_daily(parent_account_id, report_date DESC);

CREATE INDEX IF NOT EXISTS device_usage_daily_device_day_idx
    ON device_usage_daily(device_id, report_date DESC);
