-- Runtime hardening for provisioning snapshots that were first introduced in
-- 0015. The additional counter is kept in a separate migration because
-- deployed databases have already recorded 0015 as applied.

ALTER TABLE device_runtime_status
    ADD COLUMN IF NOT EXISTS provisioning_dropped_events BIGINT NOT NULL DEFAULT 0;

ALTER TABLE device_runtime_status
    DROP CONSTRAINT IF EXISTS device_runtime_provisioning_dropped_events_valid;

ALTER TABLE device_runtime_status
    ADD CONSTRAINT device_runtime_provisioning_dropped_events_valid
        CHECK (provisioning_dropped_events BETWEEN 0 AND 1000000);
