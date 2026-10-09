-- Track rollback deployments separately from the release they replace.
--
-- A rollback may target a release that the same device already installed, so
-- the original release/device uniqueness constraint cannot be reused. The
-- rollback columns keep the source deployment and target release explicit for
-- device resume, guardian status, and audit.

ALTER TABLE ota_deployments
    ADD COLUMN IF NOT EXISTS rollback_release_id UUID
        REFERENCES ota_releases(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS rollback_of_deployment_id UUID
        REFERENCES ota_deployments(id) ON DELETE SET NULL;

ALTER TABLE ota_deployments
    DROP CONSTRAINT IF EXISTS ota_deployments_release_device_unique;

CREATE UNIQUE INDEX IF NOT EXISTS ota_deployments_active_release_device_idx
    ON ota_deployments(release_id, device_id)
    WHERE rollback_of_deployment_id IS NULL;

ALTER TABLE ota_deployments
    DROP CONSTRAINT IF EXISTS ota_deployments_rollback_coherent;

ALTER TABLE ota_deployments
    ADD CONSTRAINT ota_deployments_rollback_coherent
    CHECK (
        rollback_of_deployment_id IS NULL
        OR (
            rollback_release_id IS NOT NULL
            AND rollback_release_id = release_id
        )
    );
