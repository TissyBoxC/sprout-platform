-- Remote UI text packages (远程界面文案).
--
-- One logical package is identified by (package_id, language, platform) and
-- may have many immutable versions, but only one version may be published at a
-- time. Superseded publications are withdrawn in the same transaction as the
-- new publication so a client never observes two active rows.
--
-- The catalog clock is a singleton row whose revision is bumped on every
-- publish, letting a client pull only what changed since the revision it
-- already holds.

CREATE TABLE IF NOT EXISTS ui_text_package_versions (
    package_id TEXT NOT NULL,
    package_version INTEGER NOT NULL,
    language TEXT NOT NULL,
    platform TEXT NOT NULL,
    min_client_version TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'draft',
    signature_key_id TEXT NOT NULL DEFAULT '',
    signature TEXT NOT NULL DEFAULT '',
    submitted_at TIMESTAMPTZ,
    approved_at TIMESTAMPTZ,
    approved_by UUID REFERENCES parent_accounts(id) ON DELETE SET NULL,
    published_at TIMESTAMPTZ,
    published_by UUID REFERENCES parent_accounts(id) ON DELETE SET NULL,
    withdrawn_at TIMESTAMPTZ,
    revision BIGINT NOT NULL DEFAULT 0,
    created_by UUID REFERENCES parent_accounts(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ui_text_package_versions_pk
        PRIMARY KEY (package_id, language, platform, package_version),
    CONSTRAINT ui_text_package_versions_package_id_valid
        CHECK (package_id ~ '^[a-z0-9][a-z0-9_-]*$' AND length(package_id) <= 128),
    CONSTRAINT ui_text_package_versions_version_positive
        CHECK (package_version >= 1),
    CONSTRAINT ui_text_package_versions_language_valid
        CHECK (language ~ '^[a-z]{2,3}(-[A-Z]{2})?$'),
    CONSTRAINT ui_text_package_versions_platform_valid
        CHECK (platform IN ('parent_app', 'admin_web', 'device_firmware')),
    CONSTRAINT ui_text_package_versions_min_client_valid
        CHECK (length(min_client_version) <= 32),
    CONSTRAINT ui_text_package_versions_status_valid
        CHECK (status IN ('draft', 'in_review', 'published', 'withdrawn')),
    CONSTRAINT ui_text_package_versions_publish_coherent CHECK (
        status <> 'published'
        OR (
            published_at IS NOT NULL
            AND length(btrim(signature)) > 0
            AND length(btrim(signature_key_id)) > 0
        )
    ),
    CONSTRAINT ui_text_package_versions_revision_non_negative
        CHECK (revision >= 0)
);

CREATE INDEX IF NOT EXISTS ui_text_package_versions_lookup_idx
    ON ui_text_package_versions(package_id, language, platform, package_version DESC);

-- At most one published version per logical package. A partial unique index
-- makes a duplicate publication a hard database error, not just a service rule.
CREATE UNIQUE INDEX IF NOT EXISTS ui_text_package_versions_published_unique_idx
    ON ui_text_package_versions(package_id, language, platform)
    WHERE status = 'published';

-- Incremental pull cursor: clients page by revision.
CREATE INDEX IF NOT EXISTS ui_text_package_versions_revision_idx
    ON ui_text_package_versions(revision)
    WHERE status = 'published';

CREATE TABLE IF NOT EXISTS ui_text_entries (
    package_id TEXT NOT NULL,
    language TEXT NOT NULL,
    platform TEXT NOT NULL,
    package_version INTEGER NOT NULL,
    ui_text_key TEXT NOT NULL,
    value TEXT NOT NULL DEFAULT '',
    max_length INTEGER NOT NULL,
    variables TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    CONSTRAINT ui_text_entries_pk
        PRIMARY KEY (package_id, language, platform, package_version, ui_text_key),
    CONSTRAINT ui_text_entries_package_fk
        FOREIGN KEY (package_id, language, platform, package_version)
        REFERENCES ui_text_package_versions(package_id, language, platform, package_version)
        ON DELETE CASCADE,
    CONSTRAINT ui_text_entries_key_valid
        CHECK (
            length(ui_text_key) <= 128
            AND ui_text_key ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$'
        ),
    CONSTRAINT ui_text_entries_value_valid
        CHECK (max_length BETWEEN 1 AND 2000),
    -- CHECK constraints cannot contain subqueries, so element-level variable
    -- validation (pattern, duplicates) lives in the service layer. The database
    -- still bounds the list length and rejects null/empty elements.
    CONSTRAINT ui_text_entries_variables_bounded
        CHECK (
            cardinality(variables) <= 16
            AND array_position(variables, NULL) IS NULL
            AND array_position(variables, '') IS NULL
        )
);

CREATE INDEX IF NOT EXISTS ui_text_entries_lookup_idx
    ON ui_text_entries(package_id, language, platform, package_version, ui_text_key);

-- Catalog clock: a single row whose revision advances on every publish.
CREATE TABLE IF NOT EXISTS ui_text_catalog_clock (
    singleton BOOLEAN NOT NULL DEFAULT TRUE,
    revision BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ui_text_catalog_clock_pk PRIMARY KEY (singleton),
    CONSTRAINT ui_text_catalog_clock_singleton_true CHECK (singleton),
    CONSTRAINT ui_text_catalog_clock_revision_non_negative CHECK (revision >= 0)
);

INSERT INTO ui_text_catalog_clock (singleton, revision)
VALUES (TRUE, 0)
ON CONFLICT (singleton) DO NOTHING;

-- Entries of a non-draft package are immutable. A reviewer may not silently
-- rewrite the strings a published client is already rendering. The guard runs
-- on INSERT and UPDATE only, so removing a whole draft version can still
-- cascade into its entries.
CREATE OR REPLACE FUNCTION sprout_ui_text_entry_immutable()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_status TEXT;
BEGIN
    SELECT status
    INTO target_status
    FROM ui_text_package_versions
    WHERE package_id = NEW.package_id
      AND language = NEW.language
      AND platform = NEW.platform
      AND package_version = NEW.package_version
    FOR SHARE;
    IF target_status IS DISTINCT FROM 'draft' THEN
        RAISE EXCEPTION 'UI text entries are immutable after review starts';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS ui_text_entries_immutable ON ui_text_entries;

CREATE TRIGGER ui_text_entries_immutable
BEFORE INSERT OR UPDATE ON ui_text_entries
FOR EACH ROW
EXECUTE FUNCTION sprout_ui_text_entry_immutable();
