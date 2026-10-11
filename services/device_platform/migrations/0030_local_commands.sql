-- Guardian-published offline local commands (P1-06 本地兜底).
--
-- A guardian publishes trigger phrases that map to local device actions so
-- common commands keep working with no network. Commands are family-scoped;
-- each publish or delete bumps a per-family revision the device syncs
-- against. Commands never carry audio, images, credentials, or child data:
-- only a phrase, an action, and a bounded argument.

CREATE TABLE IF NOT EXISTS local_commands (
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    command_id TEXT NOT NULL,
    phrase TEXT NOT NULL,
    action TEXT NOT NULL,
    argument TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    position INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (family_id, command_id),
    CONSTRAINT local_commands_phrase_valid
        CHECK (length(btrim(phrase)) BETWEEN 1 AND 48),
    CONSTRAINT local_commands_action_valid
        CHECK (action IN (
            'play_package',
            'pause',
            'resume',
            'stop',
            'set_volume',
            'sleep'
        )),
    CONSTRAINT local_commands_argument_valid
        CHECK (length(argument) <= 80),
    CONSTRAINT local_commands_argument_matches_action
        CHECK (
            (action IN ('play_package', 'set_volume') AND argument <> '')
            OR (action NOT IN ('play_package', 'set_volume') AND argument = '')
        ),
    CONSTRAINT local_commands_play_package_target
        CHECK (
            action <> 'play_package'
            OR argument ~ '^[a-z][a-z0-9]*(?:_[a-z0-9]+){1,7}$'
        ),
    CONSTRAINT local_commands_position_valid CHECK (position >= 0)
);

CREATE INDEX IF NOT EXISTS local_commands_family_position_idx
    ON local_commands (family_id, position, command_id);

CREATE TABLE IF NOT EXISTS local_command_revisions (
    family_id UUID PRIMARY KEY REFERENCES parent_accounts(id) ON DELETE CASCADE,
    revision INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT local_command_revisions_non_negative CHECK (revision >= 0)
);
