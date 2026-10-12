-- Platform operations tables.
--
-- Settings are versioned JSON documents so a partial write or invalid update
-- cannot silently change the runtime policy. Releases are immutable once
-- published; a new version must be registered instead of rewriting history.

CREATE TABLE IF NOT EXISTS platform_settings (
    settings_key TEXT PRIMARY KEY,
    value JSONB NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    updated_by UUID REFERENCES parent_accounts(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS platform_releases (
    id UUID PRIMARY KEY,
    version TEXT NOT NULL,
    channel TEXT NOT NULL,
    kind TEXT NOT NULL,
    platform TEXT NOT NULL,
    download_url TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    release_notes TEXT NOT NULL DEFAULT '',
    is_mandatory BOOLEAN NOT NULL DEFAULT FALSE,
    min_supported_version TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'draft',
    published_at TIMESTAMPTZ,
    published_by UUID REFERENCES parent_accounts(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT platform_releases_version_unique UNIQUE (version),
    CONSTRAINT platform_releases_channel_valid CHECK (channel IN ('stable', 'beta', 'canary')),
    CONSTRAINT platform_releases_kind_valid CHECK (kind IN ('resource', 'client', 'firmware')),
    CONSTRAINT platform_releases_platform_valid CHECK (platform IN ('android', 'ios', 'esp32_s3', 'all')),
    CONSTRAINT platform_releases_status_valid CHECK (status IN ('draft', 'published', 'retired')),
    CONSTRAINT platform_releases_sha256_valid CHECK (sha256 ~ '^[0-9a-f]{64}$')
);

CREATE INDEX IF NOT EXISTS platform_releases_lookup_idx
    ON platform_releases(platform, channel, kind, status, published_at DESC);

CREATE TABLE IF NOT EXISTS platform_usage_daily (
    usage_day DATE NOT NULL,
    parent_account_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    device_id TEXT NOT NULL DEFAULT '',
    conversation_count INTEGER NOT NULL DEFAULT 0,
    spent_usd NUMERIC(18, 6) NOT NULL DEFAULT 0,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (usage_day, parent_account_id, device_id),
    CONSTRAINT platform_usage_daily_conversations_non_negative CHECK (conversation_count >= 0),
    CONSTRAINT platform_usage_daily_spent_non_negative CHECK (spent_usd >= 0),
    CONSTRAINT platform_usage_daily_input_non_negative CHECK (input_tokens >= 0),
    CONSTRAINT platform_usage_daily_output_non_negative CHECK (output_tokens >= 0)
);

CREATE INDEX IF NOT EXISTS platform_usage_daily_parent_idx
    ON platform_usage_daily(parent_account_id, usage_day DESC);

CREATE TABLE IF NOT EXISTS platform_parent_audit (
    id UUID PRIMARY KEY,
    actor_account_id UUID REFERENCES parent_accounts(id) ON DELETE SET NULL,
    target_account_id UUID REFERENCES parent_accounts(id) ON DELETE SET NULL,
    action TEXT NOT NULL,
    detail JSONB NOT NULL DEFAULT '{}'::JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT platform_parent_audit_action_not_blank CHECK (length(btrim(action)) > 0)
);

CREATE INDEX IF NOT EXISTS platform_parent_audit_target_idx
    ON platform_parent_audit(target_account_id, created_at DESC);

-- Existing installations need an explicit default policy document. The
-- document keeps conservative defaults so a missing admin update never opens
-- safety-sensitive capabilities.
INSERT INTO platform_settings (settings_key, value)
VALUES (
    'operations',
    '{
        "ai": {
            "default_balance_usd": 0,
            "default_concurrency": 1,
            "default_models": []
        },
        "account": {
            "registration_enabled": true,
            "phone_verification_required": true,
            "email_login_enabled": true
        },
        "safety": {
            "minor_mode_default": true,
            "output_moderation_enabled": true,
            "crisis_intervention_enabled": true,
            "allow_audio_upload": false,
            "allow_image_upload": false
        },
        "voice": {
            "continuous_conversation_enabled": true,
            "idle_window_seconds": 8,
            "barge_in_enabled": true,
            "far_field_enabled": true
        },
        "update": {
            "channel": "stable",
            "min_client_version": "",
            "force_upgrade_below": ""
        },
        "retention": {
            "audio_days": 0,
            "image_days": 0,
            "conversation_days": 30
        }
    }'::JSONB
)
ON CONFLICT (settings_key) DO NOTHING;
