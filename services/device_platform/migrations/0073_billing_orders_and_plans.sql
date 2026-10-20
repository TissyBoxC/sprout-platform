-- Commercial revenue ledger, part one: catalogue, purchases, and consent.
--
-- Two amount systems coexist in the platform and must never be mixed. The AI
-- gateway tracks model cost in USD (`ai_accounts.balance_usd`), while every
-- amount below is guardian revenue in yuan cents: BIGINT integer cents, never
-- NUMERIC and never a floating point value. 0.1 + 0.2 != 0.3 silently corrupts
-- reconciliation, so the unit is fixed in the schema, not the application.
--
-- Plans are catalogued here, orders snapshot every plan field they sell so a
-- later rename or price change never rewrites history, subscriptions record
-- what a periodic purchase granted, and spending consents keep the evidence
-- that a guardian agreed to a child's spending at all.
--
-- The runner wraps this file in its own transaction and records the applied
-- version atomically, so no transaction control belongs here.

CREATE TABLE IF NOT EXISTS billing_plans (
    id UUID PRIMARY KEY,
    code TEXT NOT NULL,
    display_name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    billing_period TEXT NOT NULL DEFAULT 'monthly',
    period_months INTEGER NOT NULL DEFAULT 1,
    price_cny_cents BIGINT NOT NULL DEFAULT 0,
    -- 0 means "no strike-through price shown", not "free".
    original_price_cny_cents BIGINT NOT NULL DEFAULT 0,
    ai_quota_usd NUMERIC(18, 6) NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'draft',
    sort_order INTEGER NOT NULL DEFAULT 0,
    -- Optimistic concurrency so two operators cannot overwrite each other's
    -- edit of a published price.
    version INTEGER NOT NULL DEFAULT 1,
    published_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT billing_plans_code_format
        CHECK (code ~ '^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$'),
    CONSTRAINT billing_plans_display_name_not_blank
        CHECK (length(btrim(display_name)) > 0),
    CONSTRAINT billing_plans_billing_period_valid CHECK (
        billing_period IN ('monthly', 'quarterly', 'yearly', 'one_time')
    ),
    CONSTRAINT billing_plans_status_valid
        CHECK (status IN ('draft', 'active', 'archived')),
    CONSTRAINT billing_plans_price_non_negative CHECK (price_cny_cents >= 0),
    CONSTRAINT billing_plans_original_price_non_negative
        CHECK (original_price_cny_cents >= 0),
    CONSTRAINT billing_plans_ai_quota_non_negative CHECK (ai_quota_usd >= 0),
    CONSTRAINT billing_plans_period_months_matches_period CHECK (
        (billing_period = 'one_time' AND period_months = 0)
        OR (billing_period <> 'one_time' AND period_months > 0)
    ),
    CONSTRAINT billing_plans_version_positive CHECK (version >= 1),
    CONSTRAINT billing_plans_sort_order_non_negative CHECK (sort_order >= 0),
    -- Only a plan that is on sale must be priced. Drafts and archived plans
    -- may carry zero while being prepared or retired.
    CONSTRAINT billing_plans_active_price_positive CHECK (
        status <> 'active' OR price_cny_cents > 0
    )
);

-- §3.7 guardian spending consent. Minor spending is regulated, so the evidence
-- of who agreed, to which terms version, from where, and when it was withdrawn
-- is kept as its own append-only record; a guardian never edits a consent row.
CREATE TABLE IF NOT EXISTS billing_spending_consents (
    id UUID PRIMARY KEY,
    parent_account_id UUID NOT NULL
        REFERENCES parent_accounts(id) ON DELETE CASCADE,
    consent_version TEXT NOT NULL,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- NULL means the consent is still in force.
    revoked_at TIMESTAMPTZ,
    ip_address TEXT NOT NULL DEFAULT '',
    method TEXT NOT NULL DEFAULT 'app_confirm',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT billing_spending_consents_version_not_blank
        CHECK (length(btrim(consent_version)) > 0),
    CONSTRAINT billing_spending_consents_method_valid
        CHECK (method IN ('app_confirm')),
    CONSTRAINT billing_spending_consents_revoked_after_grant
        CHECK (revoked_at IS NULL OR revoked_at >= granted_at)
);

