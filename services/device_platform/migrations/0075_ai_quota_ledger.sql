-- Model quota becomes auditable: a signed audit trail beside the live balance.
--
-- `ai_accounts.balance_usd` is the account's real spendable provider balance.
-- It is set at account creation and decremented by the upstream provider as
-- model calls are billed, and the platform cannot recompute it because the
-- provider's pricing rules are not ours. So it stays the live balance and stays
-- provider-synced. Before this table nothing recorded how a balance reached
-- its current value: a guardian's purchase, a provider refresh, and a console
-- edit were indistinguishable, and a refund could not be traced.
--
-- This ledger is that record, and it is an AUDIT TRAIL, not the source of
-- truth. Do not "simplify" it into a projection that overwrites the balance:
-- a balance derived from this table alone would leave a paying guardian with
-- quota in the ledger but an empty provider pool. A consistency check compares
-- the two instead and reports degraded on mismatch.
--
-- amount_usd is deliberately the upstream provider's unit, not yuan cents: it
-- measures model cost exactly as the AI gateway spends it, mirrors
-- `balance_usd` and `quota_usd` in the provider contract, and is never a price.
-- It is signed (a grant adds, a clawback or expiry subtracts) because the
-- trail must sum to the movement it documents.
--
-- The runner wraps this file in its own transaction and records the applied
-- version atomically, so no transaction control belongs here.

CREATE TABLE IF NOT EXISTS ai_quota_ledger (
    id UUID PRIMARY KEY,
    parent_account_id UUID NOT NULL
        REFERENCES parent_accounts(id) ON DELETE CASCADE,
    entry_type TEXT NOT NULL,
    -- Signed movement: positive grants quota, negative consumes or removes it.
    amount_usd NUMERIC(18, 6) NOT NULL,
    -- The balance this entry was applied to (the balance after it), so the
    -- running trail stays verifiable against the provider balance.
    balance_after_usd NUMERIC(18, 6) NOT NULL,
    -- The provider balance observed when a provider_sync entry was computed.
    -- NULL for entries that were not driven by a sync.
    observed_provider_balance_usd NUMERIC(18, 6),
    -- SET NULL: a purged order must not erase the quota history it funded.
    order_id UUID REFERENCES billing_orders(id) ON DELETE SET NULL,
    refund_id UUID REFERENCES billing_refunds(id) ON DELETE SET NULL,
    operator_account_id UUID REFERENCES parent_accounts(id) ON DELETE SET NULL,
    reason TEXT NOT NULL DEFAULT '',
    detail JSONB NOT NULL DEFAULT '{}'::JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ai_quota_ledger_entry_type_valid CHECK (
        entry_type IN (
            'purchase_grant',
            'refund_clawback',
            'admin_adjustment',
            'provider_sync',
            'expiry'
        )
    ),
    -- A zero-amount entry carries no information and would let a caller pad
    -- the ledger, so an entry must actually move the balance.
    CONSTRAINT ai_quota_ledger_amount_non_zero CHECK (amount_usd <> 0),
    -- A manual adjustment is the only type a human authorizes directly, so it
    -- must state why. Every other type derives its reason from its source row.
    CONSTRAINT ai_quota_ledger_reason_required CHECK (
        entry_type <> 'admin_adjustment' OR length(btrim(reason)) > 0
    ),
    CONSTRAINT ai_quota_ledger_reason_length
        CHECK (length(reason) <= 500),
    -- Capture-time invariant only: observed - balance_after is the delta this
    -- row records, and it must equal amount_usd. The consistency check SELECTs
    -- balance_after + amount_usd and compares against the provider balance, so
    -- the check never depends on this definition and a later schema change
    -- cannot silently redefine it.
    CONSTRAINT ai_quota_ledger_observed_balance_matches_type CHECK (
        (entry_type = 'provider_sync') = (observed_provider_balance_usd IS NOT NULL)
    ),
    CONSTRAINT ai_quota_ledger_observed_balance_non_negative CHECK (
        observed_provider_balance_usd IS NULL OR observed_provider_balance_usd >= 0
    ),
    -- provider_sync records the DELTA between this observation and the
    -- previous observed balance, never the new total, so the trail stays
    -- additive. kind separates a booked consumption from a drift correction
    -- (observed != previous - booked), because the two need different review.
    CONSTRAINT ai_quota_ledger_detail_object
        CHECK (jsonb_typeof(detail) = 'object'),
    CONSTRAINT ai_quota_ledger_sync_detail_has_kind CHECK (
        entry_type <> 'provider_sync'
        OR detail->>'kind' IN ('consumption', 'drift')
    )
);

CREATE INDEX IF NOT EXISTS ai_quota_ledger_parent_created_idx
    ON ai_quota_ledger(parent_account_id, created_at DESC);

CREATE INDEX IF NOT EXISTS ai_quota_ledger_type_created_idx
    ON ai_quota_ledger(entry_type, created_at DESC);

-- One order grants quota at most once, so a redelivered payment callback or a
-- retried grant job cannot credit the same purchase twice. Refunds are
-- excluded because an order refunded in several parts produces one clawback
-- per successful refund.
CREATE UNIQUE INDEX IF NOT EXISTS ai_quota_ledger_purchase_unique_idx
    ON ai_quota_ledger(order_id)
    WHERE entry_type = 'purchase_grant' AND order_id IS NOT NULL;

-- A refund claws back at most once, so a retried provider refund success
-- cannot subtract the same money twice. An order refunded in parts keeps one
-- clawback per refund row, and a full refund is simply one more refund row.
CREATE UNIQUE INDEX IF NOT EXISTS ai_quota_ledger_clawback_unique_idx
    ON ai_quota_ledger(refund_id)
    WHERE entry_type = 'refund_clawback' AND refund_id IS NOT NULL;
