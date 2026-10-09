-- Device OTA release, group, deployment, and progress history.
--
-- A release manifest becomes immutable when it leaves draft. Status and audit
-- columns remain mutable so operators can pause, resume, withdraw, or record a
-- rollback without rewriting the artifact identity that devices verified.

CREATE TABLE IF NOT EXISTS ota_groups (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    channel TEXT NOT NULL DEFAULT 'stable',
    hardware_revision TEXT NOT NULL,
    record_version BIGINT NOT NULL DEFAULT 1,
    created_by TEXT NOT NULL,
    updated_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ota_groups_name_unique UNIQUE (name),
    CONSTRAINT ota_groups_channel_valid
        CHECK (channel IN ('stable', 'canary', 'internal')),
    CONSTRAINT ota_groups_hardware_not_blank
        CHECK (length(btrim(hardware_revision)) > 0),
    CONSTRAINT ota_groups_version_positive CHECK (record_version > 0),
    CONSTRAINT ota_groups_created_by_not_blank
        CHECK (length(btrim(created_by)) > 0),
    CONSTRAINT ota_groups_updated_by_not_blank
        CHECK (length(btrim(updated_by)) > 0)
);

CREATE TABLE IF NOT EXISTS ota_releases (
    id UUID PRIMARY KEY,
    version TEXT NOT NULL,
    channel TEXT NOT NULL,
    hardware_revision TEXT NOT NULL,
    min_source_version TEXT NOT NULL DEFAULT '',
    artifact_url TEXT NOT NULL,
    artifact_key TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    signature_key_id TEXT NOT NULL,
    signature_algorithm TEXT NOT NULL,
    rollback_allowed BOOLEAN NOT NULL DEFAULT FALSE,
    mandatory BOOLEAN NOT NULL DEFAULT FALSE,
    release_notes TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'draft',
    target_scope TEXT NOT NULL,
    target_group_id UUID REFERENCES ota_groups(id) ON DELETE SET NULL,
    target_device_id TEXT REFERENCES device_credentials(device_id) ON DELETE SET NULL,
    canary_percent INTEGER NOT NULL DEFAULT 0,
    rollback_release_id UUID REFERENCES ota_releases(id) ON DELETE SET NULL,
    record_version BIGINT NOT NULL DEFAULT 1,
    created_by TEXT NOT NULL,
    updated_by TEXT NOT NULL,
    published_by TEXT,
    paused_by TEXT,
    withdrawn_by TEXT,
    rolled_back_by TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ,
    paused_at TIMESTAMPTZ,
    withdrawn_at TIMESTAMPTZ,
    rolled_back_at TIMESTAMPTZ,
    CONSTRAINT ota_releases_identity_unique
        UNIQUE (version, hardware_revision, channel),
    CONSTRAINT ota_releases_version_valid
        CHECK (version ~ '^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$'),
    CONSTRAINT ota_releases_min_source_valid
        CHECK (
            min_source_version = ''
            OR min_source_version ~ '^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$'
        ),
    CONSTRAINT ota_releases_channel_valid
        CHECK (channel IN ('stable', 'canary', 'internal')),
    CONSTRAINT ota_releases_status_valid
        CHECK (status IN ('draft', 'published', 'paused', 'withdrawn')),
    CONSTRAINT ota_releases_hardware_not_blank
        CHECK (length(btrim(hardware_revision)) > 0),
    CONSTRAINT ota_releases_artifact_key_not_blank
        CHECK (length(btrim(artifact_key)) > 0),
    CONSTRAINT ota_releases_sha256_valid
        CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ota_releases_size_valid
        CHECK (size_bytes > 0 AND size_bytes <= 536870912),
    CONSTRAINT ota_releases_signature_key_not_blank
        CHECK (length(btrim(signature_key_id)) > 0),
    CONSTRAINT ota_releases_signature_algorithm_valid
        CHECK (
            signature_algorithm IN (
                'ed25519',
                'ecdsa_p256_sha256',
                'rsa_pss_sha256'
            )
        ),
    CONSTRAINT ota_releases_target_scope_valid
        CHECK (target_scope IN ('all', 'group', 'device', 'canary')),
    CONSTRAINT ota_releases_canary_valid
        CHECK (canary_percent BETWEEN 0 AND 99),
    CONSTRAINT ota_releases_target_coherent CHECK (
        (target_scope = 'all'
            AND target_group_id IS NULL
            AND target_device_id IS NULL
            AND canary_percent = 0)
        OR (target_scope = 'group'
            AND target_group_id IS NOT NULL
            AND target_device_id IS NULL
            AND canary_percent = 0)
        OR (target_scope = 'device'
            AND target_group_id IS NULL
            AND target_device_id IS NOT NULL
            AND canary_percent = 0)
        OR (target_scope = 'canary'
            AND target_group_id IS NULL
            AND target_device_id IS NULL
            AND canary_percent BETWEEN 1 AND 99)
    ),
    CONSTRAINT ota_releases_record_version_positive CHECK (record_version > 0),
    CONSTRAINT ota_releases_created_by_not_blank
        CHECK (length(btrim(created_by)) > 0),
    CONSTRAINT ota_releases_updated_by_not_blank
        CHECK (length(btrim(updated_by)) > 0)
);

