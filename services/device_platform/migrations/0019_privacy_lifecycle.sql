-- Privacy lifecycle: immutable consent history, data-export receipts, and
-- cancellable account deletion requests.
--
-- Deletion is a two-phase operation. The request keeps the account usable
-- during the grace period so a guardian can sign back in, export data, or
-- cancel the request. Execution happens after the deadline and is recorded in
-- the platform audit log before identifying rows are removed.

CREATE TABLE IF NOT EXISTS parent_consent_events (
    id UUID PRIMARY KEY,
    parent_account_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    consent_type TEXT NOT NULL,
    consent_version TEXT NOT NULL,
    granted BOOLEAN NOT NULL,
    source TEXT NOT NULL,
    detail JSONB NOT NULL DEFAULT '{}'::JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT parent_consent_events_type_valid CHECK (
        consent_type IN (
            'guardian_terms',
            'child_data_processing',
            'ai_interaction',
            'email_contact'
        )
    ),
    CONSTRAINT parent_consent_events_version_not_blank CHECK (
        length(btrim(consent_version)) > 0
    ),
    CONSTRAINT parent_consent_events_source_valid CHECK (
        source IN ('registration', 'guardian', 'administrator', 'system')
    ),
    CONSTRAINT parent_consent_events_detail_object CHECK (
        jsonb_typeof(detail) = 'object'
    )
);

CREATE INDEX IF NOT EXISTS parent_consent_events_parent_idx
    ON parent_consent_events(parent_account_id, created_at DESC);

CREATE TABLE IF NOT EXISTS parent_data_export_receipts (
    id UUID PRIMARY KEY,
    parent_account_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    requested_by UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    format TEXT NOT NULL DEFAULT 'json',
    status TEXT NOT NULL DEFAULT 'completed',
    item_counts JSONB NOT NULL DEFAULT '{}'::JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT parent_data_export_receipts_format_valid CHECK (format = 'json'),
    CONSTRAINT parent_data_export_receipts_status_valid CHECK (
        status IN ('completed', 'failed')
    ),
    CONSTRAINT parent_data_export_receipts_counts_object CHECK (
        jsonb_typeof(item_counts) = 'object'
    )
);

CREATE INDEX IF NOT EXISTS parent_data_export_receipts_parent_idx
    ON parent_data_export_receipts(parent_account_id, created_at DESC);

CREATE TABLE IF NOT EXISTS parent_deletion_requests (
    id UUID PRIMARY KEY,
    parent_account_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'pending',
    reason TEXT NOT NULL DEFAULT '',
    requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    execute_after TIMESTAMPTZ NOT NULL,
    cancelled_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    failure_reason TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT parent_deletion_requests_status_valid CHECK (
        status IN ('pending', 'processing', 'cancelled', 'completed', 'failed')
    ),
    CONSTRAINT parent_deletion_requests_reason_length CHECK (
        length(reason) <= 500
    )
);

-- Only one open request may exist for a guardian. Cancelled and completed
-- rows remain for audit and allow the guardian to request deletion again.
CREATE UNIQUE INDEX IF NOT EXISTS parent_deletion_requests_open_unique
    ON parent_deletion_requests(parent_account_id)
    WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS parent_deletion_requests_due_idx
    ON parent_deletion_requests(execute_after)
    WHERE status = 'pending';

-- A worker that dies after claiming a request must not strand it forever.
-- The lease timestamp lets the next worker reclaim a stale processing row.
CREATE INDEX IF NOT EXISTS parent_deletion_requests_processing_idx
    ON parent_deletion_requests(updated_at)
    WHERE status = 'processing';

CREATE INDEX IF NOT EXISTS platform_parent_audit_created_idx
    ON platform_parent_audit(created_at DESC);

CREATE INDEX IF NOT EXISTS platform_parent_audit_action_idx
    ON platform_parent_audit(action, created_at DESC);

CREATE INDEX IF NOT EXISTS platform_parent_audit_actor_idx
    ON platform_parent_audit(actor_account_id, created_at DESC);
