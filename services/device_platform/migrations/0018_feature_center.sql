-- Feature-center configuration overlays.
--
-- Feature metadata and schema stay in code so a deployed binary cannot expose
-- a capability it does not implement. This table stores only the editable
-- value overlay and its optimistic-concurrency version.
CREATE TABLE IF NOT EXISTS platform_feature_configs (
    feature_id TEXT PRIMARY KEY,
    "values" JSONB NOT NULL DEFAULT '{}'::JSONB,
    version BIGINT NOT NULL DEFAULT 1,
    updated_by UUID REFERENCES parent_accounts(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT platform_feature_configs_id_valid
        CHECK (feature_id ~ '^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$'),
    CONSTRAINT platform_feature_configs_values_object
        CHECK (jsonb_typeof("values") = 'object'),
    CONSTRAINT platform_feature_configs_version_positive
        CHECK (version > 0)
);

CREATE INDEX IF NOT EXISTS platform_feature_configs_updated_idx
    ON platform_feature_configs(updated_at DESC);