-- At most one live consent per family and terms version; a re-grant closes the
-- previous row first so the partial index never conflicts.
CREATE UNIQUE INDEX IF NOT EXISTS billing_spending_consents_active_idx
    ON billing_spending_consents(parent_account_id, consent_version)
    WHERE revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS billing_spending_consents_parent_idx
    ON billing_spending_consents(parent_account_id, granted_at DESC);

-- The business code is the stable external identifier; it is unique across
-- every status because an archived plan keeps its code reserved.
CREATE UNIQUE INDEX IF NOT EXISTS billing_plans_code_idx
    ON billing_plans(code);

CREATE INDEX IF NOT EXISTS billing_plans_status_sort_idx
    ON billing_plans(status, sort_order, created_at);

CREATE TABLE IF NOT EXISTS billing_orders (
    id UUID PRIMARY KEY,
    order_no TEXT NOT NULL,
    parent_account_id UUID NOT NULL
        REFERENCES parent_accounts(id) ON DELETE CASCADE,
    -- RESTRICT: a plan that already sold something is archived, never deleted.
    plan_id UUID NOT NULL REFERENCES billing_plans(id) ON DELETE RESTRICT,
    plan_code TEXT NOT NULL,
    plan_display_name TEXT NOT NULL,
    billing_period TEXT NOT NULL,
    period_months INTEGER NOT NULL DEFAULT 0,
    ai_quota_usd NUMERIC(18, 6) NOT NULL DEFAULT 0,
    unit_price_cny_cents BIGINT NOT NULL DEFAULT 0,
    discount_cny_cents BIGINT NOT NULL DEFAULT 0,
    payable_cny_cents BIGINT NOT NULL DEFAULT 0,
    paid_cny_cents BIGINT NOT NULL DEFAULT 0,
    refunded_cny_cents BIGINT NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending',
    idempotency_key TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ,
    paid_at TIMESTAMPTZ,
    cancelled_at TIMESTAMPTZ,
    closed_at TIMESTAMPTZ,
    client_ip TEXT NOT NULL DEFAULT '',
    remark TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT billing_orders_order_no_format
        CHECK (order_no ~ '^SP[0-9]{8}[A-Z2-7]{8}$'),
    CONSTRAINT billing_orders_plan_code_not_blank
        CHECK (length(btrim(plan_code)) > 0),
    CONSTRAINT billing_orders_billing_period_valid CHECK (
        billing_period IN ('monthly', 'quarterly', 'yearly', 'one_time')
    ),
    CONSTRAINT billing_orders_status_valid CHECK (
        status IN (
            'pending',
            'paid',
            'cancelled',
            'expired',
            'refunded',
            'partially_refunded'
        )
    ),
    CONSTRAINT billing_orders_period_months_matches_period CHECK (
        (billing_period = 'one_time' AND period_months = 0)
        OR (billing_period <> 'one_time' AND period_months > 0)
    ),
    CONSTRAINT billing_orders_amounts_non_negative CHECK (
        unit_price_cny_cents >= 0
        AND discount_cny_cents >= 0
        AND payable_cny_cents >= 0
        AND paid_cny_cents >= 0
        AND refunded_cny_cents >= 0
    ),
    -- Mirrors domain.ValidateOrderAmounts: a direct write cannot invent money.
    CONSTRAINT billing_orders_payable_matches_discount CHECK (
        payable_cny_cents = unit_price_cny_cents - discount_cny_cents
    ),
    CONSTRAINT billing_orders_discount_within_unit_price
        CHECK (discount_cny_cents <= unit_price_cny_cents),
    CONSTRAINT billing_orders_refund_within_paid
        CHECK (refunded_cny_cents <= paid_cny_cents),
    CONSTRAINT billing_orders_idempotency_key_not_blank
        CHECK (length(idempotency_key) > 0),
    -- A pending order must carry a deadline, because the sweep and the client
    -- countdown both read it. A settled order keeps the deadline it had: it
    -- is the only record of when payment was still expected, and the §5.4
    -- callback race is resolved by comparing it against paid_at. Requiring
    -- NULL after settlement would force every status change to remember an
    -- extra assignment and turn a forgotten one into a failed payment write.
    CONSTRAINT billing_orders_pending_has_expiry CHECK (
        status <> 'pending' OR expires_at IS NOT NULL
    ),
    -- An order cannot settle before it was created.
    CONSTRAINT billing_orders_paid_after_created CHECK (
        paid_at IS NULL OR paid_at >= created_at
    )
);

