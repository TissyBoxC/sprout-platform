-- Guardian eye-care display policy and device screen status (P2-01/P2-02).
--
-- One policy row per family: the guardian edits brightness, colour
-- temperature, viewing distance, daily screen time and break reminders, and
-- every bound device in the family reads the same revision. Values are bounded
-- here as well as in the service so a direct write cannot install an unsafe
-- brightness or an unbounded screen-time cap.

CREATE TABLE IF NOT EXISTS display_policies (
    family_id UUID PRIMARY KEY REFERENCES parent_accounts(id) ON DELETE CASCADE,
    schema_version INTEGER NOT NULL DEFAULT 1,
    brightness_percent INTEGER NOT NULL DEFAULT 60,
    color_temperature_kelvin INTEGER NOT NULL DEFAULT 4500,
    min_view_distance_cm INTEGER NOT NULL DEFAULT 40,
    daily_limit_minutes INTEGER NOT NULL DEFAULT 60,
    break_interval_minutes INTEGER NOT NULL DEFAULT 20,
    dim_after_seconds INTEGER NOT NULL DEFAULT 60,
    auto_brightness BOOLEAN NOT NULL DEFAULT TRUE,
    night_light BOOLEAN NOT NULL DEFAULT TRUE,
    distance_reminder BOOLEAN NOT NULL DEFAULT TRUE,
    revision INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT display_policies_schema_version_valid CHECK (schema_version >= 1),
    CONSTRAINT display_policies_brightness_valid
        CHECK (brightness_percent BETWEEN 5 AND 100),
    CONSTRAINT display_policies_color_temperature_valid
        CHECK (color_temperature_kelvin BETWEEN 2700 AND 6500),
    CONSTRAINT display_policies_view_distance_valid
        CHECK (min_view_distance_cm BETWEEN 20 AND 120),
    CONSTRAINT display_policies_daily_limit_valid
        CHECK (daily_limit_minutes BETWEEN 5 AND 600),
    CONSTRAINT display_policies_break_interval_valid
        CHECK (break_interval_minutes BETWEEN 5 AND 240),
    CONSTRAINT display_policies_dim_after_valid
        CHECK (dim_after_seconds BETWEEN 10 AND 600),
    CONSTRAINT display_policies_revision_non_negative CHECK (revision >= 0)
);

-- One row per device reporting the brightest screen state it currently applies
-- and the display geometry the platform uses for capability resolution.

CREATE TABLE IF NOT EXISTS display_device_status (
    device_id TEXT PRIMARY KEY REFERENCES device_credentials(device_id) ON DELETE CASCADE,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    brightness_percent INTEGER NOT NULL DEFAULT 0,
    screen_width INTEGER NOT NULL DEFAULT 0,
    screen_height INTEGER NOT NULL DEFAULT 0,
    touch_supported BOOLEAN NOT NULL DEFAULT FALSE,
    policy_revision INTEGER NOT NULL DEFAULT 0,
    reported_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT display_device_status_brightness_valid
        CHECK (brightness_percent BETWEEN 0 AND 100),
    CONSTRAINT display_device_status_width_valid
        CHECK (screen_width BETWEEN 0 AND 8192),
    CONSTRAINT display_device_status_height_valid
        CHECK (screen_height BETWEEN 0 AND 8192),
    CONSTRAINT display_device_status_policy_revision_non_negative
        CHECK (policy_revision >= 0)
);

CREATE INDEX IF NOT EXISTS display_device_status_family_idx
    ON display_device_status (family_id, device_id);
