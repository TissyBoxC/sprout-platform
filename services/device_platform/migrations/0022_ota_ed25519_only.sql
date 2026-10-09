-- Restrict firmware signing to Ed25519.
--
-- The platform verifier only implements detached Ed25519 signatures over the
-- lowercase SHA-256 digest. Keeping unused algorithm values in the database
-- check constraint let operators store a manifest that could never be
-- published, so the constraint is tightened to match the verifier exactly.

ALTER TABLE ota_releases
    DROP CONSTRAINT IF EXISTS ota_releases_signature_algorithm_valid;

ALTER TABLE ota_releases
    ADD CONSTRAINT ota_releases_signature_algorithm_valid
    CHECK (signature_algorithm = 'ed25519');
