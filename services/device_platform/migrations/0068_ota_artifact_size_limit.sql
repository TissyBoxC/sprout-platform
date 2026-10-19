-- Each production OTA application partition is 4 MiB. Migration 0067 has
-- already been applied in existing databases, so tighten the persisted
-- constraint in a new migration that the migrator will execute everywhere.
--
-- Published OTA manifests are immutable. Historical rows larger than the new
-- partition limit therefore cannot be withdrawn or rewritten in place. Record
-- those rows in a quarantine table so catalog queries can exclude them without
-- destroying the signed audit history.
--
-- Quarantine rows are append-only. The trigger still allows an owner with an
-- explicit maintenance session setting to correct the reason, while deletion
-- is always rejected so a legacy release cannot silently re-enter the catalog.

DO $$
BEGIN
    IF to_regclass('public.ota_releases') IS NULL THEN
        RAISE EXCEPTION 'OTA release tables are missing before migration 0068';
    END IF;
END
$$;

CREATE TABLE IF NOT EXISTS ota_legacy_quarantined_releases (
    release_id UUID PRIMARY KEY
        REFERENCES ota_releases(id) ON DELETE RESTRICT,
    reason TEXT NOT NULL,
    quarantined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ota_legacy_quarantined_reason_valid
        CHECK (reason = 'artifact_exceeds_4_mib')
);

COMMENT ON TABLE ota_legacy_quarantined_releases IS
    'Append-only quarantine records for OTA manifests excluded from device delivery because their artifact exceeded the 4 MiB partition limit.';

CREATE OR REPLACE FUNCTION sprout_ota_legacy_quarantine_guard()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'OTA quarantine records cannot be deleted';
    END IF;
    IF current_setting('sprout.allow_ota_quarantine_reason_update', true) = 'on'
       AND NEW.release_id = OLD.release_id
       AND NEW.quarantined_at = OLD.quarantined_at THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'OTA quarantine records are append-only';
END;
$$;

DROP TRIGGER IF EXISTS ota_legacy_quarantined_releases_guard
    ON ota_legacy_quarantined_releases;

CREATE TRIGGER ota_legacy_quarantined_releases_guard
BEFORE UPDATE OR DELETE ON ota_legacy_quarantined_releases
FOR EACH ROW
EXECUTE FUNCTION sprout_ota_legacy_quarantine_guard();

INSERT INTO ota_legacy_quarantined_releases (release_id, reason)
SELECT
    release.id,
    'artifact_exceeds_4_mib'
FROM ota_releases AS release
WHERE release.size_bytes > 4194304
  AND release.status <> 'draft'
ON CONFLICT (release_id) DO NOTHING;

-- Drafts are still mutable and must satisfy the new limit before the service
-- can start. Fail the migration explicitly instead of leaving an oversized
-- draft that could later be published despite its already-invalid manifest.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM ota_releases
        WHERE size_bytes > 4194304
          AND status = 'draft'
    ) THEN
        RAISE EXCEPTION
            'oversized OTA drafts must be corrected or withdrawn before applying migration 0068';
    END IF;
END
$$;

-- NOT VALID enforces the constraint for every new insert and update while
-- retaining legacy immutable rows. New oversized drafts fail immediately.
ALTER TABLE ota_releases
    DROP CONSTRAINT IF EXISTS ota_releases_size_valid;

ALTER TABLE ota_releases
    ADD CONSTRAINT ota_releases_size_valid
        CHECK (size_bytes > 0 AND size_bytes <= 4194304)
        NOT VALID;
