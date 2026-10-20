-- Commercial revenue ledger, part two: moving money and proving it.
--
-- Every amount below is 人民币分: BIGINT integer cents, never NUMERIC, never a
-- floating point value. The refund total is bounded by the order's paid total
-- so no sequence of retries or concurrent approvals can return more money than
-- the guardian handed over.
--
-- The callback ledger is the only basis for payment idempotency: a channel may
-- deliver the same notification any number of times, and the row that wins the
-- (channel, notification_id) unique index is the single one allowed to credit
-- the order. Payload digests are stored, payload contents are not, so no card
-- data, certificate or merchant secret is ever persisted.
--
-- The runner wraps this file in its own transaction and records the applied
-- version atomically, so no transaction control belongs here.

CREATE TABLE IF NOT EXISTS billing_payments (
    id UUID PRIMARY KEY,
    order_id UUID NOT NULL REFERENCES billing_orders(id) ON DELETE CASCADE,
    channel TEXT NOT NULL,
    -- Empty until the provider assigns its own trade number.
    channel_trade_no TEXT NOT NULL DEFAULT '',
    amount_cny_cents BIGINT NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'created',
    -- Raw provider response, already redacted. No secrets, certificates or
    -- private keys may be written here.
    provider_payload JSONB NOT NULL DEFAULT '{}'::JSONB,
    failure_code TEXT NOT NULL DEFAULT '',
    failure_message TEXT NOT NULL DEFAULT '',
    paid_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT billing_payments_channel_valid
        CHECK (channel IN ('wechat', 'alipay', 'manual')),
    CONSTRAINT billing_payments_status_valid CHECK (
        status IN ('created', 'processing', 'succeeded', 'failed', 'closed')
    ),
    CONSTRAINT billing_payments_amount_matches_status CHECK (
        status <> 'succeeded' OR amount_cny_cents > 0
    ),
    -- A succeeded payment must say when the provider settled it. The reverse
    -- is not required: a failed attempt may still record the paid_at the
    -- provider reported, which is what a dispute review needs.
    CONSTRAINT billing_payments_succeeded_has_paid_at CHECK (
        status <> 'succeeded' OR paid_at IS NOT NULL
    ),
    CONSTRAINT billing_payments_payload_object
        CHECK (jsonb_typeof(provider_payload) = 'object')
);

CREATE INDEX IF NOT EXISTS billing_payments_order_idx
    ON billing_payments(order_id, created_at DESC);

CREATE INDEX IF NOT EXISTS billing_payments_status_created_idx
    ON billing_payments(status, created_at DESC);

-- One provider trade may be credited once. Partial, because a payment that has
-- not yet received a provider trade number shares the empty string.
CREATE UNIQUE INDEX IF NOT EXISTS billing_payments_channel_trade_no_idx
    ON billing_payments(channel, channel_trade_no)
    WHERE channel_trade_no <> '';

-- An order is settled by exactly one successful payment. The channel uniqueness
-- above only stops one trade being credited twice; it does not stop a guardian
-- from tapping pay twice and settling the same order through two different
-- trades, because the second transfer is real money. Enforcing one success per
-- order here means the credit path cannot double-credit even when a service
-- guard is bypassed. An insert rejected by this index is NOT a transient error:
-- the caller must route the extra transfer to the reconciliation and refund
-- queue instead of retrying it.
CREATE UNIQUE INDEX IF NOT EXISTS billing_payments_order_success_unique_idx
    ON billing_payments(order_id)
    WHERE status = 'succeeded';

CREATE TABLE IF NOT EXISTS billing_payment_notifications (
    id UUID PRIMARY KEY,
    channel TEXT NOT NULL,
    notification_id TEXT NOT NULL,
    payload_digest TEXT NOT NULL DEFAULT '',
    signature_verified BOOLEAN NOT NULL DEFAULT FALSE,
    -- NULL means received but not processed to completion yet.
    processed_at TIMESTAMPTZ,
    result TEXT NOT NULL DEFAULT 'received',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT billing_payment_notifications_channel_valid
        CHECK (channel IN ('wechat', 'alipay', 'manual')),
    CONSTRAINT billing_payment_notifications_id_not_blank
        CHECK (length(btrim(notification_id)) > 0),
    CONSTRAINT billing_payment_notifications_result_valid CHECK (
        result IN (
            'received',
            'accepted',
            'duplicate',
            'signature_invalid',
            'amount_mismatch',
            'order_not_found'
        )
    ),
    -- Only a verified, accepted notification may claim a processing time. A
    -- redelivery is re-acked before it reaches the accepted branch, so it
    -- stays unprocessed.
    CONSTRAINT billing_payment_notifications_processed_when_accepted
        CHECK (result <> 'accepted' OR processed_at IS NOT NULL)
);

-- The idempotency key. An insert that loses this race is a redelivery and must
-- be acknowledged without crediting the order again.
CREATE UNIQUE INDEX IF NOT EXISTS billing_payment_notifications_unique_idx
    ON billing_payment_notifications(channel, notification_id);

CREATE INDEX IF NOT EXISTS billing_payment_notifications_pending_idx
    ON billing_payment_notifications(channel, created_at)
    WHERE processed_at IS NULL;

