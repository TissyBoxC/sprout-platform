-- A purchased quota grant needs to know how long it stays usable. Without a
-- validity window the expiry ledger entry has no source: nothing could ever
-- decide that a grant had lapsed, so granted quota would accumulate forever
-- and a family could bank years of unused allowance from a monthly plan.
--
-- 0 means "does not expire". A term plan should normally set a window close to
-- its period so unused allowance does not silently roll over indefinitely.

ALTER TABLE billing_plans
    ADD COLUMN IF NOT EXISTS quota_validity_days INTEGER NOT NULL DEFAULT 0;

ALTER TABLE billing_plans
    DROP CONSTRAINT IF EXISTS billing_plans_quota_validity_non_negative;

ALTER TABLE billing_plans
    ADD CONSTRAINT billing_plans_quota_validity_non_negative
    CHECK (quota_validity_days >= 0);

-- A granted quota's own deadline, resolved at payment time from the plan's
-- window. Storing it on the ledger row keeps the grant auditable even after
-- the plan is edited or archived.
ALTER TABLE ai_quota_ledger
    ADD COLUMN IF NOT EXISTS quota_expires_at TIMESTAMPTZ;

-- The order snapshots the validity window with the rest of the plan, so a
-- later plan edit cannot retroactively change when an already-paid order's
-- quota lapses.
ALTER TABLE billing_orders
    ADD COLUMN IF NOT EXISTS quota_validity_days INTEGER NOT NULL DEFAULT 0;

ALTER TABLE billing_orders
    DROP CONSTRAINT IF EXISTS billing_orders_quota_validity_non_negative;

ALTER TABLE billing_orders
    ADD CONSTRAINT billing_orders_quota_validity_non_negative
    CHECK (quota_validity_days >= 0);

CREATE INDEX IF NOT EXISTS ai_quota_ledger_expiry_idx
    ON ai_quota_ledger (quota_expires_at)
    WHERE entry_type = 'purchase_grant' AND quota_expires_at IS NOT NULL;

-- Finds the grants that have lapsed and still need an expiry entry written.
CREATE INDEX IF NOT EXISTS ai_quota_ledger_pending_expiry_idx
    ON ai_quota_ledger (parent_account_id, quota_expires_at)
    WHERE entry_type = 'purchase_grant' AND quota_expires_at IS NOT NULL;
