-- Guardian touch configuration and device touch status (P2-03).
--
-- One configuration row per family: the guardian enables or disables the touch
-- panel, chooses which gesture classes are active, tunes the sensitivity,
-- rotation and gesture thresholds, and every bound device in the family reads
-- the same revision. Values are bounded here as well as in the service so a
-- direct write cannot install an unsupported rotation or an unbounded gesture
-- threshold.

CREATE TABLE IF NOT EXISTS touch_configs (
    family_id UUID PRIMARY KEY REFERENCES parent_accounts(id) ON DELETE CASCADE,
    schema_version INTEGER NOT NULL DEFAULT 1,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    tap_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    swipe_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    long_press_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    haptic_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    sensitivity INTEGER NOT NULL DEFAULT 60,
    rotation_degrees INTEGER NOT NULL DEFAULT 0,
    long_press_ms INTEGER NOT NULL DEFAULT 700,
    double_tap_ms INTEGER NOT NULL DEFAULT 300,
    swipe_distance_px INTEGER NOT NULL DEFAULT 60,
    revision INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT touch_configs_schema_version_valid CHECK (schema_version >= 1),
    CONSTRAINT touch_configs_sensitivity_valid
        CHECK (sensitivity BETWEEN 10 AND 100),
    CONSTRAINT touch_configs_rotation_valid
        CHECK (rotation_degrees IN (0, 90, 180, 270)),
    CONSTRAINT touch_configs_long_press_valid
        CHECK (long_press_ms BETWEEN 250 AND 3000),
    CONSTRAINT touch_configs_double_tap_valid
        CHECK (double_tap_ms BETWEEN 120 AND 600),
    CONSTRAINT touch_configs_swipe_distance_valid
        CHECK (swipe_distance_px BETWEEN 16 AND 480),
    CONSTRAINT touch_configs_revision_non_negative CHECK (revision >= 0)
);

-- One row per device reporting whether it has a touch panel, whether it can
-- drive haptics, and how many gestures it classified in the last window.

CREATE TABLE IF NOT EXISTS touch_device_status (
    device_id TEXT PRIMARY KEY REFERENCES device_credentials(device_id) ON DELETE CASCADE,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    touch_supported BOOLEAN NOT NULL DEFAULT FALSE,
    haptic_supported BOOLEAN NOT NULL DEFAULT FALSE,
    gesture_count INTEGER NOT NULL DEFAULT 0,
    policy_revision INTEGER NOT NULL DEFAULT 0,
    reported_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT touch_device_status_gesture_count_valid
        CHECK (gesture_count BETWEEN 0 AND 1000000),
    CONSTRAINT touch_device_status_policy_revision_non_negative
        CHECK (policy_revision >= 0)
);

CREATE INDEX IF NOT EXISTS touch_device_status_family_idx
    ON touch_device_status (family_id, device_id);
