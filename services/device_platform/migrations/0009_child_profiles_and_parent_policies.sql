CREATE TABLE IF NOT EXISTS child_profiles (
    id UUID PRIMARY KEY,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    nickname TEXT NOT NULL,
    age_tier TEXT NOT NULL,
    interests TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    content_categories TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    guardian_consent_version TEXT NOT NULL,
    guardian_consented_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT child_profiles_nickname_not_blank CHECK (
        length(btrim(nickname)) BETWEEN 1 AND 32
    ),
    CONSTRAINT child_profiles_age_tier_valid CHECK (
        age_tier IN ('age_3_4', 'age_5_6', 'age_7_8')
    ),
    CONSTRAINT child_profiles_categories_not_empty CHECK (
        cardinality(content_categories) > 0
    )
);

CREATE INDEX IF NOT EXISTS child_profiles_family_id_idx
    ON child_profiles(family_id, created_at);

CREATE TABLE IF NOT EXISTS parent_policies (
    id UUID PRIMARY KEY,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    child_id UUID NOT NULL UNIQUE REFERENCES child_profiles(id) ON DELETE CASCADE,
    policy_version INTEGER NOT NULL DEFAULT 1,
    daily_limit_minutes INTEGER NOT NULL,
    allowed_categories TEXT[] NOT NULL,
    disabled_periods JSONB NOT NULL DEFAULT '[]'::JSONB,
    max_volume_percent INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT parent_policies_version_positive CHECK (policy_version > 0),
    CONSTRAINT parent_policies_daily_limit_valid CHECK (
        daily_limit_minutes BETWEEN 0 AND 720
    ),
    CONSTRAINT parent_policies_categories_not_empty CHECK (
        cardinality(allowed_categories) > 0
    ),
    CONSTRAINT parent_policies_volume_valid CHECK (
        max_volume_percent BETWEEN 0 AND 100
    )
);

CREATE INDEX IF NOT EXISTS parent_policies_family_id_idx
    ON parent_policies(family_id);
