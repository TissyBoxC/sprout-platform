-- 情绪感知：家长了解孩子近期的大致情绪状态，用于调整陪伴方式。
--
-- 这不是情绪识别监控。表里只有标签、置信度和观测时刻，没有任何图像、音频、
-- 对话文本或可以还原孩子当时在做什么的字段，也没有「为什么」之类的原因说明。
-- 家长端只读聚合分布与趋势，逐条记录只用于保留期清理和趋势计算。
CREATE TABLE IF NOT EXISTS emotion_settings (
    family_id UUID PRIMARY KEY REFERENCES parent_accounts (id) ON DELETE CASCADE,
    -- 默认关闭。孩子还不会自己判断是否适合被感知情绪，必须由家长显式开启。
    -- 没有记录的家庭同样按关闭处理，因此「没有行」不等于「开启」。
    is_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    -- 保留天数由家长选择，范围 1-90 天：更长会保存超出功能所需的儿童内心状态，
    -- 更短会让周期趋势失去意义。
    retention_days INT NOT NULL DEFAULT 30
        CONSTRAINT emotion_settings_retention_days_range CHECK (retention_days BETWEEN 1 AND 90),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS emotion_observations (
    id UUID PRIMARY KEY,
    family_id UUID NOT NULL REFERENCES parent_accounts (id) ON DELETE CASCADE,
    child_id UUID REFERENCES child_profiles (id) ON DELETE SET NULL,
    -- device_id 在整库中都是文本：设备标识由出厂配置给出，不是 UUID。
    device_id TEXT NOT NULL REFERENCES device_bindings (device_id) ON DELETE CASCADE,
    -- 固定词表。开放词表会让设备把对孩子的主观描述写进库，这正是本模块要拒绝的。
    label TEXT NOT NULL
        CONSTRAINT emotion_observations_label_vocabulary CHECK (
            label IN ('calm', 'happy', 'curious', 'tired', 'frustrated', 'sad', 'upset')
        ),
    -- 百分比整数，不做浮点：跨端渲染同一份分布时不会出现 33.33 与 33.34 的差异。
    confidence SMALLINT NOT NULL
        CONSTRAINT emotion_observations_confidence_range CHECK (confidence BETWEEN 0 AND 100),
    observed_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- 未来时刻说明设备时钟异常或数据被伪造。服务层容差为 5 分钟，数据库只做
    -- 一个宽松的上界兜底，避免把明显错误的行写进来。
    CONSTRAINT emotion_observations_observed_at_sane CHECK (
        observed_at <= created_at + INTERVAL '10 minutes'
    )
);

-- 家庭时间线：保留期清理和默认聚合都按 (家庭, 时间) 取数。
CREATE INDEX IF NOT EXISTS emotion_observations_family_observed_idx
    ON emotion_observations (family_id, observed_at DESC);

-- 单个孩子的聚合：家长按 child_id 查询时走这条索引。
CREATE INDEX IF NOT EXISTS emotion_observations_family_child_observed_idx
    ON emotion_observations (family_id, child_id, observed_at DESC);

-- 保留期清理按 observed_at 删除，跨家庭扫描时用这条索引。
CREATE INDEX IF NOT EXISTS emotion_observations_observed_at_idx
    ON emotion_observations (observed_at);
