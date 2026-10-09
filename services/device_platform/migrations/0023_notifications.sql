-- Notification centre: guardian-facing notices, remote device messages, and
-- durable per-recipient delivery state.
--
-- The design separates the authored message (notifications) from its delivery
-- work (notification_deliveries) so one broadcast fans out to many guardians
-- without duplicating the body, and so an operator can watch per-channel
-- progress. Bodies are stored as authored text only; there is no place here
-- for child conversations, audio, images, tokens, or credentials.

CREATE TABLE IF NOT EXISTS notifications (
    id UUID PRIMARY KEY,
    category TEXT NOT NULL,
    severity TEXT NOT NULL DEFAULT 'info',
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    -- Optional deep link within the parent application. Values are app
    -- relative and validated at the service layer.
    action_path TEXT NOT NULL DEFAULT '',
    action_label TEXT NOT NULL DEFAULT '',
    -- Audience is either a single guardian or every active guardian.
    audience TEXT NOT NULL,
    parent_account_id UUID REFERENCES parent_accounts(id) ON DELETE CASCADE,
    device_id TEXT NOT NULL DEFAULT '',
    -- Remote device messages reuse this row so the fixed device display
    -- contract has one source of truth.
    display_duration_seconds INTEGER NOT NULL DEFAULT 0,
    channels TEXT[] NOT NULL DEFAULT ARRAY['in_app']::TEXT[],
    created_by UUID REFERENCES parent_accounts(id) ON DELETE SET NULL,
    source TEXT NOT NULL,
    publish_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT notifications_category_valid CHECK (
        category IN (
            'account_security',
            'device_status',
            'content_release',
            'service_update',
            'usage_report',
            'family_message',
            'system_announcement'
        )
    ),
    CONSTRAINT notifications_severity_valid CHECK (
        severity IN ('info', 'success', 'warning', 'critical')
    ),
    CONSTRAINT notifications_audience_valid CHECK (
        audience IN ('parent', 'all_parents', 'family_devices')
    ),
    CONSTRAINT notifications_source_valid CHECK (
        source IN ('administrator', 'guardian', 'system')
    ),
    CONSTRAINT notifications_title_not_blank CHECK (
        length(btrim(title)) > 0
    ),
    CONSTRAINT notifications_body_not_blank CHECK (
        length(btrim(body)) > 0
    ),
    -- A single-guardian notice must carry its owner; a broadcast must not
    -- target one guardian.
    CONSTRAINT notifications_audience_target_consistent CHECK (
        (audience = 'parent') = (parent_account_id IS NOT NULL)
    ),
    CONSTRAINT notifications_display_duration_valid CHECK (
        display_duration_seconds >= 0 AND display_duration_seconds <= 3600
    ),
    CONSTRAINT notifications_channels_not_empty CHECK (
        array_length(channels, 1) IS NOT NULL
    )
);

CREATE INDEX IF NOT EXISTS notifications_parent_created_idx
    ON notifications(parent_account_id, created_at DESC)
    WHERE parent_account_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS notifications_broadcast_idx
    ON notifications(created_at DESC)
    WHERE audience = 'all_parents';

CREATE INDEX IF NOT EXISTS notifications_device_idx
    ON notifications(device_id, created_at DESC)
    WHERE device_id <> '';

CREATE TABLE IF NOT EXISTS notification_deliveries (
    id UUID PRIMARY KEY,
    notification_id UUID NOT NULL
        REFERENCES notifications(id) ON DELETE CASCADE,
    parent_account_id UUID NOT NULL
        REFERENCES parent_accounts(id) ON DELETE CASCADE,
    channel TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    read_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    failure_code TEXT NOT NULL DEFAULT '',
    retry_count INTEGER NOT NULL DEFAULT 0,
    -- Remote device messages are delivered by the device command channel and
    -- therefore reference the acknowledgement command id.
    command_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT notification_deliveries_channel_valid CHECK (
        channel IN ('in_app', 'push', 'sms', 'email', 'device')
    ),
    CONSTRAINT notification_deliveries_status_valid CHECK (
        status IN ('pending', 'delivered', 'read', 'failed', 'expired')
    ),
    CONSTRAINT notification_deliveries_retry_valid CHECK (
        retry_count >= 0 AND retry_count <= 20
    )
);

-- One delivery row per notification, guardian, and channel keeps fan-out and
-- retries idempotent.
CREATE UNIQUE INDEX IF NOT EXISTS notification_deliveries_unique_idx
    ON notification_deliveries(notification_id, parent_account_id, channel);

CREATE INDEX IF NOT EXISTS notification_deliveries_inbox_idx
    ON notification_deliveries(parent_account_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS notification_deliveries_pending_idx
    ON notification_deliveries(channel, status, created_at)
    WHERE status IN ('pending', 'failed');
