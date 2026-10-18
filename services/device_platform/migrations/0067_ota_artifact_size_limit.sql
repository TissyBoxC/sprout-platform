-- The production application partition is 16 MiB. Keep the database gate
-- aligned with the domain validator and signer so an oversized draft cannot
-- persist and later fail only during publication.

ALTER TABLE ota_releases
    DROP CONSTRAINT IF EXISTS ota_releases_size_valid;

ALTER TABLE ota_releases
    ADD CONSTRAINT ota_releases_size_valid
        CHECK (size_bytes > 0 AND size_bytes <= 16777216);
