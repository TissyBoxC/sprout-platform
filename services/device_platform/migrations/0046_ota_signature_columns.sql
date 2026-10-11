-- OTA release signature columns.
--
-- The OTA repository has always selected and written a detached signature
-- together with its verification state, but the ota_releases table only ever
-- declared signature_key_id and signature_algorithm. Every ListReleases /
-- GetRelease / CreateRelease statement therefore failed with
-- "column release.signature does not exist", which surfaced in the admin UI as
-- the app_update, ota_release and download panels reporting 功能暂时不可用 and
-- the OTA release list returning HTTP 500.
--
-- The columns are added additively so the migration is safe on a populated
-- table: existing drafts backfill to an empty signature with a pending status.

ALTER TABLE ota_releases
    ADD COLUMN IF NOT EXISTS signature TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS signature_status TEXT NOT NULL DEFAULT 'pending',
    ADD COLUMN IF NOT EXISTS signature_verified_at TIMESTAMPTZ;

ALTER TABLE ota_releases
    DROP CONSTRAINT IF EXISTS ota_releases_signature_status_valid;

ALTER TABLE ota_releases
    ADD CONSTRAINT ota_releases_signature_status_valid
        CHECK (signature_status IN ('pending', 'verified', 'failed', 'missing'));

-- The published manifest is immutable, but the detached signature was missing
-- from the guard clause, so a published release signature could still be
-- rewritten. Recreate the trigger function with signature included.
CREATE OR REPLACE FUNCTION sprout_ota_release_immutable()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.status <> 'draft' AND (
        NEW.version IS DISTINCT FROM OLD.version
        OR NEW.channel IS DISTINCT FROM OLD.channel
        OR NEW.hardware_revision IS DISTINCT FROM OLD.hardware_revision
        OR NEW.min_source_version IS DISTINCT FROM OLD.min_source_version
        OR NEW.artifact_url IS DISTINCT FROM OLD.artifact_url
        OR NEW.artifact_key IS DISTINCT FROM OLD.artifact_key
        OR NEW.sha256 IS DISTINCT FROM OLD.sha256
        OR NEW.size_bytes IS DISTINCT FROM OLD.size_bytes
        OR NEW.signature_key_id IS DISTINCT FROM OLD.signature_key_id
        OR NEW.signature_algorithm IS DISTINCT FROM OLD.signature_algorithm
        OR NEW.signature IS DISTINCT FROM OLD.signature
        OR NEW.rollback_allowed IS DISTINCT FROM OLD.rollback_allowed
        OR NEW.mandatory IS DISTINCT FROM OLD.mandatory
        OR NEW.release_notes IS DISTINCT FROM OLD.release_notes
        OR NEW.target_scope IS DISTINCT FROM OLD.target_scope
        OR NEW.target_group_id IS DISTINCT FROM OLD.target_group_id
        OR NEW.target_device_id IS DISTINCT FROM OLD.target_device_id
        OR NEW.canary_percent IS DISTINCT FROM OLD.canary_percent
        OR NEW.rollback_release_id IS DISTINCT FROM OLD.rollback_release_id
    ) THEN
        RAISE EXCEPTION 'OTA release manifest is immutable after publication';
    END IF;
    RETURN NEW;
END;
$$;
