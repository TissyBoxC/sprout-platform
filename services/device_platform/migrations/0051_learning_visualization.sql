-- Learning visualization module (触屏学习可视化): the guardian visualization
-- policy and the bounded per-device visualization status.
--
-- The guardian owns one visualization policy per family. Every bound device
-- reads the same revision over its device session and reports back a bounded
-- visualization status. The default policy keeps the feature off, so a device
-- never animates a learning surface before a guardian enables it. The platform
-- stores only bounded scalars (style, animation level, session ceiling, the
-- linked lesson id and the visual state); no frame buffers, touch traces or
-- captured media are ever persisted here.

CREATE TABLE IF NOT EXISTS learning_visualization_policies (
    family_id UUID PRIMARY KEY REFERENCES parent_accounts(id) ON DELETE CASCADE,
    schema_version INT NOT NULL DEFAULT 1,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    feedback_style TEXT NOT NULL DEFAULT 'neutral',
    animation_level TEXT NOT NULL DEFAULT 'subtle',
    touch_feedback_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    reward_effects_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    lesson_linkage_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    max_session_minutes INT NOT NULL DEFAULT 20,
    revision INT NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT learning_visualization_policies_style_valid
        CHECK (feedback_style IN (
            'neutral', 'encouraging', 'playful', 'celebration'
        )),
    CONSTRAINT learning_visualization_policies_animation_valid
        CHECK (animation_level IN ('off', 'subtle', 'full')),
    CONSTRAINT learning_visualization_policies_session_valid
        CHECK (max_session_minutes BETWEEN 1 AND 60),
    CONSTRAINT learning_visualization_policies_revision_valid
        CHECK (revision >= 1)
);

-- One row per bound device. A device that never reported is simply absent, so
-- the guardian UI hides the visualization controls instead of showing dead
-- ones.

CREATE TABLE IF NOT EXISTS device_learning_visualization_status (
    device_id TEXT PRIMARY KEY
        REFERENCES device_credentials(device_id) ON DELETE CASCADE,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    visualization_supported BOOLEAN NOT NULL DEFAULT FALSE,
    touch_supported BOOLEAN NOT NULL DEFAULT FALSE,
    linked_lesson_id TEXT NOT NULL DEFAULT '',
    visual_state TEXT NOT NULL DEFAULT 'idle',
    feedback_style TEXT NOT NULL DEFAULT 'neutral',
    animation_level TEXT NOT NULL DEFAULT 'off',
    feedback_events INT NOT NULL DEFAULT 0,
    session_minutes INT NOT NULL DEFAULT 0,
    policy_revision INT NOT NULL DEFAULT 0,
    reported_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT device_learning_visualization_status_state_valid
        CHECK (visual_state IN (
            'idle', 'lesson', 'feedback', 'reward', 'paused', 'error'
        )),
    CONSTRAINT device_learning_visualization_status_style_valid
        CHECK (feedback_style IN (
            'neutral', 'encouraging', 'playful', 'celebration'
        )),
    CONSTRAINT device_learning_visualization_status_animation_valid
        CHECK (animation_level IN ('off', 'subtle', 'full')),
    CONSTRAINT device_learning_visualization_status_events_valid
        CHECK (feedback_events BETWEEN 0 AND 100000),
    CONSTRAINT device_learning_visualization_status_minutes_valid
        CHECK (session_minutes BETWEEN 0 AND 1440),
    CONSTRAINT device_learning_visualization_status_revision_valid
        CHECK (policy_revision >= 0)
);

CREATE INDEX IF NOT EXISTS device_learning_visualization_status_family_idx
    ON device_learning_visualization_status (family_id, reported_at DESC);
