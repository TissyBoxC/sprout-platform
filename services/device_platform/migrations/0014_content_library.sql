-- Content-library packages, review history, and the incremental catalog clock.
--
-- One package ID may have many immutable versions, but only one version may be
-- published at a time. Superseded publications are archived in the same
-- transaction as the new publication so clients never observe two active rows.

CREATE TABLE IF NOT EXISTS content_package_versions (
    package_id TEXT NOT NULL,
    package_version INTEGER NOT NULL,
    title TEXT NOT NULL,
    category TEXT NOT NULL,
    age_tiers TEXT[] NOT NULL,
    asset_key TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft',
    submitted_at TIMESTAMPTZ,
    approved_at TIMESTAMPTZ,
    approved_by UUID REFERENCES parent_accounts(id) ON DELETE SET NULL,
    published_at TIMESTAMPTZ,
    catalog_revision BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT content_package_versions_pk
        PRIMARY KEY (package_id, package_version),
    CONSTRAINT content_package_versions_package_id_valid
        CHECK (package_id ~ '^[a-z][a-z0-9]*(?:_[a-z0-9]+){1,7}$'),
    CONSTRAINT content_package_versions_version_positive
        CHECK (package_version >= 1),
    CONSTRAINT content_package_versions_title_valid
        CHECK (length(btrim(title)) BETWEEN 1 AND 128),
    CONSTRAINT content_package_versions_category_valid
        CHECK (category IN (
            'story',
            'nursery_rhyme',
            'poetry',
            'english',
            'encyclopedia',
            'bedtime'
        )),
    CONSTRAINT content_package_versions_age_tiers_present
        CHECK (cardinality(age_tiers) BETWEEN 1 AND 3),
    CONSTRAINT content_package_versions_age_tiers_valid
        CHECK (age_tiers <@ ARRAY[
            'age_3_4',
            'age_5_6',
            'age_7_8'
        ]::TEXT[]),
    CONSTRAINT content_package_versions_asset_key_valid
        CHECK (
            length(asset_key) BETWEEN 1 AND 512
            AND asset_key ~ '^[a-z0-9][a-z0-9._/-]*$'
            AND position('..' IN asset_key) = 0
        ),
    CONSTRAINT content_package_versions_sha256_valid
        CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT content_package_versions_size_valid
        CHECK (size_bytes BETWEEN 1 AND 4294967296),
    CONSTRAINT content_package_versions_status_valid
        CHECK (status IN (
            'draft',
            'in_review',
            'published',
            'withdrawn',
            'archived'
        )),
    CONSTRAINT content_package_versions_publish_time_valid
        CHECK (status <> 'published' OR published_at IS NOT NULL),
    CONSTRAINT content_package_versions_revision_non_negative
        CHECK (catalog_revision >= 0)
);

CREATE INDEX IF NOT EXISTS content_package_versions_list_idx
    ON content_package_versions(category, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS content_package_versions_age_tiers_idx
    ON content_package_versions USING GIN(age_tiers);

CREATE INDEX IF NOT EXISTS content_package_versions_catalog_idx
    ON content_package_versions(catalog_revision);

CREATE UNIQUE INDEX IF NOT EXISTS content_package_versions_one_published_idx
    ON content_package_versions(package_id)
    WHERE status = 'published';

CREATE TABLE IF NOT EXISTS content_review_logs (
    id UUID PRIMARY KEY,
    package_id TEXT NOT NULL,
    package_version INTEGER NOT NULL,
    action TEXT NOT NULL,
    from_status TEXT NOT NULL,
    to_status TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    actor_account_id UUID REFERENCES parent_accounts(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT content_review_logs_package_version_fk
        FOREIGN KEY (package_id, package_version)
        REFERENCES content_package_versions(package_id, package_version)
        ON DELETE CASCADE,
    CONSTRAINT content_review_logs_action_valid
        CHECK (action IN (
            'submit',
            'approve',
            'reject',
            'publish',
            'withdraw',
            'archive'
        )),
    CONSTRAINT content_review_logs_from_status_valid
        CHECK (from_status IN (
            'draft',
            'in_review',
            'published',
            'withdrawn',
            'archived'
        )),
    CONSTRAINT content_review_logs_to_status_valid
        CHECK (to_status IN (
            'draft',
            'in_review',
            'published',
            'withdrawn',
            'archived'
        )),
    CONSTRAINT content_review_logs_reject_reason_required
        CHECK (action <> 'reject' OR length(btrim(reason)) > 0)
);

CREATE INDEX IF NOT EXISTS content_review_logs_package_idx
    ON content_review_logs(package_id, package_version, created_at DESC);

-- The revision row is updated with an atomic upsert inside every lifecycle
-- transaction. PostgreSQL serializes the row write, so concurrent publishes
-- receive distinct monotonically increasing values without an application
-- lock. A client cursor is only useful together with the package rows written
-- in the same transaction.
CREATE TABLE IF NOT EXISTS content_catalog_revision (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE,
    revision BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT content_catalog_revision_singleton CHECK (singleton),
    CONSTRAINT content_catalog_revision_non_negative CHECK (revision >= 0)
);

INSERT INTO content_catalog_revision (singleton, revision)
VALUES (TRUE, 0)
ON CONFLICT (singleton) DO NOTHING;
