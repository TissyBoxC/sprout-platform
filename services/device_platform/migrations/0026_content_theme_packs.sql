-- Content operation: curated theme packs.
--
-- A theme pack is an ordered, named collection of content packages that the
-- guardian console and firmware browse and bulk-download as one unit. The pack
-- stores member package ids as an ordered array rather than a join table
-- because the order is part of the editorial intent ("先听后唱") and the pack
-- is always read and written as a whole. Publishing a pack stamps the current
-- catalog revision so clients can tell whether their cached theme list is
-- current, and a withdrawn pack is retained so history and audits survive.

CREATE TABLE IF NOT EXISTS content_theme_packs (
    theme_id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    age_tiers TEXT[] NOT NULL,
    package_ids TEXT[] NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft',
    catalog_revision BIGINT NOT NULL DEFAULT 0,
    published_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT content_theme_packs_theme_id_valid
        CHECK (theme_id ~ '^[a-z][a-z0-9]*(?:_[a-z0-9]+){1,7}$'),
    CONSTRAINT content_theme_packs_title_valid
        CHECK (char_length(title) BETWEEN 1 AND 64),
    CONSTRAINT content_theme_packs_summary_valid
        CHECK (char_length(summary) <= 200),
    CONSTRAINT content_theme_packs_members_valid
        CHECK (array_length(package_ids, 1) BETWEEN 1 AND 50),
    CONSTRAINT content_theme_packs_age_tiers_valid
        CHECK (array_length(age_tiers, 1) BETWEEN 1 AND 3),
    CONSTRAINT content_theme_packs_status_valid
        CHECK (status IN ('draft', 'published', 'withdrawn'))
);

CREATE INDEX IF NOT EXISTS content_theme_packs_status_idx
    ON content_theme_packs(status, updated_at DESC);

CREATE INDEX IF NOT EXISTS content_theme_packs_revision_idx
    ON content_theme_packs(catalog_revision);
