-- Device content packages are downloaded into a fixed 768 KiB partition.
-- Historical publications can exceed the new firmware limit. Archive them in
-- the same migration so devices receive an incremental withdrawal instead of
-- retaining a package that can never be installed.
WITH oversized AS (
    SELECT DISTINCT package_id
    FROM content_package_versions
    WHERE size_bytes NOT BETWEEN 1 AND 786432
      AND status = 'published'
),
advanced AS (
    INSERT INTO content_catalog_revision (singleton, revision, updated_at)
    SELECT TRUE, COUNT(*), NOW()
    FROM oversized
    HAVING COUNT(*) > 0
    ON CONFLICT (singleton) DO UPDATE
    SET revision = content_catalog_revision.revision + EXCLUDED.revision,
        updated_at = EXCLUDED.updated_at
    RETURNING revision
)
UPDATE content_package_versions
SET status = 'archived',
    catalog_revision = (SELECT revision FROM advanced),
    updated_at = NOW()
WHERE status = 'published'
  AND size_bytes NOT BETWEEN 1 AND 786432;

DROP CONSTRAINT IF EXISTS content_package_versions_size_valid
    ON content_package_versions;

ALTER TABLE content_package_versions
    ADD CONSTRAINT content_package_versions_size_valid
    CHECK (size_bytes BETWEEN 1 AND 786432);
