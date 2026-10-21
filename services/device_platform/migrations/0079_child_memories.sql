-- 儿童长期记忆：把孩子的偏好与生活事实持久化到云端，按儿童隔离。
--
-- 现状是碎片的：固件 local_memory 只在本机保存 16 条键值对，语音网关只
-- 保留单次会话的十余轮上下文。换设备、重装或跨会话时，孩子告诉机器人
-- 的“我喜欢蓝色”“我的小猫叫咪咪”全部丢失，陪伴体验退化为陌生人。本表
-- 是这些事实在服务端的唯一权威副本，设备通过 revision 增量同步。
--
-- 唯一约束是 (child_id, memory_key)，而不是 (family_id, memory_key)：
-- 一个家庭可能有多个孩子，同一 key（如 pet_name）必须按孩子分别成立，
-- 否则妹妹的宠物会覆盖姐姐的。family_id 仍然落库，因为它承担越权校验
-- 和注销级联清理，且每次查询都按它先过滤，能走 (family_id, child_id) 索引。
--
-- revision 是“单儿童”单调递增的游标，不是全局序列：设备只需要拉取自己
-- 绑定孩子的变化，全局序列会让游标出现与设备无关的空洞。写入时在同一
-- 事务里对 child_id 取 advisory lock 后自增，避免并发批次复用版本号。
--
-- memory_value 可能包含儿童个人信息（姓名、宠物、住址片段），因此：
--   * 保留期由 last_reinforced_at 计算（默认 365 天，可配置），不是 created_at，
--     这样孩子反复确认的事实不会因为“第一次知道得很早”而被清掉；
--   * 任何日志、错误信息、审计详情都不得输出 memory_value，服务层不做
--     全量 String() 输出；
--   * 家长可逐条查看/编辑/删除，删除与注销走级联清理。

CREATE TABLE IF NOT EXISTS child_memories (
    id UUID PRIMARY KEY,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    child_id UUID NOT NULL,
    memory_key TEXT NOT NULL,
    memory_value TEXT NOT NULL,
    confidence NUMERIC(4, 3) NOT NULL DEFAULT 0.600,
    source TEXT NOT NULL DEFAULT 'conversation',
    revision BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_reinforced_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- 同一 child 的同一个 key 只保留一条，服务层用 ON CONFLICT 更新。
    CONSTRAINT child_memories_child_key_unique UNIQUE (child_id, memory_key),
    CONSTRAINT child_memories_key_valid
        CHECK (memory_key ~ '^[a-z][a-z0-9_]{1,63}$'),
    CONSTRAINT child_memories_value_length
        CHECK (char_length(memory_value) BETWEEN 1 AND 280),
    CONSTRAINT child_memories_confidence_range
        CHECK (confidence >= 0 AND confidence <= 1),
    CONSTRAINT child_memories_source_valid
        CHECK (source IN ('conversation', 'guardian', 'device')),
    CONSTRAINT child_memories_revision_positive
        CHECK (revision > 0)
);

-- 设备增量拉取：WHERE family_id = ? AND child_id = ? AND revision > ?
-- 家长列表：WHERE family_id = ? AND child_id = ? ORDER BY memory_key
CREATE INDEX IF NOT EXISTS child_memories_family_child_key_idx
    ON child_memories (family_id, child_id, memory_key);

CREATE INDEX IF NOT EXISTS child_memories_child_revision_idx
    ON child_memories (child_id, revision);

-- 保留期清理按 last_reinforced_at 扫描，索引让每小时一次的裁剪保持低成本。
CREATE INDEX IF NOT EXISTS child_memories_last_reinforced_idx
    ON child_memories (last_reinforced_at);

-- 删除墓碑：行被删除后仍要把 revision 推给设备，否则只拉存活行的设备会
-- 继续保留家长已经删除的记忆（对儿童隐私而言是删除未生效）。墓碑只保留
-- key 与 revision，不含记忆值，并且在保留期清理时随记忆一起回收。
CREATE TABLE IF NOT EXISTS child_memory_tombstones (
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    child_id UUID NOT NULL,
    memory_key TEXT NOT NULL,
    revision BIGINT NOT NULL,
    deleted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (child_id, memory_key),
    CONSTRAINT child_memory_tombstones_revision_positive
        CHECK (revision > 0)
);

CREATE INDEX IF NOT EXISTS child_memory_tombstones_family_child_idx
    ON child_memory_tombstones (family_id, child_id, revision);
