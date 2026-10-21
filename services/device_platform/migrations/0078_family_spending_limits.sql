-- 未成年人付费合规要求监护人能够对家庭消费设置硬上限。
--
-- 在此之前平台只有一个逻辑边界：孩子使用的设备上不出现支付入口。该边界
-- 只约束设备侧界面，家长端 App 里任何一位监护人仍然可以无限次购买套餐，
-- 因此无法向监管方证明“家庭月度消费不会超过监护人确认的金额”。
--
-- 限额是家庭级的，因为家庭只有一个付费主体：billing_orders.parent_account_id
-- 记录的就是家庭所有者账号，按孩子拆分会把同一笔支出计入多个孩子，导致
-- 限额被重复放大。monthly_limit_cents 属于人民币分，与计费模块其他金额
-- 同单位；0 表示不限，显式存储而不是“没有行”，这样监护人关掉限额与
-- 从未设置限额在审计上是两种不同事实。

CREATE TABLE IF NOT EXISTS family_spending_limits (
    family_id UUID PRIMARY KEY
        REFERENCES parent_accounts(id) ON DELETE CASCADE,
    -- 0 means "no monthly cap". Negative values are refused by the constraint
    -- below so a malformed guardian payload can never disable the guard.
    monthly_limit_cents BIGINT NOT NULL DEFAULT 0,
    updated_by UUID REFERENCES parent_accounts(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT family_spending_limits_limit_non_negative
        CHECK (monthly_limit_cents >= 0)
);

-- 超限拒绝必须留下可追溯记录：合规审计需要回答“哪一次下单、按哪个限额、
-- 当时已消费多少被拒绝”。记录是追加写入的，不参与下单事务，因此即使之后
-- 监护人上调限额，历史拒绝事实仍然保留。

CREATE TABLE IF NOT EXISTS family_spending_limit_rejections (
    id UUID PRIMARY KEY,
    family_id UUID NOT NULL
        REFERENCES parent_accounts(id) ON DELETE CASCADE,
    monthly_limit_cents BIGINT NOT NULL,
    consumed_cents BIGINT NOT NULL,
    requested_cents BIGINT NOT NULL,
    plan_code TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT family_spending_limit_rejections_limit_non_negative
        CHECK (monthly_limit_cents >= 0),
    CONSTRAINT family_spending_limit_rejections_amounts_non_negative
        CHECK (consumed_cents >= 0 AND requested_cents >= 0)
);

-- Counts this month's rejections for the Feature Center metric and the
-- operator drill-down; both filter by family and by calendar month.
CREATE INDEX IF NOT EXISTS family_spending_limit_rejections_family_created_idx
    ON family_spending_limit_rejections (family_id, created_at DESC);

CREATE INDEX IF NOT EXISTS family_spending_limit_rejections_created_idx
    ON family_spending_limit_rejections (created_at DESC);

-- 当月已消费金额按家庭汇总已支付订单的应支付金额，所以 (parent_account_id,
-- paid_at) 必须能走索引。该索引同时服务计费模块自己的“近 30 天已付订单”
-- 统计，不额外增加写放大。
CREATE INDEX IF NOT EXISTS billing_orders_parent_paid_at_idx
    ON billing_orders (parent_account_id, paid_at)
    WHERE paid_at IS NOT NULL;
