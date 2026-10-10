-- Content operation: guardian favourites and device playback history.
--
-- Favourites are guardian-scoped: one row per (parent account, package). The
-- package id is stored without a foreign key to a specific version because a
-- favourite follows the package across new published versions, and a withdrawn
-- package must still be answerable as "favourited but no longer deliverable".
--
-- Playback history is device-scoped and bounded: firmware reports completed or
-- interrupted plays so the guardian console can show "recently played" and the
-- device can resume the most recent item. Rows are pruned per device by the
-- writer so the table cannot grow without limit.

CREATE TABLE IF NOT EXISTS content_favorites (
    parent_account_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    package_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT content_favorites_pk PRIMARY KEY (parent_account_id, package_id),
    CONSTRAINT content_favorites_package_id_valid
        CHECK (package_id ~ '^[a-z][a-z0-9]*(?:_[a-z0-9]+){1,7}$')
);

CREATE INDEX IF NOT EXISTS content_favorites_account_idx
    ON content_favorites(parent_account_id, created_at DESC);

CREATE TABLE IF NOT EXISTS content_play_history (
    id UUID PRIMARY KEY,
    device_id TEXT NOT NULL,
    package_id TEXT NOT NULL,
    package_version INTEGER NOT NULL,
    -- completed: the item played to the end; interrupted: playback stopped
    -- early by the child, a policy gate, or a newer interaction.
    outcome TEXT NOT NULL,
    played_seconds INTEGER NOT NULL DEFAULT 0,
    played_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT content_play_history_package_id_valid
        CHECK (package_id ~ '^[a-z][a-z0-9]*(?:_[a-z0-9]+){1,7}$'),
    CONSTRAINT content_play_history_package_version_positive
        CHECK (package_version >= 1),
    CONSTRAINT content_play_history_outcome_valid
        CHECK (outcome IN ('completed', 'interrupted')),
    CONSTRAINT content_play_history_seconds_valid
        CHECK (played_seconds BETWEEN 0 AND 86400)
);

CREATE INDEX IF NOT EXISTS content_play_history_device_idx
    ON content_play_history(device_id, played_at DESC);

CREATE INDEX IF NOT EXISTS content_play_history_package_idx
    ON content_play_history(package_id, played_at DESC);
