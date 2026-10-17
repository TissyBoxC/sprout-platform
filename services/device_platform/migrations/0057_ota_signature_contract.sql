-- Align persisted OTA signatures with the firmware's Ed25519 manifest parser.
--
-- A detached Ed25519 signature is exactly 64 bytes, or 88 canonical standard
-- Base64 characters. Key IDs are rendered into a fixed 65-byte firmware field,
-- so accepting longer values would create releases that cannot be installed.

ALTER TABLE ota_releases
    DROP CONSTRAINT IF EXISTS ota_releases_signature_key_not_blank;

ALTER TABLE ota_releases
    ADD CONSTRAINT ota_releases_signature_key_valid
        CHECK (
            signature_key_id ~ '^[A-Za-z0-9._:-]{1,64}$'
        );

ALTER TABLE ota_releases
    DROP CONSTRAINT IF EXISTS ota_releases_signature_base64_canonical;

-- Migration 0046 backfilled historical drafts to an empty pending signature.
-- Some environments instead stored an older non-Base64 placeholder. Those are
-- unverified drafts, so normalize them to the same safe state before enforcing
-- the firmware parser's exact 64-byte signature contract.
UPDATE ota_releases
SET signature = ''
WHERE signature_status = 'pending'
  AND signature <> ''
  AND (
      length(signature) <> 88
      OR signature !~ '^[A-Za-z0-9+/]{86}==$'
  );

ALTER TABLE ota_releases
    ADD CONSTRAINT ota_releases_signature_base64_canonical
        CHECK (
            signature = ''
            OR (
                signature_status = 'pending'
                AND length(signature) <= 8192
                AND signature ~ '^[A-Za-z0-9+/]+={0,2}$'
            )
            OR (
                signature_status IN ('verified', 'failed', 'missing')
                AND length(signature) = 88
                AND signature ~ '^[A-Za-z0-9+/]{86}==$'
            )
        );
