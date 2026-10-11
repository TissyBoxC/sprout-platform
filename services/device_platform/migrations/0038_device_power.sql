-- Guardian battery and low-power policy and device power status (P2-05).
--
-- One policy row per family: the guardian sets the low-battery warning, the
-- low-power entry threshold and the deep-sleep idle window, and every bound
-- device in the family reads the same revision. Values are bounded here as well
-- as in the service so a direct write cannot install an unsafe sleep window.

CREATE TABLE IF NOT EXISTS power_policies (
    family_id UUID PRIMARY KEY REFERENCES parent_accounts(id) ON DELETE CASCADE,
    schema_version INTEGER NOT NULL DEFAULT 1,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    low_power_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    deep_sleep_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    charge_priority BOOLEAN NOT NULL DEFAULT TRUE,
    low_battery_warn_percent INTEGER NOT NULL DEFAULT 20,
    low_power_enter_percent INTEGER NOT NULL DEFAULT 15,
    sleep_after_seconds INTEGER NOT NULL DEFAULT 1800,
    revision INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT power_policies_schema_version_valid CHECK (schema_version >= 1),
    CONSTRAINT power_policies_warn_valid
        CHECK (low_battery_warn_percent BETWEEN 5 AND 50),
    CONSTRAINT power_policies_low_power_valid
        CHECK (low_power_enter_percent BETWEEN 5 AND 50),
    CONSTRAINT power_policies_sleep_valid
        CHECK (sleep_after_seconds BETWEEN 60 AND 86400),
    CONSTRAINT power_policies_revision_non_negative CHECK (revision >= 0)
);

-- One row per device reporting the latest charge state, power mode and battery
-- percentage.

CREATE TABLE IF NOT EXISTS device_power_status (
    device_id TEXT PRIMARY KEY REFERENCES device_credentials(device_id) ON DELETE CASCADE,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    charge_state TEXT NOT NULL DEFAULT 'unknown',
    power_mode TEXT NOT NULL DEFAULT 'normal',
    battery_percent INTEGER NOT NULL DEFAULT 0,
    millivolts INTEGER NOT NULL DEFAULT 0,
    charging BOOLEAN NOT NULL DEFAULT FALSE,
    policy_revision INTEGER NOT NULL DEFAULT 0,
    reported_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT device_power_status_charge_valid
        CHECK (charge_state IN ('unknown', 'discharging', 'charging', 'full')),
    CONSTRAINT device_power_status_mode_valid
        CHECK (power_mode IN ('normal', 'low_power', 'deep_sleep')),
    CONSTRAINT device_power_status_percent_valid
        CHECK (battery_percent BETWEEN 0 AND 100),
    CONSTRAINT device_power_status_millivolts_valid
        CHECK (millivolts BETWEEN 0 AND 6000),
    CONSTRAINT device_power_status_policy_revision_non_negative
        CHECK (policy_revision >= 0)
);

CREATE INDEX IF NOT EXISTS device_power_status_family_idx
    ON device_power_status (family_id, device_id);
