-- Voice conversation policy: guardian-editable realtime voice behaviour.
--
-- These columns extend the per-child parent policy so a guardian can decide
-- whether a device keeps a session open for follow-up turns (continuous
-- conversation), how long the idle window lasts, whether speaking can be
-- interrupted (barge-in), and whether the far-field preprocessing chain runs.
-- Defaults mirror the conservative service defaults; existing rows backfill to
-- the same values so a device never observes a null rule.

ALTER TABLE parent_policies
    ADD COLUMN IF NOT EXISTS voice_continuous_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS voice_idle_window_seconds INTEGER NOT NULL DEFAULT 8,
    ADD COLUMN IF NOT EXISTS voice_barge_in_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS voice_far_field_enabled BOOLEAN NOT NULL DEFAULT TRUE;

ALTER TABLE parent_policies
    DROP CONSTRAINT IF EXISTS parent_policies_voice_idle_window_valid;

ALTER TABLE parent_policies
    ADD CONSTRAINT parent_policies_voice_idle_window_valid CHECK (
        voice_idle_window_seconds BETWEEN 2 AND 60
    );
