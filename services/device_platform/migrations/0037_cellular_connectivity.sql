-- Guardian cellular policy and device connectivity status (P2-04).
--
-- One policy row per family: the guardian enables cellular, chooses the radio
-- preference and roaming, sets the monthly data quota and metered upload limit,
-- and every bound device in the family reads the same revision. Values are
-- bounded here as well as in the service so a direct write cannot install an
-- unbounded quota or an unsupported preference.

CREATE TABLE IF NOT EXISTS cellular_policies (
    family_id UUID PRIMARY KEY REFERENCES parent_accounts(id) ON DELETE CASCADE,
    schema_version INTEGER NOT NULL DEFAULT 1,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    roaming_allowed BOOLEAN NOT NULL DEFAULT FALSE,
    remote_wake_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    metered_upload_limit BOOLEAN NOT NULL DEFAULT TRUE,
    preference TEXT NOT NULL DEFAULT 'wifi_first',
    monthly_quota_mb INTEGER NOT NULL DEFAULT 2048,
    low_quota_warn_percent INTEGER NOT NULL DEFAULT 20,
    keepalive_seconds INTEGER NOT NULL DEFAULT 300,
    revision INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT cellular_policies_schema_version_valid CHECK (schema_version >= 1),
    CONSTRAINT cellular_policies_preference_valid
        CHECK (preference IN ('wifi_first', 'cellular_first', 'cellular_only')),
    CONSTRAINT cellular_policies_quota_valid
        CHECK (monthly_quota_mb BETWEEN 0 AND 512000),
    CONSTRAINT cellular_policies_warn_valid
        CHECK (low_quota_warn_percent BETWEEN 5 AND 100),
    CONSTRAINT cellular_policies_keepalive_valid
        CHECK (keepalive_seconds BETWEEN 30 AND 3600),
    CONSTRAINT cellular_policies_revision_non_negative CHECK (revision >= 0)
);

-- One row per device reporting the active bearer, roaming state, eSIM state,
-- signal strength and consumed data for the current window.

CREATE TABLE IF NOT EXISTS device_connectivity_status (
    device_id TEXT PRIMARY KEY REFERENCES device_credentials(device_id) ON DELETE CASCADE,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    bearer TEXT NOT NULL DEFAULT 'none',
    roaming BOOLEAN NOT NULL DEFAULT FALSE,
    esim_state TEXT NOT NULL DEFAULT 'absent',
    signal_percent INTEGER NOT NULL DEFAULT 0,
    used_mb INTEGER NOT NULL DEFAULT 0,
    policy_revision INTEGER NOT NULL DEFAULT 0,
    reported_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT device_connectivity_status_bearer_valid
        CHECK (bearer IN ('wifi', 'cellular', 'none')),
    CONSTRAINT device_connectivity_status_esim_valid
        CHECK (esim_state IN ('absent', 'installed', 'active', 'disabled')),
    CONSTRAINT device_connectivity_status_signal_valid
        CHECK (signal_percent BETWEEN 0 AND 100),
    CONSTRAINT device_connectivity_status_used_valid
        CHECK (used_mb BETWEEN 0 AND 512000),
    CONSTRAINT device_connectivity_status_policy_revision_non_negative
        CHECK (policy_revision >= 0)
);

CREATE INDEX IF NOT EXISTS device_connectivity_status_family_idx
    ON device_connectivity_status (family_id, device_id);