-- The order number is shown to guardians and quoted to support, so a duplicate
-- would be a customer-visible collision.
CREATE UNIQUE INDEX IF NOT EXISTS billing_orders_order_no_idx
    ON billing_orders(order_no);

-- One idempotency key maps to one order per family. A retried checkout returns
-- the existing order instead of charging twice.
CREATE UNIQUE INDEX IF NOT EXISTS billing_orders_idempotency_idx
    ON billing_orders(parent_account_id, idempotency_key);

CREATE INDEX IF NOT EXISTS billing_orders_parent_created_idx
    ON billing_orders(parent_account_id, created_at DESC);

CREATE INDEX IF NOT EXISTS billing_orders_status_created_idx
    ON billing_orders(status, created_at DESC);

-- The expiry sweep scans only pending orders that are already past due.
CREATE INDEX IF NOT EXISTS billing_orders_expiry_idx
    ON billing_orders(expires_at)
    WHERE status = 'pending';

-- §3.8 subscriptions. plan_id is nullable because a subscription must outlive
-- a plan that is archived and eventually removed from the catalogue, while the
-- plan snapshot columns keep what the guardian actually bought.
CREATE TABLE IF NOT EXISTS billing_subscriptions (
    id UUID PRIMARY KEY,
    parent_account_id UUID NOT NULL
        REFERENCES parent_accounts(id) ON DELETE CASCADE,
    plan_id UUID REFERENCES billing_plans(id) ON DELETE SET NULL,
    plan_code TEXT NOT NULL,
    plan_display_name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    current_period_start TIMESTAMPTZ NOT NULL,
    current_period_end TIMESTAMPTZ NOT NULL,
    -- SET NULL: a deleted order must not delete the subscription history the
    -- guardian paid for.
    source_order_id UUID REFERENCES billing_orders(id) ON DELETE SET NULL,
    -- Auto-renew is only a reserved semantic; no recurring charge is wired up
    -- yet, so the default is off.
    auto_renew BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT billing_subscriptions_status_valid
        CHECK (status IN ('active', 'expired', 'cancelled')),
    CONSTRAINT billing_subscriptions_plan_code_not_blank
        CHECK (length(btrim(plan_code)) > 0),
    CONSTRAINT billing_subscriptions_period_ordered
        CHECK (current_period_end > current_period_start)
);

-- At most one live subscription per family and plan; renewals replace the
-- current period instead of stacking a second active row.
CREATE UNIQUE INDEX IF NOT EXISTS billing_subscriptions_active_unique_idx
    ON billing_subscriptions(parent_account_id, plan_code)
    WHERE status = 'active';

CREATE INDEX IF NOT EXISTS billing_subscriptions_parent_idx
    ON billing_subscriptions(parent_account_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS billing_subscriptions_expiry_idx
    ON billing_subscriptions(current_period_end)
    WHERE status = 'active';

CREATE INDEX IF NOT EXISTS billing_subscriptions_source_order_idx
    ON billing_subscriptions(source_order_id)
    WHERE source_order_id IS NOT NULL;
