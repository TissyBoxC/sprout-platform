-- Image privacy module (图片本地处理与上传授权): the guardian upload policy and
-- the bounded per-device image pipeline status.
--
-- The guardian owns one image-privacy policy per family. Every bound device
-- reads the same revision over its device session and reports back a bounded
-- pipeline status. The default policy keeps image upload off, so a device never
-- uploads a frame before a guardian explicitly authorises it. The platform
-- stores only bounded scalars (processing mode, upload scope, retention days,
-- the redaction mask and the pipeline counters); no image bytes, thumbnails or
-- face templates are ever persisted here.

CREATE TABLE IF NOT EXISTS image_privacy_policies (
    family_id UUID PRIMARY KEY REFERENCES parent_accounts(id) ON DELETE CASCADE,
    schema_version INT NOT NULL DEFAULT 1,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    upload_allowed BOOLEAN NOT NULL DEFAULT FALSE,
    local_processing_only BOOLEAN NOT NULL DEFAULT TRUE,
    processing_mode TEXT NOT NULL DEFAULT 'metadata',
    upload_scope TEXT NOT NULL DEFAULT 'none',
    retention_days INT NOT NULL DEFAULT 0,
    redaction_level TEXT NOT NULL DEFAULT 'strict',
    require_child_consent BOOLEAN NOT NULL DEFAULT TRUE,
    revision INT NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT image_privacy_policies_mode_valid
        CHECK (processing_mode IN ('off', 'metadata', 'local_full')),
    CONSTRAINT image_privacy_policies_scope_valid
        CHECK (upload_scope IN ('none', 'thumbnails', 'analysis')),
    CONSTRAINT image_privacy_policies_retention_valid
        CHECK (retention_days BETWEEN 0 AND 30),
    CONSTRAINT image_privacy_policies_redaction_valid
        CHECK (redaction_level IN ('none', 'standard', 'strict')),
    CONSTRAINT image_privacy_policies_revision_valid
        CHECK (revision >= 1)
);

-- One row per bound device. A device that never reported is simply absent, so
-- the guardian UI hides the image controls instead of showing dead ones.

CREATE TABLE IF NOT EXISTS device_image_privacy_status (
    device_id TEXT PRIMARY KEY
        REFERENCES device_credentials(device_id) ON DELETE CASCADE,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    camera_supported BOOLEAN NOT NULL DEFAULT FALSE,
    local_processing BOOLEAN NOT NULL DEFAULT FALSE,
    upload_allowed BOOLEAN NOT NULL DEFAULT FALSE,
    processing_mode TEXT NOT NULL DEFAULT 'off',
    upload_scope TEXT NOT NULL DEFAULT 'none',
    redaction_level TEXT NOT NULL DEFAULT 'strict',
    processed_count INT NOT NULL DEFAULT 0,
    uploaded_count INT NOT NULL DEFAULT 0,
    blocked_count INT NOT NULL DEFAULT 0,
    last_block_reason TEXT NOT NULL DEFAULT 'none',
    policy_revision INT NOT NULL DEFAULT 0,
    reported_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT device_image_privacy_status_mode_valid
        CHECK (processing_mode IN ('off', 'metadata', 'local_full')),
    CONSTRAINT device_image_privacy_status_scope_valid
        CHECK (upload_scope IN ('none', 'thumbnails', 'analysis')),
    CONSTRAINT device_image_privacy_status_redaction_valid
        CHECK (redaction_level IN ('none', 'standard', 'strict')),
    CONSTRAINT device_image_privacy_status_reason_valid
        CHECK (last_block_reason IN (
            'none', 'disabled', 'consent', 'scope', 'quota', 'error'
        )),
    CONSTRAINT device_image_privacy_status_counts_valid
        CHECK (
            processed_count BETWEEN 0 AND 1000000 AND
            uploaded_count BETWEEN 0 AND 1000000 AND
            blocked_count BETWEEN 0 AND 1000000 AND
            uploaded_count <= processed_count
        ),
    CONSTRAINT device_image_privacy_status_revision_valid
        CHECK (policy_revision >= 0)
);

CREATE INDEX IF NOT EXISTS device_image_privacy_status_family_idx
    ON device_image_privacy_status (family_id, reported_at DESC);