CREATE INDEX IF NOT EXISTS ota_releases_lookup_idx
    ON ota_releases(status, channel, hardware_revision, published_at DESC);

CREATE INDEX IF NOT EXISTS ota_releases_version_idx
    ON ota_releases(version, published_at DESC);

CREATE TABLE IF NOT EXISTS ota_group_members (
    group_id UUID NOT NULL REFERENCES ota_groups(id) ON DELETE CASCADE,
    device_id TEXT NOT NULL REFERENCES device_credentials(device_id) ON DELETE CASCADE,
    added_by TEXT NOT NULL,
    added_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (group_id, device_id),
    CONSTRAINT ota_group_members_added_by_not_blank
        CHECK (length(btrim(added_by)) > 0)
);

CREATE INDEX IF NOT EXISTS ota_group_members_device_idx
    ON ota_group_members(device_id, group_id);

CREATE TABLE IF NOT EXISTS ota_deployments (
    id UUID PRIMARY KEY,
    release_id UUID NOT NULL REFERENCES ota_releases(id) ON DELETE RESTRICT,
    device_id TEXT NOT NULL REFERENCES device_credentials(device_id) ON DELETE CASCADE,
    group_id UUID REFERENCES ota_groups(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'queued',
    requested_by TEXT NOT NULL,
    request_id TEXT NOT NULL,
    progress_percent INTEGER NOT NULL DEFAULT 0,
    bytes_received BIGINT NOT NULL DEFAULT 0,
    bytes_total BIGINT NOT NULL DEFAULT 0,
    failure_code TEXT NOT NULL DEFAULT '',
    failure_message TEXT NOT NULL DEFAULT '',
    retry_count INTEGER NOT NULL DEFAULT 0,
    record_version BIGINT NOT NULL DEFAULT 1,
    last_event_sequence BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    offered_at TIMESTAMPTZ,
    download_started_at TIMESTAMPTZ,
    validated_at TIMESTAMPTZ,
    install_started_at TIMESTAMPTZ,
    verification_started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    failed_at TIMESTAMPTZ,
    rolled_back_at TIMESTAMPTZ,
    CONSTRAINT ota_deployments_request_unique UNIQUE (request_id),
    CONSTRAINT ota_deployments_release_device_unique UNIQUE (release_id, device_id),
    CONSTRAINT ota_deployments_status_valid CHECK (
        status IN (
            'queued',
            'offered',
            'downloading',
            'validating',
            'installing',
            'pending_verify',
            'succeeded',
            'failed',
            'rolled_back'
        )
    ),
    CONSTRAINT ota_deployments_progress_valid
        CHECK (progress_percent BETWEEN 0 AND 100),
    CONSTRAINT ota_deployments_bytes_valid
        CHECK (
            bytes_received >= 0
            AND bytes_total >= 0
            AND bytes_received <= bytes_total
        ),
    CONSTRAINT ota_deployments_retry_valid CHECK (retry_count >= 0),
    CONSTRAINT ota_deployments_record_version_positive CHECK (record_version > 0),
    CONSTRAINT ota_deployments_sequence_non_negative CHECK (last_event_sequence >= 0),
    CONSTRAINT ota_deployments_requested_by_not_blank
        CHECK (length(btrim(requested_by)) > 0)
);

CREATE INDEX IF NOT EXISTS ota_deployments_release_idx
    ON ota_deployments(release_id, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS ota_deployments_device_idx
    ON ota_deployments(device_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS ota_deployments_group_idx
    ON ota_deployments(group_id, updated_at DESC)
    WHERE group_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS ota_events (
    id UUID PRIMARY KEY,
    deployment_id UUID NOT NULL REFERENCES ota_deployments(id) ON DELETE CASCADE,
    event_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    sequence BIGINT NOT NULL,
    status TEXT NOT NULL,
    progress_percent INTEGER NOT NULL DEFAULT 0,
    bytes_received BIGINT NOT NULL DEFAULT 0,
    bytes_total BIGINT NOT NULL DEFAULT 0,
    error_code TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL DEFAULT '',
    detail JSONB NOT NULL DEFAULT '{}'::JSONB,
    reported_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ota_events_event_unique UNIQUE (deployment_id, event_id),
    CONSTRAINT ota_events_sequence_unique UNIQUE (deployment_id, sequence),
    CONSTRAINT ota_events_sequence_positive CHECK (sequence > 0),
    CONSTRAINT ota_events_type_valid CHECK (
        event_type IN (
            'started',
            'downloading',
            'downloaded',
            'validated',
            'installing',
            'pending_verify',
            'succeeded',
            'failed',
            'rollback_started',
            'rolled_back'
        )
    ),
    CONSTRAINT ota_events_status_valid CHECK (
        status IN (
            'queued',
            'offered',
            'downloading',
            'validating',
            'installing',
            'pending_verify',
            'succeeded',
            'failed',
            'rolled_back'
        )
    ),
    CONSTRAINT ota_events_progress_valid
        CHECK (progress_percent BETWEEN 0 AND 100),
    CONSTRAINT ota_events_bytes_valid
        CHECK (
            bytes_received >= 0
            AND bytes_total >= 0
            AND bytes_received <= bytes_total
        ),
    CONSTRAINT ota_events_detail_object
        CHECK (jsonb_typeof(detail) = 'object')
);

CREATE INDEX IF NOT EXISTS ota_events_deployment_idx
    ON ota_events(deployment_id, received_at DESC);

-- Refuse any manifest rewrite once the release is visible to devices. Status
-- transitions and audit fields are still allowed.
CREATE OR REPLACE FUNCTION sprout_ota_release_immutable()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.status <> 'draft' AND (
        NEW.version IS DISTINCT FROM OLD.version
        OR NEW.channel IS DISTINCT FROM OLD.channel
        OR NEW.hardware_revision IS DISTINCT FROM OLD.hardware_revision
        OR NEW.min_source_version IS DISTINCT FROM OLD.min_source_version
        OR NEW.artifact_url IS DISTINCT FROM OLD.artifact_url
        OR NEW.artifact_key IS DISTINCT FROM OLD.artifact_key
        OR NEW.sha256 IS DISTINCT FROM OLD.sha256
        OR NEW.size_bytes IS DISTINCT FROM OLD.size_bytes
        OR NEW.signature_key_id IS DISTINCT FROM OLD.signature_key_id
        OR NEW.signature_algorithm IS DISTINCT FROM OLD.signature_algorithm
        OR NEW.rollback_allowed IS DISTINCT FROM OLD.rollback_allowed
        OR NEW.mandatory IS DISTINCT FROM OLD.mandatory
        OR NEW.release_notes IS DISTINCT FROM OLD.release_notes
        OR NEW.target_scope IS DISTINCT FROM OLD.target_scope
        OR NEW.target_group_id IS DISTINCT FROM OLD.target_group_id
        OR NEW.target_device_id IS DISTINCT FROM OLD.target_device_id
        OR NEW.canary_percent IS DISTINCT FROM OLD.canary_percent
        OR NEW.rollback_release_id IS DISTINCT FROM OLD.rollback_release_id
    ) THEN
        RAISE EXCEPTION 'OTA release manifest is immutable after publication';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS ota_releases_immutable_manifest ON ota_releases;

CREATE TRIGGER ota_releases_immutable_manifest
BEFORE UPDATE ON ota_releases
FOR EACH ROW
EXECUTE FUNCTION sprout_ota_release_immutable();
