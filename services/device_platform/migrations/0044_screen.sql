-- Screen surface: expressions, animations, point-to-read and video (P2-01).
--
-- The guardian owns one screen policy per family, each bound device reports its
-- bounded capability and video state, the family owns a bounded point-to-read
-- dictionary, and the platform stores only the clamped video plan a device will
-- actually play. No pixels, frames or screenshots are ever stored here.

CREATE TABLE IF NOT EXISTS screen_policies (
    family_id UUID PRIMARY KEY REFERENCES parent_accounts(id) ON DELETE CASCADE,
    schema_version INT NOT NULL DEFAULT 1,
    default_expression TEXT NOT NULL DEFAULT 'happy',
    animation_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    point_read_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    video_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    video_max_fps INT NOT NULL DEFAULT 30,
    video_max_bitrate_kbps INT NOT NULL DEFAULT 1500,
    animation_frame_ms INT NOT NULL DEFAULT 200,
    revision INT NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT screen_policies_expression_valid
        CHECK (default_expression IN (
            'neutral', 'happy', 'sad', 'curious', 'sleepy', 'surprised', 'excited'
        )),
    CONSTRAINT screen_policies_fps_valid
        CHECK (video_max_fps BETWEEN 5 AND 60),
    CONSTRAINT screen_policies_bitrate_valid
        CHECK (video_max_bitrate_kbps BETWEEN 100 AND 8000),
    CONSTRAINT screen_policies_frame_valid
        CHECK (animation_frame_ms BETWEEN 40 AND 1000),
    CONSTRAINT screen_policies_revision_valid
        CHECK (revision >= 1)
);

-- One row per bound device. A device that never reported is simply absent, so
-- the guardian UI hides the screen controls instead of showing dead ones.

CREATE TABLE IF NOT EXISTS screen_device_status (
    device_id TEXT PRIMARY KEY REFERENCES device_credentials(device_id) ON DELETE CASCADE,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    screen_supported BOOLEAN NOT NULL DEFAULT FALSE,
    expressions_supported BOOLEAN NOT NULL DEFAULT FALSE,
    animation_supported BOOLEAN NOT NULL DEFAULT FALSE,
    point_read_supported BOOLEAN NOT NULL DEFAULT FALSE,
    video_supported BOOLEAN NOT NULL DEFAULT FALSE,
    expression TEXT NOT NULL DEFAULT 'neutral',
    animation TEXT NOT NULL DEFAULT 'none',
    video_state TEXT NOT NULL DEFAULT 'idle',
    policy_revision INT NOT NULL DEFAULT 0,
    reported_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT screen_device_status_expression_valid
        CHECK (expression IN (
            'neutral', 'happy', 'sad', 'curious', 'sleepy', 'surprised', 'excited'
        )),
    CONSTRAINT screen_device_status_animation_valid
        CHECK (animation IN ('none', 'idle', 'blink', 'wave', 'nod', 'dance')),
    CONSTRAINT screen_device_status_video_valid
        CHECK (video_state IN ('idle', 'loading', 'playing', 'paused', 'ended', 'failed')),
    CONSTRAINT screen_device_status_revision_valid
        CHECK (policy_revision >= 0)
);

CREATE INDEX IF NOT EXISTS screen_device_status_family_idx
    ON screen_device_status (family_id, reported_at DESC);

-- One row per family point-to-read term. The text is bounded so a large payload
-- never lands in the dictionary.

CREATE TABLE IF NOT EXISTS screen_point_read_entries (
    id UUID PRIMARY KEY,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    term TEXT NOT NULL,
    text TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT screen_point_read_term_valid
        CHECK (char_length(term) BETWEEN 1 AND 48),
    CONSTRAINT screen_point_read_text_valid
        CHECK (char_length(text) BETWEEN 1 AND 96),
    CONSTRAINT screen_point_read_unique_term
        UNIQUE (family_id, term)
);

CREATE INDEX IF NOT EXISTS screen_point_read_family_idx
    ON screen_point_read_entries (family_id, term);

-- One row per device video plan. The plan is the clamped profile, so a device
-- never stores an unsupported bitrate or frame rate.

CREATE TABLE IF NOT EXISTS screen_video_playback (
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    device_id TEXT NOT NULL REFERENCES device_credentials(device_id) ON DELETE CASCADE,
    bitrate_kbps INT NOT NULL,
    fps INT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (family_id, device_id),
    CONSTRAINT screen_video_bitrate_valid
        CHECK (bitrate_kbps BETWEEN 100 AND 8000),
    CONSTRAINT screen_video_fps_valid
        CHECK (fps BETWEEN 5 AND 60)
);