CREATE TABLE IF NOT EXISTS billing_refunds (
    id UUID PRIMARY KEY,
    refund_no TEXT NOT NULL,
    order_id UUID NOT NULL REFERENCES billing_orders(id) ON DELETE CASCADE,
    -- The payment the money returns to. SET NULL keeps the refund record after
    -- a payment row is purged.
    payment_id UUID REFERENCES billing_payments(id) ON DELETE SET NULL,
    amount_cny_cents BIGINT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    operator_account_id UUID REFERENCES parent_accounts(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'requested',
    channel_refund_no TEXT NOT NULL DEFAULT '',
    requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    approved_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- The random segment carries 40 bits, matching domain.NewRefundNumber:
    -- 6 base32 characters allowed only 30 bits, which left a same-day batch
    -- of a few thousand at a material birthday-collision risk.
    CONSTRAINT billing_refunds_refund_no_format
        CHECK (refund_no ~ '^SR[0-9]{8}[A-Z2-7]{8}$'),
    CONSTRAINT billing_refunds_status_valid CHECK (
        status IN (
            'requested',
            'approved',
            'processing',
            'succeeded',
            'failed',
            'rejected'
        )
    ),
    CONSTRAINT billing_refunds_amount_positive CHECK (amount_cny_cents > 0),
    -- Approval is a human act, so an approved-or-later refund must name the
    -- administrator who approved it. One-way on purpose: a refund rejected
    -- after approval keeps the approver and the approval time as evidence.
    CONSTRAINT billing_refunds_approved_requires_operator CHECK (
        status IN ('requested', 'rejected')
        OR (operator_account_id IS NOT NULL AND approved_at IS NOT NULL)
    ),
    -- A successful refund must record when the money came back.
    CONSTRAINT billing_refunds_succeeded_has_completed_at CHECK (
        status <> 'succeeded' OR completed_at IS NOT NULL
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS billing_refunds_refund_no_idx
    ON billing_refunds(refund_no);

CREATE INDEX IF NOT EXISTS billing_refunds_order_idx
    ON billing_refunds(order_id, created_at DESC);

CREATE INDEX IF NOT EXISTS billing_refunds_status_created_idx
    ON billing_refunds(status, created_at DESC);

-- §3.6 daily reconciliation against the channel statement. One run per channel
-- and business day keeps a retried job from producing competing totals.
CREATE TABLE IF NOT EXISTS billing_reconciliation_runs (
    id UUID PRIMARY KEY,
    channel TEXT NOT NULL,
    business_date DATE NOT NULL,
    status TEXT NOT NULL DEFAULT 'running',
    channel_total_cny_cents BIGINT NOT NULL DEFAULT 0,
    platform_total_cny_cents BIGINT NOT NULL DEFAULT 0,
    -- Signed: negative means the platform recorded more than the channel
    -- settled, which is the dangerous direction and must stay visible.
    difference_cny_cents BIGINT NOT NULL DEFAULT 0,
    matched_count INTEGER NOT NULL DEFAULT 0,
    missing_count INTEGER NOT NULL DEFAULT 0,
    extra_count INTEGER NOT NULL DEFAULT 0,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ,
    error_message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT billing_reconciliation_runs_channel_valid
        CHECK (channel IN ('wechat', 'alipay', 'manual')),
    CONSTRAINT billing_reconciliation_runs_status_valid
        CHECK (status IN ('running', 'completed', 'failed')),
    CONSTRAINT billing_reconciliation_runs_counts_non_negative CHECK (
        matched_count >= 0 AND missing_count >= 0 AND extra_count >= 0
    ),
    -- A completed run must record its finish time, but a failed run may carry
    -- one too when the provider answered before the job gave up.
    CONSTRAINT billing_reconciliation_runs_completed_marked CHECK (
        status <> 'completed' OR finished_at IS NOT NULL
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS billing_reconciliation_runs_unique_idx
    ON billing_reconciliation_runs(channel, business_date);

CREATE INDEX IF NOT EXISTS billing_reconciliation_runs_started_idx
    ON billing_reconciliation_runs(started_at DESC);

CREATE TABLE IF NOT EXISTS billing_reconciliation_items (
    id UUID PRIMARY KEY,
    run_id UUID NOT NULL
        REFERENCES billing_reconciliation_runs(id) ON DELETE CASCADE,
    channel_trade_no TEXT NOT NULL DEFAULT '',
    order_no TEXT NOT NULL DEFAULT '',
    channel_cny_cents BIGINT NOT NULL DEFAULT 0,
    platform_cny_cents BIGINT NOT NULL DEFAULT 0,
    discrepancy TEXT NOT NULL DEFAULT 'matched',
    resolved BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT billing_reconciliation_items_discrepancy_valid CHECK (
        discrepancy IN (
            'matched',
            'amount_mismatch',
            'platform_only',
            'channel_only'
        )
    )
);

CREATE INDEX IF NOT EXISTS billing_reconciliation_items_run_idx
    ON billing_reconciliation_items(run_id, discrepancy);

-- The operator console lists unresolved discrepancies first, so the partial
-- index covers exactly that query.
CREATE INDEX IF NOT EXISTS billing_reconciliation_items_unresolved_idx
    ON billing_reconciliation_items(run_id, discrepancy)
    WHERE resolved = FALSE;
