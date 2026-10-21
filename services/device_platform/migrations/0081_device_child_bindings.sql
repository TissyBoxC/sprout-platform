-- 设备与儿童的绑定关系。
--
-- 一台设备在同一时刻只有一个「当前儿童」，由 device_child_bindings.is_active
-- 保证；切换身份不是删除旧记录，而是把旧的置为非激活并写入新的激活记录，
-- 这样多孩家庭可以在同一台设备上轮流使用，同时留下可审计的切换历史。
--
-- 会话记忆、成长报告等按儿童区分的云端数据都以这里的 active 绑定为准，
-- 不再依赖「家庭里只有一个孩子」的假设。

CREATE TABLE IF NOT EXISTS device_child_bindings (
    id UUID PRIMARY KEY,
    device_id TEXT NOT NULL REFERENCES device_bindings(device_id) ON DELETE CASCADE,
    child_id UUID NOT NULL REFERENCES child_profiles(id) ON DELETE CASCADE,
    -- 冗余保存 family_id：设备被解绑后，绑定历史仍需知道它属于哪个家庭，
    -- 才能随家庭数据一起清理。
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    activated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deactivated_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- 非激活记录必须带解除时间，激活记录不能带；两者不一致会让「谁在什么
    -- 时候用过这台设备」无法回答。
    CONSTRAINT device_child_bindings_lifecycle CHECK (
        (is_active AND deactivated_at IS NULL)
        OR (NOT is_active AND deactivated_at IS NOT NULL)
    )
);

-- 一台设备最多一条激活绑定。这个唯一索引是真正的约束：应用层的先查后写
-- 在并发切换下会留下两条激活记录。
CREATE UNIQUE INDEX IF NOT EXISTS device_child_bindings_active_device_idx
    ON device_child_bindings(device_id)
    WHERE is_active;

CREATE INDEX IF NOT EXISTS device_child_bindings_child_idx
    ON device_child_bindings(child_id);

CREATE INDEX IF NOT EXISTS device_child_bindings_family_idx
    ON device_child_bindings(family_id);

-- 同一台设备对同一个儿童只保留一个历史行，切换回来时复用并重新激活，
-- 避免反复切换把历史表撑成噪音。
CREATE UNIQUE INDEX IF NOT EXISTS device_child_bindings_device_child_idx
    ON device_child_bindings(device_id, child_id);
