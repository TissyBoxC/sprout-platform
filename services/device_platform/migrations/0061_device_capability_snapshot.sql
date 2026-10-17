-- Runtime capability snapshots let a device reconcile its actual feature set
-- after OTA or configuration changes. The revision is device-generated and
-- monotonic; heartbeat replays must never roll back a newer snapshot.

ALTER TABLE device_credentials
    ADD COLUMN IF NOT EXISTS capability_revision BIGINT NOT NULL DEFAULT 0;

ALTER TABLE device_bindings
    ADD COLUMN IF NOT EXISTS capability_revision BIGINT NOT NULL DEFAULT 0;

ALTER TABLE device_runtime_status
    ADD COLUMN IF NOT EXISTS capabilities TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    ADD COLUMN IF NOT EXISTS capability_revision BIGINT NOT NULL DEFAULT 0;

ALTER TABLE device_credentials
    DROP CONSTRAINT IF EXISTS device_credentials_capability_revision_valid,
    ADD CONSTRAINT device_credentials_capability_revision_valid
        CHECK (capability_revision BETWEEN 0 AND 9007199254740991);

ALTER TABLE device_bindings
    DROP CONSTRAINT IF EXISTS device_bindings_capability_revision_valid,
    ADD CONSTRAINT device_bindings_capability_revision_valid
        CHECK (capability_revision BETWEEN 0 AND 9007199254740991);

ALTER TABLE device_runtime_status
    DROP CONSTRAINT IF EXISTS device_runtime_capability_revision_valid,
    ADD CONSTRAINT device_runtime_capability_revision_valid
        CHECK (capability_revision BETWEEN 0 AND 9007199254740991);

-- Capability names are validated against the shared contract vocabulary
-- before they reach persistence. These checks keep malformed or unbounded
-- snapshots out even when a future writer bypasses the service layer.
ALTER TABLE device_credentials
    DROP CONSTRAINT IF EXISTS device_credentials_capability_snapshot_valid,
    ADD CONSTRAINT device_credentials_capability_snapshot_valid
        CHECK (
            cardinality(capability_set) = 0
            OR (
                cardinality(capability_set) BETWEEN 1 AND 32
                AND capability_revision BETWEEN 1 AND 9007199254740991
            )
        );

ALTER TABLE device_bindings
    DROP CONSTRAINT IF EXISTS device_bindings_capability_snapshot_valid,
    ADD CONSTRAINT device_bindings_capability_snapshot_valid
        CHECK (
            cardinality(capability_set) = 0
            OR (
                cardinality(capability_set) BETWEEN 1 AND 32
                AND capability_revision BETWEEN 1 AND 9007199254740991
            )
        );

ALTER TABLE device_runtime_status
    DROP CONSTRAINT IF EXISTS device_runtime_capability_snapshot_valid,
    ADD CONSTRAINT device_runtime_capability_snapshot_valid
        CHECK (
            cardinality(capabilities) = 0
            OR (
                cardinality(capabilities) BETWEEN 1 AND 32
                AND capability_revision BETWEEN 1 AND 9007199254740991
            )
        );

-- Existing devices already registered a non-empty set. Seed the runtime
-- snapshot from the durable binding so guardians keep seeing a capability
-- list until the first revisioned heartbeat arrives.
UPDATE device_runtime_status AS status
SET capabilities = binding.capability_set,
    capability_revision = binding.capability_revision
FROM device_bindings AS binding
WHERE binding.device_id = status.device_id;

-- Legacy rows predate capability revisions. Seed a safe baseline only where
-- a non-empty snapshot already exists; future snapshots must use a positive
-- device-generated revision.
UPDATE device_credentials
SET capability_revision = 1
WHERE capability_revision = 0
  AND cardinality(capability_set) > 0;

UPDATE device_bindings
SET capability_revision = 1
WHERE capability_revision = 0
  AND cardinality(capability_set) > 0;

UPDATE device_runtime_status
SET capability_revision = 1
WHERE capability_revision = 0
  AND cardinality(capabilities) > 0;
