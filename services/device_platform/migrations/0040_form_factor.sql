-- Capability-composed form-factor profiles and device profile status (P2-07).
--
-- A form factor is named technically and never binds to a character or an
-- appearance. The platform stores the capability mask a body reports and the
-- action mask composed from it, so the same voice, content and business
-- modules run on a desktop, a moving body or a wearable.

CREATE TABLE IF NOT EXISTS device_form_factor (
    device_id TEXT PRIMARY KEY REFERENCES device_credentials(device_id) ON DELETE CASCADE,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    technical_name TEXT NOT NULL DEFAULT 'desktop',
    capability_mask INTEGER NOT NULL DEFAULT 0,
    action_mask INTEGER NOT NULL DEFAULT 0,
    schema_version INTEGER NOT NULL DEFAULT 1,
    reported_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT device_form_factor_name_valid CHECK (
        char_length(technical_name) BETWEEN 1 AND 23
    ),
    CONSTRAINT device_form_factor_capability_valid
        CHECK (capability_mask BETWEEN 0 AND 1023),
    CONSTRAINT device_form_factor_action_valid
        CHECK (action_mask BETWEEN 0 AND 1023),
    CONSTRAINT device_form_factor_schema_version_valid
        CHECK (schema_version >= 1)
);

CREATE INDEX IF NOT EXISTS device_form_factor_family_idx
    ON device_form_factor (family_id, device_id);
