-- Camera and vision task index.
--
-- The platform stores only bounded, symbolic task metadata and recognized
-- labels. Image bytes are handled by short-lived object storage outside this
-- schema; raw pixels, audio, credentials, and child-identifying content are
-- never persisted here. Results expire after a short retention window and are
-- deleted by the same retention job that prunes sensitive caches.

CREATE TABLE IF NOT EXISTS vision_tasks (
    id UUID PRIMARY KEY,
    device_id TEXT NOT NULL,
    task_type TEXT NOT NULL,
    status TEXT NOT NULL,
    requested_by TEXT NOT NULL,
    prompt TEXT NOT NULL DEFAULT '',
    frame_width INTEGER NOT NULL DEFAULT 0,
    frame_height INTEGER NOT NULL DEFAULT 0,
    frame_bytes INTEGER NOT NULL DEFAULT 0,
    failure_code TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    dispatched_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT vision_tasks_type_valid
        CHECK (task_type IN (
            'object_recognition',
            'picture_book_recognition',
            'photo_qa',
            'capture'
        )),
    CONSTRAINT vision_tasks_status_valid
        CHECK (status IN (
            'pending',
            'dispatched',
            'completed',
            'failed',
            'cancelled',
            'expired'
        )),
    CONSTRAINT vision_tasks_prompt_bounded
        CHECK (char_length(prompt) <= 500),
    CONSTRAINT vision_tasks_frame_bounds
        CHECK (
            frame_width >= 0 AND frame_width <= 4096 AND
            frame_height >= 0 AND frame_height <= 4096 AND
            frame_bytes >= 0 AND frame_bytes <= 8388608
        )
);

CREATE INDEX IF NOT EXISTS vision_tasks_device_created_idx
    ON vision_tasks (device_id, created_at DESC);

CREATE INDEX IF NOT EXISTS vision_tasks_status_expiry_idx
    ON vision_tasks (status, expires_at);

CREATE TABLE IF NOT EXISTS vision_results (
    task_id UUID PRIMARY KEY REFERENCES vision_tasks (id) ON DELETE CASCADE,
    device_id TEXT NOT NULL,
    task_type TEXT NOT NULL,
    labels JSONB NOT NULL DEFAULT '[]'::jsonb,
    summary TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT vision_results_summary_bounded
        CHECK (char_length(summary) <= 500)
);

CREATE INDEX IF NOT EXISTS vision_results_device_created_idx
    ON vision_results (device_id, created_at DESC);

CREATE INDEX IF NOT EXISTS vision_results_expiry_idx
    ON vision_results (expires_at);
