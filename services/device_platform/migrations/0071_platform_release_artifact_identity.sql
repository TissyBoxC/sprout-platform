-- A release version identifies a delivery line, not one downloadable file.
-- APK, Admin Web, contracts, and firmware for the same brand release must be
-- independently addressable without weakening duplicate protection.
--
-- Existing rows are already unique by version, so replacing the legacy
-- constraint cannot collide. The composite identity matches the delivery
-- semantics used by LatestPublishedUpdate and the management API.
--
-- The migration runner already wraps each file in a transaction and records
-- the applied version atomically, so this file must not manage transactions.

ALTER TABLE platform_releases
    DROP CONSTRAINT IF EXISTS platform_releases_version_unique;

ALTER TABLE platform_releases
    DROP CONSTRAINT IF EXISTS platform_releases_artifact_identity_unique;

ALTER TABLE platform_releases
    ADD CONSTRAINT platform_releases_artifact_identity_unique
    UNIQUE (version, channel, kind, platform);
