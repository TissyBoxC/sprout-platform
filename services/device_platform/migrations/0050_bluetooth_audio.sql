-- Bluetooth audio module (蓝牙音箱模式与配对): the guardian speaker policy and
-- the bounded per-device Bluetooth adapter status.
--
-- The guardian owns one Bluetooth audio policy per family. Every bound device
-- reads the same revision over its device session and reports back a bounded
-- adapter status. The default policy keeps Bluetooth audio off and pairing
-- disabled, so a device never advertises or accepts a pairing before a guardian
-- enables it. The platform stores only the policy and bounded status scalars
-- (mode, adapter state, paired/connected counts, codec, volume and a disconnect
-- reason); no audio, pairing keys or device addresses are ever persisted here.

CREATE TABLE IF NOT EXISTS bluetooth_audio_policies (
    family_id UUID PRIMARY KEY REFERENCES parent_accounts(id) ON DELETE CASCADE,
    schema_version INT NOT NULL DEFAULT 1,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    speaker_mode_allowed BOOLEAN NOT NULL DEFAULT TRUE,
    pairing_allowed BOOLEAN NOT NULL DEFAULT FALSE,
    auto_reconnect BOOLEAN NOT NULL DEFAULT TRUE,
    preferred_codec TEXT NOT NULL DEFAULT 'auto',
    max_volume_percent INT NOT NULL DEFAULT 70,
    display_name TEXT NOT NULL DEFAULT '',
    revision INT NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT bluetooth_audio_policies_codec_valid
        CHECK (preferred_codec IN ('auto', 'sbc', 'aac', 'ldac')),
    CONSTRAINT bluetooth_audio_policies_volume_valid
        CHECK (max_volume_percent BETWEEN 0 AND 100),
    CONSTRAINT bluetooth_audio_policies_display_name_valid
        CHECK (char_length(display_name) <= 48),
    CONSTRAINT bluetooth_audio_policies_revision_valid
        CHECK (revision >= 1)
);

-- One row per bound device. A device that never reported is simply absent, so
-- the guardian UI hides the controls instead of showing dead ones.

CREATE TABLE IF NOT EXISTS device_bluetooth_audio_status (
    device_id TEXT PRIMARY KEY
        REFERENCES device_credentials(device_id) ON DELETE CASCADE,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    bluetooth_supported BOOLEAN NOT NULL DEFAULT FALSE,
    speaker_mode BOOLEAN NOT NULL DEFAULT FALSE,
    adapter_state TEXT NOT NULL DEFAULT 'off',
    paired_device_count INT NOT NULL DEFAULT 0,
    connected_device_count INT NOT NULL DEFAULT 0,
    active_codec TEXT NOT NULL DEFAULT 'unknown',
    volume_percent INT NOT NULL DEFAULT 0,
    last_disconnect_reason TEXT NOT NULL DEFAULT 'none',
    policy_revision INT NOT NULL DEFAULT 0,
    reported_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT device_bluetooth_audio_status_adapter_valid
        CHECK (adapter_state IN (
            'off', 'idle', 'advertising', 'connecting', 'connected', 'error'
        )),
    CONSTRAINT device_bluetooth_audio_status_codec_valid
        CHECK (active_codec IN ('unknown', 'sbc', 'aac', 'ldac')),
    CONSTRAINT device_bluetooth_audio_status_reason_valid
        CHECK (last_disconnect_reason IN (
            'none', 'user', 'range', 'peer', 'policy', 'error'
        )),
    CONSTRAINT device_bluetooth_audio_status_counts_valid
        CHECK (
            paired_device_count BETWEEN 0 AND 16
            AND connected_device_count BETWEEN 0 AND 16
            AND connected_device_count <= paired_device_count
        ),
    CONSTRAINT device_bluetooth_audio_status_volume_valid
        CHECK (volume_percent BETWEEN 0 AND 100),
    CONSTRAINT device_bluetooth_audio_status_revision_valid
        CHECK (policy_revision >= 0)
);

CREATE INDEX IF NOT EXISTS device_bluetooth_audio_status_family_idx
    ON device_bluetooth_audio_status (family_id, reported_at DESC);
