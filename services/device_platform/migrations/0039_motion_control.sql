-- Guardian motion, motor and servo policy and device motion status (P2-06).
--
-- One policy row per family: the guardian sets the locomotion permission, the
-- speed ceiling, the maximum run time and the stop-acknowledgement rule, and
-- every bound device in the family reads the same revision. Values are bounded
-- here as well as in the service so a direct write cannot install an unsafe
-- run time or an unrestricted speed.

CREATE TABLE IF NOT EXISTS motion_policies (
    family_id UUID PRIMARY KEY REFERENCES parent_accounts(id) ON DELETE CASCADE,
    schema_version INTEGER NOT NULL DEFAULT 1,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    require_stop_ack BOOLEAN NOT NULL DEFAULT TRUE,
    allow_locomotion BOOLEAN NOT NULL DEFAULT FALSE,
    max_speed_percent INTEGER NOT NULL DEFAULT 60,
    max_runtime_ms INTEGER NOT NULL DEFAULT 5000,
    stop_grace_ms INTEGER NOT NULL DEFAULT 300,
    revision INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT motion_policies_schema_version_valid CHECK (schema_version >= 1),
    CONSTRAINT motion_policies_speed_valid
        CHECK (max_speed_percent BETWEEN 1 AND 100),
    CONSTRAINT motion_policies_runtime_valid
        CHECK (max_runtime_ms BETWEEN 200 AND 60000),
    CONSTRAINT motion_policies_grace_valid
        CHECK (stop_grace_ms BETWEEN 0 AND 60000),
    CONSTRAINT motion_policies_revision_non_negative CHECK (revision >= 0)
);

-- One row per device reporting the latest motion lifecycle state.

CREATE TABLE IF NOT EXISTS device_motion_status (
    device_id TEXT PRIMARY KEY REFERENCES device_credentials(device_id) ON DELETE CASCADE,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    state TEXT NOT NULL DEFAULT 'idle',
    action TEXT NOT NULL DEFAULT 'idle',
    actuator_count INTEGER NOT NULL DEFAULT 0,
    faulted BOOLEAN NOT NULL DEFAULT FALSE,
    policy_revision INTEGER NOT NULL DEFAULT 0,
    reported_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT device_motion_status_state_valid
        CHECK (state IN ('idle', 'running', 'stopped', 'timeout', 'fault')),
    CONSTRAINT device_motion_status_action_valid
        CHECK (action IN ('idle', 'walk', 'dance', 'tail_wag', 'gesture')),
    CONSTRAINT device_motion_status_actuator_valid
        CHECK (actuator_count BETWEEN 0 AND 32),
    CONSTRAINT device_motion_status_policy_revision_non_negative
        CHECK (policy_revision >= 0)
);

CREATE INDEX IF NOT EXISTS device_motion_status_family_idx
    ON device_motion_status (family_id, device_id);
